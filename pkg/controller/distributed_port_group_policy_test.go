package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestPeriodicDistributedPortGroupsPreservesPerFamilyGatewayRoutes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name            string
		eip             string
		snat            string
		northGateway    string
		externalGateway string
		disableEIP      bool
		wantRepair      bool
	}{
		{name: "IPv4 EIP leaves IPv6 on subnet gateway", eip: "203.0.113.2", wantRepair: true},
		{name: "IPv6 SNAT leaves IPv4 on subnet gateway", snat: "fd01::2", wantRepair: true},
		{name: "IPv4 north gateway leaves IPv6 on subnet gateway", northGateway: "192.0.2.1", wantRepair: true},
		{name: "IPv6 north gateway leaves IPv4 on subnet gateway", northGateway: "fd03::1", wantRepair: true},
		{name: "both families use north gateway", northGateway: "192.0.2.1,fd03::1"},
		{name: "both families use external gateway", eip: "203.0.113.2", snat: "fd01::2"},
		{name: "EIP and north gateway cover different families", eip: "203.0.113.2", northGateway: "fd03::1"},
		{name: "disabled EIP leaves both families on subnet gateway", eip: "203.0.113.2", snat: "fd01::2", disableEIP: true, wantRepair: true},
		{name: "external gateway lacks the SNAT address family", eip: "203.0.113.2", snat: "fd01::2", externalGateway: "203.0.113.1", wantRepair: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pod := distributedPortGroupTestPod("dual-stack", "ovn-default", "node-c")
			pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2,fd00::2"
			pod.Annotations[util.AllocatedAnnotation] = "true"
			pod.Annotations[util.RoutedAnnotation] = "true"
			pod.Annotations[util.EipAnnotation] = tt.eip
			pod.Annotations[util.SnatAnnotation] = tt.snat
			pod.Annotations[util.NorthGatewayAnnotation] = tt.northGateway
			subnet := &kubeovnv1.Subnet{
				Name: "ovn-default",
				Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, Provider: util.OvnProvider, GatewayType: kubeovnv1.GWDistributedType},
			}
			nextHop := tt.externalGateway
			if nextHop == "" {
				nextHop = "203.0.113.1,fd01::1"
			}
			cm := &corev1.ConfigMap{
				Name: util.ExternalGatewayConfig, Namespace: metav1.NamespaceSystem,
				Data: map[string]string{"external-gw-addr": nextHop},
			}
			fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}, ConfigMaps: []*corev1.ConfigMap{cm},
			})
			require.NoError(t, err)
			ctrl := fixture.fakeController
			ctrl.config.EnableEipSnat = !tt.disableEIP
			ctrl.config.ExternalGatewayConfigNS = metav1.NamespaceSystem
			t.Cleanup(ctrl.addOrUpdateSubnetQueue.ShutDown)
			port := distributedPortGroupTestLSP(pod.Name, subnet.Name)
			group := distributedPortGroupTestGroup(subnet.Name, pod.Spec.NodeName)
			nb := fixture.mockOvnClient.EXPECT()
			nb.GetSubnetPortGroupMembership(t.Context(), subnet.Name).
				Return([]ovnnb.PortGroup{group}, []ovnnb.LogicalSwitchPort{port}, nil)
			if tt.wantRepair {
				nb.ApplyPortGroupMembershipChanges(t.Context(), []ovs.PortGroupMembershipChange{
					{Group: group, Add: []string{port.UUID}},
				}, []string{port.UUID}).Return(nil)
			}

			// The routed Pod gets no new event when the membership disappears.
			// The periodic sweep must preserve routing for every uncovered family.
			ctrl.syncDistributedSubnetPortGroups(t.Context())
			require.Zero(t, ctrl.addOrUpdateSubnetQueue.NumRequeues(subnet.Name))
			current, err := fixture.kubeClient.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, "true", current.Annotations[util.RoutedAnnotation])
		})
	}
}
