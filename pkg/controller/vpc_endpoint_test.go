package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"

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
	require.Equal(t, "fd00:65::10", vpcEndpointIPFromNetworks([]string{"fd00:65::10/112"}))
	require.Equal(t, "not-a-cidr", vpcEndpointIPFromNetworks([]string{"not-a-cidr"}))
	require.Empty(t, vpcEndpointIPFromNetworks(nil))
}

func TestVpcEndpointPreferIP(t *testing.T) {
	require.Equal(t, "10.0.0.1", vpcEndpointPreferIP("10.0.0.1", "fd00::1"))
	require.Equal(t, "fd00::1", vpcEndpointPreferIP("", "fd00::1"))
	require.Empty(t, vpcEndpointPreferIP("", ""))
}

func TestVpcEndpointServicePorts(t *testing.T) {
	require.Empty(t, vpcEndpointServicePorts(&corev1.Service{}))
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Protocol: corev1.ProtocolTCP, Port: 80},
		{Protocol: corev1.ProtocolUDP, Port: 53},
	}}}
	require.Equal(t, "tcp/80,udp/53", vpcEndpointServicePorts(svc))
}

func TestVpcEndpointEffectiveServiceVpc(t *testing.T) {
	require.Equal(t, "ovn-cluster", vpcEndpointEffectiveServiceVpc(&corev1.Service{}, "ovn-cluster"))

	svc := &corev1.Service{Annotations: map[string]string{
		util.LogicalRouterAnnotation: "from-router",
	}}
	require.Equal(t, "from-router", vpcEndpointEffectiveServiceVpc(svc, "ovn-cluster"))

	svc.Annotations[util.VpcAnnotation] = "from-vpc"
	require.Equal(t, "from-vpc", vpcEndpointEffectiveServiceVpc(svc, "ovn-cluster"))
}

func TestValidateVpcEndpointServiceImmutability(t *testing.T) {
	require.NoError(t, validateVpcEndpointServiceImmutability(&kubeovnv1.VpcEndpointService{}))

	eps := &kubeovnv1.VpcEndpointService{
		Labels: map[string]string{
			util.VpcEndpointVpcLabel:     "vpc-a",
			util.VpcEndpointSvcNsLabel:   "ns-a",
			util.VpcEndpointSvcNameLabel: "svc-a",
		},
		Spec: kubeovnv1.VpcEndpointServiceSpec{Vpc: "vpc-a", Namespace: "ns-a", Service: "svc-a"},
	}
	require.NoError(t, validateVpcEndpointServiceImmutability(eps))
	eps.Spec.Vpc = "vpc-b"
	require.ErrorContains(t, validateVpcEndpointServiceImmutability(eps), "vpc is immutable")
	eps.Spec.Vpc = "vpc-a"
	eps.Spec.Namespace = "ns-b"
	require.ErrorContains(t, validateVpcEndpointServiceImmutability(eps), "namespace is immutable")
	eps.Spec.Namespace = "ns-a"
	eps.Spec.Service = "svc-b"
	require.ErrorContains(t, validateVpcEndpointServiceImmutability(eps), "service is immutable")
}

func TestValidateVpcEndpointImmutability(t *testing.T) {
	require.NoError(t, validateVpcEndpointImmutability(&kubeovnv1.VpcEndpoint{}))

	ep := &kubeovnv1.VpcEndpoint{
		Labels: map[string]string{
			util.VpcEndpointVpcLabel:     "vpc-a",
			util.VpcEndpointServiceLabel: "eps-a",
		},
		Spec: kubeovnv1.VpcEndpointSpec{Vpc: "vpc-a", EndpointService: "eps-a"},
	}
	require.NoError(t, validateVpcEndpointImmutability(ep))
	ep.Spec.Vpc = "vpc-b"
	require.ErrorContains(t, validateVpcEndpointImmutability(ep), "vpc is immutable")
	ep.Spec.Vpc = "vpc-a"
	ep.Spec.EndpointService = "eps-b"
	require.ErrorContains(t, validateVpcEndpointImmutability(ep), "endpointService is immutable")
}

func TestEnqueueVpcEndpointServiceHandlers(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcEndpointServiceQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpointService", nil),
	}
	t.Cleanup(c.addOrUpdateVpcEndpointServiceQueue.ShutDown)

	eps := &kubeovnv1.VpcEndpointService{Name: "db"}
	c.enqueueAddVpcEndpointService(eps)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
	key, _ := c.addOrUpdateVpcEndpointServiceQueue.Get()
	c.addOrUpdateVpcEndpointServiceQueue.Done(key)
	require.Equal(t, "db", key)

	updated := eps.DeepCopy()
	updated.ResourceVersion = "2"
	c.enqueueUpdateVpcEndpointService(eps, updated)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
	key, _ = c.addOrUpdateVpcEndpointServiceQueue.Get()
	c.addOrUpdateVpcEndpointServiceQueue.Done(key)

	c.enqueueUpdateVpcEndpointService(eps, eps)
	require.Zero(t, c.addOrUpdateVpcEndpointServiceQueue.Len())

	c.enqueueDeleteVpcEndpointService(eps)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
}

func TestEnqueueVpcEndpointHandlers(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcEndpointQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpoint", nil),
	}
	t.Cleanup(c.addOrUpdateVpcEndpointQueue.ShutDown)

	ep := &kubeovnv1.VpcEndpoint{Name: "client"}
	c.enqueueAddVpcEndpoint(ep)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())
	key, _ := c.addOrUpdateVpcEndpointQueue.Get()
	c.addOrUpdateVpcEndpointQueue.Done(key)
	require.Equal(t, "client", key)

	updated := ep.DeepCopy()
	updated.ResourceVersion = "2"
	c.enqueueUpdateVpcEndpoint(ep, updated)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())
	key, _ = c.addOrUpdateVpcEndpointQueue.Get()
	c.addOrUpdateVpcEndpointQueue.Done(key)

	c.enqueueUpdateVpcEndpoint(ep, ep)
	require.Zero(t, c.addOrUpdateVpcEndpointQueue.Len())

	c.enqueueDeleteVpcEndpoint(ep)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())
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
