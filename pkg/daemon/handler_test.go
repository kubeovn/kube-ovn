package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
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
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
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
		pod := &v1.Pod{Name: "pod", Namespace: "ns", UID: types.UID("uid")}
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

func TestHandleAddPrepareOnlyReturnsPlanWithoutExecutingHostNetworking(t *testing.T) {
	const (
		provider = "macvlan.default"
		subnet   = "underlay"
	)
	pod := &v1.Pod{
		Name: "pod", Namespace: "ns", UID: types.UID("real-uid"),
		Annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, provider):     "10.0.0.2",
			fmt.Sprintf(util.CidrAnnotationTemplate, provider):          "10.0.0.0/24",
			fmt.Sprintf(util.MacAddressAnnotationTemplate, provider):    "00:00:00:00:00:02",
			fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnet,
		},
	}
	handler := cniEventTestHandler(t, pod, &kubeovnv1.Subnet{Name: subnet, Spec: kubeovnv1.SubnetSpec{Provider: provider}}, &cniEventRecorder{})
	handler.KubeOvnClient = kubeovnfake.NewSimpleClientset(&kubeovnv1.IP{
		Name: ovs.PodNameToPortName(pod.Name, pod.Namespace, provider),
		Spec: kubeovnv1.IPSpec{NodeName: "node-a"},
	})

	response := serveCNIRequest(t, handler, "/api/v1/add", request.CniRequest{
		CniType: util.CniTypeName, PodName: pod.Name, PodNamespace: pod.Namespace,
		Provider: provider, IfName: "net1", ContainerID: "1234567890abcdef",
		NetNs: "/missing/netns", PrepareOnly: true,
	})
	require.Equal(t, http.StatusOK, response.Code)
	var cniResponse request.CniResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &cniResponse))
	require.NotNil(t, cniResponse.Plan)
	require.Equal(t, "net1", cniResponse.Plan.IfName)
	require.Equal(t, "10.0.0.2", cniResponse.Plan.IP)
	require.Empty(t, cniResponse.Plan.Subnet)
	// A Pod waiting for IP allocation can start terminating before prepare
	// returns. It must never receive another executable attachment plan.
	pod.DeletionTimestamp = new(metav1.Now())
	response = serveCNIRequest(t, handler, "/api/v1/add", request.CniRequest{
		CniType: util.CniTypeName, PodName: pod.Name, PodNamespace: pod.Namespace,
		Provider: provider, IfName: "net1", ContainerID: "1234567890abcdef",
		NetNs: "/missing/netns", PrepareOnly: true,
	})
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "terminating")
}

func TestHandleCommitRecordsSuccessEvent(t *testing.T) {
	pod := &v1.Pod{Name: "pod", Namespace: "ns", UID: types.UID("real-uid")}
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, pod, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/commit", request.CniRequest{Plan: &request.CNIPlan{
		PodName: pod.Name, PodNamespace: pod.Namespace, Provider: util.OvnProvider,
		IfName: "net1", IP: "10.0.0.2", MacAddress: "00:00:00:00:00:02",
	}})
	require.Equal(t, http.StatusNoContent, response.Code)
	event := requireSingleCNIEvent(t, recorder)
	require.Equal(t, v1.EventTypeNormal, event.eventType)
	require.Equal(t, "PodNetworkConfigured", event.reason)
	require.Same(t, pod, event.object)
	require.Contains(t, event.message, "ip=10.0.0.2")
}

func TestHandleCommitEnqueuesUnderlayServicesAfterExecution(t *testing.T) {
	pod := &v1.Pod{Name: "pod", Namespace: "ns", UID: types.UID("real-uid")}
	handler := cniEventTestHandler(t, pod, nil, &cniEventRecorder{})
	serviceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	matching := &v1.Service{
		Namespace: "ns",
		Name:      "underlay-service",
		Annotations: map[string]string{
			util.ServiceExternalIPFromSubnetAnnotation: "underlay",
		},
	}
	require.NoError(t, serviceIndexer.Add(matching))
	handler.Controller.servicesLister = listerv1.NewServiceLister(serviceIndexer)
	handler.Controller.serviceQueue = newTypedRateLimitingQueue[*serviceEvent]("Service", nil)

	response := serveCNIRequest(t, handler, "/api/v1/commit", request.CniRequest{
		Plan: &request.CNIPlan{
			PodName:        pod.Name,
			PodNamespace:   pod.Namespace,
			Provider:       "underlay.default",
			LocalnetSubnet: "underlay",
		},
		Execution: &request.CNIExecutionResult{},
	})
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, 1, handler.Controller.serviceQueue.Len())
}

func TestHandleAddFailureEvent(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/add", request.CniRequest{
		PodName: "missing", PodNamespace: "ns", Provider: util.OvnProvider, PrepareOnly: true,
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

func TestLegacyCNIAddIsRejectedBeforeNetworkChanges(t *testing.T) {
	handler := cniEventTestHandler(t, nil, nil, &cniEventRecorder{})
	response := serveCNIRequest(t, handler, "/api/v1/add", request.CniRequest{PodName: "pod", PodNamespace: "ns"})
	require.Equal(t, http.StatusUpgradeRequired, response.Code)
	require.Contains(t, response.Body.String(), "unsupported legacy CNI API")
}

func TestLegacyCNIDelIsRejectedBeforeNetworkChanges(t *testing.T) {
	handler := cniEventTestHandler(t, nil, nil, &cniEventRecorder{})
	response := serveCNIRequest(t, handler, "/api/v1/del", request.CniRequest{PodName: "pod", PodNamespace: "ns"})
	require.Equal(t, http.StatusUpgradeRequired, response.Code)
	require.Contains(t, response.Body.String(), "unsupported legacy CNI API")
}

func TestHandleDelPlanExecutesOVNCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		pod            *v1.Pod
		ipamOnly       bool
	}{
		{name: "OVN missing pod", provider: util.OvnProvider},
		{name: "OVN existing pod", provider: util.OvnProvider, pod: &v1.Pod{Name: "pod", Namespace: "ns"}},
		{name: "chained IPAM", provider: "macvlan.ns", ipamOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := cniEventTestHandler(t, tc.pod, nil, &cniEventRecorder{})
			response := serveCNIRequest(t, handler, "/api/v1/del", request.CniRequest{
				PodName: "pod", PodNamespace: "ns", Provider: tc.provider, CniType: util.CniTypeName,
				ContainerID: "container", IfName: "eth0", PrepareOnly: true,
			})
			require.Equal(t, http.StatusOK, response.Code)
			var reply request.CniResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &reply))
			require.NotNil(t, reply.Plan)
			require.True(t, reply.Plan.Delete)
			require.Equal(t, tc.ipamOnly, reply.Plan.IPAMOnly)
		})
	}
}

func TestHandleDelNoNetNSHasNoEvent(t *testing.T) {
	recorder := &cniEventRecorder{}
	handler := cniEventTestHandler(t, nil, nil, recorder)

	response := serveCNIRequest(t, handler, "/api/v1/del", request.CniRequest{
		PodName: "deleted", PodNamespace: "ns", Provider: util.OvnProvider, PrepareOnly: true,
	})
	require.Equal(t, http.StatusOK, response.Code)
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
	config := &Configuration{NodeName: "node-a", VswitchTables: cniEventProvider{}}
	controller := &Controller{
		config:        config,
		podsLister:    listerv1.NewPodLister(podIndexer),
		subnetsLister: kubeovnlister.NewSubnetLister(subnetIndexer),
		recorder:      recorder,
	}
	return createCniServerHandler(config, controller)
}

type cniEventProvider struct {
	filterErr error
}

func (p cniEventProvider) Table(model.Model) table.Handle {
	return cniEventHandle{filterErr: p.filterErr}
}

type cniEventHandle struct {
	table.Handle
	filterErr error
}

func (h cniEventHandle) Filter(context.Context, any, any) error {
	return h.filterErr
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
