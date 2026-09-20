package controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// newInMemoryOVNNbClient serves an in-memory OVN northbound database so handlers run against real rows.
func newInMemoryOVNNbClient(t *testing.T) *ovs.OVNNbClient {
	t.Helper()

	nbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	serverModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	db := inmemory.NewDatabase(map[string]model.ClientDBModel{ovnnb.DatabaseName: nbModel, serverdb.Schema().Name: serverModel}, nil)
	nbDB, errs := model.NewDatabaseModel(ovnnb.Schema(), nbModel)
	require.Empty(t, errs)
	serverDB, errs := model.NewDatabaseModel(serverdb.Schema(), serverModel)
	require.Empty(t, errs)
	dbServer, err := server.NewOvsdbServer(db, nil, nbDB, serverDB)
	require.NoError(t, err)

	socket := filepath.Join(t.TempDir(), "nb.sock")
	go func() {
		if err := dbServer.Serve("unix", socket); err != nil {
			t.Error(err)
		}
	}()
	require.Eventually(t, dbServer.Ready, time.Second, 10*time.Millisecond)
	t.Cleanup(dbServer.Close)

	nbClient, err := ovs.NewOvnNbClient("unix:"+socket, 3, 3, 0, 1)
	require.NoError(t, err)
	t.Cleanup(nbClient.Close)
	return nbClient
}

// nodeWithJoinPolicy sets up a node that is being deleted, with its join address in the real IPAM and
// its logical switch port and reroute policy in the northbound database.
func nodeWithJoinPolicy(t *testing.T) (fc *fakeController, node *corev1.Node, match string) {
	t.Helper()

	node = &corev1.Node{Name: "node-a", UID: "deleted-node-uid"}
	portName := util.NodeLspName(node.Name)
	joinIP := "100.64.6.63"
	match = "ip4.dst == 172.15.2.16"
	subnet := &kubeovnv1.Subnet{Name: "join", Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "100.64.0.0/16", Gateway: "100.64.0.1", Vpc: util.DefaultVpc,
		Provider: util.OvnProvider, GatewayType: kubeovnv1.GWDistributedType,
	}}
	// the node owns its IP, so deleting the node also deletes the IP
	deletingIP := &kubeovnv1.IP{
		Name: portName, DeletionTimestamp: new(metav1.Now()),
		Finalizers:      []string{util.KubeOVNControllerFinalizer},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Node", Name: node.Name, UID: node.UID}},
		Spec:            kubeovnv1.IPSpec{PodName: node.Name, NodeName: node.Name, Subnet: subnet.Name, IPAddress: joinIP, V4IPAddress: joinIP},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}, IPs: []*kubeovnv1.IP{deletingIP}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)
	ctrl.deletingNodeObjMap = xsync.NewMap[string, *corev1.Node]()
	ctrl.deletingNodeObjMap.Store(node.Name, node)
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	v4, _, mac, err := ctrl.ipam.GetStaticAddress(portName, portName, joinIP, nil, subnet.Name, true)
	require.NoError(t, err)
	require.Equal(t, joinIP, v4)

	nbClient := newInMemoryOVNNbClient(t)
	ctrl.OVNNbClient = nbClient
	require.NoError(t, nbClient.CreateLogicalRouter(util.DefaultVpc))
	require.NoError(t, nbClient.CreateBareLogicalSwitch(subnet.Name))
	require.NoError(t, nbClient.CreateBareLogicalSwitchPort(subnet.Name, portName, joinIP, mac))
	externalIDs := map[string]string{"vendor": util.CniTypeName, "node": node.Name, "address-family": "4"}
	require.NoError(t, nbClient.AddLogicalRouterPolicy(util.DefaultVpc, util.NodeRouterPolicyPriority, match, ovnnb.LogicalRouterPolicyActionReroute, []string{joinIP}, nil, externalIDs))
	return fc, node, match
}

func TestHandleDeleteNodeRemovesPolicyWhenIPIsReleasedFirst(t *testing.T) {
	for _, ipHandlerFirst := range []bool{false, true} {
		name := "node cleanup runs alone"
		if ipHandlerFirst {
			name = "ip handler releases the join address during node cleanup"
		}
		t.Run(name, func(t *testing.T) {
			fc, node, match := nodeWithJoinPolicy(t)
			ctrl := fc.fakeController

			// the chassis step comes before the policy cleanup: run the IP handler there
			fc.mockOvnSbClient.EXPECT().DeleteChassisByHost(node.Name).DoAndReturn(func(string) error {
				if ipHandlerFirst {
					require.NoError(t, ctrl.handleUpdateIP(util.NodeLspName(node.Name)))
				}
				return nil
			})

			require.NoError(t, ctrl.handleDeleteNode(node.Name))

			remaining, err := ctrl.OVNNbClient.GetLogicalRouterPolicy(util.DefaultVpc, util.NodeRouterPolicyPriority, match, true)
			require.NoError(t, err)
			require.Empty(t, remaining, "node deletion must remove the reroute policy whatever the IP handler did first")
		})
	}
}

func TestDeletePolicyRouteForNodeKeepsPolicyOfNodeReusingJoinAddress(t *testing.T) {
	fc, node, _ := nodeWithJoinPolicy(t)
	ctrl := fc.fakeController
	portName := util.NodeLspName(node.Name)
	addresses := ctrl.ipam.GetPodAddress(portName)
	require.Len(t, addresses, 1)

	// the deleted node's join address is still read from IPAM while a replacement node already reuses it as nexthop
	replacementMatch := "ip4.dst == 172.15.2.17"
	replacementIDs := map[string]string{"vendor": util.CniTypeName, "node": "node-b", "address-family": "4"}
	require.NoError(t, ctrl.OVNNbClient.AddLogicalRouterPolicy(util.DefaultVpc, util.NodeRouterPolicyPriority, replacementMatch, ovnnb.LogicalRouterPolicyActionReroute, []string{addresses[0].IP}, nil, replacementIDs))

	require.NoError(t, ctrl.deletePolicyRouteForNode(node.Name))

	remaining, err := ctrl.OVNNbClient.GetLogicalRouterPolicy(util.DefaultVpc, util.NodeRouterPolicyPriority, replacementMatch, true)
	require.NoError(t, err)
	require.Len(t, remaining, 1, "a policy owned by another node must survive, even with the deleted node's join address as nexthop")
}

func TestHandleUpdateIPReleasesNodeAddressWithNodeKey(t *testing.T) {
	fc, node, _ := nodeWithJoinPolicy(t)
	ctrl := fc.fakeController
	portName := util.NodeLspName(node.Name)
	require.Len(t, ctrl.ipam.GetPodAddress(portName), 1)

	require.NoError(t, ctrl.handleUpdateIP(portName))

	// node addresses are allocated under the port name; releasing under another key left the
	// entry behind with a nil address, which reads back as the nexthop "<nil>"
	require.Empty(t, ctrl.ipam.GetPodAddress(portName))
}
