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
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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

func TestReconcileAllocateSubnetsSecurityGroups(t *testing.T) {
	for _, backend := range []string{"tables", "legacy"} {
		for _, provider := range []string{util.OvnProvider, "net1.default.ovn"} {
			for _, tc := range []struct {
				name       string
				oldGroups  string
				newGroups  string
				wantGroups []string
			}{
				{name: "replace", oldGroups: "old", newGroups: "new", wantGroups: []string{"new"}},
				{name: "remove all", oldGroups: "old"},
				{name: "overlap", oldGroups: "old,shared", newGroups: "new,shared", wantGroups: []string{"new", "shared"}},
				{name: "unchanged", oldGroups: "shared", newGroups: "shared", wantGroups: []string{"shared"}},
				{name: "new port", newGroups: "new", wantGroups: []string{"new"}},
			} {
				t.Run(backend+"/"+provider+"/"+tc.name, func(t *testing.T) {
					pod, subnet := podEventFixture()
					pod.Name = "test-sts-0"
					pod.OwnerReferences = []metav1.OwnerReference{{
						APIVersion: "apps/v1", Kind: util.KindStatefulSet, Name: "test-sts", UID: "sts-uid", Controller: new(true),
					}}
					const ip = "10.0.0.10,fd00::10"
					const mac = "00:00:00:00:00:10"
					pod.Annotations = map[string]string{
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnet.Name,
						fmt.Sprintf(util.IPAddressAnnotationTemplate, provider):     ip,
						fmt.Sprintf(util.MacAddressAnnotationTemplate, provider):    mac,
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
					require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

					client := newInMemoryOVNNbClient(t)
					ctrl.OVNNbClient = client
					if backend == "tables" {
						ctrl.OVNNbTables = client
					}
					require.NoError(t, client.CreateBareLogicalSwitch(subnet.Name))
					indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
					ctrl.sgsLister = kubeovnlisters.NewSecurityGroupLister(indexer)
					groups := []string{"old", "new", "shared"}
					for _, name := range groups {
						pgName := ovs.GetSgPortGroupName(name)
						require.NoError(t, indexer.Add(&kubeovnv1.SecurityGroup{Name: name, Status: kubeovnv1.SecurityGroupStatus{PortGroup: pgName}}))
						require.NoError(t, client.CreatePortGroup(pgName, nil))
						require.NoError(t, client.CreateAddressSet(ovs.GetSgV4AssociatedName(name), nil))
						require.NoError(t, client.CreateAddressSet(ovs.GetSgV6AssociatedName(name), nil))
					}

					portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
					var retainedPort *ovnnb.LogicalSwitchPort
					if tc.oldGroups != "" {
						// StatefulSet replacement pods inherit the existing LSP and its SG address sets.
						require.NoError(t, client.CreateLogicalSwitchPort(subnet.Name, portName, ip, mac, pod.Name, pod.Namespace, false, tc.oldGroups, "", false, &ovs.DHCPOptionsUUIDs{}, subnet.Spec.Vpc))
						retainedPort, err = client.GetLogicalSwitchPort(portName, false)
						require.NoError(t, err)
						for _, name := range groups {
							require.NoError(t, ctrl.syncSgLogicalPort(name))
						}
						oldAddresses, err := client.GetAddressSet(ovs.GetSgV4AssociatedName("old"), false)
						require.NoError(t, err)
						if tc.oldGroups != "shared" {
							require.Contains(t, oldAddresses.Addresses, "10.0.0.10")
						}
					}

					updatedPod, err := ctrl.reconcileAllocateSubnets(pod, []*kubeovnNet{{
						Type: providerTypeOriginal, ProviderName: provider, Subnet: subnet, IsDefault: provider == util.OvnProvider,
					}})
					require.NoError(t, err)
					require.Equal(t, ip, updatedPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)])
					for ctrl.syncSgPortsQueue.Len() != 0 {
						name, shutdown := ctrl.syncSgPortsQueue.Get()
						require.False(t, shutdown)
						err := ctrl.syncSgLogicalPort(name)
						ctrl.syncSgPortsQueue.Done(name)
						require.NoError(t, err)
					}

					port, err := client.GetLogicalSwitchPort(portName, false)
					require.NoError(t, err)
					if retainedPort != nil {
						require.Equal(t, retainedPort.UUID, port.UUID)
					}
					for _, name := range groups {
						v4, err := client.GetAddressSet(ovs.GetSgV4AssociatedName(name), false)
						require.NoError(t, err)
						v6, err := client.GetAddressSet(ovs.GetSgV6AssociatedName(name), false)
						require.NoError(t, err)
						pg, err := client.GetPortGroup(ovs.GetSgPortGroupName(name), false)
						require.NoError(t, err)
						if slices.Contains(tc.wantGroups, name) {
							require.Equal(t, []string{"10.0.0.10"}, v4.Addresses, "security group %s", name)
							require.Equal(t, []string{"fd00::10"}, v6.Addresses, "security group %s", name)
							require.Equal(t, []string{port.UUID}, pg.Ports, "security group %s", name)
						} else {
							require.Empty(t, v4.Addresses, "security group %s must lose the pod's IPv4 address", name)
							require.Empty(t, v6.Addresses, "security group %s must lose the pod's IPv6 address", name)
							require.Empty(t, pg.Ports, "security group %s must lose the pod's port", name)
						}
					}
				})
			}
		}
	}
}
