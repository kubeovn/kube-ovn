package controllerruntime

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
)

// Exercise SetupWithManager with its real list/watch source, including an
// ingress notification that precedes cache sync and a terminating initial item.
func TestIPPoolNativeWatchWaitsForCacheAndStops(t *testing.T) {
	live := &kubeovnv1.IPPool{
		APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: "IPPool",
		Name: "live", UID: "live-uid", ResourceVersion: "1",
		Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet", IPCount: 1},
	}
	terminating := live.DeepCopy()
	terminating.Name = "terminating"
	terminating.UID = "terminating-uid"
	terminating.DeletionTimestamp = new(metav1.NewTime(time.Unix(100, 0)))
	api := newIPPoolWatchServer(t, live, terminating)
	scheme, err := NewScheme()
	require.NoError(t, err)
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{kubeovnv1.SchemeGroupVersion})
	mapper.Add(kubeovnv1.SchemeGroupVersion.WithKind("IPPool"), meta.RESTScopeRoot)
	mgr, err := ctrl.NewManager(&rest.Config{Host: api.server.URL}, ctrl.Options{
		Scheme:         scheme,
		MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) { return mapper, nil },
		Metrics:        metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	received := make(chan controller.IPPoolRequest, 8)
	ingressStarted := make(chan struct{})
	backend := &fakeIPPoolBackend{
		start: func(_ context.Context, enqueue func(controller.IPPoolRequest)) {
			enqueue(controller.IPPoolRequest{Name: "dependency", Operation: controller.IPPoolStatus})
			close(ingressStarted)
		},
		reconcile: func(_ context.Context, request controller.IPPoolRequest) error { received <- request; return nil },
	}
	require.NoError(t, (&IPPoolReconciler{Backend: backend}).SetupWithManager(mgr))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("manager did not stop")
		}
	})
	select {
	case <-api.initialStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("cache did not list IPPools")
	}
	select {
	case <-ingressStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("explicit ingress did not start")
	}
	require.Never(t, func() bool { return len(received) != 0 }, 50*time.Millisecond, time.Millisecond,
		"neither source may reconcile before native cache sync")
	close(api.releaseInitial)
	operations := map[string]controller.IPPoolOperation{}
	for range 3 {
		select {
		case request := <-received:
			operations[request.Name] = request.Operation
			if request.Name == terminating.Name {
				require.Equal(t, terminating.ObjectMeta, request.Deleted.ObjectMeta)
				require.Equal(t, terminating.Spec, request.Deleted.Spec)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("initial list or explicit notification was lost")
		}
	}
	require.Equal(t, map[string]controller.IPPoolOperation{"dependency": controller.IPPoolStatus, "live": controller.IPPoolReconcile, "terminating": controller.IPPoolDelete}, operations)
	select {
	case <-api.watchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("native watch did not start")
	}
	updated := live.DeepCopy()
	updated.ResourceVersion = "2"
	updated.Spec.IPCount = 2
	select {
	case api.watchEvents <- updated:
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not accept update")
	}
	select {
	case request := <-received:
		require.Equal(t, controller.IPPoolRequest{Name: live.Name, Operation: controller.IPPoolReconcile}, request)
	case <-time.After(5 * time.Second):
		t.Fatal("native watch did not reconcile membership update")
	}
}

func writeIPPoolWatchEvent(ctx context.Context, t *testing.T, w http.ResponseWriter, eventType string, pool *kubeovnv1.IPPool) bool {
	t.Helper()
	data, err := json.Marshal(struct {
		Type   string            `json:"type"`
		Object *kubeovnv1.IPPool `json:"object"`
	}{Type: eventType, Object: pool})
	if err != nil {
		t.Error(err)
		return false
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		if ctx.Err() == nil {
			t.Errorf("write IPPool %s watch event: %v", eventType, err)
		}
		return false
	}
	w.(http.Flusher).Flush()
	return true
}

type ipPoolWatchServer struct {
	server         *httptest.Server
	initialStarted chan struct{}
	releaseInitial chan struct{}
	watchStarted   chan struct{}
	watchEvents    chan *kubeovnv1.IPPool
}

func newIPPoolWatchServer(t *testing.T, live, terminating *kubeovnv1.IPPool) *ipPoolWatchServer {
	t.Helper()
	api := &ipPoolWatchServer{
		initialStarted: make(chan struct{}, 1), releaseInitial: make(chan struct{}),
		watchStarted: make(chan struct{}, 1), watchEvents: make(chan *kubeovnv1.IPPool),
	}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		api.serve(t, w, req, live, terminating)
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (api *ipPoolWatchServer) serve(t *testing.T, w http.ResponseWriter, req *http.Request, live, terminating *kubeovnv1.IPPool) {
	w.Header().Set("Content-Type", "application/json")
	if req.URL.Path != "/apis/kubeovn.io/v1/ippools" {
		t.Errorf("unexpected API request: %s", req.URL)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	isWatch := req.URL.Query().Get("watch") == "true"
	initialEvents := req.URL.Query().Get("sendInitialEvents") == "true"
	if !isWatch || initialEvents {
		select {
		case api.initialStarted <- struct{}{}:
		default:
		}
		select {
		case <-api.releaseInitial:
		case <-req.Context().Done():
			return
		}
		if !isWatch {
			data, err := json.Marshal(&kubeovnv1.IPPoolList{
				APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: "IPPoolList",
				ResourceVersion: "1", Items: []kubeovnv1.IPPool{*live, *terminating},
			})
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := w.Write(data); err != nil && req.Context().Err() == nil {
				t.Errorf("write IPPool initial list: %v", err)
			}
			return
		}
		bookmark := &kubeovnv1.IPPool{
			TypeMeta: live.TypeMeta, ResourceVersion: "1",
			Annotations: map[string]string{metav1.InitialEventsAnnotationKey: "true"},
		}
		if !writeIPPoolWatchEvent(req.Context(), t, w, "ADDED", live) ||
			!writeIPPoolWatchEvent(req.Context(), t, w, "ADDED", terminating) ||
			!writeIPPoolWatchEvent(req.Context(), t, w, "BOOKMARK", bookmark) {
			return
		}
	}
	if !initialEvents {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	}
	select {
	case api.watchStarted <- struct{}{}:
	default:
	}
	for {
		select {
		case <-req.Context().Done():
			return
		case pool := <-api.watchEvents:
			if !writeIPPoolWatchEvent(req.Context(), t, w, "MODIFIED", pool) {
				return
			}
		}
	}
}
