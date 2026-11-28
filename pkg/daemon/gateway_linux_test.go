package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCentralizedNatOutgoingNonSynDropRule(t *testing.T) {
	rule := centralizedNatOutgoingNonSynDropRule("10.26.0.0/16", "ovn40subnets")
	require.Equal(t, MANGLE, rule.Table)
	require.Equal(t, OvnPostrouting, rule.Chain)
	require.Equal(t, strings.Fields(`-s 10.26.0.0/16 -p tcp -m tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -m set ! --match-set ovn40subnets dst -j DROP`), rule.Rule)
}

func TestGenerateHostServiceSNATRules(t *testing.T) {
	services := []*corev1.Service{
		{
			Namespace: "default", Name: "web",
			Spec: corev1.ServiceSpec{
				ClusterIPs: []string{"10.96.0.10", "fd00:10:96::10"},
				Ports: []corev1.ServicePort{
					{Protocol: corev1.ProtocolTCP, Port: 80},
					{Protocol: corev1.ProtocolUDP, Port: 53},
					{Protocol: corev1.ProtocolSCTP, Port: 90},
				},
			},
		},
		{
			Namespace: "default", Name: "headless",
			Spec: corev1.ServiceSpec{
				ClusterIP: corev1.ClusterIPNone,
				Ports:     []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 80}},
			},
		},
	}

	rules := generateHostServiceSNATRules(services, "IPv4", "ovn40subnets", "172.18.0.2")
	require.Equal(t, []util.IPTableRule{
		{
			Table: NAT,
			Chain: OvnPostrouting,
			Rule:  strings.Fields(`-p tcp -m addrtype --src-type LOCAL -m set --match-set ovn40subnets dst -m conntrack --ctstate DNAT --ctorigdst 10.96.0.10 --ctorigdstport 80 -j SNAT --to-source 172.18.0.2`),
		},
		{
			Table: NAT,
			Chain: OvnPostrouting,
			Rule:  strings.Fields(`-p udp -m addrtype --src-type LOCAL -m set --match-set ovn40subnets dst -m conntrack --ctstate DNAT --ctorigdst 10.96.0.10 --ctorigdstport 53 -j SNAT --to-source 172.18.0.2`),
		},
	}, rules)

	require.Empty(t, generateHostServiceSNATRules(services, "IPv4", "ovn40subnets", ""))
}

func TestHostServiceSNATLoadBalancerIPs(t *testing.T) {
	vipMode, proxyMode := corev1.LoadBalancerIPModeVIP, corev1.LoadBalancerIPModeProxy
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			ClusterIPs: []string{"10.96.0.10", "fd00::10"},
			Ports: []corev1.ServicePort{
				{Protocol: corev1.ProtocolTCP, Port: 80},
				{Protocol: corev1.ProtocolUDP, Port: 53},
				{Protocol: corev1.ProtocolSCTP, Port: 90},
			},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{
			{IP: "192.0.2.10"},
			{IP: "192.0.2.10", IPMode: &vipMode},
			{IP: "2001:0db8::10"},
			{IP: "2001:db8::10"},
			{IP: "192.0.2.20", IPMode: &proxyMode},
			{IP: "2001:db8::20", IPMode: &proxyMode},
			{Hostname: "lb.example"},
			{IP: "invalid"},
		}}},
	}
	require.Equal(t, []string{"10.96.0.10", "192.0.2.10"}, serviceHostSNATIPsForProtocol(service, "IPv4"))
	require.Equal(t, []string{"2001:db8::10", "fd00::10"}, serviceHostSNATIPsForProtocol(service, "IPv6"))
	for _, tc := range []struct{ family, subnetSet, nodeIP, vip string }{
		{"IPv4", "ovn40subnets", "192.0.2.1", "192.0.2.10"},
		{"IPv6", "ovn60subnets", "2001:db8::1", "2001:db8::10"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			rules := generateHostServiceSNATRules([]*corev1.Service{service}, tc.family, tc.subnetSet, tc.nodeIP)
			require.Len(t, rules, 4)
			ipvsRules := generateHostServiceIPVSSNATRules([]*corev1.Service{service}, tc.family, tc.subnetSet, tc.nodeIP)
			require.Len(t, ipvsRules, 2)
			for i, port := range []struct {
				protocol string
				port     int
			}{{"tcp", 80}, {"udp", 53}} {
				want := fmt.Sprintf("-p %s -m addrtype --src-type LOCAL -m set --match-set %s dst -m conntrack --ctstate DNAT --ctorigdst %s --ctorigdstport %d -j SNAT --to-source %s", port.protocol, tc.subnetSet, tc.vip, port.port, tc.nodeIP)
				require.Contains(t, rules, util.IPTableRule{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(want)})
				want = fmt.Sprintf("-p %s -m addrtype --src-type LOCAL -m set --match-set %s dst -m ipvs --vaddr %s --vport %d --vdir ORIGINAL -j SNAT --to-source %s", port.protocol, tc.subnetSet, tc.vip, port.port, tc.nodeIP)
				require.Equal(t, util.IPTableRule{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(want)}, ipvsRules[i])
			}
			require.Empty(t, generateHostServiceIPVSSNATRules([]*corev1.Service{service}, tc.family, tc.subnetSet, ""))
		})
	}
	service.Status.LoadBalancer.Ingress = nil
	require.Len(t, generateHostServiceSNATRules([]*corev1.Service{service}, "IPv4", "ovn40subnets", "192.0.2.1"), 2)
	require.Empty(t, generateHostServiceIPVSSNATRules([]*corev1.Service{service}, "IPv4", "ovn40subnets", "192.0.2.1"))
}
