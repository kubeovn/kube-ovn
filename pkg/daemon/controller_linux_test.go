package daemon

import (
	"errors"
	"net"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newPodQoSTestController(t *testing.T, pod *v1.Pod) (*Controller, *record.FakeRecorder) {
	t.Helper()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(pod))
	recorder := record.NewFakeRecorder(10)
	return &Controller{
		config:     &Configuration{NodeName: "node-a"},
		podsLister: listerv1.NewPodLister(indexer),
		recorder:   recorder,
	}, recorder
}

func requirePodEvent(t *testing.T, recorder *record.FakeRecorder, parts ...string) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		for _, part := range parts {
			require.Contains(t, event, part)
		}
	case <-time.After(time.Second):
		t.Fatal("expected pod event")
	}
}

func requireNoPodEvent(t *testing.T, recorder *record.FakeRecorder) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected pod event: %s", event)
	default:
	}
}

func TestHandleUpdatePodValidationFailureEmitsOnlyValidationEvent(t *testing.T) {
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "pod",
		Namespace: metav1.NamespaceDefault,
		Annotations: map[string]string{
			util.IPAddressAnnotation: "invalid",
		},
	}}
	controller, recorder := newPodQoSTestController(t, pod)

	require.Error(t, controller.handleUpdatePod("default/pod"))
	requirePodEvent(t, recorder, "Warning", "ValidatePodNetworkFailed", "invalid")
	requireNoPodEvent(t, recorder)
}

func TestHandleUpdatePodBandwidthFailureEmitsQoSFailureEvent(t *testing.T) {
	failErr := errors.New("default bandwidth failure")
	stubPodQoSFunctions(t, "bandwidth", "pod.default", failErr)
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        "pod",
		Namespace:   metav1.NamespaceDefault,
		Annotations: map[string]string{},
	}}
	controller, recorder := newPodQoSTestController(t, pod)

	require.ErrorIs(t, controller.handleUpdatePod("default/pod"), failErr)
	requirePodEvent(t, recorder,
		"Warning", "PodQoSUpdateFailed", "stage=bandwidth", "provider=ovn",
		"interface=pod.default", "node=node-a", failErr.Error())
	requireNoPodEvent(t, recorder)
}

func stubPodQoSFunctions(t *testing.T, failStage, failInterface string, failErr error) map[string][]string {
	t.Helper()

	originalBandwidth := setInterfaceBandwidth
	originalMirror := configInterfaceMirror
	originalNetem := setNetemQos
	t.Cleanup(func() {
		setInterfaceBandwidth = originalBandwidth
		configInterfaceMirror = originalMirror
		setNetemQos = originalNetem
	})

	calls := map[string][]string{}
	setInterfaceBandwidth = func(_, _, iface, _, _, _, _ string) error {
		calls["bandwidth"] = append(calls["bandwidth"], iface)
		if failStage == "bandwidth" && iface == failInterface {
			return failErr
		}
		return nil
	}
	configInterfaceMirror = func(_ bool, _, iface string) error {
		calls["mirror"] = append(calls["mirror"], iface)
		if failStage == "mirror" && iface == failInterface {
			return failErr
		}
		return nil
	}
	setNetemQos = func(_, _, iface, _, _, _, _ string) error {
		calls["netem"] = append(calls["netem"], iface)
		if failStage == "netem" && iface == failInterface {
			return failErr
		}
		return nil
	}
	return calls
}

func newPodQoSTestPod(networkAnnotation string) *v1.Pod {
	annotations := map[string]string{}
	if networkAnnotation != "" {
		annotations[nadv1.NetworkAttachmentAnnot] = networkAnnotation
		annotations["net1.default.ovn.kubernetes.io/allocated"] = "true"
	}
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        "pod",
		Namespace:   metav1.NamespaceDefault,
		Annotations: annotations,
	}}
}

func TestHandleUpdatePodQoSCallFailuresEmitOneEvent(t *testing.T) {
	const multusInterface = "pod.default.net1.default.ovn"
	failErr := errors.New("injected QoS failure")

	for _, stage := range []string{"bandwidth", "mirror", "netem"} {
		t.Run(stage, func(t *testing.T) {
			calls := stubPodQoSFunctions(t, stage, multusInterface, failErr)
			controller, recorder := newPodQoSTestController(t, newPodQoSTestPod("default/net1"))

			require.ErrorIs(t, controller.handleUpdatePod("default/pod"), failErr)
			require.Contains(t, calls[stage], multusInterface)
			requirePodEvent(t, recorder,
				"Warning", "PodQoSUpdateFailed", "stage="+stage,
				"provider=net1.default.ovn", "interface="+multusInterface,
				"node=node-a", failErr.Error())
			requireNoPodEvent(t, recorder)
		})
	}
}

func TestHandleUpdatePodSuccessEmitsOneEventWithProcessedInterfaces(t *testing.T) {
	calls := stubPodQoSFunctions(t, "", "", nil)
	controller, recorder := newPodQoSTestController(t, newPodQoSTestPod("default/net1"))

	require.NoError(t, controller.handleUpdatePod("default/pod"))
	require.Equal(t, []string{"pod.default", "pod.default.net1.default.ovn"}, calls["netem"])
	requirePodEvent(t, recorder,
		"Normal", "PodQoSUpdated", "node=node-a",
		"provider=ovn interface=pod.default",
		"provider=net1.default.ovn interface=pod.default.net1.default.ovn")
	requireNoPodEvent(t, recorder)
}

func TestHandleUpdatePodKeepsMultusPodNamesIndependent(t *testing.T) {
	pod := newPodQoSTestPod("default/net1,default/net2")
	pod.Annotations["net1.default.ovn.kubernetes.io/virtualmachine"] = "vm-one"
	pod.Annotations["net2.default.ovn.kubernetes.io/allocated"] = "true"
	calls := stubPodQoSFunctions(t, "", "", nil)
	controller, recorder := newPodQoSTestController(t, pod)

	require.NoError(t, controller.handleUpdatePod("default/pod"))
	expectedInterfaces := []string{
		"pod.default",
		"vm-one.default.net1.default.ovn",
		"pod.default.net2.default.ovn",
	}
	for _, stage := range []string{"bandwidth", "mirror", "netem"} {
		require.Equal(t, expectedInterfaces, calls[stage])
	}
	requirePodEvent(t, recorder,
		"Normal", "PodQoSUpdated",
		"provider=net1.default.ovn interface=vm-one.default.net1.default.ovn",
		"provider=net2.default.ovn interface=pod.default.net2.default.ovn")
	requireNoPodEvent(t, recorder)
}

func TestHandleUpdatePodNetworkAttachmentParseFailureEmitsOneEvent(t *testing.T) {
	stubPodQoSFunctions(t, "", "", nil)
	pod := newPodQoSTestPod("[")
	controller, recorder := newPodQoSTestController(t, pod)

	err := controller.handleUpdatePod("default/pod")
	require.Error(t, err)
	requirePodEvent(t, recorder,
		"Warning", "PodQoSUpdateFailed", "stage=parseNetworkAttachment",
		"provider=unknown", "interface=unknown", "node=node-a", err.Error())
	requireNoPodEvent(t, recorder)
}

func TestHandleUpdatePodWithoutMultusEmitsDefaultInterfaceSuccess(t *testing.T) {
	stubPodQoSFunctions(t, "", "", nil)
	controller, recorder := newPodQoSTestController(t, newPodQoSTestPod(""))

	require.NoError(t, controller.handleUpdatePod("default/pod"))
	requirePodEvent(t, recorder,
		"Normal", "PodQoSUpdated", "node=node-a", "provider=ovn interface=pod.default")
	requireNoPodEvent(t, recorder)
}

func TestHandleUpdatePodNotFoundEmitsNoEvent(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	recorder := record.NewFakeRecorder(1)
	controller := &Controller{
		config:     &Configuration{NodeName: "node-a"},
		podsLister: listerv1.NewPodLister(indexer),
		recorder:   recorder,
	}

	require.NoError(t, controller.handleUpdatePod("default/missing"))
	requireNoPodEvent(t, recorder)
}

func TestEnqueueUpdatePodOnlyQueuesRelevantAnnotationChanges(t *testing.T) {
	basePod := newPodQoSTestPod("default/net1")
	basePod.Annotations[util.IngressRateAnnotation] = "10"
	basePod.Annotations[util.MirrorControlAnnotation] = "true"
	basePod.Annotations[util.NetemQosLatencyAnnotation] = "5"
	basePod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	basePod.Annotations["net1.default.ovn.kubernetes.io/ingress_rate"] = "20"

	tests := []struct {
		name       string
		annotation string
		value      string
		wantQueue  bool
	}{
		{name: "unchanged", wantQueue: false},
		{name: "unrelated annotation", annotation: "example.com/unrelated", value: "changed", wantQueue: false},
		{name: "default bandwidth", annotation: util.IngressRateAnnotation, value: "11", wantQueue: true},
		{name: "default mirror", annotation: util.MirrorControlAnnotation, value: "false", wantQueue: true},
		{name: "default netem", annotation: util.NetemQosLatencyAnnotation, value: "6", wantQueue: true},
		{name: "default IP", annotation: util.IPAddressAnnotation, value: "10.0.0.3", wantQueue: true},
		{name: "multus bandwidth", annotation: "net1.default.ovn.kubernetes.io/ingress_rate", value: "21", wantQueue: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPod := basePod.DeepCopy()
			newPod := basePod.DeepCopy()
			if tt.annotation != "" {
				newPod.Annotations[tt.annotation] = tt.value
			}
			recorder := record.NewFakeRecorder(1)
			controller := &Controller{
				updatePodQueue: newTypedRateLimitingQueue[string]("test-update-pod", nil),
				recorder:       recorder,
			}
			t.Cleanup(controller.updatePodQueue.ShutDown)

			controller.enqueueUpdatePod(oldPod, newPod)
			if tt.wantQueue {
				require.Equal(t, 1, controller.updatePodQueue.Len())
			} else {
				require.Zero(t, controller.updatePodQueue.Len())
			}
			requireNoPodEvent(t, recorder)
		})
	}
}

func newPodForPolicyRouting(name, namespace, subnetName, podIP string, podIPs []v1.PodIP) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
			},
		},
		Status: v1.PodStatus{
			PodIP:  podIP,
			PodIPs: podIPs,
		},
	}
}

func TestGetPolicyRouting(t *testing.T) {
	t.Parallel()

	const (
		clusterRouter = "ovn-cluster"
		nodeName      = "test-node"
		subnetName    = "test-subnet"
		tableID       = 100
		priority      = 200
	)

	tests := []struct {
		name          string
		subnet        *kubeovnv1.Subnet
		pods          []*v1.Pod
		expectedRules int
		expectedRtns  int
		validateRules func(t *testing.T, rules []netlink.Rule)
	}{
		{
			name:          "nil subnet returns nil",
			subnet:        nil,
			expectedRules: 0,
			expectedRtns:  0,
		},
		{
			name: "subnet without ExternalEgressGateway returns nil",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:         clusterRouter,
					GatewayType: kubeovnv1.GWDistributedType,
				},
			},
			expectedRules: 0,
			expectedRtns:  0,
		},
		{
			name: "subnet with different VPC returns nil",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   "other-vpc",
					ExternalEgressGateway: "10.0.0.1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			expectedRules: 0,
			expectedRtns:  0,
		},
		{
			name: "distributed: single-stack IPv4 EGW + IPv4 Pod",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24",
					ExternalEgressGateway: "10.0.0.1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}}),
			},
			expectedRules: 1,
			expectedRtns:  1,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.Equal(t, net.ParseIP("10.16.0.5"), rules[0].Src.IP)
				ones, _ := rules[0].Src.Mask.Size()
				require.Equal(t, 32, ones)
			},
		},
		{
			name: "distributed: dual-stack EGW + dual-stack Pod",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}, {IP: "fd00::5"}}),
			},
			expectedRules: 2,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.Equal(t, net.ParseIP("10.16.0.5"), rules[0].Src.IP)
				ones, _ := rules[0].Src.Mask.Size()
				require.Equal(t, 32, ones)

				require.Equal(t, unix.AF_INET6, rules[1].Family)
				require.Equal(t, net.ParseIP("fd00::5"), rules[1].Src.IP)
				ones, _ = rules[1].Src.Mask.Size()
				require.Equal(t, 128, ones)
			},
		},
		{
			name: "distributed: dual-stack EGW + IPv4-only Pod should skip IPv6 rule",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}}),
			},
			expectedRules: 1,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.Equal(t, net.ParseIP("10.16.0.5"), rules[0].Src.IP)
			},
		},
		{
			name: "distributed: dual-stack EGW + IPv6-only Pod should skip IPv4 rule",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "fd00::5",
					[]v1.PodIP{{IP: "fd00::5"}}),
			},
			expectedRules: 1,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET6, rules[0].Family)
				require.Equal(t, net.ParseIP("fd00::5"), rules[0].Src.IP)
				ones, _ := rules[0].Src.Mask.Size()
				require.Equal(t, 128, ones)
			},
		},
		{
			name: "distributed: multiple pods with mixed stacks",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				// dual-stack pod: should generate 2 rules
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}, {IP: "fd00::5"}}),
				// IPv4-only pod: should generate 1 rule
				newPodForPolicyRouting("pod2", "default", subnetName, "10.16.0.6",
					[]v1.PodIP{{IP: "10.16.0.6"}}),
				// pod in different subnet: should be skipped
				newPodForPolicyRouting("pod3", "default", "other-subnet", "10.16.0.7",
					[]v1.PodIP{{IP: "10.16.0.7"}}),
				// pod without IP: should be skipped
				newPodForPolicyRouting("pod4", "default", subnetName, "",
					nil),
			},
			expectedRules: 3, // 2 from pod1 + 1 from pod2
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				for _, r := range rules {
					require.NotNil(t, r.Src, "rule.Src must not be nil")
					require.NotNil(t, r.Src.IP, "rule.Src.IP must not be nil")
				}
			},
		},
		{
			name: "centralized: dual-stack EGW + dual-stack CIDR",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWCentralizedType,
					GatewayNode:           nodeName,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			expectedRules: 2,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.NotNil(t, rules[0].Src)
				require.Equal(t, "10.16.0.0/24", rules[0].Src.String())

				require.Equal(t, unix.AF_INET6, rules[1].Family)
				require.NotNil(t, rules[1].Src)
				require.Equal(t, "fd00::/120", rules[1].Src.String())
			},
		},
		{
			name: "centralized: dual-stack EGW + single-stack CIDR skips missing protocol",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWCentralizedType,
					GatewayNode:           nodeName,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			expectedRules: 1,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.NotNil(t, rules[0].Src)
				require.Equal(t, "10.16.0.0/24", rules[0].Src.String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build pod indexer
			podIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			for _, pod := range tt.pods {
				require.NoError(t, podIndexer.Add(pod))
			}

			// Build node indexer for centralized gateway tests
			nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, nodeIndexer.Add(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			}))

			c := &Controller{
				podsLister:  listerv1.NewPodLister(podIndexer),
				nodesLister: listerv1.NewNodeLister(nodeIndexer),
				config: &Configuration{
					ClusterRouter: clusterRouter,
					NodeName:      nodeName,
				},
			}

			rules, routes, err := c.getPolicyRouting(tt.subnet)
			require.NoError(t, err)
			require.Len(t, rules, tt.expectedRules)
			require.Len(t, routes, tt.expectedRtns)

			// Validate all rules have non-nil Src.IP
			for i, r := range rules {
				require.NotNil(t, r.Src, "rule[%d].Src must not be nil", i)
				require.NotNil(t, r.Src.IP, "rule[%d].Src.IP must not be nil", i)
			}

			if tt.validateRules != nil {
				tt.validateRules(t, rules)
			}
		})
	}
}
