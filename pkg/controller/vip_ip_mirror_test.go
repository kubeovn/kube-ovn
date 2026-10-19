package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// errorSubnetLister simulates a subnet informer that cannot look up a subnet, e.g. a
// transient cache miss unrelated to the subnet actually being deleted.
type errorSubnetLister struct {
	err error
}

func (l *errorSubnetLister) List(labels.Selector) ([]*kubeovnv1.Subnet, error) { return nil, l.err }
func (l *errorSubnetLister) Get(string) (*kubeovnv1.Subnet, error)             { return nil, l.err }

// errorIPLister simulates an ip informer that cannot look up an ip, e.g. a transient cache
// error unrelated to the ip actually being absent.
type errorIPLister struct {
	err error
}

func (l *errorIPLister) List(labels.Selector) ([]*kubeovnv1.IP, error) { return nil, l.err }
func (l *errorIPLister) Get(string) (*kubeovnv1.IP, error)             { return nil, l.err }

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

func TestCheckIPOwnerExistsFailsClosedOnSubnetLookupError(t *testing.T) {
	// a transient subnet informer error inside isVMVipMirror must not be swallowed into
	// "not a mirror": gcIP must skip (not delete) rather than gc a live vip's mirror ip
	// just because the subnet lookup momentarily failed.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, nil)
	fc.fakeController.subnetsLister = &errorSubnetLister{err: errors.New("subnet lister unavailable")}
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	ip := &kubeovnv1.IP{
		Name: portName,
		Spec: kubeovnv1.IPSpec{PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip},
	}

	_, err := fc.fakeController.checkIPOwnerExists(ip)
	require.Error(t, err, "a subnet lookup failure must be propagated, not treated as owner-gone")
}

func TestCalculateUsingIPsSkipsMirroredVMVips(t *testing.T) {
	subnet := vipMirrorSubnet()
	vmVip := vipMirrorVip(util.KubeHostVMVip)
	vmVip.Labels[util.IPReservedLabel] = ""
	vmVip.Status.V4ip, vmVip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	plainVip := vipMirrorVip("")
	plainVip.Name = "reserved"
	plainVip.Labels[util.IPReservedLabel] = ""
	// isVMVipMirror needs the subnet to be real, so go through the full fake controller
	// rather than a bare virtualIpsLister indexer.
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vmVip, plainVip}, nil)

	mirror := &kubeovnv1.IP{
		Name: ovs.PodNameToPortName(vmVip.Name, vmVip.Spec.Namespace, subnet.Spec.Provider),
		Spec: kubeovnv1.IPSpec{
			PodName: vmVip.Name, Namespace: vmVip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: vmVip.Status.V4ip, MacAddress: vmVip.Status.Mac,
		},
	}
	using, err := fc.fakeController.calculateUsingIPs(subnet, []*kubeovnv1.IP{mirror}, nil)
	require.NoError(t, err)
	// 1 mirror ip + 1 plain vip; the vm vip must not be counted twice
	require.True(t, using.EqualInt64(2), "got %s", using)
}

func TestCalculateUsingIPsCountsMismatchedMirrorSeparately(t *testing.T) {
	// a same-named ip CR whose other identifying fields do not match the vip it claims to
	// mirror (issue #7696 review P2) must not suppress the vip's own usage count: the vip
	// is counted on its own, in addition to the (fabricated) mirror ip.
	subnet := vipMirrorSubnet()
	vmVip := vipMirrorVip(util.KubeHostVMVip)
	vmVip.Labels[util.IPReservedLabel] = ""
	vmVip.Status.V4ip, vmVip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vmVip}, nil)

	mismatched := &kubeovnv1.IP{
		Name: ovs.PodNameToPortName(vmVip.Name, vmVip.Spec.Namespace, subnet.Spec.Provider),
		Spec: kubeovnv1.IPSpec{
			PodName: vmVip.Name, Namespace: vmVip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: "10.0.0.99", MacAddress: "02:ac:10:ff:02:99", // does not match vmVip's allocated address/mac
		},
	}
	using, err := fc.fakeController.calculateUsingIPs(subnet, []*kubeovnv1.IP{mismatched}, nil)
	require.NoError(t, err)
	// 1 fabricated ip + 1 real vip usage, counted separately
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
		Spec: kubeovnv1.IPSpec{
			PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: vip.Status.V4ip, MacAddress: vip.Status.Mac,
		},
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

func TestHandleUpdateVirtualIPLeavesMismatchedSameNamedIPAlone(t *testing.T) {
	// a same-named ip CR that is not actually this vip's mirror (e.g. hand-crafted, wrong
	// fields) must survive the vip's deletion untouched: only a confirmed mirror may be
	// deleted here.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Spec.V4ip, vip.Spec.MacAddress = "10.0.0.6", "02:ac:10:ff:02:06"
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	vip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	now := metav1.Now()
	vip.DeletionTimestamp = &now
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	foreign := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec: kubeovnv1.IPSpec{
			PodName: "someone-else", Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: "10.0.0.9", MacAddress: "02:ac:10:ff:02:09",
		},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{foreign})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(vip.Name, portName, "10.0.0.6", &vip.Spec.MacAddress, subnet.Name, true)
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)
	fc.mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: subnet.Name}, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Return(nil).AnyTimes() // virtual port names

	require.NoError(t, fc.fakeController.handleUpdateVirtualIP(vip.Name))

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	require.NoError(t, err, "a foreign ip CR must not be deleted by an unrelated vip's deletion")
	require.True(t, got.DeletionTimestamp.IsZero(), "a foreign ip CR must not even be marked for deletion")
}

func TestDeleteVMVipIPCRFailsClosedOnMirrorLookupError(t *testing.T) {
	// a transient, non-NotFound failure identifying the mirror must not be treated as "safe
	// to delete": the caller must get the error back instead of risking a foreign ip CR.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	mirror := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec: kubeovnv1.IPSpec{
			PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: vip.Status.V4ip, MacAddress: vip.Status.Mac,
		},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{mirror})

	fc.fakeController.subnetsLister = &errorSubnetLister{err: errors.New("subnet lister unavailable")}

	err := fc.fakeController.deleteVMVipIPCR(vip, portName)
	require.EqualError(t, err, "subnet lister unavailable")

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	require.NoError(t, err, "the mirror must not be deleted on a failed identity check")
	require.True(t, got.DeletionTimestamp.IsZero())
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
		Spec: kubeovnv1.IPSpec{
			PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: vip.Status.V4ip, MacAddress: vip.Status.Mac,
		},
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

func TestHandleUpdateIPLeavesLivePlainVipLspAndAddressAlone(t *testing.T) {
	// a podType: Vip ip CR is never itself the owner of a real lsp/ipam address - the vip
	// is. A stale or hand-crafted podType: Vip ip naming a live plain (non
	// kube_host_vm_vip) vip's port must be cleaned up as a CR-only delete: it must never
	// reach the generic lsp-delete/ipam-release path and tear down the live vip's port.
	subnet := vipMirrorSubnet()
	plainVip := vipMirrorVip("") // plain reservation vip, not a kube_host_vm_vip
	portName := ovs.PodNameToPortName(plainVip.Name, plainVip.Spec.Namespace, subnet.Spec.Provider)
	now := metav1.Now()
	staleIP := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now,
		Spec: kubeovnv1.IPSpec{
			PodName: plainVip.Name, Namespace: plainVip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: "10.0.0.6",
		},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{plainVip}, []*kubeovnv1.IP{staleIP})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(plainVip.Name, portName, "10.0.0.6", nil, subnet.Name, true)
	require.NoError(t, err)

	// strict mock: no GetLogicalSwitchPort / DeleteLogicalSwitchPort call is expected
	require.NoError(t, fc.fakeController.handleUpdateIP(portName))

	// the live plain vip's address must still be held
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", "10.0.0.6", nil, subnet.Name, true)
	require.Error(t, err, "a non-mirror podType: Vip ip must not release a live plain vip's address")

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	if err == nil {
		require.Empty(t, got.Finalizers, "the stale ip CR's finalizer must still be removed")
	} else {
		require.True(t, k8serrors.IsNotFound(err))
	}
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

func TestHandleUpdateIPFailsClosedOnMirrorSubnetLookupError(t *testing.T) {
	// the vip itself reads fine, but isVMVipMirror's own subnet lookup fails: this must not
	// be swallowed into "not a mirror" either, or the delete path falls through and releases
	// a possibly-live vip's lsp and ipam address.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	now := metav1.Now()
	mirror := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now,
		Spec: kubeovnv1.IPSpec{
			PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: vip.Status.V4ip, MacAddress: vip.Status.Mac,
		},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{mirror})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(vip.Name, portName, "10.0.0.6", &vip.Status.Mac, subnet.Name, true)
	require.NoError(t, err)

	fc.fakeController.subnetsLister = &errorSubnetLister{err: errors.New("subnet lister unavailable")}

	// strict mock: no GetLogicalSwitchPort / DeleteLogicalSwitchPort call is expected
	err = fc.fakeController.handleUpdateIP(portName)
	require.EqualError(t, err, "subnet lister unavailable")

	// address still held by the vip: the failed mirror check must not fall through to release it
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", "10.0.0.6", nil, subnet.Name, true)
	require.Error(t, err, "the vip's address must not be released on a transient subnet lookup error")
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

func TestEnsureVMVipIPCRPropagatesIPLookupError(t *testing.T) {
	// a transient, non-NotFound failure reading the existing mirror ip must not be treated
	// as "no mirror yet": it must be retried instead of blindly creating/updating on stale
	// information.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	fc := vipMirrorTestController(t, subnet, nil, nil)
	fc.fakeController.ipsLister = &errorIPLister{err: errors.New("ip lister unavailable")}

	err := fc.fakeController.ensureVMVipIPCR(vip, subnet, portName, "10.0.0.6", "02:ac:10:ff:02:06")
	require.EqualError(t, err, "ip lister unavailable")
}

// ---- issue #7696 review P2: a bare podType: Vip ip CR that owns its own reserved-ip
// allocation, with no vip - live or otherwise - claiming its name ----

// orphanedVipTypeIP builds a podType: Vip ip CR named after podName as if it were a
// kube_host_vm_vip mirror, but with no matching Vip object ever created: exactly the bare,
// hand-crafted ip ValidateIP and handleAddReservedIP both accept.
func orphanedVipTypeIP(subnet *kubeovnv1.Subnet, podName, v4ip string) (*kubeovnv1.IP, string) {
	portName := ovs.PodNameToPortName(podName, "tenant", subnet.Spec.Provider)
	ip := &kubeovnv1.IP{
		Name: portName,
		Spec: kubeovnv1.IPSpec{
			PodName: podName, Namespace: "tenant", Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: v4ip,
		},
	}
	return ip, portName
}

func TestHandleAddReservedIPAcceptsBareVipTypeIP(t *testing.T) {
	// ValidateIP and handleAddReservedIP both accept podType: Vip on a bare, hand-created ip
	// CR with no backing Vip object: this really allocates an address and adds the controller
	// finalizer, exactly like any other reserved ip - it does not get treated as a vip mirror
	// placeholder with no allocation of its own.
	subnet := vipMirrorSubnet()
	ip, portName := orphanedVipTypeIP(subnet, "ghost-vip", "")
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{ip})
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(nil, nil)

	require.NoError(t, fc.fakeController.handleAddReservedIP(ip.Name))

	got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer,
		"a bare podType: Vip ip must get the controller finalizer like any reserved ip")
	require.NotEmpty(t, got.Spec.V4IPAddress, "a bare podType: Vip ip must really allocate an address")

	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("someone-else", "someone-else-port", got.Spec.V4IPAddress, nil, subnet.Name, true)
	require.Error(t, err, "the allocated address must be held in ipam, not left untracked")
}

func TestHandleUpdateIPReleasesOrphanedVipTypeReservedIPAddress(t *testing.T) {
	// The actual issue #7696 review P2 regression: a podType: Vip ip accepted through
	// handleAddReservedIP (no vip ever claimed its name) owns a real ipam allocation. Deleting
	// it must release that allocation like any other owned ip, not strand it as if a Vip
	// controller still owned it - otherwise reallocating the address fails with
	// AddressConflict until a controller restart.
	subnet := vipMirrorSubnet()
	ip, portName := orphanedVipTypeIP(subnet, "ghost-vip", "10.0.0.6")
	ip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	now := metav1.Now()
	ip.DeletionTimestamp = &now
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{ip})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress("whatever", portName, "10.0.0.6", nil, subnet.Name, true)
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(nil, nil)

	require.NoError(t, fc.fakeController.handleUpdateIP(portName))

	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("someone-else", "someone-else-port", "10.0.0.6", nil, subnet.Name, true)
	require.NoError(t, err, "the orphaned reserved-ip's address must be released, not stranded")

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	if err == nil {
		require.Empty(t, got.Finalizers, "the orphaned ip CR's finalizer must be removed")
	} else {
		require.True(t, k8serrors.IsNotFound(err))
	}
}

func TestHandleUpdateIPDeletesOrphanedVipTypeReservedIPLSP(t *testing.T) {
	// if the orphaned reserved-ip's own port actually exists in OVN, it must be torn down
	// too, same as the generic (non-vip) ip deletion path does.
	subnet := vipMirrorSubnet()
	ip, portName := orphanedVipTypeIP(subnet, "ghost-vip", "10.0.0.6")
	ip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	now := metav1.Now()
	ip.DeletionTimestamp = &now
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{ip})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress("whatever", portName, "10.0.0.6", nil, subnet.Name, true)
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(&ovnnb.LogicalSwitchPort{Name: portName}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)

	require.NoError(t, fc.fakeController.handleUpdateIP(portName))
}

func TestHandleUpdateIPOrphanedVipTypePropagatesLSPLookupError(t *testing.T) {
	// a transient OVN lookup failure while releasing an orphaned reserved-ip's own port must
	// be retried, not swallowed into "release done".
	subnet := vipMirrorSubnet()
	ip, portName := orphanedVipTypeIP(subnet, "ghost-vip", "10.0.0.6")
	ip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	now := metav1.Now()
	ip.DeletionTimestamp = &now
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{ip})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress("whatever", portName, "10.0.0.6", nil, subnet.Name, true)
	require.NoError(t, err)

	injectedErr := errors.New("get logical switch port failed")
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(nil, injectedErr)

	err = fc.fakeController.handleUpdateIP(portName)
	require.ErrorIs(t, err, injectedErr)

	// the address must still be held: a failed release must not silently drop the finalizer
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("someone-else", "someone-else-port", "10.0.0.6", nil, subnet.Name, true)
	require.Error(t, err, "the address must not be released when the lsp lookup fails")
}

func TestHandleUpdateIPOrphanedVipTypeSkipsReleaseWhenSubnetGone(t *testing.T) {
	// the subnet backing an orphaned reserved-ip may already be gone by the time it is
	// deleted; this must not panic (the same class of bug as Test_handleUpdateIP_deletedSubnet)
	// and must still drop the finalizer so the CR can go away.
	subnet := vipMirrorSubnet()
	ip, portName := orphanedVipTypeIP(subnet, "ghost-vip", "10.0.0.6")
	ip.Spec.Subnet = "deleted-subnet"
	ip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	now := metav1.Now()
	ip.DeletionTimestamp = &now
	fc := vipMirrorTestController(t, subnet, nil, []*kubeovnv1.IP{ip})

	require.NoError(t, fc.fakeController.handleUpdateIP(portName))

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1()
	got, err := client.IPs().Get(context.Background(), portName, metav1.GetOptions{})
	if err == nil {
		require.Empty(t, got.Finalizers, "the orphaned ip CR's finalizer must still be removed")
	} else {
		require.True(t, k8serrors.IsNotFound(err))
	}
}

func TestHandleUpdateIPLeavesTerminatingVipMirrorAddressAlone(t *testing.T) {
	// a vip sharing the ip's name exists but is itself terminating: its own deletion flow
	// (deleteVMVipIPCR) owns releasing the real lsp/ipam, so this ip CR must only drop its
	// finalizer, never race that flow by releasing the address here too.
	subnet := vipMirrorSubnet()
	vip := vipMirrorVip(util.KubeHostVMVip)
	vip.Status.V4ip, vip.Status.Mac = "10.0.0.6", "02:ac:10:ff:02:06"
	now := metav1.Now()
	vip.DeletionTimestamp = &now
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	mirror := &kubeovnv1.IP{
		Name: portName, Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: &now,
		Spec: kubeovnv1.IPSpec{
			PodName: vip.Name, Namespace: vip.Spec.Namespace, Subnet: subnet.Name, PodType: util.KindVip,
			V4IPAddress: vip.Status.V4ip, MacAddress: vip.Status.Mac,
		},
	}
	fc := vipMirrorTestController(t, subnet, []*kubeovnv1.Vip{vip}, []*kubeovnv1.IP{mirror})
	_, _, _, err := fc.fakeController.ipam.GetStaticAddress(vip.Name, portName, "10.0.0.6", &vip.Status.Mac, subnet.Name, true)
	require.NoError(t, err)

	// strict mock: no GetLogicalSwitchPort / DeleteLogicalSwitchPort call is expected
	require.NoError(t, fc.fakeController.handleUpdateIP(portName))

	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("other", "other-port", "10.0.0.6", nil, subnet.Name, true)
	require.Error(t, err, "a terminating vip's address must not be released by its mirror ip's own delete handling")
}
