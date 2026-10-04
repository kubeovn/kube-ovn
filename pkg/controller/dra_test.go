package controller

import (
	"fmt"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	testDRADriver       = "nic.kubeovn.io"
	testDRAOtherDriver  = "gpu.example.com"
	testDRAOtherDevice  = "gpu-0"
	testDRAPool         = "node1"
	testDRAClaimName    = "claim-1"
	testDRAOtherClaim   = "claim-2"
	testDRARequest      = "nic"
	testDRAIface1       = "net1"
	testDRAIface2       = "net2"
	testDRAPodUID       = "uid-1"
	testDRABlueSubnet   = "blue"
	testDRABlueProvider = "blue.default.ovn"
	testDRABlueDevice   = "subnet-blue"
	testDRABlueCIDR     = "10.10.0.0/24"
	testDRABlueGateway  = "10.10.0.1"
	testDRARedSubnet    = "red"
	testDRARedProvider  = "red.default.ovn"
	testDRARedDevice    = "subnet-red"
	// testDRAMovedDevice changes its subnet between pool generations.
	testDRAMovedDevice = "subnet-x"
)

func newTestDRAController(t *testing.T, subnets []*kubeovnv1.Subnet, claims []*resourcev1.ResourceClaim, slices []*resourcev1.ResourceSlice) *Controller {
	t.Helper()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: subnets})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableDRANic = true
	ctrl.config.DRANicDriverName = testDRADriver

	ctrl.resourceClaimIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draClaimPodIndex: draClaimPodIndexFunc})
	for _, claim := range claims {
		require.NoError(t, ctrl.resourceClaimIndexer.Add(claim))
	}
	ctrl.resourceSliceIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draSlicePoolIndex: draSlicePoolIndexFunc})
	for _, slice := range slices {
		require.NoError(t, ctrl.resourceSliceIndexer.Add(slice))
	}
	return ctrl
}

func testDRASlice(name string, generation int64, devices ...resourcev1.Device) *resourcev1.ResourceSlice {
	return &resourcev1.ResourceSlice{
		Name: name,
		Spec: resourcev1.ResourceSliceSpec{
			Driver:  testDRADriver,
			Pool:    resourcev1.ResourcePool{Name: testDRAPool, Generation: generation},
			Devices: devices,
		},
	}
}

func testDRADevice(name, subnet string) resourcev1.Device {
	return resourcev1.Device{
		Name: name,
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			testDRADriver + "/" + draAttrSubnetName: {StringValue: new(subnet)},
		},
	}
}

func testDRAClaim(name, podName string, podUID types.UID, results ...resourcev1.DeviceRequestAllocationResult) *resourcev1.ResourceClaim {
	return &resourcev1.ResourceClaim{
		Name: name, Namespace: metav1.NamespaceDefault,
		Status: resourcev1.ResourceClaimStatus{
			Allocation: &resourcev1.AllocationResult{
				Devices: resourcev1.DeviceAllocationResult{Results: results},
			},
			ReservedFor: []resourcev1.ResourceClaimConsumerReference{{Resource: "pods", Name: podName, UID: podUID}},
		},
	}
}

// withNicConfig adds the driver's opaque config with an interfaceName for the requests (all if none).
func withNicConfig(claim *resourcev1.ResourceClaim, ifaceName string, requests ...string) *resourcev1.ResourceClaim {
	claim.Status.Allocation.Devices.Config = append(claim.Status.Allocation.Devices.Config, resourcev1.DeviceAllocationConfiguration{
		Source:   resourcev1.AllocationConfigSourceClaim,
		Requests: requests,
		Opaque: &resourcev1.OpaqueDeviceConfiguration{
			Driver:     testDRADriver,
			Parameters: runtime.RawExtension{Raw: fmt.Appendf(nil, `{"apiVersion":"nic.resource.kube-ovn.io/v1alpha1","kind":"NicConfig","interfaceName":%q}`, ifaceName)},
		},
	})
	return claim
}

func testDRAResult(driver, device string) resourcev1.DeviceRequestAllocationResult {
	return resourcev1.DeviceRequestAllocationResult{Request: testDRARequest, Driver: driver, Pool: testDRAPool, Device: device}
}

func TestGetPodDRANets(t *testing.T) {
	pod := &corev1.Pod{Name: "pod1", Namespace: metav1.NamespaceDefault, UID: testDRAPodUID}
	subnets := []*kubeovnv1.Subnet{
		{Name: testDRABlueSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: testDRABlueProvider}},
		{Name: testDRARedSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.20.0.0/24", Provider: testDRARedProvider}},
		{Name: "primary", Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.30.0.0/24", Provider: "ovn"}},
	}
	slices := []*resourcev1.ResourceSlice{testDRASlice("slice-1", 1,
		testDRADevice(testDRABlueDevice, testDRABlueSubnet),
		testDRADevice(testDRARedDevice, testDRARedSubnet),
		testDRADevice("subnet-primary", "primary"),
	)}

	t.Run("maps allocated devices to subnet providers", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{
			testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice), testDRAResult(testDRAOtherDriver, testDRAOtherDevice)),
			testDRAClaim(testDRAOtherClaim, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRARedDevice)),
		}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		providers := make([]string, 0, len(nets))
		for _, n := range nets {
			require.Equal(t, providerTypeOriginal, n.Type)
			require.True(t, n.IsDRA)
			providers = append(providers, n.ProviderName)
		}
		require.ElementsMatch(t, []string{testDRABlueProvider, testDRARedProvider}, providers)
	})

	t.Run("ignores claims reserved for a previous pod with the same name", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, "old-uid", testDRAResult(testDRADriver, testDRABlueDevice))}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Empty(t, nets)
	})

	t.Run("rejects the default provider", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, "subnet-primary"))}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "default provider")
	})

	t.Run("rejects nics sharing a provider without interface names", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID,
			testDRAResult(testDRADriver, testDRABlueDevice), testDRAResult(testDRADriver, testDRABlueDevice))}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "more than one dra nic")
	})

	t.Run("names nics sharing a provider after their interface", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{
			withNicConfig(testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice), testDRAResult(testDRADriver, testDRARedDevice)), testDRAIface1),
			withNicConfig(testDRAClaim(testDRAOtherClaim, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), testDRAIface2),
		}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		providers := make([]string, 0, len(nets))
		for _, n := range nets {
			providers = append(providers, n.ProviderName)
		}
		// red is alone on its provider and keeps the plain key, like a single Multus attachment.
		require.ElementsMatch(t, []string{testDRABlueProvider + "." + testDRAIface1, testDRARedProvider, testDRABlueProvider + "." + testDRAIface2}, providers)
	})

	t.Run("rejects nics sharing a provider and an interface name", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{
			withNicConfig(testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), testDRAIface1),
			withNicConfig(testDRAClaim(testDRAOtherClaim, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), testDRAIface1),
		}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "more than one dra nic named "+testDRABlueProvider+"."+testDRAIface1)
	})

	t.Run("uses the newest pool generation", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{
			testDRASlice("slice-old", 1, testDRADevice(testDRAMovedDevice, testDRABlueSubnet)),
			testDRASlice("slice-new", 2, testDRADevice(testDRAMovedDevice, testDRARedSubnet)),
		})

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRARedProvider, nets[0].ProviderName)
	})

	t.Run("ignores a device only left in an outdated generation", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{
			testDRASlice("slice-old", 1, testDRADevice(testDRAMovedDevice, testDRABlueSubnet)),
			testDRASlice("slice-new", 2, testDRADevice(testDRARedDevice, testDRARedSubnet)),
		})

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "not published")
	})

	t.Run("finds a device in any slice of the newest generation", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{
			testDRASlice("slice-new-a", 2, testDRADevice(testDRARedDevice, testDRARedSubnet)),
			testDRASlice("slice-new-b", 2, testDRADevice(testDRAMovedDevice, testDRABlueSubnet)),
		})

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRABlueProvider, nets[0].ProviderName)
	})

	t.Run("fails while the allocated device is not published", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, "subnet-missing"))}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "not published")
	})

	t.Run("disabled returns nothing", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))}
		ctrl := newTestDRAController(t, subnets, claims, slices)
		ctrl.config.EnableDRANic = false

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Empty(t, nets)
	})
}

func TestDRAInterfaceName(t *testing.T) {
	claim := testDRAClaim("claim", "pod", "uid", testDRAResult(testDRADriver, testDRABlueDevice))
	require.Empty(t, draInterfaceName(claim, testDRARequest, testDRADriver))

	withNicConfig(claim, "net0")
	withNicConfig(claim, testDRAIface1, testDRARequest)
	withNicConfig(claim, "other", "other-request")
	claim.Status.Allocation.Devices.Config = append(claim.Status.Allocation.Devices.Config, resourcev1.DeviceAllocationConfiguration{
		Opaque: &resourcev1.OpaqueDeviceConfiguration{
			Driver: testDRAOtherDriver, Parameters: runtime.RawExtension{Raw: []byte(`{"interfaceName":"gpu"}`)},
		},
	})

	// The request-specific config comes after the claim-wide one and wins; configs of other
	// requests and other drivers are ignored. Subrequests match their parent request.
	require.Equal(t, testDRAIface1, draInterfaceName(claim, testDRARequest, testDRADriver))
	require.Equal(t, testDRAIface1, draInterfaceName(claim, testDRARequest+"/first", testDRADriver))
	require.Equal(t, "net0", draInterfaceName(claim, "unrelated", testDRADriver))
}

func TestEnqueueClaimPods(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.DRANicDriverName = testDRADriver
	ctrl.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("TestAddOrUpdatePod", nil)
	t.Cleanup(ctrl.addOrUpdatePodQueue.ShutDown)

	pending := testDRAClaim("pending", "pod-a", "uid-a")
	pending.Status = resourcev1.ResourceClaimStatus{}
	ctrl.enqueueAddResourceClaim(pending)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len())

	// A claim of another driver still requeues the pod: its DRA nics wait for all claims.
	gpu := testDRAClaim("gpu", "pod-b", "uid-b", testDRAResult(testDRAOtherDriver, testDRAOtherDevice))
	ctrl.enqueueUpdateResourceClaim(pending, gpu)
	require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len())
	key, _ := ctrl.addOrUpdatePodQueue.Get()
	require.Equal(t, "default/pod-b", key)
	ctrl.addOrUpdatePodQueue.Done(key)

	// Updates that change neither allocation nor reservation are ignored.
	relabeled := gpu.DeepCopy()
	relabeled.Labels = map[string]string{"changed": "true"}
	ctrl.enqueueUpdateResourceClaim(gpu, relabeled)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len())
}

func TestSyncKubeOvnNetKeepsPortsWhileClaimsPending(t *testing.T) {
	pod := &corev1.Pod{
		Name: "virt-launcher-vm1", Namespace: metav1.NamespaceDefault, UID: testDRAPodUID,
		Spec: corev1.PodSpec{ResourceClaims: []corev1.PodResourceClaim{{Name: "nic"}}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableDRANic = true
	ctrl.resourceClaimIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draClaimPodIndex: draClaimPodIndexFunc})

	stale := ovs.PodNameToPortName(pod.Name, pod.Namespace, testDRABlueProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: stale}}, nil)
	// No DeleteLogicalSwitchPort expectation: gomock fails the test if the port is removed.

	_, details, err := ctrl.syncKubeOvnNet(pod, nil)
	require.NoError(t, err)
	require.Empty(t, details)
}

func TestHasPendingDRAClaims(t *testing.T) {
	pod := &corev1.Pod{
		Name: "virt-launcher-vm1-abcde", Namespace: metav1.NamespaceDefault, UID: testDRAPodUID,
		Spec: corev1.PodSpec{ResourceClaims: []corev1.PodResourceClaim{{Name: "nic"}, {Name: "gpu"}}},
	}
	nicClaim := testDRAClaim("vm1-nic", pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))
	gpuClaim := testDRAClaim("vm1-gpu", pod.Name, pod.UID, testDRAResult(testDRAOtherDriver, testDRAOtherDevice))
	staleClaim := testDRAClaim("vm1-old", pod.Name, "old-uid", testDRAResult(testDRADriver, testDRABlueDevice))

	tests := []struct {
		name    string
		pod     *corev1.Pod
		claims  []*resourcev1.ResourceClaim
		pending bool
	}{
		{"no claims allocated yet", pod, nil, true},
		{"claim of the previous pod does not count", pod, []*resourcev1.ResourceClaim{staleClaim, gpuClaim}, true},
		{"one of two claims allocated", pod, []*resourcev1.ResourceClaim{nicClaim}, true},
		{"all claims allocated, including other drivers", pod, []*resourcev1.ResourceClaim{nicClaim, gpuClaim}, false},
		{"pod without claims", &corev1.Pod{ObjectMeta: pod.ObjectMeta}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := newTestDRAController(t, nil, tt.claims, nil)
			require.Equal(t, tt.pending, ctrl.hasPendingDRAClaims(tt.pod))
		})
	}
}

func TestDeferUnscheduledDRANets(t *testing.T) {
	primary := &kubeovnNet{ProviderName: "ovn"}
	dra := &kubeovnNet{ProviderName: testDRABlueProvider, IsDRA: true}

	unscheduled := &corev1.Pod{}
	require.Equal(t, []*kubeovnNet{primary}, deferUnscheduledDRANets(unscheduled, []*kubeovnNet{primary, dra}))

	scheduled := &corev1.Pod{Spec: corev1.PodSpec{NodeName: testDRAPool}}
	require.Equal(t, []*kubeovnNet{primary, dra}, deferUnscheduledDRANets(scheduled, []*kubeovnNet{primary, dra}))
}

func TestGetPodKubeovnNetsRejectsProviderSharedWithAttachment(t *testing.T) {
	const provider = "attachnet-a.default.ovn"
	pod := &corev1.Pod{
		Name: "test-pod", Namespace: metav1.NamespaceDefault, UID: testDRAPodUID,
		Annotations: map[string]string{nadv1.NetworkAttachmentAnnot: `[{"name": "attachnet-a"}]`},
	}
	nad := &nadv1.NetworkAttachmentDefinition{
		Name: "attachnet-a", Namespace: metav1.NamespaceDefault,
		Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion": "0.3.1", "name": "attachnet-a", "type": "kube-ovn",
			"server_socket": "/run/openvswitch/kube-ovn-daemon.sock", "provider": "` + provider + `"}`},
	}
	subnets := []*kubeovnv1.Subnet{
		{Name: util.DefaultSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.3.0.0/16", Provider: util.OvnProvider, Default: true}},
		{Name: "attach-subnet", Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: provider}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{nad},
		Subnets:            subnets,
		Pods:               []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableDRANic = true
	ctrl.config.DRANicDriverName = testDRADriver
	ctrl.resourceClaimIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draClaimPodIndex: draClaimPodIndexFunc})
	require.NoError(t, ctrl.resourceClaimIndexer.Add(testDRAClaim("claim", pod.Name, pod.UID, testDRAResult(testDRADriver, "subnet-attach"))))
	ctrl.resourceSliceIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draSlicePoolIndex: draSlicePoolIndexFunc})
	require.NoError(t, ctrl.resourceSliceIndexer.Add(testDRASlice("slice", 1, testDRADevice("subnet-attach", "attach-subnet"))))

	nets, err := ctrl.getPodKubeovnNets(pod)
	require.ErrorContains(t, err, "uses provider "+provider+" for a dra nic and network attachment default/attachnet-a")
	require.Nil(t, nets)
}
