package daemon

import (
	"cmp"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/knftables"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type nftAddressPort struct {
	Address  string
	Protocol string
	Port     int32
}

type nftProtocolPort struct {
	Protocol string
	Port     int32
}

type nftNATPolicy struct {
	SubnetCIDR string
	RuleID     string
	Order      int
	SrcIPs     []string
	DstIPs     []string
	Action     string
}

type nftCentralizedSNAT struct {
	CIDR string
	IP   string
}

type nftTProxyTarget struct {
	Address string
	Port    int32
}

type nftSubnetCounter struct {
	UID  string
	Name string
	CIDR string
}

type nftFamilySnapshot struct {
	Family               knftables.Family
	Table                string
	NodeInternalIP       string
	ClusterIPPorts       []nftAddressPort
	ServiceVIPPorts      []nftAddressPort
	Subnets              []string
	NATSubnets           []string
	DistributedGWSubnets []string
	OtherNodeIPs         []string
	NodeIPs              []string
	LocalNodePorts       []nftProtocolPort
	NATPolicies          []nftNATPolicy
	CentralizedSNATs     []nftCentralizedSNAT
	TProxyTargets        []nftTProxyTarget
	SubnetCounters       []nftSubnetCounter
}

type gatewayNFTSnapshot struct {
	Families []nftFamilySnapshot
}

type nftSnapshotInput struct {
	Protocol       string
	ClusterRouter  string
	NodeName       string
	Services       []*corev1.Service
	Subnets        []*kubeovnv1.Subnet
	Nodes          []*corev1.Node
	TProxyPods     []*corev1.Pod
	LocalAddresses []string
}

func buildNFTGatewaySnapshot(input nftSnapshotInput) (gatewayNFTSnapshot, error) {
	snapshot := gatewayNFTSnapshot{Families: newNFTFamilySnapshots(input.Protocol)}
	for i := range snapshot.Families {
		family := &snapshot.Families[i]
		addNFTServices(family, input.Services)
		if err := addNFTSubnets(family, input); err != nil {
			return gatewayNFTSnapshot{}, err
		}
		addNFTNodes(family, input)
		addNFTTProxyTargets(family, input.TProxyPods)
		normalizeNFTFamilySnapshot(family)
	}
	return snapshot, nil
}

func newNFTFamilySnapshots(protocol string) []nftFamilySnapshot {
	var families []nftFamilySnapshot
	if protocol == "" || protocol == kubeovnv1.ProtocolIPv4 || protocol == kubeovnv1.ProtocolDual {
		families = append(families, nftFamilySnapshot{Family: knftables.IPv4Family, Table: "kube-ovn"})
	}
	if protocol == kubeovnv1.ProtocolIPv6 || protocol == kubeovnv1.ProtocolDual {
		families = append(families, nftFamilySnapshot{Family: knftables.IPv6Family, Table: "kube-ovn"})
	}
	return families
}

func addNFTServices(snapshot *nftFamilySnapshot, services []*corev1.Service) {
	for _, service := range services {
		clusterIPs := service.Spec.ClusterIPs
		if len(clusterIPs) == 0 && service.Spec.ClusterIP != "" {
			clusterIPs = []string{service.Spec.ClusterIP}
		}

		familyHasClusterIP := false
		for _, address := range clusterIPs {
			if !isNFTAddressFamily(address, snapshot.Family) {
				continue
			}
			familyHasClusterIP = true
			for _, port := range service.Spec.Ports {
				item := nftAddressPort{Address: address, Protocol: nftServiceProtocol(port.Protocol), Port: port.Port}
				snapshot.ClusterIPPorts = append(snapshot.ClusterIPPorts, item)
				snapshot.ServiceVIPPorts = append(snapshot.ServiceVIPPorts, item)
			}
		}

		for _, address := range service.Spec.ExternalIPs {
			addNFTServiceVIP(snapshot, address, service.Spec.Ports)
		}
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			addNFTServiceVIP(snapshot, ingress.IP, service.Spec.Ports)
		}

		if familyHasClusterIP && service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyLocal {
			for _, port := range service.Spec.Ports {
				if port.NodePort != 0 {
					snapshot.LocalNodePorts = append(snapshot.LocalNodePorts, nftProtocolPort{
						Protocol: nftServiceProtocol(port.Protocol),
						Port:     port.NodePort,
					})
				}
			}
		}
	}
}

func addNFTServiceVIP(snapshot *nftFamilySnapshot, address string, ports []corev1.ServicePort) {
	if !isNFTAddressFamily(address, snapshot.Family) {
		return
	}
	for _, port := range ports {
		snapshot.ServiceVIPPorts = append(snapshot.ServiceVIPPorts, nftAddressPort{
			Address:  address,
			Protocol: nftServiceProtocol(port.Protocol),
			Port:     port.Port,
		})
	}
}

func nftServiceProtocol(protocol corev1.Protocol) string {
	if protocol == "" {
		return "tcp"
	}
	return strings.ToLower(string(protocol))
}

func addNFTSubnets(snapshot *nftFamilySnapshot, input nftSnapshotInput) error {
	for _, subnet := range input.Subnets {
		if subnet.Spec.Vpc != input.ClusterRouter || (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.CIDRBlock == "" {
			continue
		}

		cidr, err := nftCIDRForFamily(subnet.Spec.CIDRBlock, snapshot.Family)
		if err != nil {
			return fmt.Errorf("parse CIDR for subnet %s: %w", subnet.Name, err)
		}
		if cidr == "" {
			continue
		}

		snapshot.Subnets = append(snapshot.Subnets, cidr)
		snapshot.SubnetCounters = append(snapshot.SubnetCounters, nftSubnetCounter{
			UID:  string(subnet.UID),
			Name: subnet.Name,
			CIDR: cidr,
		})

		if subnet.DeletionTimestamp.IsZero() && subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
			snapshot.DistributedGWSubnets = append(snapshot.DistributedGWSubnets, cidr)
		}
		if !nftSubnetNeedsNAT(subnet) {
			continue
		}

		snapshot.NATSubnets = append(snapshot.NATSubnets, cidr)
		if err := addNFTNATPolicies(snapshot, subnet, cidr); err != nil {
			return err
		}
		if snat, ok := nftCentralizedSNATForSubnet(subnet, input.NodeName, cidr); ok {
			snapshot.CentralizedSNATs = append(snapshot.CentralizedSNATs, snat)
		}
	}
	return nil
}

func nftSubnetNeedsNAT(subnet *kubeovnv1.Subnet) bool {
	return subnet.DeletionTimestamp.IsZero() && subnet.Spec.NatOutgoing
}

func addNFTNATPolicies(snapshot *nftFamilySnapshot, subnet *kubeovnv1.Subnet, cidr string) error {
	for order, rule := range subnet.Status.NatOutgoingPolicyRules {
		if rule.RuleID == "" {
			continue
		}
		if rule.Match.SrcIPs == "" && rule.Match.DstIPs == "" {
			continue
		}
		srcIPs, err := nftAddressesForFamily(rule.Match.SrcIPs, snapshot.Family)
		if err != nil {
			return fmt.Errorf("parse source address for NAT policy %s in subnet %s: %w", rule.RuleID, subnet.Name, err)
		}
		dstIPs, err := nftAddressesForFamily(rule.Match.DstIPs, snapshot.Family)
		if err != nil {
			return fmt.Errorf("parse destination address for NAT policy %s in subnet %s: %w", rule.RuleID, subnet.Name, err)
		}
		if (rule.Match.SrcIPs != "" && len(srcIPs) == 0) || (rule.Match.DstIPs != "" && len(dstIPs) == 0) {
			continue
		}
		snapshot.NATPolicies = append(snapshot.NATPolicies, nftNATPolicy{
			SubnetCIDR: cidr,
			RuleID:     rule.RuleID,
			Order:      order,
			SrcIPs:     srcIPs,
			DstIPs:     dstIPs,
			Action:     rule.Action,
		})
	}
	return nil
}

func nftCentralizedSNATForSubnet(subnet *kubeovnv1.Subnet, nodeName, cidr string) (nftCentralizedSNAT, bool) {
	if subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType || !util.GatewayContains(subnet.Spec.GatewayNode, nodeName) {
		return nftCentralizedSNAT{}, false
	}
	if !subnet.Spec.EnableEcmp && subnet.Status.ActivateGateway != nodeName {
		return nftCentralizedSNAT{}, false
	}
	for item := range strings.SplitSeq(subnet.Spec.GatewayNode, ",") {
		node, address, ok := strings.Cut(strings.TrimSpace(item), ":")
		address = strings.TrimSpace(address)
		if !ok || node != nodeName || !isNFTAddressFamily(address, nftFamilyForAddress(cidr)) {
			continue
		}
		return nftCentralizedSNAT{CIDR: cidr, IP: address}, true
	}
	return nftCentralizedSNAT{}, false
}

func addNFTNodes(snapshot *nftFamilySnapshot, input nftSnapshotInput) {
	for _, node := range input.Nodes {
		for _, address := range node.Status.Addresses {
			if address.Type != corev1.NodeInternalIP || !isNFTAddressFamily(address.Address, snapshot.Family) {
				continue
			}
			if node.Name == input.NodeName {
				if snapshot.NodeInternalIP == "" {
					snapshot.NodeInternalIP = address.Address
				}
			} else {
				snapshot.OtherNodeIPs = append(snapshot.OtherNodeIPs, address.Address)
			}
		}
	}
	for _, address := range input.LocalAddresses {
		ip := net.ParseIP(strings.TrimSpace(address))
		if ip == nil || ip.IsLoopback() || !isNFTAddressFamily(address, snapshot.Family) {
			continue
		}
		snapshot.NodeIPs = append(snapshot.NodeIPs, ip.String())
	}
}

func addNFTTProxyTargets(snapshot *nftFamilySnapshot, pods []*corev1.Pod) {
	for _, pod := range pods {
		ports := getProbePorts(pod).SortedList()
		if len(ports) == 0 {
			continue
		}
		addresses := make([]string, 0, len(pod.Status.PodIPs)+1)
		for _, podIP := range pod.Status.PodIPs {
			addresses = append(addresses, podIP.IP)
		}
		if len(addresses) == 0 && pod.Status.PodIP != "" {
			addresses = append(addresses, pod.Status.PodIP)
		}
		for _, address := range addresses {
			if !isNFTAddressFamily(address, snapshot.Family) {
				continue
			}
			for _, port := range ports {
				snapshot.TProxyTargets = append(snapshot.TProxyTargets, nftTProxyTarget{Address: address, Port: port})
			}
		}
	}
}

func nftCIDRForFamily(value string, family knftables.Family) (string, error) {
	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return "", err
		}
		if isNFTAddressFamily(network.IP.String(), family) {
			return network.String(), nil
		}
	}
	return "", nil
}

func nftAddressesForFamily(value string, family knftables.Family) ([]string, error) {
	var result []string
	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		var address string
		if strings.Contains(item, "/") {
			_, network, err := net.ParseCIDR(item)
			if err != nil {
				return nil, err
			}
			address = network.String()
		} else if ip := net.ParseIP(item); ip != nil {
			address = ip.String()
		} else {
			return nil, fmt.Errorf("invalid address %q", item)
		}
		if isNFTAddressFamily(address, family) {
			result = append(result, address)
		}
	}
	return compactNFTIntervals(result), nil
}

func isNFTAddressFamily(address string, family knftables.Family) bool {
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	if strings.Contains(address, "/") {
		address, _, _ = strings.Cut(address, "/")
	}
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil {
		return false
	}
	if family == knftables.IPv4Family {
		return ip.To4() != nil
	}
	return ip.To4() == nil
}

func nftFamilyForAddress(address string) knftables.Family {
	if isNFTAddressFamily(address, knftables.IPv4Family) {
		return knftables.IPv4Family
	}
	return knftables.IPv6Family
}

func normalizeNFTFamilySnapshot(snapshot *nftFamilySnapshot) {
	snapshot.ClusterIPPorts = sortedUniqueAddressPorts(snapshot.ClusterIPPorts)
	snapshot.ServiceVIPPorts = sortedUniqueAddressPorts(snapshot.ServiceVIPPorts)
	snapshot.LocalNodePorts = sortedUniqueProtocolPorts(snapshot.LocalNodePorts)
	snapshot.Subnets = compactNFTIntervals(snapshot.Subnets)
	snapshot.NATSubnets = compactNFTIntervals(snapshot.NATSubnets)
	snapshot.DistributedGWSubnets = compactNFTIntervals(snapshot.DistributedGWSubnets)
	snapshot.OtherNodeIPs = sortedUniqueStrings(snapshot.OtherNodeIPs)
	snapshot.NodeIPs = sortedUniqueStrings(snapshot.NodeIPs)
	snapshot.NATPolicies = sortedUniqueNATPolicies(snapshot.NATPolicies)
	snapshot.CentralizedSNATs = sortedUniqueCentralizedSNATs(snapshot.CentralizedSNATs)
	snapshot.TProxyTargets = sortedUniqueTProxyTargets(snapshot.TProxyTargets)
	snapshot.SubnetCounters = sortedUniqueSubnetCounters(snapshot.SubnetCounters)
}

func sortedUniqueStrings(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

func compactNFTIntervals(values []string) []string {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		var (
			prefix netip.Prefix
			err    error
		)
		if strings.Contains(value, "/") {
			prefix, err = netip.ParsePrefix(value)
		} else {
			var address netip.Addr
			address, err = netip.ParseAddr(value)
			if err == nil {
				prefix = netip.PrefixFrom(address, address.BitLen())
			}
		}
		if err != nil {
			return sortedUniqueStrings(values)
		}
		prefixes = append(prefixes, prefix.Masked())
	}

	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		if n := a.Addr().Compare(b.Addr()); n != 0 {
			return n
		}
		return cmp.Compare(a.Bits(), b.Bits())
	})
	compacted := make([]netip.Prefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		if len(compacted) != 0 {
			existing := compacted[len(compacted)-1]
			if existing.Bits() <= prefix.Bits() && existing.Contains(prefix.Addr()) {
				continue
			}
		}
		compacted = append(compacted, prefix)
	}

	result := make([]string, 0, len(compacted))
	for _, prefix := range compacted {
		if prefix.Bits() == prefix.Addr().BitLen() {
			result = append(result, prefix.Addr().String())
		} else {
			result = append(result, prefix.String())
		}
	}
	return result
}

func sortedUniqueAddressPorts(values []nftAddressPort) []nftAddressPort {
	slices.SortFunc(values, func(a, b nftAddressPort) int {
		if n := cmp.Compare(a.Address, b.Address); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Protocol, b.Protocol); n != 0 {
			return n
		}
		return cmp.Compare(a.Port, b.Port)
	})
	return slices.Compact(values)
}

func sortedUniqueProtocolPorts(values []nftProtocolPort) []nftProtocolPort {
	slices.SortFunc(values, func(a, b nftProtocolPort) int {
		if n := cmp.Compare(a.Protocol, b.Protocol); n != 0 {
			return n
		}
		return cmp.Compare(a.Port, b.Port)
	})
	return slices.Compact(values)
}

func sortedUniqueNATPolicies(values []nftNATPolicy) []nftNATPolicy {
	slices.SortFunc(values, func(a, b nftNATPolicy) int {
		if n := cmp.Compare(a.SubnetCIDR, b.SubnetCIDR); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Order, b.Order); n != 0 {
			return n
		}
		return cmp.Compare(nftNATPolicyKey(a), nftNATPolicyKey(b))
	})
	return slices.CompactFunc(values, func(a, b nftNATPolicy) bool {
		return nftNATPolicyKey(a) == nftNATPolicyKey(b)
	})
}

func nftNATPolicyKey(policy nftNATPolicy) string {
	return strings.Join([]string{
		policy.SubnetCIDR,
		strconv.Itoa(policy.Order),
		policy.RuleID,
		strings.Join(policy.SrcIPs, ","),
		strings.Join(policy.DstIPs, ","),
		policy.Action,
	}, "|")
}

func sortedUniqueCentralizedSNATs(values []nftCentralizedSNAT) []nftCentralizedSNAT {
	slices.SortFunc(values, func(a, b nftCentralizedSNAT) int {
		if n := cmp.Compare(a.CIDR, b.CIDR); n != 0 {
			return n
		}
		return cmp.Compare(a.IP, b.IP)
	})
	return slices.Compact(values)
}

func sortedUniqueTProxyTargets(values []nftTProxyTarget) []nftTProxyTarget {
	slices.SortFunc(values, func(a, b nftTProxyTarget) int {
		if n := cmp.Compare(a.Address, b.Address); n != 0 {
			return n
		}
		return cmp.Compare(a.Port, b.Port)
	})
	return slices.Compact(values)
}

func sortedUniqueSubnetCounters(values []nftSubnetCounter) []nftSubnetCounter {
	slices.SortFunc(values, func(a, b nftSubnetCounter) int {
		return cmp.Compare(a.UID+"|"+a.CIDR, b.UID+"|"+b.CIDR)
	})
	return slices.CompactFunc(values, func(a, b nftSubnetCounter) bool {
		return a.UID == b.UID && a.CIDR == b.CIDR
	})
}
