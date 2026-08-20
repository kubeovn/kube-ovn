package ovn_ic_controller

import (
	"maps"
	"sort"
	"strings"

	"github.com/scylladb/go-set/strset"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	icNoAction = iota
	icFirstEstablish
	icConfigChange
	icGatewayChange
)

func cloneICConfig(data map[string]string) map[string]string {
	if data == nil {
		return nil
	}
	return maps.Clone(data)
}

func configWithoutGwNodes(data map[string]string) map[string]string {
	out := cloneICConfig(data)
	if out == nil {
		return nil
	}
	delete(out, "gw-nodes")
	return out
}

func classifyICConfig(enabled string, last, cur map[string]string) int {
	if enabled != "true" && len(last) == 0 && cur["enable-ic"] == "true" {
		return icFirstEstablish
	}
	if enabled == "true" && last != nil {
		if maps.Equal(last, cur) {
			return icNoAction
		}
		if maps.Equal(configWithoutGwNodes(last), configWithoutGwNodes(cur)) {
			return icGatewayChange
		}
	}
	return icConfigChange
}

func mergeConflictCIDRs(localCIDRs, learnedPrefixes, sticky []string) []string {
	out := strset.New()
	for _, cidr := range sticky {
		for _, local := range localCIDRs {
			if util.CIDROverlap(local, cidr) {
				out.Add(cidr)
				break
			}
		}
	}
	for _, local := range localCIDRs {
		for _, learned := range learnedPrefixes {
			if util.CIDROverlap(local, learned) {
				out.Add(local)
				out.Add(learned)
				break
			}
		}
	}
	list := out.List()
	sort.Strings(list)
	return list
}

func parseGwNodes(gwNodes string) []string {
	parts := strings.Split(gwNodes, ",")
	nodes := make([]string, 0, len(parts))
	for _, node := range parts {
		node = strings.TrimSpace(node)
		if node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func subnetCIDRs(subnets []*kubeovnv1.Subnet) []string {
	cidrs := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if subnet != nil && subnet.Spec.CIDRBlock != "" {
			v4, v6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
			if v4 != "" {
				cidrs = append(cidrs, v4)
			}
			if v6 != "" {
				cidrs = append(cidrs, v6)
			}
		}
	}
	return cidrs
}

func filterPersistedConflictCIDRs(localCIDRs []string, blacklist string) []string {
	var persisted []string
	for _, entry := range strings.Split(blacklist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		for _, local := range localCIDRs {
			if util.CIDROverlap(local, entry) {
				persisted = append(persisted, entry)
				break
			}
		}
	}
	return persisted
}
