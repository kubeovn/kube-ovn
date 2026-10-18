package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	kubevirtv1 "kubevirt.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	testDRAResourceClaims = "resourceclaims"
	testDRAResourceSlices = "resourceslices"
	testDRAOtherResource  = "deviceclasses"
	testDRADiscoveryError = "connection refused"
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

	testDRAPod              = "pod1"
	testDRAOtherPod         = "pod"
	testDRAOtherClaimName   = "claim"
	testDRAOtherUID         = "uid"
	testDRAOtherUIDB        = "uid-other"
	testDRAPodA             = "pod-a"
	testDRAPodB             = "pod-b"
	testDRAPodC             = "pod-c"
	testDRAUIDA             = "uid-a"
	testDRAUIDB             = "uid-b"
	testDRAUIDC             = "uid-c"
	testDRAOldUID           = "old-uid"
	testDRAPodsResource     = "pods"
	testDRAPendingClaim     = "pending"
	testDRAGPUClaim         = "gpu"
	testDRAOtherRequest     = "other-request"
	testDRAOtherPool        = "other-pool"
	testDRAQueueName        = "TestAddOrUpdatePod"
	testDRALabelKey         = "changed"
	testDRALabelValue       = "true"
	testDRAPathSeparator    = "/"
	testDRASlice1           = "slice-1"
	testDRASliceName        = "slice"
	testDRASliceOld         = "slice-old"
	testDRASliceNew         = "slice-new"
	testDRASliceNewA        = "slice-new-a"
	testDRASliceNewB        = "slice-new-b"
	testDRAMissingDevice    = "subnet-missing"
	testDRARedCIDR          = "10.20.0.0/24"
	testDRAPrimarySubnet    = "primary"
	testDRAPrimaryDevice    = "subnet-primary"
	testDRAPrimaryCIDR      = "10.30.0.0/24"
	testDRAMacvlanSubnet    = "macvlan"
	testDRAMacvlanDevice    = "subnet-macvlan"
	testDRAMacvlanProvider  = "macvlan.default"
	testDRAMacvlanCIDR      = "10.40.0.0/24"
	testDRADefaultCIDR      = "10.3.0.0/16"
	testDRACapacityIPv4     = "ipv4"
	testDRACapacityValue    = "5"
	testDRAIface0           = "net0"
	testDRAIface3           = "net3"
	testDRAOtherIface       = "other"
	testDRAPrimaryIface     = "eth0"
	testDRAVlanIface        = "vlan7"
	testDRAVM1              = "vm1"
	testDRAVM2              = "vm2"
	testDRALauncher         = "virt-launcher-vm1"
	testDRALauncherPod      = "virt-launcher-vm1-abcde"
	testDRASourceLauncher   = "virt-launcher-vm1-a"
	testDRATargetLauncher   = "virt-launcher-vm1-b"
	testDRAVMNICClaim       = "vm1-nic"
	testDRAVMGPUClaim       = "vm1-gpu"
	testDRAVMOldClaim       = "vm1-old"
	testDRAAttachNAD        = "attachnet-a"
	testDRAAttachProvider   = "attachnet-a.default.ovn"
	testDRAAttachSubnet     = "attach-subnet"
	testDRAAttachPod        = "test-pod"
	testDRAAttachA          = "attach-a"
	testDRAAttachAB         = testDRAAttachA + ",attach-b"
	testDRAAttachDevice     = "subnet-attach"
	testDRABlueKeyNet1      = testDRABlueProvider + "." + testDRAIface1
	testDRABlueKeyNet2      = testDRABlueProvider + "." + testDRAIface2
	testDRASubRequest       = testDRARequest + "/first"
	testDRAUnrelatedRequest = "unrelated"
	testDRAInvalidRecord    = "{"
)

func newTestDRAController(t *testing.T, subnets []*kubeovnv1.Subnet, claims []*resourcev1.ResourceClaim, slices []*resourcev1.ResourceSlice, pods ...*corev1.Pod) *Controller {
	t.Helper()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: subnets, Pods: pods})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableDRANic = true
	ctrl.config.DRANicDriverName = testDRADriver

	ctrl.resourceClaimIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draClaimPodIndex: draClaimPodIndexFunc, draClaimPoolIndex: draClaimPoolIndexFunc})
	for _, claim := range claims {
		require.NoError(t, ctrl.resourceClaimIndexer.Add(claim))
		// the controller records the claim's nics on the API object
		_, err := ctrl.config.KubeClient.ResourceV1().ResourceClaims(claim.Namespace).Create(context.Background(), claim, metav1.CreateOptions{})
		require.NoError(t, err)
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
			Pool:    resourcev1.ResourcePool{Name: testDRAPool, Generation: generation, ResourceSliceCount: 1},
			Devices: devices,
		},
	}
}

func testDRADevice(name, subnet string) resourcev1.Device {
	return resourcev1.Device{
		Name: name,
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			testDRADriver + testDRAPathSeparator + draAttrSubnetName: {StringValue: new(subnet)},
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
			ReservedFor: []resourcev1.ResourceClaimConsumerReference{{Resource: testDRAPodsResource, Name: podName, UID: podUID}},
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

func TestDRANicAPIServed(t *testing.T) {
	resourceList := func(names ...string) []*metav1.APIResourceList {
		list := &metav1.APIResourceList{GroupVersion: resourcev1.SchemeGroupVersion.String()}
		for _, name := range names {
			list.APIResources = append(list.APIResources, metav1.APIResource{Name: name})
		}
		return []*metav1.APIResourceList{list}
	}
	tests := []struct {
		name      string
		resources []*metav1.APIResourceList
		failWith  error
		served    bool
		wantErr   bool
	}{
		{name: "claims and slices served", resources: resourceList(testDRAResourceClaims, testDRAResourceSlices, testDRAOtherResource), served: true},
		{name: "slices missing", resources: resourceList(testDRAResourceClaims, testDRAOtherResource)},
		{name: "group version not served"},
		{name: "discovery fails", failWith: errors.New(testDRADiscoveryError), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakediscovery.FakeDiscovery{Fake: &kubetesting.Fake{Resources: tt.resources}}
			if tt.failWith != nil {
				client.AddReactor("*", "*", func(kubetesting.Action) (bool, runtime.Object, error) {
					return true, nil, tt.failWith
				})
			}
			served, err := draNicAPIServed(client)
			if tt.wantErr {
				require.ErrorIs(t, err, tt.failWith)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.served, served)
		})
	}
}

func TestGetPodDRANets(t *testing.T) {
	pod := &corev1.Pod{Name: testDRAPod, Namespace: metav1.NamespaceDefault, UID: testDRAPodUID}
	subnets := []*kubeovnv1.Subnet{
		{Name: testDRABlueSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: testDRABlueProvider}},
		{Name: testDRARedSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRARedCIDR, Provider: testDRARedProvider}},
		{Name: testDRAPrimarySubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRAPrimaryCIDR, Provider: util.OvnProvider}},
	}
	slices := []*resourcev1.ResourceSlice{testDRASlice(testDRASlice1, 1,
		testDRADevice(testDRABlueDevice, testDRABlueSubnet),
		testDRADevice(testDRARedDevice, testDRARedSubnet),
		testDRADevice(testDRAPrimaryDevice, testDRAPrimarySubnet),
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
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, testDRAOldUID, testDRAResult(testDRADriver, testDRABlueDevice))}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Empty(t, nets)
	})

	t.Run("rejects the default provider", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAPrimaryDevice))}
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
		require.ElementsMatch(t, []string{testDRABlueKeyNet1, testDRARedProvider, testDRABlueKeyNet2}, providers)
	})

	t.Run("rejects nics sharing a provider and an interface name", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{
			withNicConfig(testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), testDRAIface1),
			withNicConfig(testDRAClaim(testDRAOtherClaim, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), testDRAIface1),
		}
		ctrl := newTestDRAController(t, subnets, claims, slices)

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "more than one dra nic named "+testDRABlueKeyNet1)
	})

	t.Run("uses the newest pool generation", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{
			testDRASlice(testDRASliceOld, 1, testDRADevice(testDRAMovedDevice, testDRABlueSubnet)),
			testDRASlice(testDRASliceNew, 2, testDRADevice(testDRAMovedDevice, testDRARedSubnet)),
		})

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRARedProvider, nets[0].ProviderName)
	})

	t.Run("ignores a device only left in an outdated generation", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{
			testDRASlice(testDRASliceOld, 1, testDRADevice(testDRAMovedDevice, testDRABlueSubnet)),
			testDRASlice(testDRASliceNew, 2, testDRADevice(testDRARedDevice, testDRARedSubnet)),
		})

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "not published")
	})

	t.Run("finds a device in any slice of the newest generation", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		sliceA := testDRASlice(testDRASliceNewA, 2, testDRADevice(testDRARedDevice, testDRARedSubnet))
		sliceB := testDRASlice(testDRASliceNewB, 2, testDRADevice(testDRAMovedDevice, testDRABlueSubnet))
		sliceA.Spec.Pool.ResourceSliceCount, sliceB.Spec.Pool.ResourceSliceCount = 2, 2
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{sliceA, sliceB})

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRABlueProvider, nets[0].ProviderName)
	})

	t.Run("fails while the newest generation is incomplete", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMovedDevice))}
		slice := testDRASlice(testDRASliceNewB, 2, testDRADevice(testDRAMovedDevice, testDRABlueSubnet))
		slice.Spec.Pool.ResourceSliceCount = 2
		ctrl := newTestDRAController(t, subnets, claims, []*resourcev1.ResourceSlice{
			testDRASlice(testDRASliceOld, 1, testDRADevice(testDRAMovedDevice, testDRARedSubnet)), slice,
		})

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "incomplete: 1 of 2 slices of generation 2")
	})

	t.Run("rejects a non-OVN provider", func(t *testing.T) {
		nonOVN := append(append([]*kubeovnv1.Subnet{}, subnets...), &kubeovnv1.Subnet{Name: testDRAMacvlanSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRAMacvlanCIDR, Provider: testDRAMacvlanProvider}})
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMacvlanDevice))}
		ctrl := newTestDRAController(t, nonOVN, claims, []*resourcev1.ResourceSlice{testDRASlice(testDRASliceName, 1, testDRADevice(testDRAMacvlanDevice, testDRAMacvlanSubnet))})

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, "non-OVN provider macvlan.default")
	})

	t.Run("fails while the allocated device is not published", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMissingDevice))}
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
	claim := testDRAClaim(testDRAOtherClaimName, testDRAOtherPod, testDRAOtherUID, testDRAResult(testDRADriver, testDRABlueDevice))
	require.Empty(t, draInterfaceName(claim, testDRARequest, testDRADriver))

	withNicConfig(claim, testDRAIface0)
	withNicConfig(claim, testDRAIface1, testDRARequest)
	withNicConfig(claim, testDRAOtherIface, testDRAOtherRequest)
	claim.Status.Allocation.Devices.Config = append(claim.Status.Allocation.Devices.Config, resourcev1.DeviceAllocationConfiguration{
		Opaque: &resourcev1.OpaqueDeviceConfiguration{
			Driver: testDRAOtherDriver, Parameters: runtime.RawExtension{Raw: fmt.Appendf(nil, `{"interfaceName":%q}`, testDRAGPUClaim)},
		},
	})

	// The request-specific config comes after the claim-wide one and wins; configs of other
	// requests and other drivers are ignored. Subrequests match their parent request.
	require.Equal(t, testDRAIface1, draInterfaceName(claim, testDRARequest, testDRADriver))
	require.Equal(t, testDRAIface1, draInterfaceName(claim, testDRASubRequest, testDRADriver))
	require.Equal(t, testDRAIface0, draInterfaceName(claim, testDRAUnrelatedRequest, testDRADriver))
}

func TestEnqueueClaimPods(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.DRANicDriverName = testDRADriver
	ctrl.addOrUpdatePodQueue = newTypedRateLimitingQueue[string](testDRAQueueName, nil)
	t.Cleanup(ctrl.addOrUpdatePodQueue.ShutDown)

	pending := testDRAClaim(testDRAPendingClaim, testDRAPodA, testDRAUIDA)
	pending.Status = resourcev1.ResourceClaimStatus{}
	ctrl.enqueueAddResourceClaim(pending)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len())

	// A claim of another driver still requeues the pod: its DRA nics wait for all claims.
	gpu := testDRAClaim(testDRAGPUClaim, testDRAPodB, testDRAUIDB, testDRAResult(testDRAOtherDriver, testDRAOtherDevice))
	ctrl.enqueueUpdateResourceClaim(pending, gpu)
	require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len())
	key, _ := ctrl.addOrUpdatePodQueue.Get()
	require.Equal(t, cache.NewObjectName(metav1.NamespaceDefault, testDRAPodB).String(), key)
	ctrl.addOrUpdatePodQueue.Done(key)

	// Updates that change neither allocation nor reservation are ignored.
	relabeled := gpu.DeepCopy()
	relabeled.Labels = map[string]string{testDRALabelKey: testDRALabelValue}
	ctrl.enqueueUpdateResourceClaim(gpu, relabeled)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len())
}

// A claim keeps an allocation while its consumers change: only a new consumer that can still run is
// requeued, not the pods that were there before and not a finished pod the claim is reserved for again.
func TestEnqueueClaimPodsNewConsumers(t *testing.T) {
	finished := &corev1.Pod{
		Name: testDRAPodB, Namespace: metav1.NamespaceDefault, UID: testDRAUIDB,
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{finished}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.DRANicDriverName = testDRADriver
	ctrl.addOrUpdatePodQueue = newTypedRateLimitingQueue[string](testDRAQueueName, nil)
	t.Cleanup(ctrl.addOrUpdatePodQueue.ShutDown)

	source := testDRAClaim(testDRAVMNICClaim, testDRAPodA, testDRAUIDA, testDRAResult(testDRADriver, testDRAOtherDevice))
	withFinished := source.DeepCopy()
	withFinished.Status.ReservedFor = append(withFinished.Status.ReservedFor, resourcev1.ResourceClaimConsumerReference{
		Resource: testDRAPodsResource, Name: testDRAPodB, UID: testDRAUIDB,
	})

	ctrl.enqueueUpdateResourceClaim(source, withFinished)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len(), "a finished pod and the pod that was there before")
	ctrl.enqueueUpdateResourceClaim(withFinished, source)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len(), "a removed consumer")

	// A pod the lister does not know yet is a new consumer, not a finished one.
	withTarget := source.DeepCopy()
	withTarget.Status.ReservedFor = append(withTarget.Status.ReservedFor, resourcev1.ResourceClaimConsumerReference{
		Resource: testDRAPodsResource, Name: testDRAPodC, UID: testDRAUIDC,
	})
	ctrl.enqueueUpdateResourceClaim(source, withTarget)
	require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len())
	key, _ := ctrl.addOrUpdatePodQueue.Get()
	require.Equal(t, cache.NewObjectName(metav1.NamespaceDefault, testDRAPodC).String(), key)
	ctrl.addOrUpdatePodQueue.Done(key)
}

// The claim is allocated by the time the ports are listed, but was pending when the pod's nets were
// built, so its nic is missing from them: its port must not be released as stale.
func TestSyncKubeOvnNetKeepsPortsWhileClaimsPending(t *testing.T) {
	pod := &corev1.Pod{
		Name: testDRALauncher, Namespace: metav1.NamespaceDefault, UID: testDRAPodUID,
		Spec: corev1.PodSpec{ResourceClaims: []corev1.PodResourceClaim{{Name: testDRARequest}}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableDRANic = true
	ctrl.resourceClaimIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draClaimPodIndex: draClaimPodIndexFunc})
	pending := ctrl.hasPendingDRAClaims(pod)
	require.True(t, pending)
	require.NoError(t, ctrl.resourceClaimIndexer.Add(testDRAClaim(testDRAVMNICClaim, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))))
	require.False(t, ctrl.hasPendingDRAClaims(pod))

	stale := ovs.PodNameToPortName(pod.Name, pod.Namespace, testDRABlueProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: stale}}, nil)
	// No DeleteLogicalSwitchPort expectation: gomock fails the test if the port is removed.

	_, details, err := ctrl.syncKubeOvnNet(pod, nil, pending)
	require.NoError(t, err)
	require.Empty(t, details)
}

func TestHasPendingDRAClaims(t *testing.T) {
	pod := &corev1.Pod{
		Name: testDRALauncherPod, Namespace: metav1.NamespaceDefault, UID: testDRAPodUID,
		Spec: corev1.PodSpec{ResourceClaims: []corev1.PodResourceClaim{{Name: testDRARequest}, {Name: testDRAGPUClaim}}},
	}
	nicClaim := testDRAClaim(testDRAVMNICClaim, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))
	gpuClaim := testDRAClaim(testDRAVMGPUClaim, pod.Name, pod.UID, testDRAResult(testDRAOtherDriver, testDRAOtherDevice))
	staleClaim := testDRAClaim(testDRAVMOldClaim, pod.Name, testDRAOldUID, testDRAResult(testDRADriver, testDRABlueDevice))

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
	primary := &kubeovnNet{ProviderName: util.OvnProvider}
	dra := &kubeovnNet{ProviderName: testDRABlueProvider, IsDRA: true}

	unscheduled := &corev1.Pod{}
	require.Equal(t, []*kubeovnNet{primary}, deferUnscheduledDRANets(unscheduled, []*kubeovnNet{primary, dra}))

	scheduled := &corev1.Pod{Spec: corev1.PodSpec{NodeName: testDRAPool}}
	require.Equal(t, []*kubeovnNet{primary, dra}, deferUnscheduledDRANets(scheduled, []*kubeovnNet{primary, dra}))
}

func TestGetPodKubeovnNetsRejectsProviderSharedWithAttachment(t *testing.T) {
	const provider = testDRAAttachProvider
	pod := &corev1.Pod{
		Name: testDRAAttachPod, Namespace: metav1.NamespaceDefault, UID: testDRAPodUID,
		Annotations: map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf(`[{"name": %q}]`, testDRAAttachNAD)},
	}
	nad := &nadv1.NetworkAttachmentDefinition{
		Name: testDRAAttachNAD, Namespace: metav1.NamespaceDefault,
		Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: fmt.Sprintf(`{"cniVersion": "0.3.1", "name": %q, "type": "kube-ovn",
			"server_socket": "/run/openvswitch/kube-ovn-daemon.sock", "provider": %q}`, testDRAAttachNAD, provider)},
	}
	subnets := []*kubeovnv1.Subnet{
		{Name: util.DefaultSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRADefaultCIDR, Provider: util.OvnProvider, Default: true}},
		{Name: testDRAAttachSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: provider}},
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
	claim := testDRAClaim(testDRAOtherClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAAttachDevice))
	require.NoError(t, ctrl.resourceClaimIndexer.Add(claim))
	_, err = ctrl.config.KubeClient.ResourceV1().ResourceClaims(claim.Namespace).Create(context.Background(), claim, metav1.CreateOptions{})
	require.NoError(t, err)
	ctrl.resourceSliceIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{draSlicePoolIndex: draSlicePoolIndexFunc})
	require.NoError(t, ctrl.resourceSliceIndexer.Add(testDRASlice(testDRASliceName, 1, testDRADevice(testDRAAttachDevice, testDRAAttachSubnet))))

	nets, err := ctrl.getPodKubeovnNets(pod)
	require.ErrorContains(t, err, "uses provider "+provider+" for a dra nic and network attachment default/attachnet-a")
	require.Nil(t, nets)
}

// A claim can be shared by the virt-launcher pods of one VM (live migration), not by others.
func TestGetPodDRANetsSharedClaim(t *testing.T) {
	launcher := func(name string, uid types.UID, vm string) *corev1.Pod {
		pod := &corev1.Pod{Name: name, Namespace: metav1.NamespaceDefault, UID: uid}
		if vm != "" {
			pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: vm}}
		}
		return pod
	}
	subnets := []*kubeovnv1.Subnet{{Name: testDRABlueSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: testDRABlueProvider}}}
	slices := []*resourcev1.ResourceSlice{testDRASlice(testDRASlice1, 1, testDRADevice(testDRABlueDevice, testDRABlueSubnet))}
	target := launcher(testDRATargetLauncher, testDRAUIDB, testDRAVM1)
	shared := func() *resourcev1.ResourceClaim {
		claim := testDRAClaim(testDRAClaimName, target.Name, target.UID, testDRAResult(testDRADriver, testDRABlueDevice))
		claim.Status.ReservedFor = append([]resourcev1.ResourceClaimConsumerReference{{Resource: testDRAPodsResource, Name: testDRASourceLauncher, UID: testDRAUIDA}}, claim.Status.ReservedFor...)
		return claim
	}

	tests := []struct {
		name    string
		source  *corev1.Pod
		pod     *corev1.Pod
		wantErr string
	}{
		{"source and target of one vm", launcher(testDRASourceLauncher, testDRAUIDA, testDRAVM1), target, ""},
		{"source already gone", nil, target, ""},
		{"source recreated with another uid", launcher(testDRASourceLauncher, testDRAOtherUIDB, testDRAVM2), target, ""},
		{"pods of two vms", launcher(testDRASourceLauncher, testDRAUIDA, testDRAVM2), target, "can only be shared by the pods of one kubevirt vm"},
		{"plain pods", launcher(testDRASourceLauncher, testDRAUIDA, ""), launcher(target.Name, target.UID, ""), "can only be shared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := []*corev1.Pod{tt.pod}
			if tt.source != nil {
				pods = append(pods, tt.source)
			}
			ctrl := newTestDRAController(t, subnets, []*resourcev1.ResourceClaim{shared()}, slices, pods...)
			nets, err := ctrl.getPodDRANets(tt.pod)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, nets, 1)
		})
	}
}

func TestCheckDRAInterfaceNames(t *testing.T) {
	dra := func(iface string) []*kubeovnNet {
		return []*kubeovnNet{{ProviderName: testDRABlueProvider, InterfaceName: iface, IsDRA: true}}
	}
	podWith := func(networks string) *corev1.Pod {
		pod := &corev1.Pod{Name: testDRAPod, Namespace: metav1.NamespaceDefault}
		if networks != "" {
			pod.Annotations = map[string]string{nadv1.NetworkAttachmentAnnot: networks}
		}
		return pod
	}
	tests := []struct {
		name     string
		networks string
		nets     []*kubeovnNet
		wantErr  bool
	}{
		{"no attachments", "", dra(""), false},
		{"primary interface", "", dra(testDRAPrimaryIface), true},
		{"default name of the first attachment", testDRAAttachA, dra(""), true},
		{"default name of the second attachment", testDRAAttachAB, dra(testDRAIface2), true},
		{"name requested by an attachment", fmt.Sprintf(`[{"name":%q,"interface":%q}]`, testDRAAttachA, testDRAVlanIface), dra(testDRAVlanIface), true},
		{"attachment renamed away from net1", fmt.Sprintf(`[{"name":%q,"interface":%q}]`, testDRAAttachA, testDRAVlanIface), dra(""), false},
		{"distinct names", testDRAAttachAB, dra(testDRAIface3), false},
		{"no dra nics", testDRAAttachA, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDRAInterfaceNames(podWith(tt.networks), tt.nets)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEnqueueSlicePods(t *testing.T) {
	claim := testDRAClaim(testDRAClaimName, testDRAPodA, testDRAUIDA, testDRAResult(testDRADriver, testDRABlueDevice))
	other := testDRAClaim(testDRAOtherClaim, testDRAPodB, testDRAUIDB, resourcev1.DeviceRequestAllocationResult{Request: testDRARequest, Driver: testDRADriver, Pool: testDRAOtherPool, Device: testDRABlueDevice})
	ctrl := newTestDRAController(t, nil, []*resourcev1.ResourceClaim{claim, other}, nil)
	ctrl.addOrUpdatePodQueue = newTypedRateLimitingQueue[string](testDRAQueueName, nil)
	t.Cleanup(ctrl.addOrUpdatePodQueue.ShutDown)

	slice := testDRASlice(testDRASlice1, 1, testDRADevice(testDRABlueDevice, testDRABlueSubnet))
	ctrl.enqueueAddResourceSlice(slice)
	require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len(), "only the pods of claims from the slice's pool")
	key, _ := ctrl.addOrUpdatePodQueue.Get()
	require.Equal(t, cache.NewObjectName(metav1.NamespaceDefault, testDRAPodA).String(), key)
	ctrl.addOrUpdatePodQueue.Done(key)

	// A capacity update, which the driver publishes on every address allocation, is ignored.
	withCapacity := slice.DeepCopy()
	withCapacity.Spec.Devices[0].Capacity = map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{testDRACapacityIPv4: {Value: resource.MustParse(testDRACapacityValue)}}
	ctrl.enqueueUpdateResourceSlice(slice, withCapacity)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len())

	newGeneration := slice.DeepCopy()
	newGeneration.Spec.Pool.Generation = 2
	ctrl.enqueueUpdateResourceSlice(slice, newGeneration)
	require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len())
	key, _ = ctrl.addOrUpdatePodQueue.Get()
	ctrl.addOrUpdatePodQueue.Done(key)

	ctrl.enqueueDeleteResourceSlice(cache.DeletedFinalStateUnknown{Key: testDRASlice1, Obj: slice})
	require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len())
	key, _ = ctrl.addOrUpdatePodQueue.Get()
	ctrl.addOrUpdatePodQueue.Done(key)

	foreign := slice.DeepCopy()
	foreign.Spec.Driver = testDRAOtherDriver
	ctrl.enqueueAddResourceSlice(foreign)
	require.Equal(t, 0, ctrl.addOrUpdatePodQueue.Len(), "slices of other drivers are ignored")
}

// recordedClaim returns the claim as the API server has it, with the recorded nics.
func recordedClaim(t *testing.T, ctrl *Controller, name string) map[string]draNicRecord {
	t.Helper()
	claim, err := ctrl.config.KubeClient.ResourceV1().ResourceClaims(metav1.NamespaceDefault).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	records, err := draNicRecords(claim)
	require.NoError(t, err)
	return records
}

// withRecords sets the recorded nics of a claim, as the controller writes them.
func withRecords(t *testing.T, claim *resourcev1.ResourceClaim, records map[string]draNicRecord) *resourcev1.ResourceClaim {
	t.Helper()
	value, err := json.Marshal(records)
	require.NoError(t, err)
	claim.Annotations = map[string]string{util.DRAProvidersAnnotation: string(value)}
	return claim
}

// A nic keeps the subnet and provider it was allocated with: the first resolution records them
// on the claim, and a later change of the subnet's provider does not move the nic.
func TestGetPodDRANetsRecordsProviders(t *testing.T) {
	pod := &corev1.Pod{Name: testDRAPod, Namespace: metav1.NamespaceDefault, UID: testDRAPodUID}
	blue := &kubeovnv1.Subnet{Name: testDRABlueSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: testDRABlueProvider}}
	moved := &kubeovnv1.Subnet{Name: testDRABlueSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: testDRARedProvider}}
	slices := []*resourcev1.ResourceSlice{testDRASlice(testDRASlice1, 1, testDRADevice(testDRABlueDevice, testDRABlueSubnet))}
	recordOfBlue := map[string]draNicRecord{
		draNicKey(testDRAResult(testDRADriver, testDRABlueDevice)):    {Subnet: testDRABlueSubnet, Provider: testDRABlueProvider},
		draNicKey(testDRAResult(testDRADriver, testDRAMissingDevice)): {Subnet: testDRABlueSubnet, Provider: testDRABlueProvider},
	}

	t.Run("records the nics on the first resolution", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))}
		ctrl := newTestDRAController(t, []*kubeovnv1.Subnet{blue}, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRABlueProvider, nets[0].ProviderName)
		require.Equal(t, map[string]draNicRecord{
			draNicKey(testDRAResult(testDRADriver, testDRABlueDevice)): {Subnet: testDRABlueSubnet, Provider: testDRABlueProvider},
		}, recordedClaim(t, ctrl, testDRAClaimName))
	})

	t.Run("keeps the recorded provider after the subnet's provider changed", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{withRecords(t, testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), recordOfBlue)}
		ctrl := newTestDRAController(t, []*kubeovnv1.Subnet{moved}, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRABlueProvider, nets[0].ProviderName)
		require.Equal(t, testDRABlueSubnet, nets[0].Subnet.Name)
	})

	t.Run("keeps the recorded subnet when the device is not published anymore", func(t *testing.T) {
		claims := []*resourcev1.ResourceClaim{withRecords(t, testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRAMissingDevice)), recordOfBlue)}
		ctrl := newTestDRAController(t, []*kubeovnv1.Subnet{blue}, claims, slices)

		nets, err := ctrl.getPodDRANets(pod)
		require.NoError(t, err)
		require.Len(t, nets, 1)
		require.Equal(t, testDRABlueProvider, nets[0].ProviderName)
	})

	t.Run("a deleting pod records nothing", func(t *testing.T) {
		deleting := pod.DeepCopy()
		deleting.DeletionTimestamp = new(metav1.Now())
		claims := []*resourcev1.ResourceClaim{testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))}
		ctrl := newTestDRAController(t, []*kubeovnv1.Subnet{blue}, claims, slices)

		_, err := ctrl.getPodDRANets(deleting)
		require.NoError(t, err)
		require.Empty(t, recordedClaim(t, ctrl, testDRAClaimName))
	})

	t.Run("a deleting pod whose recorded subnet is gone is skipped", func(t *testing.T) {
		deleting := pod.DeepCopy()
		deleting.DeletionTimestamp = new(metav1.Now())
		claims := []*resourcev1.ResourceClaim{withRecords(t, testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice)), recordOfBlue)}
		ctrl := newTestDRAController(t, nil, claims, slices)

		nets, err := ctrl.getPodDRANets(deleting)
		require.NoError(t, err)
		require.Empty(t, nets)
	})

	t.Run("rejects an invalid record", func(t *testing.T) {
		claim := testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))
		claim.Annotations = map[string]string{util.DRAProvidersAnnotation: testDRAInvalidRecord}
		ctrl := newTestDRAController(t, []*kubeovnv1.Subnet{blue}, []*resourcev1.ResourceClaim{claim}, slices)

		_, err := ctrl.getPodDRANets(pod)
		require.ErrorContains(t, err, util.DRAProvidersAnnotation)
	})
}

// A nic whose device or subnet cannot be resolved fails the pod, except while the pod is deleted:
// then its device or subnet may be gone already, and gc cleans its ip cr.
func TestGetPodDRANetsUnresolvable(t *testing.T) {
	pod := &corev1.Pod{Name: testDRAPod, Namespace: metav1.NamespaceDefault, UID: testDRAPodUID}
	deleting := pod.DeepCopy()
	deleting.DeletionTimestamp = new(metav1.Now())
	blue := []*kubeovnv1.Subnet{{Name: testDRABlueSubnet, Spec: kubeovnv1.SubnetSpec{CIDRBlock: testDRABlueCIDR, Provider: testDRABlueProvider}}}
	published := []*resourcev1.ResourceSlice{testDRASlice(testDRASlice1, 1, testDRADevice(testDRABlueDevice, testDRABlueSubnet))}
	withoutSubnet := []*resourcev1.ResourceSlice{testDRASlice(testDRASlice1, 1, resourcev1.Device{Name: testDRABlueDevice})}
	unqualified := []*resourcev1.ResourceSlice{testDRASlice(testDRASlice1, 1, resourcev1.Device{
		Name:       testDRABlueDevice,
		Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{draAttrSubnetName: {StringValue: new(testDRABlueSubnet)}},
	})}
	recordOfBlue := map[string]draNicRecord{
		draNicKey(testDRAResult(testDRADriver, testDRABlueDevice)): {Subnet: testDRABlueSubnet, Provider: testDRABlueProvider},
	}

	tests := []struct {
		name    string
		pod     *corev1.Pod
		subnets []*kubeovnv1.Subnet
		slices  []*resourcev1.ResourceSlice
		records map[string]draNicRecord
		// notInAPI leaves the claim out of the API server, so recording its nics fails
		notInAPI bool
		nets     int
		err      string
	}{
		{name: "a deleting pod whose device is not published anymore is skipped", pod: deleting, subnets: blue},
		{name: "a deleting pod whose subnet is gone is skipped", pod: deleting, slices: published},
		{name: "fails while the subnet of the device does not exist", pod: pod, slices: published, err: "not found"},
		{name: "fails while the recorded subnet does not exist", pod: pod, slices: published, records: recordOfBlue, err: "not found"},
		{name: "rejects a device without a subnet name", pod: pod, subnets: blue, slices: withoutSubnet, err: "has no " + draAttrSubnetName + " attribute"},
		{name: "reads a subnet name attribute without the driver prefix", pod: pod, subnets: blue, slices: unqualified, nets: 1},
		{name: "fails when the nics cannot be recorded on the claim", pod: pod, subnets: blue, slices: published, notInAPI: true, err: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claim := testDRAClaim(testDRAClaimName, pod.Name, pod.UID, testDRAResult(testDRADriver, testDRABlueDevice))
			if tt.records != nil {
				withRecords(t, claim, tt.records)
			}
			ctrl := newTestDRAController(t, tt.subnets, []*resourcev1.ResourceClaim{claim}, tt.slices)
			if tt.notInAPI {
				require.NoError(t, ctrl.config.KubeClient.ResourceV1().ResourceClaims(claim.Namespace).Delete(context.Background(), claim.Name, metav1.DeleteOptions{}))
			}

			nets, err := ctrl.getPodDRANets(tt.pod)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Len(t, nets, tt.nets)
		})
	}
}
