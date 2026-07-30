package daemon

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCentralizedNatOutgoingNonSynDropRule(t *testing.T) {
	rule := centralizedNatOutgoingNonSynDropRule("10.26.0.0/16", "ovn40subnets")
	require.Equal(t, MANGLE, rule.Table)
	require.Equal(t, OvnPostrouting, rule.Chain)
	require.Equal(t, strings.Fields(`-s 10.26.0.0/16 -p tcp -m tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -m set ! --match-set ovn40subnets dst -j DROP`), rule.Rule)
}

func TestFindRulePositionsInList(t *testing.T) {
	jumpRule := util.IPTableRule{
		Table: "nat",
		Chain: "PREROUTING",
		Rule:  []string{"-m", "comment", "--comment", "kube-ovn prerouting rules", "-j", "OVN-PREROUTING"},
	}
	kubeProxyRule := util.IPTableRule{
		Table: "nat",
		Chain: "PREROUTING",
		Rule:  []string{"-m", "comment", "--comment", "kubernetes service portals", "-j", "KUBE-SERVICES"},
	}
	const (
		jumpLine      = `-A PREROUTING -m comment --comment "kube-ovn prerouting rules" -j OVN-PREROUTING`
		kubeProxyLine = `-A PREROUTING -m comment --comment "kubernetes service portals" -j KUBE-SERVICES`
		policyLine    = "-P PREROUTING ACCEPT"
	)

	tests := []struct {
		name     string
		rules    []string
		rule     util.IPTableRule
		expected []int
	}{
		{
			name:     "single match returns its position",
			rules:    []string{policyLine, jumpLine, kubeProxyLine},
			rule:     jumpRule,
			expected: []int{1},
		},
		{
			name:     "kube-proxy rule located independently",
			rules:    []string{policyLine, jumpLine, kubeProxyLine},
			rule:     kubeProxyRule,
			expected: []int{2},
		},
		{
			name:     "no match returns empty",
			rules:    []string{policyLine, kubeProxyLine},
			rule:     jumpRule,
			expected: []int{},
		},
		{
			name:     "duplicates returned bottom-to-top (descending index)",
			rules:    []string{policyLine, jumpLine, kubeProxyLine, jumpLine},
			rule:     jumpRule,
			expected: []int{3, 1},
		},
		{
			name:     "index 0 is never matched",
			rules:    []string{jumpLine, policyLine},
			rule:     jumpRule,
			expected: []int{},
		},
		{
			name:     "short rule lines are skipped without matching",
			rules:    []string{policyLine, "-N FOO", jumpLine},
			rule:     jumpRule,
			expected: []int{2},
		},
		{
			name:     "empty list returns empty",
			rules:    []string{},
			rule:     jumpRule,
			expected: []int{},
		},
		{
			name:     "only policy line returns empty",
			rules:    []string{policyLine},
			rule:     jumpRule,
			expected: []int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, findRulePositionsInList(tc.rules, tc.rule))
		})
	}
}

func TestGenerateHostServiceSNATRules(t *testing.T) {
	services := []*corev1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
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
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "headless"},
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

func TestGenerateServiceNodePortLocalRules(t *testing.T) {
	services := []*corev1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "remote"},
			Spec: corev1.ServiceSpec{
				ClusterIPs:            []string{"10.96.0.10", "fd00:10:96::10"},
				ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
				Ports: []corev1.ServicePort{
					{Protocol: corev1.ProtocolTCP, NodePort: 30080},
					{Protocol: corev1.ProtocolUDP, NodePort: 30053},
					{Protocol: corev1.ProtocolTCP},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster"},
			Spec: corev1.ServiceSpec{
				ClusterIPs: []string{"10.96.0.11"},
				Ports:      []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, NodePort: 30081}},
			},
		},
	}

	rules := generateServiceNodePortLocalRules(services, "IPv4", "ovn40other-node")
	require.Equal(t, []util.IPTableRule{
		{
			Table: NAT,
			Chain: OvnPrerouting,
			Rule:  strings.Fields(`-p tcp -m addrtype --dst-type LOCAL -m tcp --dport 30080 -j MARK --set-xmark 0x80000/0x80000`),
		},
		{
			Table: NAT,
			Chain: OvnPrerouting,
			Rule:  strings.Fields(`-p tcp -m set --match-set ovn40other-node src -m addrtype --dst-type LOCAL -m tcp --dport 30080 -j MARK --set-xmark 0x4000/0x4000`),
		},
		{
			Table: NAT,
			Chain: OvnPrerouting,
			Rule:  strings.Fields(`-p udp -m addrtype --dst-type LOCAL -m udp --dport 30053 -j MARK --set-xmark 0x80000/0x80000`),
		},
		{
			Table: NAT,
			Chain: OvnPrerouting,
			Rule:  strings.Fields(`-p udp -m set --match-set ovn40other-node src -m addrtype --dst-type LOCAL -m udp --dport 30053 -j MARK --set-xmark 0x4000/0x4000`),
		},
	}, rules)
}
