package daemon

import (
	"slices"
	"strings"

	"github.com/kubeovn/felix/ipsets"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// setTunnelIPSets creates only tunnel sets in a family absent from the overlay.
func (c *Controller) setTunnelIPSets(protocol string) error {
	peers, err := c.getOtherNodes(protocol)
	if err != nil {
		return err
	}
	var local []string
	for _, ip := range c.getLocalEncapIPs() {
		if util.CheckProtocol(ip) == protocol {
			local = append(local, ip)
		}
	}
	manager := c.ipsets[protocol]
	manager.AddOrReplaceIPSet(ipsets.IPSetMetadata{MaxSize: 1048576, SetID: OtherNodeSet, Type: ipsets.IPSetTypeHashNet}, peers)
	manager.AddOrReplaceIPSet(ipsets.IPSetMetadata{MaxSize: 1048576, SetID: LocalEncapSet, Type: ipsets.IPSetTypeHashIP}, local)
	manager.ApplyUpdates()
	return nil
}

// reconcileTunnelIptables is independent of overlay NAT/filter reconciliation.
// Both families are visited so removing the last encap address also retires rules.
func (c *Controller) reconcileTunnelIptables() {
	localIPs := c.getLocalEncapIPs()
	for _, protocol := range getProtocols(kubeovnv1.ProtocolDual) {
		ipt := c.iptables[protocol]
		if ipt == nil {
			continue
		}
		prefix := "ovn40"
		if protocol == kubeovnv1.ProtocolIPv6 {
			prefix = "ovn60"
		}
		port := 0
		if slices.ContainsFunc(localIPs, func(ip string) bool { return util.CheckProtocol(ip) == protocol }) {
			port = c.getTunnelPort()
		}
		active, obsolete := generateTunnelNoTrackRules(port, prefix+OtherNodeSet, prefix+LocalEncapSet)
		for _, rule := range active {
			if exists, err := ipt.ChainExists(RAW, rule.Chain); err != nil || !exists {
				klog.V(4).Infof("raw chain %s is unavailable for %s: %v", rule.Chain, protocol, err)
				continue
			}
			if err := c.createIptablesRule(ipt, rule); err != nil {
				klog.Warningf("skip optional tunnel rule %q for %s: %v", strings.Join(rule.Rule, " "), protocol, err)
			}
		}
		for _, rule := range obsolete {
			bestEffortDeleteIptablesRule(ipt, rule)
		}
		if old := c.iptablesObsolete[protocol]; old != nil {
			_, rules := generateTunnelNoTrackRules(0, prefix+OtherNodeSet, prefix+LocalEncapSet)
			for _, rule := range rules {
				bestEffortDeleteIptablesRule(old, rule)
			}
			if !slices.Contains(getProtocols(c.protocol), protocol) {
				delete(c.iptablesObsolete, protocol)
			}
		}
	}
}
