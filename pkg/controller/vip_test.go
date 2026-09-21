package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
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
		{name: "keepalived-vip", ip: "192.168.255.100"},
		{name: "keepalived-vip-vip-2001:db8:1203:9::f", ip: "2001:db8:1203:9::f"},
	}, ports)
}

func TestEnqueueUpdateVirtualIPOnStatusChange(t *testing.T) {
	queue := newTypedRateLimitingQueue[string]("UpdateVirtualParents", nil)
	defer queue.ShutDown()

	ctrl := &Controller{updateVirtualParentsQueue: queue}
	oldVip := &kubeovnv1.Vip{ObjectMeta: metav1.ObjectMeta{Name: "keepalived-vip"}}
	newVip := oldVip.DeepCopy()
	newVip.Status.V4ip = "192.168.255.100"

	ctrl.enqueueUpdateVirtualIP(oldVip, newVip)
	key, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, "keepalived-vip", key)
	queue.Done(key)
}

func TestHandleUpdateVirtualParentsSyncsDistributedVipPortGroup(t *testing.T) {
	const (
		subnetName = "public-subnet"
		vipName    = "keepalived-vip"
	)

	pod := &corev1.Pod{
		Name:      "keepalived-0",
		Namespace: metav1.NamespaceDefault,
		Labels:    map[string]string{"app": "keepalived"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.AAPsAnnotation:          vipName,
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
	}
	subnet := &kubeovnv1.Subnet{
		Name: subnetName,
		Spec: kubeovnv1.SubnetSpec{
			Provider:    util.OvnProvider,
			Vpc:         util.DefaultVpc,
			GatewayType: kubeovnv1.GWDistributedType,
		},
	}
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

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Pods:    []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.updatePodSecurityQueue = newTypedRateLimitingQueue[string]("UpdatePodSecurity", nil)
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, vipIndexer.Add(vip))
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)

	mockOvnClient := fakeController.mockOvnClient
	mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(vipName, subnetName, vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortAddresses(vipName, vip.Status.Mac+" "+vip.Status.V4ip).Return(nil)
	mockOvnClient.EXPECT().SetVirtualLogicalSwitchPortVirtualParents(vipName, "keepalived-0.default").Return(nil)
	mockOvnClient.EXPECT().ListPortGroups(map[string]string{
		"subnet":         subnetName,
		"node":           "",
		networkPolicyKey: "",
	}).Return([]ovnnb.PortGroup{
		{Name: "public.subnet.node.1", ExternalIDs: map[string]string{"node": "node-1"}},
		{Name: "public.subnet.node.2", ExternalIDs: map[string]string{"node": "node-2"}},
	}, nil)
	mockOvnClient.EXPECT().RemovePortFromPortGroups(vipName, "public.subnet.node.1", "public.subnet.node.2").Return(nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.node.1", vipName).Return(nil)

	require.NoError(t, ctrl.handleUpdateVirtualParents(vipName))
}

func TestHandleUpdateVirtualParentsSplitsDualStackVipPorts(t *testing.T) {
	const subnetName = "public-subnet-dual"
	const vipName = "keepalived-vip-dual"

	pod := &corev1.Pod{
		Name:      "keepalived-0",
		Namespace: metav1.NamespaceDefault,
		Labels:    map[string]string{"app": "keepalived"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.AAPsAnnotation:          vipName,
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
	}
	subnet := &kubeovnv1.Subnet{
		Name: subnetName,
		Spec: kubeovnv1.SubnetSpec{
			Provider:    util.OvnProvider,
			Vpc:         util.DefaultVpc,
			GatewayType: kubeovnv1.GWDistributedType,
		},
	}
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

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Pods:    []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.updatePodSecurityQueue = newTypedRateLimitingQueue[string]("UpdatePodSecurity", nil)
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, vipIndexer.Add(vip))
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)

	primaryPort := vipName
	secondaryPort := vipName + "-vip-2001:db8:1203:9::f"
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
	mockOvnClient.EXPECT().RemovePortFromPortGroups(primaryPort, "public.subnet.dual.node.1", "public.subnet.dual.node.2").Return(nil)
	mockOvnClient.EXPECT().RemovePortFromPortGroups(secondaryPort, "public.subnet.dual.node.1", "public.subnet.dual.node.2").Return(nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.dual.node.1", primaryPort).Return(nil)
	mockOvnClient.EXPECT().PortGroupAddPorts("public.subnet.dual.node.1", secondaryPort).Return(nil)

	require.NoError(t, ctrl.handleUpdateVirtualParents(vipName))
}
