package controller

import (
	"errors"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"
	"k8s.io/utils/set"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestRecordVpcEgressGatewayEvent(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
	}

	c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, "ReconcileWorkloadFailed", "boom")

	var event string
	select {
	case event = <-recorder.Events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	require.Contains(t, event, corev1.EventTypeWarning)
	require.Contains(t, event, "ReconcileWorkloadFailed")
	require.Contains(t, event, "boom")
}

func TestVpcEgressGatewayReadyConditionChanged(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	gw.Generation = 2
	gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", "waiting", gw.Generation)

	require.False(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, "Processing", "waiting"))
	require.True(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, "Processing", "still waiting"))
	require.True(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionTrue, "ReconcileSuccess", ""))
}

func TestSetVpcEgressGatewayNotReadyClearsStaleReady(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	gw.Generation = 2
	gw.Status.Ready = true
	gw.Status.Phase = kubeovnv1.PhaseCompleted
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)

	changed := setVpcEgressGatewayNotReady(gw, "ReconcileOVNRoutesFailed", "route failed")

	require.True(t, changed)
	require.False(t, gw.Status.Ready)
	require.Equal(t, kubeovnv1.PhaseProcessing, gw.Status.Phase)
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.Ready)
	require.NotNil(t, condition)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "ReconcileOVNRoutesFailed", condition.Reason)
	require.Equal(t, "route failed", condition.Message)
}

func TestHandleDelVpcEgressGatewayReturnsFinalizerUpdateError(t *testing.T) {
	updateErr := errors.New("update failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(gw))
	kubeOvnClient := kubeovnfake.NewSimpleClientset(gw)
	kubeOvnClient.PrependReactor("update", "vpc-egress-gateways", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, updateErr
	})

	mockCtrl := gomock.NewController(t)
	mockOvnClient := mockovs.NewMockNbClient(mockCtrl)
	mockOvnClient.EXPECT().FindBFD(gomock.Any()).Return(nil, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(util.DefaultVpc, -1, gomock.Any()).Return(nil)
	mockOvnClient.EXPECT().DeletePortGroup(gomock.Any()).Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSet(gomock.Any()).Return(nil).Times(2)

	c := &Controller{
		config: &Configuration{
			ClusterRouter: util.DefaultVpc,
			KubeOvnClient: kubeOvnClient,
		},
		OVNNbClient:              mockOvnClient,
		recorder:                 record.NewFakeRecorder(10),
		vpcEgressGatewayKeyMutex: keymutex.NewHashed(0),
		vpcEgressGatewayLister:   kubeovnlister.NewVpcEgressGatewayLister(indexer),
	}

	err := c.handleDelVpcEgressGateway("default/egress-gw")
	require.ErrorIs(t, err, updateErr)
}

func TestVpcEgressGatewayContainerBFDDDefaultResources(t *testing.T) {
	container := genGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5)

	require.Equal(t, "200m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "200m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "50Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "50Mi", container.Resources.Limits.Memory().String())
	ephemeralStorage := container.Resources.Limits[corev1.ResourceEphemeralStorage]
	require.Equal(t, "1Gi", ephemeralStorage.String())
}

func TestLocalGatewayPolicyBFDSessionsSkipsEmptySession(t *testing.T) {
	require.Empty(t, localGatewayPolicyBFDSessions(map[string]string{"10.244.10.4": ""}, "10.244.10.4"))
	require.Equal(t, set.New("bfd-1"), localGatewayPolicyBFDSessions(map[string]string{"10.244.10.4": "bfd-1"}, "10.244.10.4"))
}

func newVegWorkloadPod(name, node, podIP, attachment string) *corev1.Pod {
	annotations := map[string]string{}
	if attachment != "" {
		annotations[nadv1.NetworkStatusAnnot] = attachment
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: node,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIPs: []corev1.PodIP{{
				IP: podIP,
			}},
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func TestCollectVpcEgressGatewayWorkloadStatus(t *testing.T) {
	attachmentNetwork := "default/eth1"
	readyAttachment := `[{"name":"default/eth1","ips":["172.17.1.10"]}]`

	tests := []struct {
		name              string
		pods              []*corev1.Pod
		wantInternalIPs   []string
		wantExternalIPs   []string
		wantNodes         []string
		wantNotReadyCount int
	}{
		{
			name: "all workload pods have attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`),
			},
			wantInternalIPs: []string{"10.16.1.10", "10.16.1.11"},
			wantExternalIPs: []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:       []string{"node-1", "node-2"},
		},
		{
			name: "one workload pod misses attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"kube-ovn","ips":["10.16.1.11"]}]`),
			},
			wantInternalIPs:   []string{"10.16.1.10"},
			wantExternalIPs:   []string{"172.17.1.10"},
			wantNodes:         []string{"node-1"},
			wantNotReadyCount: 2,
		},
		{
			name: "one workload pod has attachment network without ip",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":[]}]`),
			},
			wantInternalIPs:   []string{"10.16.1.10"},
			wantExternalIPs:   []string{"172.17.1.10"},
			wantNodes:         []string{"node-1"},
			wantNotReadyCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcEgressGateway{
				Spec: kubeovnv1.VpcEgressGatewaySpec{
					Replicas: 2,
				},
			}

			_, _, messages := collectVpcEgressGatewayWorkloadStatus(gw, tt.pods, attachmentNetwork)

			require.Equal(t, tt.wantInternalIPs, gw.Status.InternalIPs)
			require.Equal(t, tt.wantExternalIPs, gw.Status.ExternalIPs)
			require.Equal(t, tt.wantNodes, gw.Status.Workload.Nodes)
			require.Len(t, messages, tt.wantNotReadyCount)
		})
	}
}
