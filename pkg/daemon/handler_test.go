package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type recordedCNIEvent struct {
	object    runtime.Object
	eventType string
	reason    string
	message   string
}

type cniEventRecorder struct {
	events []recordedCNIEvent
}

func (r *cniEventRecorder) Event(object runtime.Object, eventType, reason, message string) {
	r.events = append(r.events, recordedCNIEvent{object: object, eventType: eventType, reason: reason, message: message})
}

func (r *cniEventRecorder) Eventf(object runtime.Object, eventType, reason, messageFmt string, args ...any) {
	r.Event(object, eventType, reason, fmt.Sprintf(messageFmt, args...))
}

func (r *cniEventRecorder) AnnotatedEventf(object runtime.Object, _ map[string]string, eventType, reason, messageFmt string, args ...any) {
	r.Eventf(object, eventType, reason, messageFmt, args...)
}

func TestPodForCNIEvent(t *testing.T) {
	t.Run("real pod", func(t *testing.T) {
		pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: types.UID("uid")}}
		require.Same(t, pod, podForCNIEvent(pod, &request.CniRequest{}))
		require.Equal(t, types.UID("uid"), podForCNIEvent(pod, &request.CniRequest{}).UID)
	})

	t.Run("synthetic pod", func(t *testing.T) {
		pod := podForCNIEvent(nil, &request.CniRequest{PodName: "pod", PodNamespace: "ns"})
		require.Equal(t, "v1", pod.APIVersion)
		require.Equal(t, "Pod", pod.Kind)
		require.Equal(t, "ns", pod.Namespace)
		require.Equal(t, "pod", pod.Name)
	})
}

func TestRecordCNIPodEvent(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)
	podRequest := &request.CniRequest{PodName: "pod", PodNamespace: "ns", Provider: "provider"}
	handler.recordCNIPodEvent(podForCNIEvent(nil, podRequest), podRequest, v1.EventTypeWarning, "PodNetworkConfigureFailed", "stage=get-pod error=boom")

	event := requireSingleCNIEvent(t, recorder)
	require.Equal(t, v1.EventTypeWarning, event.eventType)
	require.Equal(t, "PodNetworkConfigureFailed", event.reason)
	require.Contains(t, event.message, "stage=get-pod")
	require.Contains(t, event.message, "error=boom")
	require.Contains(t, event.message, "provider=provider")
	require.Contains(t, event.message, "interface=eth0")
	require.Contains(t, event.message, "node=node-a")
}

func TestRecordCNIPodEventRedactsRuntimeDetails(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)
	podRequest := &request.CniRequest{
		PodName: "pod", PodNamespace: "ns", Provider: "provider",
		ContainerID: "1234567890abcdef", NetNs: "/runtime/netns/pod", DeviceID: "0000:65:00.1",
	}
	handler.recordCNIPodEvent(
		podForCNIEvent(nil, podRequest), podRequest, v1.EventTypeWarning, "PodNetworkConfigureFailed",
		`stage=configure-nic error=failed on device 0000:65:00.1 and 1234567890ab_h in /runtime/netns/pod for 1234567890abcdef: boom`,
	)

	event := requireSingleCNIEvent(t, recorder)
	require.Contains(t, event.message, "stage=configure-nic")
	require.Contains(t, event.message, "boom")
	require.NotContains(t, event.message, "1234567890ab_h")
	require.NotContains(t, event.message, "1234567890ab")
	require.NotContains(t, event.message, podRequest.ContainerID)
	require.NotContains(t, event.message, podRequest.NetNs)
	require.NotContains(t, event.message, podRequest.DeviceID)
}

func TestRecordCNIPodEventHandlesUnsafeDerivedNameInputs(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)
	podRequest := &request.CniRequest{
		PodName: "pod", PodNamespace: "ns", Provider: "provider",
		ContainerID: "short", IfName: "interface-name-longer-than-twelve",
	}
	require.NotPanics(t, func() {
		handler.recordCNIPodEvent(
			podForCNIEvent(nil, podRequest), podRequest, v1.EventTypeWarning, "PodNetworkConfigureFailed", "stage=configure-nic error=boom",
		)
	})
	require.Contains(t, requireSingleCNIEvent(t, recorder).message, "error=boom")
}

func TestHandleAddSuccessEvent(t *testing.T) {
	const (
		provider = "macvlan.default"
		subnet   = "underlay"
		ip       = "10.0.0.2"
		mac      = "00:00:00:00:00:02"
	)
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "pod", Namespace: "ns", UID: types.UID("real-uid"),
		Annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, provider):     ip,
			fmt.Sprintf(util.CidrAnnotationTemplate, provider):          "10.0.0.0/24",
			fmt.Sprintf(util.MacAddressAnnotationTemplate, provider):    mac,
			fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnet,
		},
	}}
	podSubnet := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: subnet}, Spec: kubeovnv1.SubnetSpec{Provider: provider}}
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, pod, podSubnet, recorder)
	ipCRName := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
	handler.KubeOvnClient = kubeovnfake.NewSimpleClientset(&kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: ipCRName},
		Spec:       kubeovnv1.IPSpec{NodeName: "node-a"},
	})

	response := serveCNIRequest(t, handler, "/api/v1/add", request.CniRequest{
		CniType: util.CniTypeName, PodName: pod.Name, PodNamespace: pod.Namespace,
		Provider: provider, IfName: "net1",
	})
	require.Equal(t, http.StatusOK, response.Code)
	event := requireSingleCNIEvent(t, recorder)
	require.Equal(t, v1.EventTypeNormal, event.eventType)
	require.Equal(t, "PodNetworkConfigured", event.reason)
	require.Same(t, pod, event.object)
	for _, part := range []string{"subnet=" + subnet, "ip=" + ip, "mac=" + mac, "provider=" + provider, "interface=net1", "node=node-a"} {
		require.Contains(t, event.message, part)
	}
}

func TestHandleAddFailureEvent(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/add", request.CniRequest{
		PodName: "missing", PodNamespace: "ns", Provider: util.OvnProvider,
	})
	require.Equal(t, http.StatusInternalServerError, response.Code)
	event := requireSingleCNIEvent(t, recorder)
	require.Equal(t, v1.EventTypeWarning, event.eventType)
	require.Equal(t, "PodNetworkConfigureFailed", event.reason)
	require.Contains(t, event.message, "stage=get-pod")
	require.Contains(t, event.message, `pod "missing" not found`)
	pod, ok := event.object.(*v1.Pod)
	require.True(t, ok)
	require.Equal(t, "missing", pod.Name)
	require.Equal(t, "ns", pod.Namespace)
}

func TestHandleDelSuccessEventPreservesPodReference(t *testing.T) {
	useFakeOVSVsctl(t, true)
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "virt-launcher", Namespace: "ns", UID: types.UID("real-uid"),
		Annotations: map[string]string{
			fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider): "vm-name",
		},
	}}
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, pod, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/del", request.CniRequest{
		CniType: util.CniTypeName, PodName: pod.Name, PodNamespace: pod.Namespace,
		ContainerID: "1234567890abcdef", NetNs: "/missing/netns", Provider: util.OvnProvider,
	})
	require.Equal(t, http.StatusNoContent, response.Code)
	event := requireSingleCNIEvent(t, recorder)
	require.Equal(t, v1.EventTypeNormal, event.eventType)
	require.Equal(t, "PodNetworkRemoved", event.reason)
	require.Same(t, pod, event.object)
	require.Equal(t, "virt-launcher", event.object.(*v1.Pod).Name)
	require.NotContains(t, event.message, "vm-name")
	for _, part := range []string{"provider=ovn", "interface=eth0", "node=node-a"} {
		require.Contains(t, event.message, part)
	}
}

func TestHandleDelFailureEvent(t *testing.T) {
	useFakeOVSVsctl(t, false)
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/del", request.CniRequest{
		PodName: "deleted", PodNamespace: "ns", ContainerID: "1234567890abcdef",
		NetNs: "/missing/netns", Provider: util.OvnProvider, IfName: "net1",
	})
	require.Equal(t, http.StatusInternalServerError, response.Code)
	event := requireSingleCNIEvent(t, recorder)
	require.Equal(t, v1.EventTypeWarning, event.eventType)
	require.Equal(t, "PodNetworkRemoveFailed", event.reason)
	require.Contains(t, event.message, "stage=delete-nic")
	require.Contains(t, event.message, "exit status 1")
	require.NotContains(t, event.message, "12345678_net1_h")
	require.NotContains(t, event.message, "12345678")
	pod := event.object.(*v1.Pod)
	require.Equal(t, "deleted", pod.Name)
	require.Equal(t, "ns", pod.Namespace)
}

func TestHandleDelNoNetNSHasNoEvent(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/del", request.CniRequest{
		PodName: "deleted", PodNamespace: "ns", Provider: util.OvnProvider,
	})
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, recorder.events)
}

func TestHandleAddAndDelInvalidRequestHaveNoEvent(t *testing.T) {
	for _, path := range []string{"/api/v1/add", "/api/v1/del"} {
		t.Run(path, func(t *testing.T) {
			recorder := &cniEventRecorder{}
			handler := cniEventTestHandler(t, nil, nil, recorder)
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			createHandler(handler).ServeHTTP(response, request)

			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Empty(t, recorder.events)
		})
	}
}

func cniEventTestHandler(t *testing.T, pod *v1.Pod, subnet *kubeovnv1.Subnet, recorder *cniEventRecorder) *cniServerHandler {
	t.Helper()
	podIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if pod != nil {
		require.NoError(t, podIndexer.Add(pod))
	}
	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if subnet != nil {
		require.NoError(t, subnetIndexer.Add(subnet))
	}
	config := &Configuration{NodeName: "node-a"}
	controller := &Controller{
		config:        config,
		podsLister:    listerv1.NewPodLister(podIndexer),
		subnetsLister: kubeovnlister.NewSubnetLister(subnetIndexer),
		recorder:      recorder,
	}
	return createCniServerHandler(config, controller)
}

func serveCNIRequest(t *testing.T, handler *cniServerHandler, path string, podRequest request.CniRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(podRequest)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	createHandler(handler).ServeHTTP(response, request)
	return response
}

func requireSingleCNIEvent(t *testing.T, recorder *cniEventRecorder) recordedCNIEvent {
	t.Helper()
	require.Len(t, recorder.events, 1)
	return recorder.events[0]
}

func useFakeOVSVsctl(t *testing.T, succeed bool) {
	t.Helper()
	target := "/bin/false"
	if succeed {
		target = "/bin/true"
	}
	dir := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "ovs-vsctl")))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
