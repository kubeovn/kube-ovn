package controller

import (
	"context"
	"testing"
	"time"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	eepSubnet   = "eep-subnet"
	eepVpc      = "eep-vpc"
	eepNs       = "tenant"
	eepName     = "vm1-eth0"
	eepUID      = "6f1c8e9a-1b2c-4d3e-8f90-123456789abc"
	eepDHCPv4ID = "dhcp-v4-uuid"
	eepChassis  = "hv-1"
)

func newEEPSubnet(namespaces ...string) *kubeovnv1.Subnet {
	subnet := &kubeovnv1.Subnet{
		Name: eepSubnet,
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock:  "10.10.0.0/24",
			Gateway:    "10.10.0.1",
			Protocol:   kubeovnv1.ProtocolIPv4,
			Provider:   util.OvnProvider,
			Vpc:        eepVpc,
			EnableDHCP: true,
			Namespaces: namespaces,
		},
		Status: kubeovnv1.SubnetStatus{DHCPv4OptionsUUID: eepDHCPv4ID},
	}
	subnet.Status.Ready("Init", "")
	return subnet
}

func newEEP(mutate ...func(*kubeovnv1.ExternalEndpoint)) *kubeovnv1.ExternalEndpoint {
	ep := &kubeovnv1.ExternalEndpoint{
		Name:      eepName,
		Namespace: eepNs,
		UID:       types.UID(eepUID),
		Spec:      kubeovnv1.ExternalEndpointSpec{Subnet: eepSubnet, Chassis: eepChassis},
	}
	for _, m := range mutate {
		m(ep)
	}
	return ep
}

func newEEPController(t *testing.T, subnet *kubeovnv1.Subnet, eps ...*kubeovnv1.ExternalEndpoint) *fakeController {
	t.Helper()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:           []*kubeovnv1.Subnet{subnet},
		ExternalEndpoints: eps,
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableExternalEndpoint = true
	ctrl.syncSgPortsQueue = newTypedRateLimitingQueue[string]("SyncSgPorts", nil)
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	return fc
}

func getEEP(t *testing.T, fc *fakeController) *kubeovnv1.ExternalEndpoint {
	t.Helper()
	ep, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().ExternalEndpoints(eepNs).Get(context.Background(), eepName, metav1.GetOptions{})
	require.NoError(t, err)
	return ep
}

func eepKey() string { return cache.NewObjectName(eepNs, eepName).String() }

func TestHandleAddExternalEndpointProvisionsPort(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet(), newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Spec.Chassis = "hv-1"
		ep.Spec.SecurityGroups = []string{"sg-b", "sg-a"}
	}))
	ctrl, nb := fc.fakeController, fc.mockOvnClient
	owner := eepNs + "/" + eepName

	var gotIP, gotMac string
	nb.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(nil, nil)
	// the owner and the chassis pin are written in the same transaction as the port
	nb.EXPECT().CreateLogicalSwitchPortWithOwner(eepSubnet, eepUID, gomock.Any(), gomock.Any(), true, "sg-a,sg-b",
		true, &ovs.DHCPOptionsUUIDs{DHCPv4OptionsUUID: eepDHCPv4ID}, eepVpc, map[string]string{externalEndpointExternalIDKey: owner}, "hv-1").
		DoAndReturn(func(_, _, ip, mac string, _ bool, _ string, _ bool, _ *ovs.DHCPOptionsUUIDs, _ string, _ map[string]string, _ string) error {
			gotIP, gotMac = ip, mac
			return nil
		})
	nb.EXPECT().GetLogicalSwitchPort(eepUID, false).Return(&ovnnb.LogicalSwitchPort{Name: eepUID, Up: new(true)}, nil)

	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))

	ep := getEEP(t, fc)
	require.Contains(t, ep.Finalizers, util.KubeOVNControllerFinalizer)
	require.Equal(t, eepUID, ep.Status.InterfaceID)
	require.Equal(t, gotIP, ep.Status.V4IP)
	require.Equal(t, gotMac, ep.Status.MacAddress)
	require.True(t, util.CIDRContainIP("10.10.0.0/24", ep.Status.V4IP))
	require.True(t, ep.Status.Up)
	require.True(t, meta.IsStatusConditionTrue(ep.Status.Conditions, kubeovnv1.ExternalEndpointConditionReady))
	require.True(t, meta.IsStatusConditionTrue(ep.Status.Conditions, kubeovnv1.ExternalEndpointConditionBound))

	// deleting a pod with the same namespace and name must not free the endpoint's address
	ctrl.ipam.ReleaseAddressByPod(eepNs+"/"+eepName, eepSubnet)
	owner, assigned := ctrl.ipam.IsIPAssignedToOtherPod(gotIP, eepSubnet, "tenant/someone")
	require.True(t, assigned)
	require.Equal(t, externalEndpointIPAMKey(eepNs, eepName), owner)

	ipCR, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), eepUID, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, util.KindExternalEndpoint, ipCR.Spec.PodType)
	require.Equal(t, eepName, ipCR.Spec.PodName)
	require.Equal(t, eepNs, ipCR.Spec.Namespace)
	require.Equal(t, gotIP, ipCR.Spec.V4IPAddress)
	require.Equal(t, gotMac, ipCR.Spec.MacAddress)
	// a cluster scoped IP CR cannot carry an owner reference to a namespaced endpoint
	require.Empty(t, ipCR.OwnerReferences)
	require.Equal(t, eepUID, ipCR.Labels[util.ExternalEndpointUIDLabel])
	require.Equal(t, "true", ipCR.Labels[util.IPReservedLabel])
	require.Contains(t, ipCR.Finalizers, util.KubeOVNControllerFinalizer)
	require.Equal(t, 2, ctrl.syncSgPortsQueue.Len())
}

func TestHandleAddExternalEndpointStaticAddress(t *testing.T) {
	const ifaceID = "0b8a1f2e-3c4d-4e5f-9a0b-1c2d3e4f5a6b"
	fc := newEEPController(t, newEEPSubnet(), newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Spec.IPAddress = "10.10.0.50"
		ep.Spec.MacAddress = "52:54:00:AA:BB:CC"
		ep.Spec.InterfaceID = ifaceID
		ep.Spec.PortSecurity = new(false)
	}))
	ctrl, nb := fc.fakeController, fc.mockOvnClient

	nb.EXPECT().GetLogicalSwitchPort(ifaceID, true).Return(nil, nil)
	nb.EXPECT().CreateLogicalSwitchPortWithOwner(eepSubnet, ifaceID, "10.10.0.50", "52:54:00:aa:bb:cc", false, "",
		true, gomock.Any(), eepVpc, gomock.Any(), eepChassis).Return(nil)
	nb.EXPECT().GetLogicalSwitchPort(ifaceID, false).Return(&ovnnb.LogicalSwitchPort{Name: ifaceID}, nil)

	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))

	ep := getEEP(t, fc)
	require.Equal(t, ifaceID, ep.Status.InterfaceID)
	require.Equal(t, "10.10.0.50", ep.Status.V4IP)
	require.Equal(t, "52:54:00:aa:bb:cc", ep.Status.MacAddress)
	require.False(t, ep.Status.Up)
	require.True(t, meta.IsStatusConditionFalse(ep.Status.Conditions, kubeovnv1.ExternalEndpointConditionBound))
}

func TestHandleAddExternalEndpointSkipsUpToDatePort(t *testing.T) {
	ep := newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Finalizers = []string{util.KubeOVNControllerFinalizer}
		ep.Status = kubeovnv1.ExternalEndpointStatus{InterfaceID: eepUID, V4IP: "10.10.0.7", MacAddress: "52:54:00:00:00:07"}
	})
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl, nb := fc.fakeController, fc.mockOvnClient

	upToDate := &ovnnb.LogicalSwitchPort{
		Name:          eepUID,
		Addresses:     []string{"52:54:00:00:00:07 10.10.0.7"},
		PortSecurity:  []string{"52:54:00:00:00:07 10.10.0.7"},
		Dhcpv4Options: new(eepDHCPv4ID),
		Options:       map[string]string{"requested-chassis": eepChassis},
		ExternalIDs: map[string]string{
			ovs.LogicalSwitchKey:          eepSubnet,
			externalEndpointExternalIDKey: eepNs + "/" + eepName,
		},
	}
	// no CreateLogicalSwitchPortWithOwner: the mock fails on any NB write
	nb.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(upToDate, nil)
	nb.EXPECT().GetLogicalSwitchPort(eepUID, false).Return(upToDate, nil)

	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))
	require.Equal(t, "10.10.0.7", getEEP(t, fc).Status.V4IP)

	// the second resync finds the address already held in ipam and writes nothing
	require.Eventually(t, func() bool {
		cached, err := ctrl.externalEndpointsLister.ExternalEndpoints(eepNs).Get(eepName)
		return err == nil && meta.IsStatusConditionTrue(cached.Status.Conditions, kubeovnv1.ExternalEndpointConditionReady)
	}, 5*time.Second, 10*time.Millisecond)
	nb.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(upToDate, nil)
	nb.EXPECT().GetLogicalSwitchPort(eepUID, false).Return(upToDate, nil)
	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))
	require.True(t, ctrl.ipamHoldsAddress(externalEndpointIPAMKey(eepNs, eepName), eepSubnet, "10.10.0.7", "52:54:00:00:00:07"))
}

func TestHandleAddExternalEndpointRejectsInvalidSpec(t *testing.T) {
	cases := []struct {
		name   string
		subnet *kubeovnv1.Subnet
		ep     *kubeovnv1.ExternalEndpoint
		reason string
	}{{
		name:   "subnet not found",
		subnet: newEEPSubnet(),
		ep:     newEEP(func(ep *kubeovnv1.ExternalEndpoint) { ep.Spec.Subnet = "missing" }),
		reason: "SubnetNotFound",
	}, {
		name:   "namespace not bound to the subnet",
		subnet: newEEPSubnet("other"),
		ep:     newEEP(),
		reason: "NamespaceNotAllowed",
	}, {
		name: "underlay subnet",
		subnet: func() *kubeovnv1.Subnet {
			s := newEEPSubnet()
			s.Spec.Vlan = "vlan100"
			return s
		}(),
		ep:     newEEP(),
		reason: "UnsupportedSubnet",
	}, {
		name:   "no chassis",
		subnet: newEEPSubnet(),
		ep:     newEEP(func(ep *kubeovnv1.ExternalEndpoint) { ep.Spec.Chassis = "" }),
		reason: "ChassisRequired",
	}, {
		name:   "invalid static address",
		subnet: newEEPSubnet(),
		ep:     newEEP(func(ep *kubeovnv1.ExternalEndpoint) { ep.Spec.IPAddress = "10.10.0.300" }),
		reason: "AcquireAddressFailed",
	}, {
		name:   "static address outside the subnet",
		subnet: newEEPSubnet(),
		ep:     newEEP(func(ep *kubeovnv1.ExternalEndpoint) { ep.Spec.IPAddress = "192.168.0.10" }),
		reason: "AcquireAddressFailed",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := newEEPController(t, tc.subnet, tc.ep)
			// permanent errors are not retried, the periodic resync picks the endpoint up again
			require.NoError(t, fc.fakeController.handleAddOrUpdateExternalEndpoint(eepKey()))
			ep := getEEP(t, fc)
			cond := meta.FindStatusCondition(ep.Status.Conditions, kubeovnv1.ExternalEndpointConditionReady)
			require.NotNil(t, cond)
			require.Equal(t, metav1.ConditionFalse, cond.Status)
			require.Equal(t, tc.reason, cond.Reason)
			require.Empty(t, ep.Status.V4IP)
		})
	}
}

func TestHandleAddExternalEndpointAddressConflict(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet(), newEEP(func(ep *kubeovnv1.ExternalEndpoint) { ep.Spec.IPAddress = "10.10.0.9" }))
	ctrl := fc.fakeController
	_, _, _, err := ctrl.ipam.GetStaticAddress("tenant/pod", "pod.tenant", "10.10.0.9", nil, eepSubnet, true)
	require.NoError(t, err)

	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))
	cond := meta.FindStatusCondition(getEEP(t, fc).Status.Conditions, kubeovnv1.ExternalEndpointConditionReady)
	require.NotNil(t, cond)
	require.Equal(t, "AcquireAddressFailed", cond.Reason)
	require.Contains(t, cond.Message, "AddressConflict")
}

func TestHandleAddExternalEndpointRefusesForeignPort(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet(), newEEP())
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(&ovnnb.LogicalSwitchPort{
		Name:        eepUID,
		ExternalIDs: map[string]string{"pod": "default/web"},
	}, nil)

	require.NoError(t, fc.fakeController.handleAddOrUpdateExternalEndpoint(eepKey()))
	cond := meta.FindStatusCondition(getEEP(t, fc).Status.Conditions, kubeovnv1.ExternalEndpointConditionReady)
	require.NotNil(t, cond)
	require.Equal(t, "CreateOVNPortFailed", cond.Reason)
}

func TestCheckExternalEndpointPortOwner(t *testing.T) {
	owner := eepNs + "/" + eepName
	port := func(ids map[string]string) *ovnnb.LogicalSwitchPort {
		return &ovnnb.LogicalSwitchPort{Name: eepUID, ExternalIDs: ids}
	}
	require.NoError(t, checkExternalEndpointPortOwner(port(map[string]string{externalEndpointExternalIDKey: owner}), owner))
	require.Error(t, checkExternalEndpointPortOwner(port(map[string]string{externalEndpointExternalIDKey: "other/ep"}), owner), "another endpoint")
	require.Error(t, checkExternalEndpointPortOwner(port(map[string]string{"vendor": "neutron"}), owner), "another cms")
	require.Error(t, checkExternalEndpointPortOwner(port(map[string]string{"vendor": util.CniTypeName, ovs.LogicalSwitchKey: eepSubnet}), owner), "a kube-ovn port without an owner")
	require.Error(t, checkExternalEndpointPortOwner(port(map[string]string{"vendor": util.CniTypeName, "pod": "tenant/web"}), owner), "a pod port")
}

func TestHandleDelExternalEndpointCleansUp(t *testing.T) {
	now := metav1.Now()
	ep := newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Finalizers = []string{util.KubeOVNControllerFinalizer}
		ep.DeletionTimestamp = &now
		ep.Status = kubeovnv1.ExternalEndpointStatus{InterfaceID: eepUID, V4IP: "10.10.0.7", MacAddress: "52:54:00:00:00:07"}
	})
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl, nb := fc.fakeController, fc.mockOvnClient
	_, _, _, err := ctrl.ipam.GetStaticAddress(externalEndpointIPAMKey(eepNs, eepName), eepUID, "10.10.0.7", nil, eepSubnet, true)
	require.NoError(t, err)
	require.NoError(t, ctrl.createOrUpdateExternalEndpointIPCR(ep, eepSubnet, eepUID, "10.10.0.7", "52:54:00:00:00:07"))

	nb.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(&ovnnb.LogicalSwitchPort{
		Name: eepUID,
		ExternalIDs: map[string]string{
			externalEndpointExternalIDKey: eepNs + "/" + eepName,
			"associated_sg_sg-a":          "true",
		},
	}, nil)
	nb.EXPECT().DeleteLogicalSwitchPort(eepUID).Return(nil)

	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))

	// the fake client keeps an object whose finalizers are removed even if it has a
	// deletion timestamp, so check the finalizer rather than the object
	require.NotContains(t, getEEP(t, fc).Finalizers, util.KubeOVNControllerFinalizer)
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), eepUID, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
	require.Equal(t, 1, ctrl.syncSgPortsQueue.Len())
	// the address is free again
	_, _, _, err = ctrl.ipam.GetStaticAddress("tenant/pod", "pod.tenant", "10.10.0.7", nil, eepSubnet, true)
	require.NoError(t, err)
}

func TestHandleDelExternalEndpointKeepsForeignPort(t *testing.T) {
	now := metav1.Now()
	fc := newEEPController(t, newEEPSubnet(), newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Finalizers = []string{util.KubeOVNControllerFinalizer}
		ep.DeletionTimestamp = &now
	}))
	// a port with the same name that the endpoint does not own is never deleted
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(&ovnnb.LogicalSwitchPort{
		Name:        eepUID,
		ExternalIDs: map[string]string{"pod": "default/web"},
	}, nil)

	require.NoError(t, fc.fakeController.handleAddOrUpdateExternalEndpoint(eepKey()))
	require.NotContains(t, getEEP(t, fc).Finalizers, util.KubeOVNControllerFinalizer)
}

func TestHandleUpdateIPLeavesExternalEndpointPort(t *testing.T) {
	now := metav1.Now()
	ep := newEEP()
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.createOrUpdateExternalEndpointIPCR(ep, eepSubnet, eepUID, "10.10.0.7", "52:54:00:00:00:07"))
	ipCR, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), eepUID, metav1.GetOptions{})
	require.NoError(t, err)
	ipCR.DeletionTimestamp = &now
	require.NoError(t, fc.fakeInformers.ipInformer.Informer().GetIndexer().Update(ipCR))

	// no NB expectation: deleting the ip CR must not delete the endpoint's port
	require.NoError(t, ctrl.handleUpdateIP(eepUID))

	ipCR, err = ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), eepUID, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, ipCR.Finalizers)
	require.Equal(t, 1, ctrl.addOrUpdateExternalEndpointQueue.Len())
}

func TestCheckIPOwnerExistsForExternalEndpoint(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet(), newEEP())
	ctrl := fc.fakeController
	ip := func(name string, uid types.UID) *kubeovnv1.IP {
		return &kubeovnv1.IP{
			Name: "ip", Labels: map[string]string{util.ExternalEndpointUIDLabel: string(uid)},
			Spec: kubeovnv1.IPSpec{PodName: name, Namespace: eepNs, Subnet: eepSubnet, PodType: util.KindExternalEndpoint},
		}
	}

	exists, err := ctrl.checkIPOwnerExists(ip(eepName, eepUID))
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = ctrl.checkIPOwnerExists(ip(eepName, "11111111-2222-3333-4444-555555555555"))
	require.NoError(t, err)
	require.False(t, exists, "an ip CR of a previous endpoint with the same name is stale")

	exists, err = ctrl.checkIPOwnerExists(ip("gone", eepUID))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestMarkAndCleanLSPKeepsExternalEndpointPort(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet(), newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Status.InterfaceID = eepUID
	}))
	ctrl := fc.fakeController
	ctrl.config.EnableKeepVMIP = false
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	previousLastNoPodLSP := lastNoPodLSP
	lastNoPodLSP = strset.New()
	t.Cleanup(func() { lastNoPodLSP = previousLastNoPodLSP })

	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{
		{Name: eepUID, ExternalIDs: map[string]string{externalEndpointExternalIDKey: eepNs + "/" + eepName}},
		// the port of an endpoint that was deleted without its finalizer is garbage
		{Name: "stale-endpoint-port", ExternalIDs: map[string]string{externalEndpointExternalIDKey: eepNs + "/gone"}},
		{Name: "orphan"},
	}, nil).Times(1)

	require.NoError(t, ctrl.markAndCleanLSP())
	require.False(t, lastNoPodLSP.Has(eepUID))
	require.True(t, lastNoPodLSP.Has("stale-endpoint-port"))
	require.True(t, lastNoPodLSP.Has("orphan"))
}

func TestGcChassisKeepsExternalEndpointChassis(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet())
	// the pins are read from the ports, whatever the state of the endpoints
	fc.mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, nil, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{
		{Name: "p1", Options: map[string]string{"requested-chassis": "hv-by-hostname"}},
		{Name: "p2", Options: map[string]string{"requested-chassis": "chassis-by-name"}},
	}, nil)
	fc.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
		{Name: "chassis-a", Hostname: "hv-by-hostname"},
		{Name: "chassis-by-name", Hostname: "hv-b"},
		{Name: "chassis-stale", Hostname: "decommissioned"},
	}, nil)
	fc.mockOvnSbClient.EXPECT().DeleteChassis("chassis-stale").Return(nil)

	require.NoError(t, fc.fakeController.gcChassis())
}

func eepIPCR(name, podName, ip string, created time.Time) *kubeovnv1.IP {
	return &kubeovnv1.IP{
		Name:              name,
		CreationTimestamp: metav1.NewTime(created),
		Spec: kubeovnv1.IPSpec{
			PodName: podName, Namespace: eepNs, Subnet: eepSubnet, IPAddress: ip,
			MacAddress: "52:54:00:00:00:07", PodType: util.KindExternalEndpoint,
		},
	}
}

func TestRestoreExternalEndpointsToIPAM(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet())
	ctrl := fc.fakeController
	now := time.Now()
	ctrl.restoreExternalEndpointsToIPAM([]*kubeovnv1.IP{
		// a younger ip CR that conflicts with the older one: logged and skipped, does not
		// take the address over, whatever the order of the list
		eepIPCR("33333333-3333-3333-3333-333333333333", "a-dup", "10.10.0.7", now.Add(time.Hour)),
		eepIPCR(eepUID, eepName, "10.10.0.7", now),
		// not an external endpoint
		{Name: "pod.tenant", Spec: kubeovnv1.IPSpec{PodName: "pod", Namespace: eepNs, Subnet: eepSubnet, IPAddress: "10.10.0.8"}},
	})

	owner, assigned := ctrl.ipam.IsIPAssignedToOtherPod("10.10.0.7", eepSubnet, "tenant/someone")
	require.True(t, assigned)
	require.Equal(t, externalEndpointIPAMKey(eepNs, eepName), owner)
	_, assigned = ctrl.ipam.IsIPAssignedToOtherPod("10.10.0.8", eepSubnet, "tenant/someone")
	require.False(t, assigned)
}

func TestHandleAddExternalEndpointReusesAddressOfIPCR(t *testing.T) {
	// the controller crashed after it created the IP CR but before it wrote the status
	ep := newEEP(func(ep *kubeovnv1.ExternalEndpoint) { ep.Finalizers = []string{util.KubeOVNControllerFinalizer} })
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl, nb := fc.fakeController, fc.mockOvnClient
	require.NoError(t, ctrl.createOrUpdateExternalEndpointIPCR(ep, eepSubnet, eepUID, "10.10.0.42", "52:54:00:00:00:42"))
	require.Eventually(t, func() bool { _, err := ctrl.ipsLister.Get(eepUID); return err == nil }, 5*time.Second, 10*time.Millisecond)

	nb.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(nil, nil)
	nb.EXPECT().CreateLogicalSwitchPortWithOwner(eepSubnet, eepUID, "10.10.0.42", "52:54:00:00:00:42", true, "", true, gomock.Any(), eepVpc, gomock.Any(), eepChassis).Return(nil)
	nb.EXPECT().GetLogicalSwitchPort(eepUID, false).Return(&ovnnb.LogicalSwitchPort{Name: eepUID}, nil)

	require.NoError(t, ctrl.handleAddOrUpdateExternalEndpoint(eepKey()))
	require.Equal(t, "10.10.0.42", getEEP(t, fc).Status.V4IP)
}

func TestExternalEndpointDisabledKeepsPortsAndAddresses(t *testing.T) {
	fc := newEEPController(t, newEEPSubnet())
	ctrl := fc.fakeController
	ctrl.config.EnableExternalEndpoint = false
	ctrl.externalEndpointsLister = nil
	ctrl.addOrUpdateExternalEndpointQueue = nil
	ctrl.config.EnableKeepVMIP = false
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	// gcIP keeps the address of an endpoint it cannot look up
	exists, err := ctrl.checkIPOwnerExists(eepIPCR(eepUID, eepName, "10.10.0.7", time.Now()))
	require.NoError(t, err)
	require.True(t, exists)

	// the lsp gc keeps ports tagged as endpoint ports
	previousLastNoPodLSP := lastNoPodLSP
	lastNoPodLSP = strset.New(eepUID, "orphan")
	t.Cleanup(func() { lastNoPodLSP = previousLastNoPodLSP })
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{
		{Name: eepUID, ExternalIDs: map[string]string{externalEndpointExternalIDKey: eepNs + "/" + eepName}},
		{Name: "orphan", ExternalIDs: map[string]string{"pod": "tenant/gone"}},
	}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort("orphan").Return(nil)
	require.NoError(t, ctrl.markAndCleanLSP())

	// the chassis gc still keeps the hypervisors the kept ports are pinned to
	fc.mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, nil, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{
		{Name: eepUID, Options: map[string]string{"requested-chassis": "hv-kept"}},
	}, nil)
	fc.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-stale", Hostname: "gone"}, {Name: "c-kept", Hostname: "hv-kept"}}, nil)
	fc.mockOvnSbClient.EXPECT().DeleteChassis("chassis-stale").Return(nil)
	require.NoError(t, ctrl.gcChassis())
}

func TestExternalEndpointPortUpToDate(t *testing.T) {
	dhcp := &ovs.DHCPOptionsUUIDs{DHCPv4OptionsUUID: eepDHCPv4ID}
	base := func() *ovnnb.LogicalSwitchPort {
		return &ovnnb.LogicalSwitchPort{
			Addresses:     []string{"52:54:00:00:00:07 10.10.0.7"},
			PortSecurity:  []string{"52:54:00:00:00:07 10.10.0.7"},
			Dhcpv4Options: new(eepDHCPv4ID),
			Options:       map[string]string{"requested-chassis": eepChassis},
			ExternalIDs: map[string]string{
				ovs.LogicalSwitchKey: eepSubnet, externalEndpointExternalIDKey: eepNs + "/" + eepName,
				"associated_sg_sg-a": "true", "associated_sg_old": "false",
			},
		}
	}
	upToDate := func(lsp *ovnnb.LogicalSwitchPort, portSecurity, enableDHCP bool, sgs ...string) bool {
		return externalEndpointPortUpToDate(lsp, eepNs+"/"+eepName, eepSubnet, "10.10.0.7", "52:54:00:00:00:07", eepChassis, portSecurity, enableDHCP, dhcp, sgs)
	}

	require.True(t, upToDate(base(), true, true, "sg-a"))
	require.False(t, upToDate(base(), true, true), "security group removed")
	require.False(t, upToDate(base(), true, true, "sg-a", "sg-b"), "security group added")
	require.False(t, upToDate(base(), false, true, "sg-a"), "port security disabled")
	require.False(t, upToDate(base(), true, false, "sg-a"), "dhcp disabled")

	lsp := base()
	lsp.Addresses = []string{"52:54:00:00:00:07 10.10.0.8"}
	require.False(t, upToDate(lsp, true, true, "sg-a"), "address drift")

	lsp = base()
	lsp.Dhcpv4Options = new("stale")
	require.False(t, upToDate(lsp, true, true, "sg-a"), "stale dhcp options")

	lsp = base()
	lsp.ExternalIDs[ovs.LogicalSwitchKey] = "other"
	require.False(t, upToDate(lsp, true, true, "sg-a"), "moved to another switch")

	lsp = base()
	delete(lsp.Options, "requested-chassis")
	require.False(t, upToDate(lsp, true, true, "sg-a"), "chassis pin removed")

	lsp = base()
	delete(lsp.ExternalIDs, externalEndpointExternalIDKey)
	require.False(t, upToDate(lsp, true, true, "sg-a"), "owner missing")
}

func TestReleaseOrphanedExternalEndpointAddress(t *testing.T) {
	// the endpoint is gone (deleted without its finalizer), then its IP CR is deleted
	fc := newEEPController(t, newEEPSubnet())
	ctrl, nb := fc.fakeController, fc.mockOvnClient
	ctrl.updateSubnetStatusQueue = newTypedRateLimitingQueue[string]("UpdateSubnetStatus", nil)
	gone := newEEP()
	_, _, _, err := ctrl.ipam.GetStaticAddress(externalEndpointIPAMKey(eepNs, eepName), eepUID, "10.10.0.7", nil, eepSubnet, true)
	require.NoError(t, err)
	ipCR := eepIPCR(eepUID, eepName, "10.10.0.7", time.Now())
	ipCR.Labels = map[string]string{util.ExternalEndpointUIDLabel: string(gone.UID)}

	nb.EXPECT().GetLogicalSwitchPort(eepUID, true).Return(&ovnnb.LogicalSwitchPort{
		Name: eepUID, ExternalIDs: map[string]string{externalEndpointExternalIDKey: eepNs + "/" + eepName},
	}, nil)
	nb.EXPECT().DeleteLogicalSwitchPort(eepUID).Return(nil)
	require.NoError(t, ctrl.releaseOrphanedExternalEndpointAddress(ipCR))
	_, assigned := ctrl.ipam.IsIPAssignedToOtherPod("10.10.0.7", eepSubnet, "tenant/someone")
	require.False(t, assigned, "the address is released")
}

func TestReleaseOrphanedExternalEndpointAddressKeepsLiveEndpoint(t *testing.T) {
	ep := newEEP()
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl := fc.fakeController
	_, _, _, err := ctrl.ipam.GetStaticAddress(externalEndpointIPAMKey(eepNs, eepName), eepUID, "10.10.0.7", nil, eepSubnet, true)
	require.NoError(t, err)
	ipCR := eepIPCR(eepUID, eepName, "10.10.0.7", time.Now())
	ipCR.Labels = map[string]string{util.ExternalEndpointUIDLabel: string(ep.UID)}

	// no NB expectation: the live endpoint keeps its port and recreates the IP CR
	require.NoError(t, ctrl.releaseOrphanedExternalEndpointAddress(ipCR))
	_, assigned := ctrl.ipam.IsIPAssignedToOtherPod("10.10.0.7", eepSubnet, "tenant/someone")
	require.True(t, assigned)
}

func TestRestoreExternalEndpointsToIPAMFallsBackToStatus(t *testing.T) {
	// the IP CR is missing at startup while the port still serves the status address
	ep := newEEP(func(ep *kubeovnv1.ExternalEndpoint) {
		ep.Status = kubeovnv1.ExternalEndpointStatus{InterfaceID: eepUID, V4IP: "10.10.0.9", MacAddress: "52:54:00:00:00:09"}
	})
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl := fc.fakeController
	ctrl.restoreExternalEndpointsToIPAM(nil)

	owner, assigned := ctrl.ipam.IsIPAssignedToOtherPod("10.10.0.9", eepSubnet, "tenant/someone")
	require.True(t, assigned)
	require.Equal(t, externalEndpointIPAMKey(eepNs, eepName), owner)
}

func TestHandleUpdateIPKeepsExternalEndpointIPCRWhileDisabled(t *testing.T) {
	now := metav1.Now()
	ep := newEEP()
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.createOrUpdateExternalEndpointIPCR(ep, eepSubnet, eepUID, "10.10.0.7", "52:54:00:00:00:07"))
	ipCR, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), eepUID, metav1.GetOptions{})
	require.NoError(t, err)
	ipCR.DeletionTimestamp = &now
	require.NoError(t, fc.fakeInformers.ipInformer.Informer().GetIndexer().Update(ipCR))
	ctrl.config.EnableExternalEndpoint = false

	// no NB expectation, and the finalizer stays: the address is restored from this IP CR at the next start
	require.NoError(t, ctrl.handleUpdateIP(eepUID))
	ipCR, err = ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), eepUID, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, ipCR.Finalizers, util.KubeOVNControllerFinalizer)
}

func TestHandleAddReservedIPSkipsExternalEndpoint(t *testing.T) {
	ep := newEEP()
	fc := newEEPController(t, newEEPSubnet(), ep)
	ctrl := fc.fakeController
	ipCR := eepIPCR(eepUID, eepName, "10.10.0.7", time.Now())
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), ipCR, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeInformers.ipInformer.Informer().GetIndexer().Add(ipCR))
	// no NB expectation and no error: the endpoint handler owns this IP CR
	require.NoError(t, ctrl.handleAddReservedIP(eepUID))
}
