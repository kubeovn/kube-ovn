package controller

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestReconcileAllocateSubnetsSecurityGroupLookupFailure(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	injectedErr := errors.New("cannot read existing port")
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(nil, injectedErr)
	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	updatedPod, err := ctrl.reconcileAllocateSubnets(pod, []*kubeovnNet{{
		Type: providerTypeOriginal, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true,
	}})
	require.Nil(t, updatedPod)
	require.ErrorIs(t, err, injectedErr)
	assertPodEvent(t, ctrl, "Warning PodNetworkAllocationFailed", "stage=getLogicalSwitchPort", injectedErr.Error())
}

type podSecurityGroupCase struct {
	name       string
	oldGroups  string
	newGroups  string
	wantGroups []string
}

func TestReconcileAllocateSubnetsSecurityGroups(t *testing.T) {
	for _, provider := range []string{util.OvnProvider, "net1.default.ovn"} {
		for _, tc := range []podSecurityGroupCase{
			{name: "replace", oldGroups: "old", newGroups: "new", wantGroups: []string{"new"}},
			{name: "remove all", oldGroups: "old"},
			{name: "overlap", oldGroups: "old,shared", newGroups: "new,shared", wantGroups: []string{"new", "shared"}},
			{name: "unchanged", oldGroups: "shared", newGroups: "shared", wantGroups: []string{"shared"}},
			{name: "new port", newGroups: "new", wantGroups: []string{"new"}},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				fixture := newPodSecurityGroupFixture(t, provider, tc)
				fixture.reconcile(t)
				fixture.syncGroups(t)
				fixture.assertGroups(t, tc.wantGroups)
			})
		}
	}
}

func TestReconcileAllocateSubnetsSecurityGroupsLayer2Retry(t *testing.T) {
	for _, tc := range []podSecurityGroupCase{
		{name: "replace", oldGroups: "old", newGroups: "new", wantGroups: []string{"new"}},
		{name: "remove all", oldGroups: "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPodSecurityGroupFixture(t, util.OvnProvider, tc)
			fixture.pod.Annotations[fmt.Sprintf(util.Layer2ForwardAnnotationTemplate, util.OvnProvider)] = "true"
			injectedErr := errors.New("enable layer2 forward failed once")
			fixture.ctrl.OVNNbClient = &layer2FailureClient{NbClient: fixture.client, err: injectedErr}

			updatedPod, err := fixture.ctrl.reconcileAllocateSubnets(fixture.pod, []*kubeovnNet{fixture.network})
			require.Nil(t, updatedPod)
			require.ErrorIs(t, err, injectedErr)
			port, err := fixture.client.GetLogicalSwitchPort(fixture.portName, false)
			require.NoError(t, err)
			groups, _ := fixture.ctrl.getPortSg(port)
			require.ElementsMatch(t, tc.wantGroups, groups, "the failed attempt already replaced the old SG external IDs")
			fixture.syncGroups(t)
			fixture.reconcile(t)
			fixture.syncGroups(t)
			fixture.assertGroups(t, tc.wantGroups)
			port, err = fixture.client.GetLogicalSwitchPort(fixture.portName, false)
			require.NoError(t, err)
			require.Contains(t, port.Addresses, "unknown", "retry must complete the L2 update")
		})
	}
}

type podSecurityGroupFixture struct {
	ctrl         *Controller
	client       *ovs.OVNNbClient
	pod          *corev1.Pod
	network      *kubeovnNet
	portName     string
	retainedUUID string
}

func newPodSecurityGroupFixture(t *testing.T, provider string, tc podSecurityGroupCase) *podSecurityGroupFixture {
	t.Helper()
	pod, subnet := podEventFixture()
	pod.Name = "test-sts-0"
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "apps/v1", Kind: util.KindStatefulSet, Name: "test-sts", UID: "sts-uid", Controller: new(true),
	}}
	pod.Annotations = map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnet.Name,
		fmt.Sprintf(util.IPAddressAnnotationTemplate, provider):     "10.0.0.10,fd00::10",
		fmt.Sprintf(util.MacAddressAnnotationTemplate, provider):    "00:00:00:00:00:10",
		fmt.Sprintf(util.SecurityGroupAnnotationTemplate, provider): tc.newGroups,
	}
	subnet.Spec.CIDRBlock = "10.0.0.0/24,fd00::/64"
	subnet.Spec.Gateway = "10.0.0.1,fd00::1"
	subnet.Spec.Protocol = kubeovnv1.ProtocolDual
	subnet.Spec.Provider = provider
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.sgKeyMutex = keymutex.NewHashed(0)
	ctrl.syncSgPortsQueue = newTypedRateLimitingQueue[string]("test-sync-sg", nil)
	t.Cleanup(ctrl.syncSgPortsQueue.ShutDown)
	ctrl.addOrUpdateSgQueue = newTypedRateLimitingQueue[string]("test-update-sg", nil)
	t.Cleanup(ctrl.addOrUpdateSgQueue.ShutDown)
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	client := newInMemoryOVNNbClient(t)
	ctrl.OVNNbClient = client
	require.NoError(t, client.CreateBareLogicalSwitch(subnet.Name))
	fixture := &podSecurityGroupFixture{
		ctrl: ctrl, client: client, pod: pod,
		network:  &kubeovnNet{Type: providerTypeOriginal, ProviderName: provider, Subnet: subnet, IsDefault: provider == util.OvnProvider},
		portName: ovs.PodNameToPortName(pod.Name, pod.Namespace, provider),
	}
	fixture.createGroups(t)
	if tc.oldGroups != "" {
		// StatefulSet replacement pods inherit the existing LSP and its SG address sets.
		require.NoError(t, client.CreateLogicalSwitchPort(subnet.Name, fixture.portName, "10.0.0.10,fd00::10", "00:00:00:00:00:10", pod.Name, pod.Namespace, false, tc.oldGroups, "", false, &ovs.DHCPOptionsUUIDs{}, subnet.Spec.Vpc))
		port, err := client.GetLogicalSwitchPort(fixture.portName, false)
		require.NoError(t, err)
		fixture.retainedUUID = port.UUID
		for _, name := range []string{"old", "new", "shared"} {
			require.NoError(t, ctrl.syncSgLogicalPort(name))
		}
		oldAddresses, err := client.GetAddressSet(ovs.GetSgV4AssociatedName("old"), false)
		require.NoError(t, err)
		if tc.oldGroups != "shared" {
			require.Contains(t, oldAddresses.Addresses, "10.0.0.10")
		}
	}
	return fixture
}

func (f *podSecurityGroupFixture) createGroups(t *testing.T) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	f.ctrl.sgsLister = kubeovnlisters.NewSecurityGroupLister(indexer)
	for _, name := range []string{"old", "new", "shared"} {
		pgName := ovs.GetSgPortGroupName(name)
		require.NoError(t, indexer.Add(&kubeovnv1.SecurityGroup{Name: name, Status: kubeovnv1.SecurityGroupStatus{PortGroup: pgName}}))
		require.NoError(t, f.client.CreatePortGroup(pgName, nil))
		require.NoError(t, f.client.CreateAddressSet(ovs.GetSgV4AssociatedName(name), nil))
		require.NoError(t, f.client.CreateAddressSet(ovs.GetSgV6AssociatedName(name), nil))
	}
}

func (f *podSecurityGroupFixture) reconcile(t *testing.T) {
	t.Helper()
	updatedPod, err := f.ctrl.reconcileAllocateSubnets(f.pod, []*kubeovnNet{f.network})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.10,fd00::10", updatedPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, f.network.ProviderName)])
}

func (f *podSecurityGroupFixture) syncGroups(t *testing.T) {
	t.Helper()
	for f.ctrl.syncSgPortsQueue.Len() != 0 {
		name, shutdown := f.ctrl.syncSgPortsQueue.Get()
		require.False(t, shutdown)
		err := f.ctrl.syncSgLogicalPort(name)
		f.ctrl.syncSgPortsQueue.Done(name)
		require.NoError(t, err)
	}
}

func (f *podSecurityGroupFixture) assertGroups(t *testing.T, wantGroups []string) {
	t.Helper()
	port, err := f.client.GetLogicalSwitchPort(f.portName, false)
	require.NoError(t, err)
	if f.retainedUUID != "" {
		require.Equal(t, f.retainedUUID, port.UUID)
	}
	for _, name := range []string{"old", "new", "shared"} {
		v4, err := f.client.GetAddressSet(ovs.GetSgV4AssociatedName(name), false)
		require.NoError(t, err)
		v6, err := f.client.GetAddressSet(ovs.GetSgV6AssociatedName(name), false)
		require.NoError(t, err)
		pg, err := f.client.GetPortGroup(ovs.GetSgPortGroupName(name), false)
		require.NoError(t, err)
		if slices.Contains(wantGroups, name) {
			require.Equal(t, []string{"10.0.0.10"}, v4.Addresses, "security group %s", name)
			require.Equal(t, []string{"fd00::10"}, v6.Addresses, "security group %s", name)
			require.Equal(t, []string{port.UUID}, pg.Ports, "security group %s", name)
		} else {
			require.Empty(t, v4.Addresses, "security group %s must lose the pod's IPv4 address", name)
			require.Empty(t, v6.Addresses, "security group %s must lose the pod's IPv6 address", name)
			require.Empty(t, pg.Ports, "security group %s must lose the pod's port", name)
		}
	}
}

type layer2FailureClient struct {
	ovs.NbClient
	err error
}

func (c *layer2FailureClient) EnablePortLayer2forward(name string) error {
	if c.err != nil {
		err := c.err
		c.err = nil
		return err
	}
	return c.NbClient.EnablePortLayer2forward(name)
}
