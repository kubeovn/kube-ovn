package controllerruntime

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestSubnetShadowCacheWaitsForAllWatches(t *testing.T) {
	for _, blocked := range []string{"subnets", "vlans", "provider-networks", "ips"} {
		t.Run(blocked, func(t *testing.T) {
			api := newSubnetShadowWatchServer(t)
			api.resources["subnets"].initial = []client.Object{testSubnet("")}
			reader := startSubnetShadowManager(t, api)
			for name, resource := range api.resources {
				if name != blocked {
					close(resource.releaseInitial)
				}
			}
			for _, resource := range api.resources {
				awaitSubnetShadowSignal(t, resource.initialStarted, "initial list")
			}
			require.Never(t, func() bool { return reader.entered.Load() != 0 }, 50*time.Millisecond, time.Millisecond,
				"reconciliation must wait for every dependency cache")
			close(api.resources[blocked].releaseInitial)
			awaitSubnetShadowReads(t, reader, "subnet-a")
			api.awaitWatches(t)
		})
	}
}

func TestSubnetShadowCacheMapsDependencyUpdates(t *testing.T) {
	api := newSubnetShadowWatchServer(t)
	api.resources["subnets"].initial = []client.Object{
		testSubnet("vlan-a"),
		&kubeovnv1.Subnet{Name: "subnet-b"},
		&kubeovnv1.Subnet{Name: "subnet-c"},
	}
	reader := startSubnetShadowManager(t, api)
	api.release()
	awaitSubnetShadowReads(t, reader, "subnet-a", "subnet-b", "subnet-c")
	api.awaitWatches(t)

	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{"subnet-a"}
	api.send(t, "vlans", "ADDED", vlan, "2")
	awaitSubnetShadowVersionReads(t, reader, "vlans", "2", "subnet-a")
	vlan = vlan.DeepCopy()
	vlan.Status.Subnets = nil
	vlan.Status.Conflict = true
	api.send(t, "vlans", "MODIFIED", vlan, "3")
	awaitSubnetShadowVersionReads(t, reader, "vlans", "3", "subnet-a")

	provider := testProvider("provider-a", false, "vlan-a")
	api.send(t, "provider-networks", "ADDED", provider, "4")
	awaitSubnetShadowVersionReads(t, reader, "provider-networks", "4", "subnet-a")
	provider = provider.DeepCopy()
	provider.Status.Ready = true
	api.send(t, "provider-networks", "MODIFIED", provider, "5")
	awaitSubnetShadowVersionReads(t, reader, "provider-networks", "5", "subnet-a")

	ip := &kubeovnv1.IP{Name: "ip-a", Spec: kubeovnv1.IPSpec{Subnet: "subnet-a", AttachSubnets: []string{"subnet-b"}}}
	api.send(t, "ips", "ADDED", ip, "6")
	awaitSubnetShadowVersionReads(t, reader, "ips", "6", "subnet-a", "subnet-b")
	ip = ip.DeepCopy()
	ip.Spec.Subnet = "subnet-c"
	ip.Spec.AttachSubnets = nil
	api.send(t, "ips", "MODIFIED", ip, "7")
	awaitSubnetShadowVersionReads(t, reader, "ips", "7", "subnet-a", "subnet-b", "subnet-c")
	api.send(t, "ips", "DELETED", ip, "8")
	awaitSubnetShadowVersionReads(t, reader, "ips", "missing", "subnet-c")
}

func TestSubnetShadowCacheRetriesWithoutAnotherEvent(t *testing.T) {
	api := newSubnetShadowWatchServer(t)
	api.resources["subnets"].initial = []client.Object{testSubnet("")}
	reader := startSubnetShadowManager(t, api)
	reader.failNext.Store(true)
	api.release()
	select {
	case read := <-reader.reads:
		require.Equal(t, "subnet-a", read.name)
		require.True(t, apierrors.IsServiceUnavailable(read.err))
	case <-time.After(5 * time.Second):
		t.Fatal("initial reconciliation did not fail")
	}
	awaitSubnetShadowReads(t, reader, "subnet-a")
	api.awaitWatches(t)
}

func TestSubnetShadowCacheMapsVlanDeletion(t *testing.T) {
	api := newSubnetShadowWatchServer(t)
	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{"subnet-a"}
	api.resources["subnets"].initial = []client.Object{testSubnet("")}
	api.resources["vlans"].initial = []client.Object{vlan}
	reader := startSubnetShadowManager(t, api)
	api.release()
	awaitSubnetShadowReads(t, reader, "subnet-a")
	api.awaitWatches(t)

	api.send(t, "vlans", "DELETED", vlan, "2")
	awaitSubnetShadowVersionReads(t, reader, "vlans", "missing", "subnet-a")
}

type subnetShadowRead struct {
	name     string
	err      error
	versions map[string]string
}

type subnetShadowReader struct {
	client.Reader
	reads    chan subnetShadowRead
	failNext atomic.Bool
	entered  atomic.Int64
}

func (r *subnetShadowReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	_, isSubnet := obj.(*kubeovnv1.Subnet)
	if isSubnet {
		r.entered.Add(1)
	}
	err := r.Reader.Get(ctx, key, obj, opts...)
	if isSubnet {
		versions := r.dependencyVersions(ctx)
		if r.failNext.CompareAndSwap(true, false) {
			err = apierrors.NewServiceUnavailable("injected transient read failure")
		}
		select {
		case r.reads <- subnetShadowRead{name: key.Name, err: err, versions: versions}:
		case <-ctx.Done():
		}
	}
	return err
}

// Record cache revisions with each reconciliation read, so an earlier cached
// read cannot satisfy an assertion for a later dependency update.
func (r *subnetShadowReader) dependencyVersions(ctx context.Context) map[string]string {
	versions := make(map[string]string, 3)
	for resource, obj := range map[string]client.Object{
		"vlans":             &kubeovnv1.Vlan{Name: "vlan-a"},
		"provider-networks": &kubeovnv1.ProviderNetwork{Name: "provider-a"},
		"ips":               &kubeovnv1.IP{Name: "ip-a"},
	} {
		if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				versions[resource] = "missing"
			} else {
				versions[resource] = err.Error()
			}
			continue
		}
		versions[resource] = obj.GetResourceVersion()
	}
	return versions
}

func startSubnetShadowManager(t *testing.T, api *subnetShadowWatchServer) *subnetShadowReader {
	t.Helper()
	scheme, err := NewScheme()
	require.NoError(t, err)
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{kubeovnv1.SchemeGroupVersion})
	for name, resource := range api.resources {
		mapper.AddSpecific(kubeovnv1.SchemeGroupVersion.WithKind(resource.kind),
			kubeovnv1.SchemeGroupVersion.WithResource(name),
			kubeovnv1.SchemeGroupVersion.WithResource(strings.TrimSuffix(name, "s")), meta.RESTScopeRoot)
	}
	mgr, err := ctrl.NewManager(&rest.Config{Host: api.server.URL}, ctrl.Options{
		Scheme:         scheme,
		MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) { return mapper, nil },
		Cache: cache.Options{
			DefaultTransform: trimCacheObject, ReaderFailOnMissingInformer: true,
			DefaultEnableWatchBookmarks: new(true),
		},
		Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
		Controller: config.Controller{SkipNameValidation: new(true)},
	})
	require.NoError(t, err)
	reader := &subnetShadowReader{Reader: mgr.GetClient(), reads: make(chan subnetShadowRead, 64)}
	require.NoError(t, (&SubnetShadowReconciler{Reader: reader}).SetupWithManager(mgr))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("Subnet shadow manager did not stop")
		}
		require.Eventually(t, func() bool { return api.active.Load() == 0 }, 5*time.Second, time.Millisecond,
			"all HTTP watches must stop with the manager")
	})
	return reader
}

func awaitSubnetShadowReads(t *testing.T, reader *subnetShadowReader, names ...string) {
	t.Helper()
	awaitSubnetShadowVersionReads(t, reader, "", "", names...)
}

func awaitSubnetShadowVersionReads(t *testing.T, reader *subnetShadowReader, resource, version string, names ...string) {
	t.Helper()
	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[name] = true
	}
	timeout := time.After(5 * time.Second)
	for len(want) != 0 {
		select {
		case read := <-reader.reads:
			require.NoError(t, read.err)
			if resource == "" || read.versions[resource] == version {
				delete(want, read.name)
			}
		case <-timeout:
			t.Fatalf("Subnet shadow did not read subnets: %v", want)
		}
	}
}

func awaitSubnetShadowSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("Subnet shadow %s did not start", operation)
	}
}

type subnetShadowWatchEvent struct {
	Type   string        `json:"type"`
	Object client.Object `json:"object"`
}

type subnetShadowWatchResource struct {
	kind           string
	initial        []client.Object
	initialStarted chan struct{}
	releaseInitial chan struct{}
	watchStarted   chan struct{}
	events         chan subnetShadowWatchEvent
}

type subnetShadowWatchServer struct {
	server    *httptest.Server
	resources map[string]*subnetShadowWatchResource
	active    atomic.Int64
}

func newSubnetShadowWatchServer(t *testing.T) *subnetShadowWatchServer {
	t.Helper()
	api := &subnetShadowWatchServer{resources: make(map[string]*subnetShadowWatchResource)}
	for name, kind := range map[string]string{"subnets": "Subnet", "vlans": "Vlan", "provider-networks": "ProviderNetwork", "ips": "IP"} {
		api.resources[name] = &subnetShadowWatchResource{
			kind: kind, initialStarted: make(chan struct{}, 1), releaseInitial: make(chan struct{}),
			watchStarted: make(chan struct{}, 1), events: make(chan subnetShadowWatchEvent),
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /apis/kubeovn.io/v1/{resource}", func(w http.ResponseWriter, req *http.Request) { api.serve(t, w, req) })
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
		w.WriteHeader(http.StatusNotFound)
	})
	api.server = httptest.NewServer(mux)
	t.Cleanup(api.server.Close)
	return api
}

func (api *subnetShadowWatchServer) release() {
	for _, resource := range api.resources {
		close(resource.releaseInitial)
	}
}

func (api *subnetShadowWatchServer) awaitWatches(t *testing.T) {
	t.Helper()
	for name, resource := range api.resources {
		awaitSubnetShadowSignal(t, resource.watchStarted, name+" watch")
	}
}

func (api *subnetShadowWatchServer) send(t *testing.T, name, eventType string, obj client.Object, version string) {
	t.Helper()
	resource := api.resources[name]
	obj = obj.DeepCopyObject().(client.Object)
	obj.GetObjectKind().SetGroupVersionKind(kubeovnv1.SchemeGroupVersion.WithKind(resource.kind))
	obj.SetResourceVersion(version)
	select {
	case resource.events <- subnetShadowWatchEvent{Type: eventType, Object: obj}:
	case <-time.After(5 * time.Second):
		t.Fatalf("Subnet shadow %s watch did not accept event", name)
	}
}

func (api *subnetShadowWatchServer) serve(t *testing.T, w http.ResponseWriter, req *http.Request) {
	resource, ok := api.resources[req.PathValue("resource")]
	if !ok {
		t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	api.active.Add(1)
	defer api.active.Add(-1)
	w.Header().Set("Content-Type", "application/json")
	isWatch := req.URL.Query().Get("watch") == "true"
	initialEvents := req.URL.Query().Get("sendInitialEvents") == "true"
	if !isWatch || initialEvents {
		select {
		case resource.initialStarted <- struct{}{}:
		default:
		}
		select {
		case <-resource.releaseInitial:
		case <-req.Context().Done():
			return
		}
		if !api.writeInitial(req.Context(), t, w, resource, isWatch) || !isWatch {
			return
		}
	}
	if !initialEvents {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	}
	select {
	case resource.watchStarted <- struct{}{}:
	default:
	}
	for {
		select {
		case <-req.Context().Done():
			return
		case event := <-resource.events:
			if !writeSubnetShadowResponse(req.Context(), t, w, event) {
				return
			}
		}
	}
}

func (api *subnetShadowWatchServer) writeInitial(ctx context.Context, t *testing.T, w http.ResponseWriter, resource *subnetShadowWatchResource, watch bool) bool {
	initial := make([]client.Object, 0, len(resource.initial))
	for _, obj := range resource.initial {
		obj = obj.DeepCopyObject().(client.Object)
		obj.GetObjectKind().SetGroupVersionKind(kubeovnv1.SchemeGroupVersion.WithKind(resource.kind))
		obj.SetResourceVersion("1")
		initial = append(initial, obj)
	}
	if !watch {
		return writeSubnetShadowResponse(ctx, t, w, map[string]any{
			"apiVersion": kubeovnv1.SchemeGroupVersion.String(), "kind": resource.kind + "List",
			"metadata": map[string]string{"resourceVersion": "1"}, "items": initial,
		})
	}
	for _, obj := range initial {
		if !writeSubnetShadowResponse(ctx, t, w, subnetShadowWatchEvent{Type: "ADDED", Object: obj}) {
			return false
		}
	}
	bookmark := &metav1.PartialObjectMetadata{
		APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: resource.kind,
		ResourceVersion: "1", Annotations: map[string]string{metav1.InitialEventsAnnotationKey: "true"},
	}
	return writeSubnetShadowResponse(ctx, t, w, subnetShadowWatchEvent{Type: "BOOKMARK", Object: bookmark})
}

func writeSubnetShadowResponse(ctx context.Context, t *testing.T, w http.ResponseWriter, value any) bool {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Error(err)
		return false
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		if ctx.Err() == nil {
			t.Errorf("write Subnet shadow API response: %v", err)
		}
		return false
	}
	w.(http.Flusher).Flush()
	return true
}
