package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestServiceProxyFallbackPortGroupsIsolateDuplicateIPs(t *testing.T) {
	fc, subnet := newServiceProxyFallbackController(t)
	ctrl := fc.fakeController
	subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
	subnet.Spec.EnableLb = new(false)
	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetIndexer().Update(subnet))
	worker2 := &corev1.Node{Name: "worker-2", Annotations: map[string]string{util.IPAddressAnnotation: "100.64.0.3"}}
	require.NoError(t, fc.fakeInformers.nodeInformer.Informer().GetIndexer().Add(worker2))
	physical := subnet.DeepCopy()
	physical.Name, physical.Spec.Vlan = "physical", "vlan-100"
	require.NoError(t, util.ValidateCidrConflict(*physical, []kubeovnv1.Subnet{*subnet}))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitch(physical.Name))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitchPort(subnet.Name, "overlay-pod", "10.16.0.2,fd00:10:16::2", "00:00:00:00:16:02"))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitchPort(physical.Name, "physical-pod", "10.16.0.2,fd00:10:16::2", "00:00:00:00:18:02"))
	require.NoError(t, ctrl.OVNNbClient.CreatePortGroup("node.worker.1", map[string]string{"node": "worker-1"}))
	require.NoError(t, ctrl.OVNNbClient.CreatePortGroup("node.worker.2", map[string]string{"node": "worker-2"}))
	require.NoError(t, ctrl.OVNNbClient.PortGroupAddPorts("node.worker.1", "overlay-pod"))
	require.NoError(t, ctrl.OVNNbClient.PortGroupAddPorts("node.worker.2", "physical-pod"))
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
	port, err := ctrl.OVNNbClient.GetLogicalSwitchPort("overlay-pod", false)
	require.NoError(t, err)
	assertPorts := func(nodeName string, want []string) {
		t.Helper()
		pg, err := ctrl.OVNNbClient.GetPortGroup(serviceProxyFallbackPortGroupName(subnet.Name, nodeName), false)
		require.NoError(t, err)
		require.ElementsMatch(t, want, pg.Ports)
		require.Empty(t, pg.ACLs)
	}
	assertPorts("worker-1", []string{port.UUID})
	assertPorts("worker-2", nil)

	// Migration must refresh the old and new subnet-node groups immediately.
	require.NoError(t, ctrl.OVNNbClient.PortGroupRemovePorts("node.worker.1", "overlay-pod"))
	require.NoError(t, ctrl.OVNNbClient.PortGroupAddPorts("node.worker.2", "overlay-pod"))
	require.NoError(t, ctrl.syncServiceProxyFallbackPodPort(subnet, "worker-2", "overlay-pod"))
	assertPorts("worker-1", nil)
	assertPorts("worker-2", []string{port.UUID})

	// A later node-group rebuild must also converge the derived membership.
	require.NoError(t, ctrl.OVNNbClient.PortGroupRemovePorts("node.worker.2", "overlay-pod"))
	require.NoError(t, ctrl.syncServiceProxyFallbackPortGroups())
	assertPorts("worker-2", nil)
	physicalGroup, err := ctrl.OVNNbClient.GetPortGroup("node.worker.2", false)
	require.NoError(t, err)
	require.Len(t, physicalGroup.Ports, 1)
}

func TestServiceProxyFallbackPortGroupLifecycle(t *testing.T) {
	for _, transition := range []string{"lb-enabled", "distributed", "physical", "delete", "orphan-subnet", "orphan-node", "join-address-removed"} {
		t.Run(transition, func(t *testing.T) {
			fc, subnet := newServiceProxyFallbackController(t)
			ctrl := fc.fakeController
			subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
			subnet.Spec.EnableLb = new(false)
			subnetIndexer := fc.fakeInformers.subnetInformer.Informer().GetIndexer()
			require.NoError(t, subnetIndexer.Update(subnet))
			require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
			pgName := serviceProxyFallbackPortGroupName(subnet.Name, "worker-1")
			pg, err := ctrl.OVNNbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.NotNil(t, pg)
			require.NoError(t, ctrl.OVNNbClient.CreatePortGroup("foreign", map[string]string{"subnet": subnet.Name}))
			switch transition {
			case "lb-enabled":
				subnet.Spec.EnableLb = new(true)
			case "distributed":
				subnet.Spec.GatewayType = kubeovnv1.GWDistributedType
			case "physical":
				subnet.Spec.Vlan = "vlan-100"
			case "orphan-subnet":
				require.NoError(t, subnetIndexer.Delete(subnet))
			case "orphan-node", "join-address-removed":
				node, err := ctrl.nodesLister.Get("worker-1")
				require.NoError(t, err)
				nodeIndexer := fc.fakeInformers.nodeInformer.Informer().GetIndexer()
				if transition == "orphan-node" {
					require.NoError(t, nodeIndexer.Delete(node))
				} else {
					node = node.DeepCopy()
					delete(node.Annotations, util.IPAddressAnnotation)
					require.NoError(t, nodeIndexer.Update(node))
				}
			}
			switch transition {
			case "delete":
				require.NoError(t, ctrl.deleteServiceProxyFallbackPolicies(subnet, "isServiceProxyFallbackReroutePolicy"))
			case "orphan-subnet", "orphan-node", "join-address-removed":
				require.NoError(t, ctrl.syncServiceProxyFallbackPortGroups())
			default:
				require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
			}
			pg, err = ctrl.OVNNbClient.GetPortGroup(pgName, true)
			require.NoError(t, err)
			require.Nil(t, pg)
			require.Empty(t, serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy"))
			foreign, err := ctrl.OVNNbClient.GetPortGroup("foreign", false)
			require.NoError(t, err)
			require.NotNil(t, foreign)
		})
	}
}
