package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
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
		Name: "dn3-net2", Labels: map[string]string{util.SubnetNameLabel: "subnet-3"},
		Spec: kubeovnv1.VipSpec{Namespace: "tenant", Subnet: "subnet-3", Type: vipType},
	}
}

func TestCheckIPOwnerExistsVipMirror(t *testing.T) {
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	// checkIPOwnerExists now does a live read of the Vip and verifies, via
	// isVMVipMirror, that ip really is that vip's own kube_host_vm_vip mirror (not just
	// a podName/podType match), so both the subnet and the vip must exist for real.
	fc := vipMirrorTestController(t, subnet, nil, nil)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	ip := &kubeovnv1.IP{
		Name: portName,
		Spec: kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip},
	}

	exists, err := fc.fakeController.checkIPOwnerExists(ip)
	require.NoError(t, err)
	require.False(t, exists, "a mirror ip without its vip must look orphaned to gcIP")

	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{})
	require.NoError(t, err)
	exists, err = fc.fakeController.checkIPOwnerExists(ip)
	require.NoError(t, err)
	require.True(t, exists, "gcIP must not delete the mirror while the vip is still live")
}

func TestCheckIPOwnerExistsRejectsMismatchedMirror(t *testing.T) {
	// a podType/podName match alone is not enough: an ip CR naming a live vip that is not
	// actually its kube_host_vm_vip mirror (wrong type, or a stale/fabricated name) must
	// not be kept alive by that vip's existence.
	subnet := vipMirrorSubnet()
	plainVip := vipMirrorVip("") // live, but not a kube_host_vm_vip
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{plainVip}, nil)
	ip := &kubeovnv1.IP{
		Name: "not-the-real-mirror-name",
		Spec: kubeovnv1.IPSpec{PodName: plainVip.Name, Namespace: plainVip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip},
	}
	exists, err := fc.fakeController.checkIPOwnerExists(ip)
	require.NoError(t, err)
	require.False(t, exists, "an ip CR naming a live but non-matching vip must not be kept alive")
}

func TestCalculateUsingIPsSkipsMirroredVMVips(t *testing.T) {
	subnet := vipMirrorSubnet()
	vmVip := vipMirrorVip(util.KubeHostVMVip)
	vmVip.Labels[util.IPReservedLabel] = ""
	plainVip := vipMirrorVip("")
	plainVip.Name = "reserved"
	plainVip.Labels[util.IPReservedLabel] = ""
	// isVMVipMirror needs the subnet to be real, so go through the full fake controller
	// rather than a bare virtualIpsLister indexer.
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vmVip, plainVip}, nil)

	mirror := &kubeovnv1.IP{
		Name: ovs.PodNameToPortName(vmVip.Name, vmVip.Spec.Namespace, subnet.Spec.Provider),
		Spec: kubeovnv1.IPSpec{PodName: vmVip.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"},
	}
	using, err := fc.fakeController.calculateUsingIPs(subnet, []*kubeovnv1.IP{mirror}, nil)
	require.NoError(t, err)
	// 1 mirror ip + 1 plain vip; the vm vip must not be counted twice
	require.True(t, using.EqualInt64(2), "got %s", using)
}

func TestCalculateUsingIPsCountsVMVipUntilMirrored(t *testing.T) {
	subnet := vipMirrorSubnet()
	vmVip := vipMirrorVip(util.KubeHostVMVip)
	vmVip.Labels[util.IPReservedLabel] = ""
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vmVip}, nil)

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
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec: kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"},
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
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now,
		Spec: kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"},
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

func TestHandleAddVirtualIPSkipsVipBeingDeleted(t *testing.T) {
	// a queued add racing with deletion must not (re)allocate ipam, create the lsp, or
	// recreate the mirror once cleanup has started.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	now := metav1.Now()
	vip.DeletionTimestamp = &now
	vip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, nil)
	// strict mock: no CreateLogicalSwitchPort call is expected

	require.NoError(t, fc.fakeController.handleAddVirtualIP(vip.Name))

	got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, got.Status.V4ip, "a deleting vip must not have an address allocated")
	list, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, list.Items, "a deleting vip must not get a mirror created")
}

func TestHandleUpdateIPPropagatesVipGetError(t *testing.T) {
	// a transient, non-NotFound failure reading the owning vip must not be treated as "the
	// vip is gone": the delete must be retried instead of releasing a possibly-live vip's
	// lsp and ipam address.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	now := metav1.Now()
	mirror := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now,
		Spec: kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip, V4IPAddress: "10.0.0.6"},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{mirror})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(vip.Name, portName, "10.0.0.6", &vip.Status.Mac, subnet.Name, true)
	require.NoError(t, err)

	fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("get", "vips", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("get vip failed")
	})

	// strict mock: no GetLogicalSwitchPort / DeleteLogicalSwitchPort call is expected
	err = fc.fakeController.handleUpdateIP(portName)
	require.EqualError(t, err, "get vip failed")

	// address still held by the vip: the failed read must not fall through to release it
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", "10.0.0.6", nil, subnet.Name, true)
	require.Error(t, err, "the vip's address must not be released on a transient vip read error")
}

func TestEnsureVMVipIPCRRetriesWhileOldMirrorIsDeleting(t *testing.T) {
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	now := metav1.Now()
	mirror := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now,
		Spec: kubeovnv1.IPSpec{PodName: vip.Name, Subnet: subnet.Name, PodType: util.KindVip},
	}
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{mirror})
	err := fc.fakeController.ensureVMVipIPCR(vip, subnet, portName, "10.0.0.6", "02:ac:10:ff:02:06")
	require.ErrorContains(t, err, "still being deleted")
}
