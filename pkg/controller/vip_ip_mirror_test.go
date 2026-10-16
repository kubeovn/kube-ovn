package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Tests for the IP CR mirror of a kube_host_vm_vip (issue #7695): such a vip allocates a
// real address and creates a real LSP, but on stock kube-ovn no IP CR exists for it, so it
// is invisible to `kubectl get ips` and unsafe to add by hand (gcIP deletes an orphan-looking
// IP CR at the next restart even while the vip is still live).

func vipMirrorSubnet() *kubeovnv1.Subnet {
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

func vipMirrorVip(vipType string) *kubeovnv1.Vip {
	return &kubeovnv1.Vip{
		ObjectMeta: metav1.ObjectMeta{Name: "dn3-net2", Labels: map[string]string{util.SubnetNameLabel: "subnet-3"}},
		Spec:       kubeovnv1.VipSpec{Namespace: "tenant", Subnet: "subnet-3", Type: vipType},
	}
}

func TestCheckIPOwnerExistsVipMirror(t *testing.T) {
	fc := newFakeController(t)
	subnet := vipMirrorSubnet()
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	vip := vipMirrorVip(util.KubeHostVMVip)
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)

	ip := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)},
		Spec:       kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip},
	}

	exists, err := fc.fakeController.checkIPOwnerExists(ip)
	require.NoError(t, err)
	require.False(t, exists, "a mirror ip without its vip must look orphaned to gcIP")

	require.NoError(t, vipIndexer.Add(vip))
	exists, err = fc.fakeController.checkIPOwnerExists(ip)
	require.NoError(t, err)
	require.True(t, exists, "gcIP must not delete the mirror while the vip is still live")
}

func TestCalculateUsingIPsSkipsMirroredVMVips(t *testing.T) {
	fc := newFakeController(t)
	subnet := vipMirrorSubnet()
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	vmVip := vipMirrorVip(util.KubeHostVMVip)
	vmVip.Labels[util.IPReservedLabel] = ""
	plainVip := vipMirrorVip("")
	plainVip.Name = "reserved"
	plainVip.Labels[util.IPReservedLabel] = ""
	require.NoError(t, vipIndexer.Add(vmVip))
	require.NoError(t, vipIndexer.Add(plainVip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)

	mirror := &kubeovnv1.IP{ObjectMeta: metav1.ObjectMeta{Name: "mirror"}, Spec: kubeovnv1.IPSpec{PodName: vmVip.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"}}
	using, err := fc.fakeController.calculateUsingIPs(subnet, []*kubeovnv1.IP{mirror}, nil)
	require.NoError(t, err)
	// 1 mirror ip + 1 plain vip; the vm vip must not be counted twice
	require.True(t, using.EqualInt64(2), "got %s", using)
}

func TestCalculateUsingIPsCountsVMVipUntilMirrored(t *testing.T) {
	fc := newFakeController(t)
	subnet := vipMirrorSubnet()
	vipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	vmVip := vipMirrorVip(util.KubeHostVMVip)
	vmVip.Labels[util.IPReservedLabel] = ""
	require.NoError(t, vipIndexer.Add(vmVip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(vipIndexer)

	using, err := fc.fakeController.calculateUsingIPs(subnet, nil, nil)
	require.NoError(t, err)
	require.True(t, using.EqualInt64(1), "an unmirrored vm vip must still count once, got %s", using)
}

// ---- feature tests: full handler paths with fake clients + mock OVN ----

func vipMirrorTestController(t *testing.T, subnet *kubeovnv1.Subnet, vips []*kubeovnv1.Vip, ips []*kubeovnv1.IP) *fakeController {
	t.Helper()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    vips,
		IPs:     ips,
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.addVirtualIPQueue = newTypedRateLimitingQueue[string]("AddVirtualIP", nil)
	c.updateIPQueue = newTypedRateLimitingQueue[string]("UpdateIP", nil)
	t.Cleanup(func() { c.addVirtualIPQueue.ShutDown(); c.updateIPQueue.ShutDown() })
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	return fc
}

func TestHandleAddVirtualIPVMVipCreatesIPMirror(t *testing.T) {
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Spec.MacAddress = "02:ac:10:ff:02:01"
	vip.Spec.V4ip = "10.0.0.6"
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, nil)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(subnet.Name, portName, "10.0.0.6", vip.Spec.MacAddress,
		vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc).Return(nil)

	require.NoError(t, fc.fakeController.handleAddVirtualIP(vip.Name))

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.Vips().Get(context.Background(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.6", got.Status.V4ip)
	require.Equal(t, vip.Spec.MacAddress, got.Status.Mac)

	mirror, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	require.NoError(t, err, "a kube_host_vm_vip must get an IP CR mirror (issue #7695)")
	require.Equal(t, util.KindVip, mirror.Spec.PodType)
	require.Equal(t, vip.Name, mirror.Spec.PodName)
	require.Equal(t, vip.Spec.Namespace, mirror.Spec.Namespace)
	require.Equal(t, got.Status.V4ip, mirror.Spec.V4IPAddress)
	require.Equal(t, vip.Spec.MacAddress, mirror.Spec.MacAddress)
	require.Equal(t, subnet.Name, mirror.Labels[util.SubnetNameLabel])
}

func TestHandleAddVirtualIPReconcilesMirrorOfAllocatedVMVip(t *testing.T) {
	// a vip that already has status (e.g. after its mirror was deleted by hand) must
	// recreate the mirror instead of returning early on the already-allocated shortcut.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Spec.V4ip, vip.Spec.MacAddress = "10.0.0.6", "02:ac:10:ff:02:06"
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, nil)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	// no CreateLogicalSwitchPort expectation: the lsp is not touched by this reconcile,
	// only the mirror is re-created

	require.NoError(t, fc.fakeController.handleAddVirtualIP(vip.Name))
	mirror, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), portName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.6", mirror.Spec.V4IPAddress)
}

func TestHandleAddVirtualIPLeavesOtherAllocatedVipsAlone(t *testing.T) {
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip("") // plain reservation, already allocated
	vip.Spec.V4ip, vip.Spec.MacAddress = "10.0.0.7", "02:ac:10:ff:01:02"
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.7", "02:ac:10:ff:01:02"
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, nil)
	// no OVN expectations and no mirror: nothing may be created for a plain vip
	require.NoError(t, fc.fakeController.handleAddVirtualIP(vip.Name))
	list, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, list.Items)
}

func TestHandleUpdateVirtualIPDeletesVMVipMirror(t *testing.T) {
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Spec.V4ip, vip.Spec.MacAddress = "10.0.0.6", "02:ac:10:ff:02:06"
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	vip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	now := metav1.Now()
	vip.DeletionTimestamp = &now
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	mirror := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}},
		Spec:       kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{mirror})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(vip.Name, portName, "10.0.0.6", &vip.Spec.MacAddress, subnet.Name, true)
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)
	fc.mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: subnet.Name}, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Return(nil).AnyTimes() // virtual port names

	require.NoError(t, fc.fakeController.handleUpdateVirtualIP(vip.Name))

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	if err == nil {
		require.False(t, got.DeletionTimestamp.IsZero(), "mirror must be marked for deletion")
	} else {
		require.True(t, k8serrors.IsNotFound(err))
	}
}

func TestHandleUpdateIPKeepsPortAndAddressOfLiveVMVipMirror(t *testing.T) {
	// deleting the mirror IP CR by hand must not release the vip's address or touch its
	// lsp: the vip remains the real owner, and the mirror is re-queued for recreation.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	now := metav1.Now()
	mirror := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now},
		Spec:       kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{mirror})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(vip.Name, portName, "10.0.0.6", &vip.Status.Mac, subnet.Name, true)
	require.NoError(t, err)

	// strict mock: no GetLogicalSwitchPort / DeleteLogicalSwitchPort call is expected
	require.NoError(t, fc.fakeController.handleUpdateIP(portName))

	// address still held by the vip
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", "10.0.0.6", nil, subnet.Name, true)
	require.Error(t, err, "the vip's address must not be released by deleting its mirror")
	// vip re-enqueued to recreate the mirror
	key, shutdown := fc.fakeController.addVirtualIPQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, vip.Name, key)
}

func TestEnsureVMVipIPCRRetriesWhileOldMirrorIsDeleting(t *testing.T) {
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	now := metav1.Now()
	mirror := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now},
		Spec:       kubeovnv1.IPSpec{PodName: vip.Name, Subnet: subnet.Name, PodType: util.KindVip},
	}
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{mirror})
	err := fc.fakeController.ensureVMVipIPCR(vip, subnet, portName, "10.0.0.6", "02:ac:10:ff:02:06")
	require.ErrorContains(t, err, "still being deleted")
}
