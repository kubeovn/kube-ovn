package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVpcEndpointNaming(t *testing.T) {
	require.Equal(t, "tenant-a-vpc-endpoint-transit", vpcEndpointTransitLrpName("tenant-a", "vpc-endpoint-transit"))
	require.Equal(t, "vpc-endpoint-transit-tenant-a", vpcEndpointTransitLspName("tenant-a", "vpc-endpoint-transit"))
	require.Equal(t, "vpc-eps-db-tcp", vpcEndpointServiceLBName("db", "TCP"))
	require.Equal(t, "vpc-ep-client-udp", vpcEndpointLBName("client", "UDP"))
	require.Equal(t, "vpc-ep-client", vpcEndpointVipCRName("client"))
	require.Equal(t, "vpc-eps-db", vpcEndpointServiceLSPName("db"))
	require.Equal(t, "vpc-eps/db", vpcEndpointServiceIPAMName("db"))
	require.Equal(t, "vpc-ep-snat/tenant-a", vpcEndpointSnatIPAMName("tenant-a"))
}

func TestVpcEndpointSnatMatch(t *testing.T) {
	require.Equal(t, "ip4.dst == 100.65.1.20", vpcEndpointSnatMatch("100.65.1.20"))
	require.Equal(t, "ip6.dst == fd00:65::20", vpcEndpointSnatMatch("fd00:65::20"))
	require.Equal(t, "0.0.0.0/0", vpcEndpointSnatLogicalIP("100.65.1.20"))
	require.Equal(t, "::/0", vpcEndpointSnatLogicalIP("fd00:65::20"))
}

func TestVpcEndpointServiceAllowed(t *testing.T) {
	open := &kubeovnv1.VpcEndpointService{}
	require.True(t, vpcEndpointServiceAllowed(open, "consumer"))

	restricted := &kubeovnv1.VpcEndpointService{
		Spec: kubeovnv1.VpcEndpointServiceSpec{AllowedVpcs: []string{"a", "b"}},
	}
	require.True(t, vpcEndpointServiceAllowed(restricted, "a"))
	require.False(t, vpcEndpointServiceAllowed(restricted, "c"))
}

func TestVpcEndpointIPFromNetworks(t *testing.T) {
	require.Equal(t, "100.65.0.10", vpcEndpointIPFromNetworks([]string{"100.65.0.10/16"}))
	require.Equal(t, "100.65.0.10", vpcEndpointIPFromNetworks([]string{"100.65.0.10/16", "fd00:65::10/112"}))
	require.Empty(t, vpcEndpointIPFromNetworks(nil))
}

func TestVpcEndpointEffectiveServiceVpc(t *testing.T) {
	require.Equal(t, "ovn-cluster", vpcEndpointEffectiveServiceVpc(&corev1.Service{}, "ovn-cluster"))

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		util.LogicalRouterAnnotation: "from-router",
	}}}
	require.Equal(t, "from-router", vpcEndpointEffectiveServiceVpc(svc, "ovn-cluster"))

	svc.Annotations[util.VpcAnnotation] = "from-vpc"
	require.Equal(t, "from-vpc", vpcEndpointEffectiveServiceVpc(svc, "ovn-cluster"))
}

func TestValidateVpcEndpointServiceImmutability(t *testing.T) {
	eps := &kubeovnv1.VpcEndpointService{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			util.VpcEndpointVpcLabel:     "vpc-a",
			util.VpcEndpointSvcNsLabel:   "ns-a",
			util.VpcEndpointSvcNameLabel: "svc-a",
		}},
		Spec: kubeovnv1.VpcEndpointServiceSpec{Vpc: "vpc-a", Namespace: "ns-a", Service: "svc-a"},
	}
	require.NoError(t, validateVpcEndpointServiceImmutability(eps))
	eps.Spec.Vpc = "vpc-b"
	require.Error(t, validateVpcEndpointServiceImmutability(eps))
}

func TestEndpointSlicePortMatchesServicePort(t *testing.T) {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	http := "http"
	port80 := int32(80)

	unnamedSvc := corev1.ServicePort{Protocol: tcp, Port: 80}
	emptyName := ""
	require.True(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp}, unnamedSvc))
	require.True(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp, Name: &emptyName}, unnamedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp, Name: &http}, unnamedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Protocol: &tcp}, unnamedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &udp}, unnamedSvc))

	namedSvc := corev1.ServicePort{Name: "http", Protocol: tcp, Port: 80}
	require.True(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp, Name: &http}, namedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp}, namedSvc))
}
