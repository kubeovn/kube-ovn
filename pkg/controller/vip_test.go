package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
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
