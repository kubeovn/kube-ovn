package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/kubeovn/go-iptables/iptables"

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

func TestGenerateServiceNodePortLocalRules(t *testing.T) {
	services := []*corev1.Service{
		{
			Namespace: "default", Name: "remote",
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
			Namespace: "default", Name: "cluster",
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

func TestGenerateTunnelNoTrackRules(t *testing.T) {
	for _, prefix := range []string{"ovn40", "ovn60"} {
		for _, port := range []int{6081, 4789, 0} {
			t.Run(fmt.Sprintf("%s/%d", prefix, port), func(t *testing.T) {
				peerSet, localSet := prefix+OtherNodeSet, prefix+LocalEncapSet
				active, obsolete := generateTunnelNoTrackRules(port, peerSet, localSet)
				if port == 0 {
					require.Empty(t, active)
				} else {
					require.Equal(t, []util.IPTableRule{
						{Table: RAW, Chain: Prerouting, Rule: strings.Fields(fmt.Sprintf("-p udp -m udp --dport %d -m set --match-set %s src -m set --match-set %s dst -j NOTRACK", port, peerSet, localSet))},
						{Table: RAW, Chain: Output, Rule: strings.Fields(fmt.Sprintf("-p udp -m udp --dport %d -m set --match-set %s src -m set --match-set %s dst -j NOTRACK", port, localSet, peerSet))},
					}, active)
				}
				for _, oldPort := range []int{6081, 4789, 8472} {
					restricted, _ := generateTunnelNoTrackRules(oldPort, peerSet, localSet)
					for _, rule := range restricted {
						if oldPort == port {
							require.NotContains(t, obsolete, rule)
						} else {
							require.Contains(t, obsolete, rule)
						}
					}
				}
			})
		}
	}
}

func TestTunnelNoTrackCleanupIncludesAllPorts(t *testing.T) {
	for _, protocol := range []string{"IPv4", "IPv6"} {
		rules := getKubeOVNBaseIptablesRulesForCleanup(protocol)
		set := "ovn40other-node"
		if protocol == "IPv6" {
			set = "ovn60other-node"
		}
		localSet := "ovn40" + LocalEncapSet
		if protocol == "IPv6" {
			localSet = "ovn60" + LocalEncapSet
		}
		_, expected := generateTunnelNoTrackRules(0, set, localSet)
		for _, rule := range expected {
			require.Contains(t, rules, rule)
		}
	}
}

// This test runs in an isolated Linux network namespace with CAP_NET_ADMIN.
func TestCleanObsoleteTunnelRulesNetfilter(t *testing.T) {
	if os.Getenv("KUBE_OVN_TEST_NETFILTER") != "true" {
		t.Skip("requires isolated Linux netfilter environment")
	}
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	require.NoError(t, err)
	set := "kube-ovn-notrack-test"
	out, err := exec.Command("ipset", "create", set, "hash:net").CombinedOutput()
	require.NoError(t, err, string(out))
	localSet := "kube-ovn-local-encap-test"
	out, err = exec.Command("ipset", "create", localSet, "hash:ip").CombinedOutput()
	require.NoError(t, err, string(out))
	_, rules := generateTunnelNoTrackRules(0, set, localSet)
	t.Cleanup(func() {
		for _, rule := range rules {
			bestEffortDeleteIptablesRule(ipt, rule)
		}
		out, err := exec.Command("ipset", "destroy", set).CombinedOutput()
		require.NoError(t, err, string(out))
		out, err = exec.Command("ipset", "destroy", localSet).CombinedOutput()
		require.NoError(t, err, string(out))
	})
	for _, rule := range rules {
		require.NoError(t, ipt.Append(rule.Table, rule.Chain, rule.Rule...))
	}
	c := &Controller{iptables: map[string]*iptables.IPTables{"IPv4": ipt}}
	active, obsolete := generateTunnelNoTrackRules(4789, set, localSet)
	require.NoError(t, c.cleanObsoleteIptablesRules("IPv4", obsolete))
	for _, rule := range obsolete {
		exists, err := ipt.Exists(rule.Table, rule.Chain, rule.Rule...)
		require.NoError(t, err)
		require.False(t, exists)
	}
	for _, rule := range active {
		exists, err := ipt.Exists(rule.Table, rule.Chain, rule.Rule...)
		require.NoError(t, err)
		require.True(t, exists)
	}
	require.NoError(t, c.cleanObsoleteIptablesRules("IPv4", rules))
	require.NoError(t, c.cleanObsoleteIptablesRules("IPv4", rules))
}
