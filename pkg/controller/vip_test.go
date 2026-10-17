package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVirtualVipPortsSplitsDualStackAddresses(t *testing.T) {
	vip := &kubeovnv1.Vip{
		Name: "keepalived-vip",
		Status: kubeovnv1.VipStatus{
			V4ip: "192.168.255.100",
			V6ip: "2001:db8:1203:9::f",
		},
	}

	ports := virtualVipPorts(vip)
	require.Equal(t, []virtualVipPort{
		{name: "vip:keepalived-vip:ipv4", ip: "192.168.255.100"},
		{name: "vip:keepalived-vip:ipv6", ip: "2001:db8:1203:9::f"},
	}, ports)
}

func TestVirtualVipPortsUsesAddressFamilyNames(t *testing.T) {
	vip := &kubeovnv1.Vip{
		Name:   "foo",
		Status: kubeovnv1.VipStatus{V4ip: "192.168.255.100", V6ip: "2001:db8::f"},
	}

	ports := virtualVipPorts(vip)
	require.Equal(t, "vip:foo:ipv6", ports[1].name)
}

func TestVirtualVipPortsUsesAddressFamilyNameForIPv6Only(t *testing.T) {
	vip := &kubeovnv1.Vip{
		Name: "keepalived-vip",
		Status: kubeovnv1.VipStatus{
			V6ip: "2001:db8:1203:9::f",
		},
	}

	require.Equal(t, []virtualVipPort{
		{name: "vip:keepalived-vip:ipv6", ip: "2001:db8:1203:9::f"},
	}, virtualVipPorts(vip))
}

func TestDeleteVirtualVipPortsFindsSecondaryPortWithoutIPStatus(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	vip := &kubeovnv1.Vip{Name: "foo", Spec: kubeovnv1.VipSpec{Subnet: "public-subnet"}}

	fakeController.mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: vip.Spec.Subnet}, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{
		{Name: vip.Name, Type: "virtual"},
		{Name: "vip:foo:ipv6", Type: "virtual"},
	}, nil)
	fakeController.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(vip.Name).Return(nil)
	fakeController.mockOvnClient.EXPECT().DeleteLogicalSwitchPort("vip:foo:ipv6").Return(nil)

	require.NoError(t, fakeController.fakeController.deleteVirtualVipPorts(vip))
}

func TestEnqueueUpdateVirtualIPOnStatusChange(t *testing.T) {
	queue := newTypedRateLimitingQueue[string]("UpdateVirtualParents", nil)
	defer queue.ShutDown()

	ctrl := &Controller{updateVirtualParentsQueue: queue}
	oldVip := &kubeovnv1.Vip{Name: "keepalived-vip"}
	newVip := oldVip.DeepCopy()
	newVip.Status.V4ip = "192.168.255.100"

	ctrl.enqueueUpdateVirtualIP(oldVip, newVip)
	key, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, "keepalived-vip", key)
	queue.Done(key)
}

func newVipParentsTestController(t *testing.T, subnet *kubeovnv1.Subnet, vip *kubeovnv1.Vip, pods ...*corev1.Pod) *fakeController {
	t.Helper()
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Pods:    pods,
	})
	require.NoError(t, err)
	fakeController.fakeController.updatePodSecurityQueue = newTypedRateLimitingQueue[string]("UpdatePodSecurity", nil)
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, vipIndexer.Add(vip))
	fakeController.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)
	return fakeController
}

func distributedAAPSubnet(name string) *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{
		Name: name,
		Spec: kubeovnv1.SubnetSpec{
			Provider:    util.OvnProvider,
			Vpc:         util.DefaultVpc,
			GatewayType: kubeovnv1.GWDistributedType,
		},
	}
}

func keepalivedAAPPod(subnetName, vipName, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		Name:      "keepalived-0",
		Namespace: metav1.NamespaceDefault,
		Labels:    map[string]string{"app": "keepalived"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.AAPsAnnotation:          vipName,
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
	}
}

func TestHandleUpdateVirtualParentsSyncsDistributedVipPortGroup(t *testing.T) {
	const (
		subnetName = "public-subnet"
		vipName    = "keepalived-vip"
	)

	subnet := distributedAAPSubnet(subnetName)
	vip := &kubeovnv1.Vip{
		Name: vipName,
		Spec: kubeovnv1.VipSpec{
			Namespace: metav1.NamespaceDefault,
			Subnet:    subnetName,
			Selector:  []string{"app: keepalived"},
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "192.168.255.100",
			Mac:  "2e:a5:9b:20:42:d2",
		},
	}
	fakeController := newVipParentsTestController(t, subnet, vip, keepalivedAAPPod(subnetName, vipName, "node-1"))
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	primaryPort := "vip:" + vipName + ":ipv4"
	mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(primaryPort, subnetName, vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortAddresses(primaryPort, vip.Status.Mac+" "+vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortVirtualParents(primaryPort, "keepalived-0.default").Return(nil)
	mockOvnClient.EXPECT().ListPortGroups(map[string]string{
		"subnet":         subnetName,
		"node":           "",
		networkPolicyKey: "",
	}).Return([]ovnnb.PortGroup{
		{Name: "public.subnet.node.1", ExternalIDs: map[string]string{"node": "node-1"}},
		{Name: "public.subnet.node.2", ExternalIDs: map[string]string{"node": "node-2"}},
	}, nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.node.1", primaryPort).Return(nil)
	mockOvnClient.EXPECT().RemovePortFromPortGroups(primaryPort, "public.subnet.node.2").Return(nil)

	require.NoError(t, ctrl.handleUpdateVirtualParents(vipName))
}

func TestHandleUpdateVirtualParentsSplitsDualStackVipPorts(t *testing.T) {
	const (
		subnetName = "public-subnet-dual"
		vipName    = "keepalived-vip-dual"
	)

	subnet := distributedAAPSubnet(subnetName)
	vip := &kubeovnv1.Vip{
		Name: vipName,
		Spec: kubeovnv1.VipSpec{
			Namespace: metav1.NamespaceDefault,
			Subnet:    subnetName,
			Selector:  []string{"app: keepalived"},
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "192.168.255.100",
			V6ip: "2001:db8:1203:9::f",
			Mac:  "2e:a5:9b:20:42:d2",
		},
	}
	fakeController := newVipParentsTestController(t, subnet, vip, keepalivedAAPPod(subnetName, vipName, "node-1"))
	ctrl := fakeController.fakeController
	primaryPort := "vip:" + vipName + ":ipv4"
	secondaryPort := "vip:" + vipName + ":ipv6"
	mockOvnClient := fakeController.mockOvnClient
	mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(primaryPort, subnetName, "192.168.255.100").Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortAddresses(primaryPort, "2e:a5:9b:20:42:d2 192.168.255.100").Return(nil)
	mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(secondaryPort, subnetName, "2001:db8:1203:9::f").Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortAddresses(secondaryPort, "2e:a5:9b:20:42:d2 2001:db8:1203:9::f").Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortVirtualParents(primaryPort, "keepalived-0.default").Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortVirtualParents(secondaryPort, "keepalived-0.default").Return(nil)
	mockOvnClient.EXPECT().ListPortGroups(map[string]string{
		"subnet":         subnetName,
		"node":           "",
		networkPolicyKey: "",
	}).Return([]ovnnb.PortGroup{
		{Name: "public.subnet.dual.node.1", ExternalIDs: map[string]string{"node": "node-1"}},
		{Name: "public.subnet.dual.node.2", ExternalIDs: map[string]string{"node": "node-2"}},
	}, nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.dual.node.1", primaryPort).Return(nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.dual.node.1", secondaryPort).Return(nil)
	mockOvnClient.EXPECT().RemovePortFromPortGroups(primaryPort, "public.subnet.dual.node.2").Return(nil)
	mockOvnClient.EXPECT().RemovePortFromPortGroups(secondaryPort, "public.subnet.dual.node.2").Return(nil)

	require.NoError(t, ctrl.handleUpdateVirtualParents(vipName))
}

func TestHandleUpdateVirtualParentsRemovesStalePortGroups(t *testing.T) {
	const (
		subnetName = "public-subnet"
		vipName    = "keepalived-vip"
	)

	subnet := distributedAAPSubnet(subnetName)
	vip := &kubeovnv1.Vip{
		Name: vipName,
		Spec: kubeovnv1.VipSpec{
			Namespace: metav1.NamespaceDefault,
			Subnet:    subnetName,
			Selector:  []string{"app: keepalived"},
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "192.168.255.100",
			Mac:  "2e:a5:9b:20:42:d2",
		},
	}
	fakeController := newVipParentsTestController(t, subnet, vip)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	primaryPort := "vip:" + vipName + ":ipv4"
	mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(primaryPort, subnetName, vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortAddresses(primaryPort, vip.Status.Mac+" "+vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortVirtualParents(primaryPort, "").Return(nil)
	mockOvnClient.EXPECT().ListPortGroups(map[string]string{
		"subnet":         subnetName,
		"node":           "",
		networkPolicyKey: "",
	}).Return([]ovnnb.PortGroup{
		{Name: "public.subnet.node.1", ExternalIDs: map[string]string{"node": "node-1"}},
		{Name: "public.subnet.node.2", ExternalIDs: map[string]string{"node": "node-2"}},
	}, nil)
	mockOvnClient.EXPECT().RemovePortFromPortGroups(primaryPort, "public.subnet.node.1", "public.subnet.node.2").Return(nil)

	require.NoError(t, ctrl.handleUpdateVirtualParents(vipName))
}

func TestHandleUpdateVirtualParentsSkipsAddressesWithoutMAC(t *testing.T) {
	const (
		subnetName = "public-subnet"
		vipName    = "keepalived-vip"
	)

	subnet := distributedAAPSubnet(subnetName)
	vip := &kubeovnv1.Vip{
		Name: vipName,
		Spec: kubeovnv1.VipSpec{
			Namespace: metav1.NamespaceDefault,
			Subnet:    subnetName,
			Selector:  []string{"app: keepalived"},
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "192.168.255.100",
		},
	}
	fakeController := newVipParentsTestController(t, subnet, vip, keepalivedAAPPod(subnetName, vipName, "node-1"))
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	primaryPort := "vip:" + vipName + ":ipv4"
	mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(primaryPort, subnetName, vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortVirtualParents(primaryPort, "keepalived-0.default").Return(nil)
	mockOvnClient.EXPECT().ListPortGroups(map[string]string{
		"subnet":         subnetName,
		"node":           "",
		networkPolicyKey: "",
	}).Return([]ovnnb.PortGroup{
		{Name: "public.subnet.node.1", ExternalIDs: map[string]string{"node": "node-1"}},
	}, nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.node.1", primaryPort).Return(nil)

	require.NoError(t, ctrl.handleUpdateVirtualParents(vipName))
}

func TestHandleAddVirtualIP_SwitchLBRuleCreatesNoLsp(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "test-subnet",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24",
			Gateway:   "10.0.0.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       "test-vpc",
		},
	}
	vip := &kubeovnv1.Vip{
		Name: "test-switch-lb-vip",
		Spec: kubeovnv1.VipSpec{
			Namespace: "default",
			Subnet:    subnet.Name,
			Type:      util.SwitchLBRuleVip,
			V4ip:      "10.0.0.50",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    []*kubeovnv1.Vip{vip},
		Vpcs:    []*kubeovnv1.Vpc{{Name: subnet.Spec.Vpc}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	const gatewayMac = "00:00:00:00:00:01"
	require.NoError(t, ctrl.ipam.RecordGatewayMAC(subnet.Name, gatewayMac))

	// No OVN call is expected: the vip gets no lsp and its IPAM-assigned mac is only
	// recorded in the CR. The subnet router port answers ARP for it, see
	// syncSwitchLBVipArpProxy.
	require.NoError(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, got.Status.Mac)
	require.NotEqual(t, gatewayMac, got.Status.Mac)
}

// TestHandleAddVirtualIP_SwitchLBRuleRepairsStaleGatewayMac covers vips created before
// the own-mac fix: their Status.Mac and lsp mac were left equal to the subnet gateway
// mac. On the next reconcile they must be repaired with a freshly allocated mac,
// keeping the same IP.
func TestHandleAddVirtualIP_SwitchLBRuleRepairsStaleGatewayMac(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "test-subnet-repair",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.1.0/24",
			Gateway:   "10.0.1.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       "test-vpc",
		},
	}
	const gatewayMac = "00:00:00:00:00:02"
	vip := &kubeovnv1.Vip{
		Name: "test-switch-lb-vip-repair",
		Spec: kubeovnv1.VipSpec{
			Namespace: "default",
			Subnet:    subnet.Name,
			Type:      util.SwitchLBRuleVip,
			V4ip:      "10.0.1.50",
			// A real pre-fix vip has Spec.MacAddress set to the gateway mac too, since
			// the old createOrUpdateVipCR wrote it there; the repair must not let this
			// stale value leak back into the reallocation.
			MacAddress: gatewayMac,
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "10.0.1.50",
			Mac:  gatewayMac,
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    []*kubeovnv1.Vip{vip},
		Vpcs:    []*kubeovnv1.Vpc{{Name: subnet.Spec.Vpc}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	// Simulate the pre-fix state: on a controller restart, init.go re-registers the vip's
	// nic in ipam using its (stale) Status.Mac before the subnet controller has recorded
	// the real gateway mac, so the registration itself does not see a conflict.
	_, _, _, err = ctrl.ipam.GetStaticAddress(vip.Name, portName, vip.Status.V4ip, &vip.Status.Mac, subnet.Name, false)
	require.NoError(t, err)
	require.NoError(t, ctrl.ipam.RecordGatewayMAC(subnet.Name, gatewayMac))

	require.NoError(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, vip.Status.V4ip, got.Status.V4ip)
	require.NotEqual(t, gatewayMac, got.Status.Mac)
	// Virtual parent (re)creation for the repaired vip is deferred to the dedicated queue.
	require.Equal(t, 1, ctrl.updateVirtualParentsQueue.Len())
}

// TestHandleAddVirtualIP_SwitchLBRuleRepairRetriesUntilGatewayMacKnown covers a
// controller restart where this vip's queue entry is processed before subnet
// reconciliation has called RecordGatewayMAC. The repair must not be silently
// skipped (RecordGatewayMAC never re-enqueues vips), so it must return an error
// so the caller retries.
func TestHandleAddVirtualIP_SwitchLBRuleRepairRetriesUntilGatewayMacKnown(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "test-subnet-repair-no-gw-mac",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.2.0/24",
			Gateway:   "10.0.2.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       "test-vpc",
		},
	}
	const staleMac = "00:00:00:00:00:03"
	vip := &kubeovnv1.Vip{
		Name: "test-switch-lb-vip-repair-no-gw-mac",
		Spec: kubeovnv1.VipSpec{
			Namespace:  "default",
			Subnet:     subnet.Name,
			Type:       util.SwitchLBRuleVip,
			V4ip:       "10.0.2.50",
			MacAddress: staleMac,
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "10.0.2.50",
			Mac:  staleMac,
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    []*kubeovnv1.Vip{vip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	// Deliberately do not call RecordGatewayMAC, simulating subnet reconciliation
	// not having run yet.

	require.Error(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, staleMac, got.Status.Mac)
}

func TestHandleAddVirtualIP_SwitchLBRuleRepairNotSilentlySkippedWhenIPAMSubnetUnregistered(t *testing.T) {
	t.Parallel()

	const staleMac = "00:00:00:00:00:04"
	subnet := &kubeovnv1.Subnet{
		Name: "test-subnet-repair-ipam-unregistered",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.3.0/24",
			Gateway:   "10.0.3.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       "test-vpc",
		},
		Status: kubeovnv1.SubnetStatus{GatewayMAC: staleMac},
	}
	vip := &kubeovnv1.Vip{
		Name: "test-switch-lb-vip-repair-ipam-unregistered",
		Spec: kubeovnv1.VipSpec{
			Namespace:  "default",
			Subnet:     subnet.Name,
			Type:       util.SwitchLBRuleVip,
			V4ip:       "10.0.3.50",
			MacAddress: staleMac,
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "10.0.3.50",
			Mac:  staleMac,
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    []*kubeovnv1.Vip{vip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	// no ctrl.ipam.AddOrUpdateSubnet call: ipam hasn't registered this subnet yet.

	require.Error(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, staleMac, got.Status.Mac)
}

func newSwitchLBVipTestController(t *testing.T, subnet *kubeovnv1.Subnet, vips ...*kubeovnv1.Vip) *fakeController {
	t.Helper()
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, vip := range vips {
		require.NoError(t, vipIndexer.Add(vip))
	}
	fakeController.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)
	return fakeController
}

func switchLBVip(name, subnet, v4ip, v6ip string) *kubeovnv1.Vip {
	return &kubeovnv1.Vip{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kubeovnv1.VipSpec{Namespace: "ns", Subnet: subnet, Type: util.SwitchLBRuleVip},
		Status:     kubeovnv1.VipStatus{V4ip: v4ip, V6ip: v6ip, Mac: "00:00:00:00:00:01"},
	}
}

func TestSyncSwitchLBVipArpProxyCollectsSwitchLBVipsOfSubnet(t *testing.T) {
	const subnetName = "foo-subnet"
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: subnetName},
		Spec:       kubeovnv1.SubnetSpec{Vpc: "foo-vpc"},
	}
	deleting := switchLBVip("deleting", subnetName, "10.0.1.9", "")
	deleting.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	otherType := switchLBVip("other-type", subnetName, "10.0.1.8", "")
	otherType.Spec.Type = ""

	fakeController := newSwitchLBVipTestController(t, subnet,
		switchLBVip("b", subnetName, "10.0.1.3", "fd00::3"),
		switchLBVip("a", subnetName, "10.0.1.2", ""),
		switchLBVip("other-subnet", "bar-subnet", "10.0.2.2", ""),
		deleting,
		otherType,
	)
	fakeController.mockOvnClient.EXPECT().
		SetLogicalSwitchPortArpProxy("foo-subnet-foo-vpc", []string{"10.0.1.2", "10.0.1.3", "fd00::3"}).
		Return(nil)

	require.NoError(t, fakeController.fakeController.syncSwitchLBVipArpProxy(subnetName))
}

func TestSyncSwitchLBVipArpProxyClearsOptionWithoutVips(t *testing.T) {
	const subnetName = "foo-subnet"
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: subnetName},
		Spec:       kubeovnv1.SubnetSpec{Vpc: "foo-vpc"},
	}
	fakeController := newSwitchLBVipTestController(t, subnet)
	mockOvnClient := fakeController.mockOvnClient

	t.Run("router port exists", func(t *testing.T) {
		mockOvnClient.EXPECT().LogicalSwitchPortExists("foo-subnet-foo-vpc").Return(true, nil)
		mockOvnClient.EXPECT().SetLogicalSwitchPortArpProxy("foo-subnet-foo-vpc", []string(nil)).Return(nil)
		require.NoError(t, fakeController.fakeController.syncSwitchLBVipArpProxy(subnetName))
	})

	t.Run("router port does not exist", func(t *testing.T) {
		mockOvnClient.EXPECT().LogicalSwitchPortExists("foo-subnet-foo-vpc").Return(false, nil)
		require.NoError(t, fakeController.fakeController.syncSwitchLBVipArpProxy(subnetName))
	})
}

func TestReconcileSwitchLBVipPortSetsProxyBeforeDeletingStalePort(t *testing.T) {
	const subnetName = "foo-subnet"
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: subnetName},
		Spec:       kubeovnv1.SubnetSpec{Vpc: "foo-vpc"},
	}
	vip := switchLBVip("a", subnetName, "10.0.1.2", "")
	fakeController := newSwitchLBVipTestController(t, subnet, vip)
	mockOvnClient := fakeController.mockOvnClient

	gomock.InOrder(
		mockOvnClient.EXPECT().SetLogicalSwitchPortArpProxy("foo-subnet-foo-vpc", []string{"10.0.1.2"}).Return(nil),
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)).Return(nil),
	)

	require.NoError(t, fakeController.fakeController.reconcileSwitchLBVipPort(vip))
}
