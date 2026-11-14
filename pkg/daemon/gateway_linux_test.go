package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubeovn/go-iptables/iptables"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	k8sipset "k8s.io/kubernetes/pkg/proxy/ipvs/ipset"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipsets"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/vishvananda/netlink"
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
				peerSet, localSet := prefix+TunnelRemoteIPSet, prefix+LocalEncapSet
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
		tunnelSet := "ovn40" + TunnelRemoteIPSet
		if protocol == "IPv6" {
			tunnelSet = "ovn60" + TunnelRemoteIPSet
		}
		localSet := "ovn40" + LocalEncapSet
		if protocol == "IPv6" {
			localSet = "ovn60" + LocalEncapSet
		}
		_, expected := generateTunnelNoTrackRules(0, tunnelSet, localSet)
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

func TestMixedFamilyIptablesReconciliation(t *testing.T) {
	tests := []struct {
		name               string
		clusterProtocol    string
		localEncapIPs      []string
		expectedProtocols  []string
		tunnelOnlyProtocol string
	}{
		{
			name:               "IPv6 overlay with IPv4 underlay tunnel",
			clusterProtocol:    kubeovnv1.ProtocolIPv6,
			localEncapIPs:      []string{"192.0.2.10"},
			expectedProtocols:  []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6},
			tunnelOnlyProtocol: kubeovnv1.ProtocolIPv4,
		},
		{
			name:               "IPv4 overlay with IPv6 underlay tunnel",
			clusterProtocol:    kubeovnv1.ProtocolIPv4,
			localEncapIPs:      []string{"2001:db8::10"},
			expectedProtocols:  []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6},
			tunnelOnlyProtocol: kubeovnv1.ProtocolIPv6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocols := getTunnelProtocols(tt.clusterProtocol, tt.localEncapIPs)
			require.Equal(t, tt.expectedProtocols, protocols)

			overlayProtocols := getProtocols(tt.clusterProtocol)
			require.NotContains(t, overlayProtocols, tt.tunnelOnlyProtocol)

			// Tunnel-only family: verify only raw NOTRACK rules are generated
			prefix := "ovn40"
			if tt.tunnelOnlyProtocol == kubeovnv1.ProtocolIPv6 {
				prefix = "ovn60"
			}
			active, obsolete := generateTunnelNoTrackRules(6081, prefix+TunnelRemoteIPSet, prefix+LocalEncapSet)
			require.Len(t, active, 2)
			for _, r := range active {
				require.Equal(t, RAW, r.Table)
				require.True(t, r.Chain == Prerouting || r.Chain == Output)
				require.Contains(t, r.Rule, "-j")
				require.Contains(t, r.Rule, "NOTRACK")
			}
			require.NotEmpty(t, obsolete)
		})
	}

	// Ordinary single-family nodes do NOT discover foreign encap family
	require.Equal(t, []string{kubeovnv1.ProtocolIPv4}, getTunnelProtocols(kubeovnv1.ProtocolIPv4, []string{"192.0.2.10"}))
	require.Equal(t, []string{kubeovnv1.ProtocolIPv6}, getTunnelProtocols(kubeovnv1.ProtocolIPv6, []string{"2001:db8::10"}))

	// Pre-initialized family bypasses new client creation
	c := &Controller{
		iptables: map[string]*iptables.IPTables{
			kubeovnv1.ProtocolIPv4: {},
		},
		ipsets: map[string]*ipsets.IPSets{
			kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
		},
	}
	require.NoError(t, c.ensureIptablesFamily(kubeovnv1.ProtocolIPv4))
}

func TestConcurrentIptablesInitializationAndMetrics(t *testing.T) {
	c := &Controller{
		protocol: kubeovnv1.ProtocolIPv4,
		ControllerRuntime: ControllerRuntime{
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv4: {},
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
			},
		},
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Metric reader iterating c.iptables concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				c.setOvnSubnetGatewayMetric()
			}
		}
	}()

	// TProxy cleaner reading c.iptables concurrently without lock
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				c.cleanTProxyIPTableRules(kubeovnv1.ProtocolIPv4)
				c.cleanTProxyIPTableRules(kubeovnv1.ProtocolIPv6)
			}
		}
	}()

	// Writers dynamically ensuring family
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.ensureIptablesFamily(kubeovnv1.ProtocolIPv4)
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestMixedFamilyCleanupCoversTunnelOnlyFamily(t *testing.T) {
	mockIPTables(t)
	c := &Controller{
		protocol: kubeovnv1.ProtocolIPv6,
		config: &Configuration{
			DefaultEncapIP: "192.0.2.10",
		},
		ControllerRuntime: ControllerRuntime{
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv4: {},
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
			},
		},
	}

	protocols := getTunnelProtocols(c.protocol, c.getLocalEncapIPs())
	require.Equal(t, []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6}, protocols)
	require.NoError(t, c.cleanupKubeOVNIptablesAndIPSets())
}

type dummyIPSetClient struct {
	k8sipset.Interface
	sets []string
}

func (d *dummyIPSetClient) ListSets() ([]string, error) {
	return d.sets, nil
}

func mockIPTables(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "iptables.log")
	script := fmt.Sprintf(`#!/bin/sh
echo "$0 $@" >> %s
if [ "$1" = "--version" ]; then
    echo "iptables v1.8.7 (legacy)"
    exit 0
fi
for arg in "$@"; do
    if [ "$arg" = "-S" ]; then
        echo "-N default"
        exit 0
    fi
done
exit 0
`, logPath)
	for _, bin := range []string{"iptables", "ip6tables", "ipset"} {
		path := filepath.Join(dir, bin)
		require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return logPath
}

func TestRestartIntoNFTCleansUpUnderlayTunnelIptables(t *testing.T) {
	mockIPTables(t)

	v6Ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	require.NoError(t, err)

	// Case 1: IPv6 overlay over IPv4 underlay tunnel.
	// On daemon restart into nftables, initRuntime() only initializes IPv6 overlay clients.
	// IPv4 underlay tunnel client is initially nil.
	c1 := &Controller{
		protocol: kubeovnv1.ProtocolIPv6,
		config: &Configuration{
			DefaultEncapIP: "192.0.2.10",
		},
		ControllerRuntime: ControllerRuntime{
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv6: v6Ipt,
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv6: ipsets.NewIPSets(netlink.FAMILY_V6, IPSetPrefix),
			},
		},
	}

	require.Nil(t, c1.getIptables(kubeovnv1.ProtocolIPv4))
	require.Nil(t, c1.getIPSet(kubeovnv1.ProtocolIPv4))

	require.NoError(t, c1.cleanupKubeOVNIptablesAndIPSets())
	require.NotNil(t, c1.getIptables(kubeovnv1.ProtocolIPv4))
	require.NotNil(t, c1.getIPSet(kubeovnv1.ProtocolIPv4))

	v4Ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	require.NoError(t, err)

	// Case 2: IPv4 overlay over IPv6 underlay tunnel.
	// IPv6 underlay tunnel client is initially nil.
	c2 := &Controller{
		protocol: kubeovnv1.ProtocolIPv4,
		config: &Configuration{
			DefaultEncapIP: "2001:db8::1",
		},
		ControllerRuntime: ControllerRuntime{
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv4: v4Ipt,
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
			},
		},
	}

	require.Nil(t, c2.getIptables(kubeovnv1.ProtocolIPv6))
	require.Nil(t, c2.getIPSet(kubeovnv1.ProtocolIPv6))

	require.NoError(t, c2.cleanupKubeOVNIptablesAndIPSets())
	require.NotNil(t, c2.getIptables(kubeovnv1.ProtocolIPv6))
	require.NotNil(t, c2.getIPSet(kubeovnv1.ProtocolIPv6))
}

func TestAddAndRemoveTunnelOnlyAddressFamily(t *testing.T) {
	logPath := mockIPTables(t)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
			},
		},
	}
	nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, nodeIndexer.Add(node))

	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	serviceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	v4Ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	require.NoError(t, err)

	c := &Controller{
		protocol: kubeovnv1.ProtocolIPv4,
		config: &Configuration{
			NodeName:            "test-node",
			DefaultEncapIP:      "192.0.2.10",
			NodeNetworks:        map[string]string{"eth0": "2001:db8::10"},
			EnableTunnelNotrack: true,
			NetworkType:         util.NetworkTypeGeneve,
		},
		nodesLister:      listerv1.NewNodeLister(nodeIndexer),
		subnetsLister:    kubeovnlister.NewSubnetLister(subnetIndexer),
		servicesLister:   listerv1.NewServiceLister(serviceIndexer),
		serviceCIDRStore: util.NewServiceCIDRStore(kubeovnv1.ProtocolIPv4),
		ControllerRuntime: ControllerRuntime{
			k8sipsets: &dummyIPSetClient{},
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv4: v4Ipt,
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
			},
		},
	}

	// 1. Initial reconcile: IPv4 overlay with IPv6 NodeNetworks.
	// Both IPv4 (overlay) and IPv6 (tunnel-only) are active.
	require.NoError(t, c.setIPSet())
	require.NoError(t, c.setIptables())
	c.gcIPSet()

	require.NotNil(t, c.getIptables(kubeovnv1.ProtocolIPv6))
	require.NotNil(t, c.getIPSet(kubeovnv1.ProtocolIPv6))
	require.ElementsMatch(t, []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6}, c.getReconcileProtocols())

	// 2. Remove IPv6 from NodeNetworks (simulating deleting the extra address family configuration).
	c.config.nodeNetworksMutex.Lock()
	c.config.NodeNetworks = nil
	c.config.nodeNetworksMutex.Unlock()

	// getTunnelProtocols now returns only IPv4
	active := getTunnelProtocols(c.protocol, c.getLocalEncapIPs())
	require.Equal(t, []string{kubeovnv1.ProtocolIPv4}, active)
	// getReconcileProtocols still covers the previously initialized IPv6 family
	require.ElementsMatch(t, []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6}, c.getReconcileProtocols())

	// Clear log to capture removal operations
	_ = os.Truncate(logPath, 0)

	// 3. Reconcile with IPv6 removed:
	// setIPSet() cleans up tunnel ipsets for IPv6, setIptables() deletes IPv6 raw NOTRACK rules, gcIPSet() applies deletions.
	require.NoError(t, c.setIPSet())
	require.NoError(t, c.setIptables())
	c.gcIPSet()

	// Verify that IPv6 NOTRACK deletion was invoked on ip6tables
	logBytes, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(logBytes)
	require.Contains(t, logContent, "ip6tables")
	require.Contains(t, logContent, "-t raw -D PREROUTING")
	require.Contains(t, logContent, "-t raw -D OUTPUT")
	require.Contains(t, logContent, "ovn60tunnel-remote-ips")
	require.Contains(t, logContent, "ovn60local-encap-ips")
}

func TestCleanupAfterRestartWithResidualTunnelIPSets(t *testing.T) {
	logPath := mockIPTables(t)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
			},
		},
	}
	nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, nodeIndexer.Add(node))

	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	serviceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	v4Ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	require.NoError(t, err)

	// Simulate daemon restarting after IPv6 tunnel config was deleted:
	// - Overlay is IPv4 only
	// - NodeNetworks has no IPv6
	// - In-memory runtime clients only have IPv4
	// - Host ipset still contains residual IPv6 tunnel sets
	c := &Controller{
		protocol: kubeovnv1.ProtocolIPv4,
		config: &Configuration{
			NodeName:            "test-node",
			DefaultEncapIP:      "192.0.2.10",
			EnableTunnelNotrack: true,
			NetworkType:         util.NetworkTypeGeneve,
		},
		nodesLister:      listerv1.NewNodeLister(nodeIndexer),
		subnetsLister:    kubeovnlister.NewSubnetLister(subnetIndexer),
		servicesLister:   listerv1.NewServiceLister(serviceIndexer),
		serviceCIDRStore: util.NewServiceCIDRStore(kubeovnv1.ProtocolIPv4),
		ControllerRuntime: ControllerRuntime{
			k8sipsets: &dummyIPSetClient{
				sets: []string{"ovn60tunnel-remote-ips", "ovn60local-encap-ips"},
			},
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv4: v4Ipt,
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
			},
		},
	}

	// 1. Host ipset residual discovery in getReconcileProtocols
	require.ElementsMatch(t, []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6}, c.getReconcileProtocols())
	require.Nil(t, c.getIptables(kubeovnv1.ProtocolIPv6))
	require.Nil(t, c.getIPSet(kubeovnv1.ProtocolIPv6))

	// 2. Normal reconcile pass cleans up IPv6 residual tunnel rules and sets
	require.NoError(t, c.setIPSet())
	require.NoError(t, c.setIptables())
	c.gcIPSet()

	// Verify that ip6tables was invoked to delete the residual tunnel raw NOTRACK rules
	logBytes, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(logBytes)
	require.Contains(t, logContent, "ip6tables")
	require.Contains(t, logContent, "-t raw -D PREROUTING")
	require.Contains(t, logContent, "-t raw -D OUTPUT")
	require.Contains(t, logContent, "ovn60tunnel-remote-ips")
	require.Contains(t, logContent, "ovn60local-encap-ips")
	require.NotNil(t, c.getIptables(kubeovnv1.ProtocolIPv6))
	require.NotNil(t, c.getIPSet(kubeovnv1.ProtocolIPv6))

	// 3. Test cleanupKubeOVNIptablesAndIPSets on restart into nftables with residual host ipsets
	_ = os.Truncate(logPath, 0)
	cRestart := &Controller{
		protocol: kubeovnv1.ProtocolIPv4,
		config: &Configuration{
			NodeName:            "test-node",
			DefaultEncapIP:      "192.0.2.10",
			EnableTunnelNotrack: true,
			NetworkType:         util.NetworkTypeGeneve,
		},
		ControllerRuntime: ControllerRuntime{
			k8sipsets: &dummyIPSetClient{
				sets: []string{"ovn60tunnel-remote-ips", "ovn60local-encap-ips"},
			},
			iptables: map[string]*iptables.IPTables{
				kubeovnv1.ProtocolIPv4: v4Ipt,
			},
			ipsets: map[string]*ipsets.IPSets{
				kubeovnv1.ProtocolIPv4: ipsets.NewIPSets(netlink.FAMILY_V4, IPSetPrefix),
			},
		},
	}
	require.NoError(t, cRestart.cleanupKubeOVNIptablesAndIPSets())
	logBytesRestart, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContentRestart := string(logBytesRestart)
	require.Contains(t, logContentRestart, "ip6tables")
	require.Contains(t, logContentRestart, "-t raw -D PREROUTING")
	require.Contains(t, logContentRestart, "-t raw -D OUTPUT")
	require.Contains(t, logContentRestart, "ovn60tunnel-remote-ips")
	require.Contains(t, logContentRestart, "ovn60local-encap-ips")
	require.NotNil(t, cRestart.getIptables(kubeovnv1.ProtocolIPv6))
	require.NotNil(t, cRestart.getIPSet(kubeovnv1.ProtocolIPv6))
}
