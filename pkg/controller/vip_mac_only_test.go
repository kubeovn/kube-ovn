package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Tests for a kube_host_vm_vip Vip with only macAddress set (issue #7703): acquireVipAddress
// used to route any non-empty macAddress into acquireStaticIPAddress, which requires a real
// ip string and loops forever on "failed to parse vip ip" when none is given. The fix adds
// an explicit "mac pinned, ip random" branch via acquireIPAddressWithMAC, which must behave
// exactly like a fully random vip otherwise: dual-stack, idempotent on reconcile, retrying
// past a node-IP collision, and rejecting a mac already in use.

func macOnlyV4Subnet() *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{
		Name: "subnet-3",
		Spec: kubeovnv1.SubnetSpec{
			Provider:  "subnet-3.tenant.ovn",
			Vpc:       "vpc-3",
			CIDRBlock: "10.0.0.0/24",
			Gateway:   "10.0.0.2",
			Protocol:  kubeovnv1.ProtocolIPv4,
		},
	}
}

func macOnlyDualStackSubnet() *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{
		Name: "subnet-dual",
		Spec: kubeovnv1.SubnetSpec{
			Provider:  "subnet-dual.tenant.ovn",
			Vpc:       "vpc-3",
			CIDRBlock: "10.0.0.0/24,fd00:10:0::/64",
			Gateway:   "10.0.0.2,fd00:10:0::2",
			Protocol:  kubeovnv1.ProtocolDual,
		},
	}
}

func macOnlyVip(subnetName, mac string) *kubeovnv1.Vip {
	return &kubeovnv1.Vip{
		ObjectMeta: metav1.ObjectMeta{Name: "dn5-net2", Labels: map[string]string{util.SubnetNameLabel: subnetName}},
		Spec: kubeovnv1.VipSpec{
			Namespace:  "tenant",
			Subnet:     subnetName,
			Type:       util.KubeHostVMVip,
			MacAddress: mac,
		},
	}
}

func TestAcquireVipAddressMacOnlyAllocatesRandomIPv4(t *testing.T) {
	subnet := macOnlyV4Subnet()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	// validatePodIP (now on this path, see acquireIPAddressWithMAC) needs the subnet in
	// the lister above; ipam itself needs it seeded separately here.
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	vip := macOnlyVip(subnet.Name, "02:ac:10:ff:01:02")
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	v4ip, v6ip, mac, err := fc.fakeController.acquireVipAddress(vip, subnet, portName, vip.Spec.MacAddress)
	require.NoError(t, err)
	require.NotEmpty(t, v4ip, "a free v4 address must be allocated")
	require.Empty(t, v6ip, "v4-only subnet must not produce a v6 address")
	require.Equal(t, vip.Spec.MacAddress, mac, "the caller's mac must be kept, not randomized")

	// the address is really held in ipam under the vip's key: a second claimant must fail
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", v4ip, nil, subnet.Name, true)
	require.Error(t, err)
}

func TestAcquireVipAddressMacOnlyAllocatesDualStack(t *testing.T) {
	subnet := macOnlyDualStackSubnet()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	vip := macOnlyVip(subnet.Name, "02:ac:10:ff:01:03")
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	v4ip, v6ip, mac, err := fc.fakeController.acquireVipAddress(vip, subnet, portName, vip.Spec.MacAddress)
	require.NoError(t, err)
	require.NotEmpty(t, v4ip, "dual-stack subnet must allocate a v4 address")
	require.NotEmpty(t, v6ip, "dual-stack subnet must allocate a v6 address")
	require.Equal(t, vip.Spec.MacAddress, mac)

	// both halves are really reserved under the vip's key
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", v4ip, nil, subnet.Name, true)
	require.Error(t, err, "v4 half must be reserved")
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", v6ip, nil, subnet.Name, true)
	require.Error(t, err, "v6 half must be reserved")
}

func TestAcquireVipAddressMacOnlyIsIdempotentOnReconcile(t *testing.T) {
	// re-running acquireVipAddress for the same vip (e.g. a resync before status is
	// observed, or the add-path falling through again) must return the exact same
	// address and mac rather than allocating a second one or erroring on self-conflict.
	subnet := macOnlyV4Subnet()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	vip := macOnlyVip(subnet.Name, "02:ac:10:ff:01:04")
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	v4ip1, _, mac1, err := fc.fakeController.acquireVipAddress(vip, subnet, portName, vip.Spec.MacAddress)
	require.NoError(t, err)
	v4ip2, _, mac2, err := fc.fakeController.acquireVipAddress(vip, subnet, portName, vip.Spec.MacAddress)
	require.NoError(t, err)

	require.Equal(t, v4ip1, v4ip2, "a second acquire for the same nic must return the same ip")
	require.Equal(t, mac1, mac2)
	require.Equal(t, vip.Spec.MacAddress, mac1)
}

func TestAcquireVipAddressMacOnlyRejectsMacAlreadyInUse(t *testing.T) {
	subnet := macOnlyV4Subnet()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	taken := "02:ac:10:ff:01:05"

	first := macOnlyVip(subnet.Name, taken)
	firstPort := ovs.PodNameToPortName(first.Name, first.Spec.Namespace, subnet.Spec.Provider)
	_, _, _, err = fc.fakeController.acquireVipAddress(first, subnet, firstPort, first.Spec.MacAddress)
	require.NoError(t, err)

	second := macOnlyVip(subnet.Name, taken)
	second.Name = "dn6-net2"
	secondPort := ovs.PodNameToPortName(second.Name, second.Spec.Namespace, subnet.Spec.Provider)
	_, _, _, err = fc.fakeController.acquireVipAddress(second, subnet, secondPort, second.Spec.MacAddress)
	require.Error(t, err, "a mac already owned by another nic must not be silently handed out again")
}

func TestAcquireIPAddressWithMACRetriesPastNodeIPCollision(t *testing.T) {
	// validatePodIP rejects an allocated address that collides with a node's own internal
	// IP, same protection a fully random vip/pod already gets; acquireIPAddressWithMAC
	// must retry (skipping that address) while keeping the pinned mac, not fail or drop it.
	subnet := &kubeovnv1.Subnet{
		Name: "subnet-collide",
		Spec: kubeovnv1.SubnetSpec{
			Provider:   "subnet-collide.tenant.ovn",
			Vpc:        util.DefaultVpc, // must match c.config.ClusterRouter for validatePodIP to check nodes
			CIDRBlock:  "10.0.2.0/29",
			Gateway:    "10.0.2.1",
			Protocol:   kubeovnv1.ProtocolIPv4,
			ExcludeIps: []string{"10.0.2.1"},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.2.2"}},
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Nodes:   []*corev1.Node{node},
	})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps))

	mac := "02:ac:10:ff:01:06"
	v4ip, _, retMac, err := fc.fakeController.acquireIPAddressWithMAC(subnet.Name, "dn7-net2", "dn7-net2-port", &mac)
	require.NoError(t, err)
	require.NotEqual(t, node.Status.Addresses[0].Address, v4ip, "must not hand out the node's own IP")
	require.Equal(t, "10.0.2.3", v4ip, "lowest free address (.2) is the node's IP and must be skipped for the next one (.3)")
	require.Equal(t, mac, retMac, "the pinned mac must survive the retry unchanged")
}

func TestHandleAddVirtualIPMacOnlyCreatesLSPAndMirror(t *testing.T) {
	// end-to-end: the mac-only allocation path must integrate with everything else a
	// kube_host_vm_vip does today, LSP creation and the IP CR mirror (#7695/#7696) alike.
	subnet := macOnlyV4Subnet()
	vip := macOnlyVip(subnet.Name, "02:ac:10:ff:01:07")
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, nil)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(subnet.Name, portName, gomock.Any(), vip.Spec.MacAddress,
		vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc).Return(nil)

	require.NoError(t, fc.fakeController.handleAddVirtualIP(vip.Name))

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.Vips().Get(context.Background(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, got.Status.V4ip)
	require.Equal(t, vip.Spec.MacAddress, got.Status.Mac)

	mirror, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	require.NoError(t, err, "a mac-only kube_host_vm_vip must still get an IP CR mirror")
	require.Equal(t, got.Status.V4ip, mirror.Spec.V4IPAddress)
	require.Equal(t, vip.Spec.MacAddress, mirror.Spec.MacAddress)
}
