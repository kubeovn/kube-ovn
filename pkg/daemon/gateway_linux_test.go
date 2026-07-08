package daemon

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
