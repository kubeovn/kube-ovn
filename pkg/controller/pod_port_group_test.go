package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestReconcileRouteSubnetsPortGroups(t *testing.T) {
	for _, gateway := range []string{kubeovnv1.GWCentralizedType, kubeovnv1.GWDistributedType, "u2o"} {
		t.Run(gateway, func(t *testing.T) {
			for _, tc := range []struct {
				name             string
				currentGroups    bool
				otherNodeGroup   bool
				otherSubnetGroup bool
			}{
				{name: "no groups"},
				{name: "current groups only", currentGroups: true},
				{name: "other node group only", currentGroups: true, otherNodeGroup: true},
				{name: "other subnet group only", currentGroups: true, otherSubnetGroup: true},
				{name: "both other groups", currentGroups: true, otherNodeGroup: true, otherSubnetGroup: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					fc, pod, subnet := newRoutePortGroupFixture(t, gateway)
					const portName = "protected.sg-repro"
					var nodeGroups, subnetGroups []ovnnb.PortGroup
					if tc.currentGroups {
						nodeGroups = append(nodeGroups, ovnnb.PortGroup{Name: "node.one"})
						subnetGroups = append(subnetGroups, ovnnb.PortGroup{Name: "ovn.default.one", ExternalIDs: map[string]string{"subnet": subnet.Name}})
					}
					if tc.otherNodeGroup {
						nodeGroups = append(nodeGroups, ovnnb.PortGroup{Name: "node.other"})
					}
					if tc.otherSubnetGroup {
						subnetGroups = append(subnetGroups, ovnnb.PortGroup{Name: "ovn.default.other", ExternalIDs: map[string]string{"subnet": subnet.Name}})
					}
					// The node/np query also returns subnet groups. They must not be
					// mistaken for stale node groups, even when the latter are empty.
					nodeGroups = append(nodeGroups, subnetGroups...)
					fc.mockOvnClient.EXPECT().ListPortGroups(map[string]string{"node": "", networkPolicyKey: ""}).Return(nodeGroups, nil)
					fc.mockOvnClient.EXPECT().ListPortGroups(map[string]string{"subnet": subnet.Name, "node": "", networkPolicyKey: ""}).Return(subnetGroups, nil)

					// An empty variadic list means remove from ALL groups, including
					// security groups and deny-all. Never permit that call during routing.
					if gateway != "u2o" {
						if tc.otherNodeGroup {
							fc.mockOvnClient.EXPECT().RemovePortFromPortGroups(portName, "node.other").Return(nil)
						}
						fc.mockOvnClient.EXPECT().PortGroupAddPorts("node.one", portName).Return(nil)
					}
					if gateway != kubeovnv1.GWCentralizedType {
						if tc.otherSubnetGroup {
							fc.mockOvnClient.EXPECT().RemovePortFromPortGroups(portName, "ovn.default.other").Return(nil)
						}
						fc.mockOvnClient.EXPECT().PortGroupAddPorts("ovn.default.one", portName).Return(nil)
					}

					err := fc.fakeController.reconcileRouteSubnets(pod, []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})
					require.NoError(t, err)
					routed, err := fc.fakeController.config.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
					require.NoError(t, err)
					require.Equal(t, "true", routed.Annotations[util.RoutedAnnotation])
				})
			}
		})
	}
}

func newRoutePortGroupFixture(t *testing.T, gateway string) (*fakeController, *corev1.Pod, *kubeovnv1.Subnet) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "protected", Namespace: "sg-repro",
			Annotations: map[string]string{
				util.AllocatedAnnotation: "true", util.IPAddressAnnotation: "10.244.1.10",
				util.LogicalSwitchAnnotation: "ovn-default", util.PortSecurityAnnotation: "true",
			},
		},
		Spec: corev1.PodSpec{NodeName: "one"},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "one", Annotations: map[string]string{
		util.PortNameAnnotation: "node-one", util.IPAddressAnnotation: "100.64.0.2",
	}}}
	subnet := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "ovn-default"}, Spec: kubeovnv1.SubnetSpec{
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
	return fc, pod, subnet
}
