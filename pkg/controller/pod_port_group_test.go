package controller

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestReconcileRouteSubnetsPortGroups(t *testing.T) {
	for _, gateway := range []string{kubeovnv1.GWCentralizedType, kubeovnv1.GWDistributedType, "u2o"} {
		for _, interrupted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/interrupted=%t", gateway, interrupted), func(t *testing.T) {
				fc, pod, subnet := newRoutePortGroupFixture(t, gateway)
				const portName = "protected.sg-repro"
				var moveErr error
				if interrupted {
					moveErr = errors.New("NB connection interrupted")
				}
				nb := fc.mockOvnClient.EXPECT()
				if gateway != "u2o" {
					// Only actual members in the node scope may move. Subnet and
					// security groups must not be cleared, including on a retry.
					nodeErr := moveErr
					if gateway == kubeovnv1.GWDistributedType {
						nodeErr = nil
					}
					nb.MovePortToPortGroup(gomock.Any(), portName, "node.one",
						map[string]string{"node": "", networkPolicyKey: ""}, "subnet").Return(nodeErr)
				}
				if gateway == kubeovnv1.GWCentralizedType && !interrupted {
					pgName := serviceProxyFallbackPortGroupName(subnet.Name, "one")
					port := ovnnb.LogicalSwitchPort{UUID: "pod-port", Name: portName}
					nb.GetLogicalSwitchPort(portName, false).Return(&port, nil)
					nb.ListPortGroups(buildPolicyRouteExternalIDs(subnet.Name, map[string]string{serviceProxyFallbackPortGroupKey: "true"})).Return(nil, nil)
					nb.CreatePortGroup(pgName, map[string]string{serviceProxyFallbackPortGroupKey: "true", "subnet": subnet.Name, "node": "one"}).Return(nil)
					nb.GetPortGroup(pgName, false).Return(&ovnnb.PortGroup{Name: pgName}, nil)
					nb.GetPortGroup("node.one", true).Return(&ovnnb.PortGroup{Ports: []string{port.UUID}}, nil)
					nb.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: subnet.Name}, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{port}, nil)
					nb.PortGroupSetPorts(pgName, []string{portName}).Return(nil)
				}
				if gateway != kubeovnv1.GWCentralizedType {
					scope := map[string]string{"subnet": subnet.Name, "node": "", networkPolicyKey: ""}
					if gateway == "u2o" {
						scope = map[string]string{logicalRouterKey: subnet.Spec.Vpc, logicalSwitchKey: subnet.Name, u2oKey: "true"}
					}
					nb.MovePortToPortGroup(gomock.Any(), portName, "ovn.default.one", scope).Return(moveErr)
				}

				err := fc.fakeController.reconcileRouteSubnets(pod, []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})
				require.ErrorIs(t, err, moveErr)
				routed, err := fc.kubeClient.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
				require.NoError(t, err)
				if interrupted {
					require.Empty(t, routed.Annotations[util.RoutedAnnotation])
				} else {
					require.Equal(t, "true", routed.Annotations[util.RoutedAnnotation])
				}
			})
		}
	}
}

func TestReconcileRouteSubnetsPortGroupsProvider(t *testing.T) {
	for _, gateway := range []string{kubeovnv1.GWCentralizedType, kubeovnv1.GWDistributedType, "u2o"} {
		t.Run(gateway, func(t *testing.T) {
			fc, pod, subnet := newRoutePortGroupFixture(t, gateway)
			client := newInMemoryOVNNbClient(t)
			fc.fakeController.OVNNbTables = client
			const portName = "protected.sg-repro"
			require.NoError(t, client.CreateBareLogicalSwitch(subnet.Name))
			require.NoError(t, client.CreateBareLogicalSwitchPort(subnet.Name, portName, "10.244.1.10", "00:00:00:00:00:02"))
			port, err := client.GetLogicalSwitchPort(portName, false)
			require.NoError(t, err)
			subnetIDs := map[string]string{"node": "one", networkPolicyKey: subnet.Name + "/one", "subnet": subnet.Name}
			if gateway == "u2o" {
				subnetIDs = map[string]string{logicalRouterKey: subnet.Spec.Vpc, logicalSwitchKey: subnet.Name, u2oKey: "true"}
			}
			for _, pg := range []ovnnb.PortGroup{
				{Name: "node.one", ExternalIDs: map[string]string{"node": "one", networkPolicyKey: "node/one"}},
				{Name: "ovn.default.one", ExternalIDs: subnetIDs},
				{Name: "security.group"},
				{Name: "deny.all"},
			} {
				require.NoError(t, client.CreatePortGroup(pg.Name, pg.ExternalIDs))
				require.NoError(t, client.PortGroupAddPorts(pg.Name, portName))
			}

			// No stale routing groups exist. An unguarded empty removal would
			// also erase security-group and deny-all membership through Provider.
			require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}))
			for _, name := range []string{"node.one", "ovn.default.one", "security.group", "deny.all"} {
				pg, err := client.GetPortGroup(name, false)
				require.NoError(t, err)
				require.Contains(t, pg.Ports, port.UUID, "port group %s must retain its member", name)
			}
			routed, err := fc.kubeClient.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, "true", routed.Annotations[util.RoutedAnnotation])
		})
	}
}

func newRoutePortGroupFixture(t *testing.T, gateway string) (*fakeController, *corev1.Pod, *kubeovnv1.Subnet) {
	t.Helper()
	pod := &corev1.Pod{
		Name: "protected", Namespace: "sg-repro", Spec: corev1.PodSpec{NodeName: "one"},
		Annotations: map[string]string{
			util.AllocatedAnnotation: "true", util.IPAddressAnnotation: "10.244.1.10",
			util.LogicalSwitchAnnotation: "ovn-default", util.PortSecurityAnnotation: "true",
		},
	}
	node := &corev1.Node{Name: "one", Annotations: map[string]string{
		util.PortNameAnnotation: "node-one", util.IPAddressAnnotation: "100.64.0.2",
	}}
	subnet := &kubeovnv1.Subnet{Name: "ovn-default", Spec: kubeovnv1.SubnetSpec{
		Vpc: util.DefaultVpc, Provider: util.OvnProvider, GatewayType: gateway,
	}}
	if gateway == "u2o" {
		subnet.Spec.GatewayType = kubeovnv1.GWDistributedType
		subnet.Spec.Vlan = "vlan1"
		subnet.Spec.U2OInterconnection = true
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod}, Nodes: []*corev1.Node{node}, Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableEipSnat = false
	fc.fakeController.config.EnableOvnLB = false
	return fc, pod, subnet
}
