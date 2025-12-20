package util

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func ValidateSubnet(subnet kubeovnv1.Subnet) error {
	if subnet.Spec.Gateway != "" {
		// v6 ip address can not use upper case
		if ContainsUppercase(subnet.Spec.Gateway) {
			err := fmt.Errorf("subnet gateway %s v6 ip address can not contain upper case", subnet.Spec.Gateway)
			klog.Error(err)
			return err
		}
		if !CIDRContainIP(subnet.Spec.CIDRBlock, subnet.Spec.Gateway) {
			return fmt.Errorf("gateway %s is not in cidr %s", subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
		}
		if err := ValidateNetworkBroadcast(subnet.Spec.CIDRBlock, subnet.Spec.Gateway); err != nil {
			klog.Error(err)
			return fmt.Errorf("validate gateway %s for cidr %s failed: %w", subnet.Spec.Gateway, subnet.Spec.CIDRBlock, err)
		}
	}

	if err := CIDRGlobalUnicast(subnet.Spec.CIDRBlock); err != nil {
		klog.Error(err)
		return err
	}
	if CheckProtocol(subnet.Spec.CIDRBlock) == "" {
		return fmt.Errorf("CIDRBlock: %q format error", subnet.Spec.CIDRBlock)
	}
	excludeIps := subnet.Spec.ExcludeIps
	for _, ipr := range excludeIps {
		// v6 ip address can not use upper case
		if ContainsUppercase(ipr) {
			err := fmt.Errorf("subnet exclude ip %s can not contain upper case", ipr)
			return err
		}
		ips := strings.Split(ipr, "..")
		if len(ips) > 2 {
			return fmt.Errorf("%s in excludeIps is not a valid ip range", ipr)
		}

		if len(ips) == 1 {
			if net.ParseIP(ips[0]) == nil {
				return fmt.Errorf("ip %s in excludeIps is not a valid address", ips[0])
			}
		}

		if len(ips) == 2 {
			for _, ip := range ips {
				if net.ParseIP(ip) == nil {
					return fmt.Errorf("ip %s in excludeIps is not a valid address", ip)
				}
			}
			if IP2BigInt(ips[0]).Cmp(IP2BigInt(ips[1])) == 1 {
				return fmt.Errorf("%s in excludeIps is not a valid ip range", ipr)
			}
		}
	}

	for cidr := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
		// v6 ip address can not use upper case
		if ContainsUppercase(subnet.Spec.CIDRBlock) {
			err := fmt.Errorf("subnet cidr block %s v6 ip address can not contain upper case", subnet.Spec.CIDRBlock)
			klog.Error(err)
			return err
		}
		if err := InvalidSpecialCIDR(cidr); err != nil {
			klog.Errorf("invalid subnet %s cidr %s, %s", subnet.Name, cidr, err)
			return err
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			err = fmt.Errorf("subnet %s cidr %s is invalid, due to %w", subnet.Name, cidr, err)
			klog.Error(err)
			return err
		}
		// check network mask is 32 in ipv4 or 128 in ipv6
		if err = InvalidNetworkMask(network); err != nil {
			err = fmt.Errorf("subnet %s cidr %s mask is invalid, due to %w", subnet.Name, cidr, err)
			klog.Error(err)
			return err
		}
	}

	allow := subnet.Spec.AllowSubnets
	for _, cidr := range allow {
		// v6 ip address can not use upper case
		if ContainsUppercase(cidr) {
			err := fmt.Errorf("subnet %s allow subnet %s v6 ip address can not contain upper case", subnet.Name, cidr)
			klog.Error(err)
			return err
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			klog.Error(err)
			return fmt.Errorf("%s in allowSubnets is not a valid address", cidr)
		}
	}

	gwType := subnet.Spec.GatewayType
	if gwType != "" && gwType != kubeovnv1.GWDistributedType && gwType != kubeovnv1.GWCentralizedType {
		return fmt.Errorf("%s is not a valid gateway type", gwType)
	}

	protocol := subnet.Spec.Protocol
	if protocol != "" && protocol != kubeovnv1.ProtocolIPv4 &&
		protocol != kubeovnv1.ProtocolIPv6 &&
		protocol != kubeovnv1.ProtocolDual {
		return fmt.Errorf("%s is not a valid protocol type", protocol)
	}

	if subnet.Spec.Vpc == subnet.Name {
		return fmt.Errorf("subnet %s and vpc %s cannot have the same name", subnet.Name, subnet.Spec.Vpc)
	}

	if subnet.Spec.Vpc == DefaultVpc {
		k8sAPIServer := os.Getenv(EnvKubernetesServiceHost)
		if k8sAPIServer != "" && CIDRContainIP(subnet.Spec.CIDRBlock, k8sAPIServer) {
			return fmt.Errorf("subnet %s cidr %s conflicts with k8s apiserver svc ip %s", subnet.Name, subnet.Spec.CIDRBlock, k8sAPIServer)
		}
	}

	if egw := subnet.Spec.ExternalEgressGateway; egw != "" {
		if subnet.Spec.NatOutgoing {
			return errors.New("conflict configuration: natOutgoing and externalEgressGateway")
		}
		// v6 ip address can not use upper case
		if ContainsUppercase(egw) {
			err := fmt.Errorf("subnet %s external egress gateway %s v6 ip address can not contain upper case", subnet.Name, egw)
			klog.Error(err)
			return err
		}
		ips := strings.Split(egw, ",")
		if len(ips) > 2 {
			return errors.New("invalid external egress gateway configuration")
		}
		for _, ip := range ips {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("IP %s in externalEgressGateway is not a valid address", ip)
			}
		}
		egwProtocol, cidrProtocol := CheckProtocol(egw), CheckProtocol(subnet.Spec.CIDRBlock)
		if egwProtocol != cidrProtocol && cidrProtocol != kubeovnv1.ProtocolDual {
			return errors.New("invalid external egress gateway configuration: address family is conflict with CIDR")
		}
	}

	if len(subnet.Spec.Vips) != 0 {
		for _, vip := range subnet.Spec.Vips {
			// v6 ip address can not use upper case
			if ContainsUppercase(vip) {
				err := fmt.Errorf("subnet %s vips %s v6 ip address can not contain upper case", subnet.Name, vip)
				klog.Error(err)
				return err
			}
			if !CIDRContainIP(subnet.Spec.CIDRBlock, vip) {
				return fmt.Errorf("vip %s conflicts with subnet %s cidr %s", vip, subnet.Name, subnet.Spec.CIDRBlock)
			}
		}
	}

	if subnet.Spec.LogicalGateway && subnet.Spec.U2OInterconnection {
		return errors.New("logicalGateway and u2oInterconnection can't be opened at the same time")
	}

	if len(subnet.Spec.NatOutgoingPolicyRules) != 0 {
		if err := validateNatOutgoingPolicyRules(subnet); err != nil {
			klog.Error(err)
			return err
		}
	}

	if subnet.Spec.U2OInterconnectionIP != "" {
		// v6 ip address can not use upper case
		if ContainsUppercase(subnet.Spec.U2OInterconnectionIP) {
			err := fmt.Errorf("subnet %s U2O interconnection ip %s v6 ip address can not contain upper case", subnet.Name, subnet.Spec.U2OInterconnectionIP)
			klog.Error(err)
			return err
		}
		if !CIDRContainIP(subnet.Spec.CIDRBlock, subnet.Spec.U2OInterconnectionIP) {
			return fmt.Errorf("u2oInterconnectionIP %s is not in subnet %s cidr %s",
				subnet.Spec.U2OInterconnectionIP,
				subnet.Name, subnet.Spec.CIDRBlock)
		}
	}

	return nil
}

func validateNatOutgoingPolicyRules(subnet kubeovnv1.Subnet) error {
	for _, rule := range subnet.Spec.NatOutgoingPolicyRules {
		var srcProtocol, dstProtocol string
		var err error

		if rule.Match.SrcIPs != "" {
			if srcProtocol, err = validateNatOutGoingPolicyRuleIPs(rule.Match.SrcIPs); err != nil {
				klog.Error(err)
				return fmt.Errorf("validate nat policy rules src ips %s failed with err %w", rule.Match.SrcIPs, err)
			}
		}
		if rule.Match.DstIPs != "" {
			if dstProtocol, err = validateNatOutGoingPolicyRuleIPs(rule.Match.DstIPs); err != nil {
				klog.Error(err)
				return fmt.Errorf("validate nat policy rules dst ips %s failed with err %w", rule.Match.DstIPs, err)
			}
		}

		if srcProtocol != "" && dstProtocol != "" && srcProtocol != dstProtocol {
			return fmt.Errorf("Match.SrcIPS protocol %s not equal to Match.DstIPs protocol %s", srcProtocol, dstProtocol)
		}
	}
	return nil
}

func validateNatOutGoingPolicyRuleIPs(matchIPStr string) (string, error) {
	if matchIPStr = strings.TrimSpace(matchIPStr); matchIPStr == "" {
		return "", errors.New("IPStr should not be empty")
	}

	ipItems := strings.Split(matchIPStr, ",")
	lastProtocol := ""
	checkProtocolConsistent := func(ipCidr string) bool {
		currentProtocol := CheckProtocol(ipCidr)
		if lastProtocol != "" && lastProtocol != currentProtocol {
			return false
		}
		lastProtocol = currentProtocol
		return true
	}

	for _, ipItem := range ipItems {
		_, ipCidr, err := net.ParseCIDR(ipItem)
		if err == nil {
			if !checkProtocolConsistent(ipCidr.String()) {
				return "", fmt.Errorf("match ips %s protocol is not consistent", matchIPStr)
			}
			continue
		}

		if IsValidIP(ipItem) {
			if !checkProtocolConsistent(ipItem) {
				return "", fmt.Errorf("match ips %s protocol is not consistent", matchIPStr)
			}
			continue
		}

		return "", fmt.Errorf("match ips %s is not ip or ipcidr", matchIPStr)
	}
	return lastProtocol, nil
}

func ValidatePodNetwork(annotations map[string]string) error {
	errors := []error{}

	if ipAddress := annotations[IPAddressAnnotation]; ipAddress != "" {
		// The format of IP Annotation in dual-stack is 10.244.0.0/16,fd00:10:244:0:2::/80
		for ip := range strings.SplitSeq(ipAddress, ",") {
			if strings.Contains(ip, "/") {
				if _, _, err := net.ParseCIDR(ip); err != nil {
					klog.Error(err)
					errors = append(errors, fmt.Errorf("%s is not a valid %s", ip, IPAddressAnnotation))
					continue
				}
			} else {
				if net.ParseIP(ip) == nil {
					errors = append(errors, fmt.Errorf("%s is not a valid %s", ip, IPAddressAnnotation))
					continue
				}
			}

			if cidrStr := annotations[CidrAnnotation]; cidrStr != "" {
				if err := CheckCidrs(cidrStr); err != nil {
					klog.Error(err)
					errors = append(errors, fmt.Errorf("invalid cidr %s", cidrStr))
					continue
				}

				if !CIDRContainIP(cidrStr, ip) {
					errors = append(errors, fmt.Errorf("%s not in cidr %s", ip, cidrStr))
					continue
				}
			}
		}
	}

	mac := annotations[MacAddressAnnotation]
	if mac != "" {
		if _, err := net.ParseMAC(mac); err != nil {
			klog.Error(err)
			errors = append(errors, fmt.Errorf("%s is not a valid %s", mac, MacAddressAnnotation))
		}
	}

	ipPool := annotations[IPPoolAnnotation]
	if ipPool != "" {
		if strings.ContainsRune(ipPool, ';') || strings.ContainsRune(ipPool, ',') || net.ParseIP(ipPool) != nil {
			for ips := range strings.SplitSeq(ipPool, ";") {
				found := false
				for ip := range strings.SplitSeq(ips, ",") {
					if net.ParseIP(strings.TrimSpace(ip)) == nil {
						errors = append(errors, fmt.Errorf("%s in %s is not a valid address", ip, IPPoolAnnotation))
					}

					// After ns supports multiple subnets, the ippool static addresses can be allocated in any subnets, such as "ovn.kubernetes.io/ip_pool: 11.16.10.14,12.26.11.21"
					// so if anyone ip is included in cidr, return true
					if cidrStr := annotations[CidrAnnotation]; cidrStr != "" {
						if CIDRContainIP(cidrStr, ip) {
							found = true
							break
						}
					} else {
						// annotation maybe empty when a pod is new created, do not return err in this situation
						found = true
						break
					}
				}

				if !found {
					errors = append(errors, fmt.Errorf("%s not in cidr %s", ips, annotations[CidrAnnotation]))
					continue
				}
			}
		}
	}

	ingress := annotations[IngressRateAnnotation]
	if ingress != "" {
		if _, err := strconv.Atoi(ingress); err != nil {
			klog.Error(err)
			errors = append(errors, fmt.Errorf("%s is not a valid %s", ingress, IngressRateAnnotation))
		}
	}

	egress := annotations[EgressRateAnnotation]
	if egress != "" {
		if _, err := strconv.Atoi(egress); err != nil {
			klog.Error(err)
			errors = append(errors, fmt.Errorf("%s is not a valid %s", egress, EgressRateAnnotation))
		}
	}

	return utilerrors.NewAggregate(errors)
}

func ValidateNetworkBroadcast(cidr, ip string) error {
	for cidrBlock := range strings.SplitSeq(cidr, ",") {
		for ipAddr := range strings.SplitSeq(ip, ",") {
			if CheckProtocol(cidrBlock) != CheckProtocol(ipAddr) {
				continue
			}
			_, network, _ := net.ParseCIDR(cidrBlock)
			if AddressCount(network) == 1 {
				return fmt.Errorf("subnet %s is configured with /32 or /128 netmask", cidrBlock)
			}

			ipStr := IPToString(ipAddr)
			if SubnetBroadcast(cidrBlock) == ipStr {
				return fmt.Errorf("%s is the broadcast ip in cidr %s", ipStr, cidrBlock)
			}
			if SubnetNumber(cidrBlock) == ipStr {
				return fmt.Errorf("%s is the network number ip in cidr %s", ipStr, cidrBlock)
			}
		}
	}
	return nil
}

func ValidateCidrConflict(subnet kubeovnv1.Subnet, subnetList []kubeovnv1.Subnet) error {
	for _, sub := range subnetList {
		if sub.Spec.Vpc != subnet.Spec.Vpc || sub.Spec.Vlan != subnet.Spec.Vlan || sub.Name == subnet.Name {
			continue
		}

		if CIDROverlap(sub.Spec.CIDRBlock, subnet.Spec.CIDRBlock) {
			err := fmt.Errorf("subnet %s cidr %s is conflict with subnet %s cidr %s", subnet.Name, subnet.Spec.CIDRBlock, sub.Name, sub.Spec.CIDRBlock)
			return err
		}

		if subnet.Spec.ExternalEgressGateway != "" && sub.Spec.ExternalEgressGateway != "" &&
			subnet.Spec.PolicyRoutingTableID == sub.Spec.PolicyRoutingTableID {
			err := fmt.Errorf("subnet %s policy routing table ID %d is conflict with subnet %s policy routing table ID %d", subnet.Name, subnet.Spec.PolicyRoutingTableID, sub.Name, sub.Spec.PolicyRoutingTableID)
			return err
		}
	}
	return nil
}

func ValidateVpc(vpc *kubeovnv1.Vpc) error {
	for _, item := range vpc.Spec.StaticRoutes {
		if item.Policy != "" && item.Policy != kubeovnv1.PolicyDst && item.Policy != kubeovnv1.PolicySrc {
			return fmt.Errorf("unknown policy type: %s", item.Policy)
		}

		if strings.Contains(item.CIDR, "/") {
			if _, _, err := net.ParseCIDR(item.CIDR); err != nil {
				klog.Error(err)
				return fmt.Errorf("invalid cidr %s: %w", item.CIDR, err)
			}
		} else if ip := net.ParseIP(item.CIDR); ip == nil {
			return fmt.Errorf("invalid IP %s", item.CIDR)
		}

		if ip := net.ParseIP(item.NextHopIP); ip == nil {
			return fmt.Errorf("invalid next hop IP %s", item.NextHopIP)
		}
	}

	for _, item := range vpc.Spec.PolicyRoutes {
		if item.Action != kubeovnv1.PolicyRouteActionReroute &&
			item.Action != kubeovnv1.PolicyRouteActionAllow &&
			item.Action != kubeovnv1.PolicyRouteActionDrop {
			return fmt.Errorf("unknown policy action: %s", item.Action)
		}

		if item.Action == kubeovnv1.PolicyRouteActionReroute {
			// ecmp policy route may reroute to multiple next hop ips
			for ipStr := range strings.SplitSeq(item.NextHopIP, ",") {
				if ip := net.ParseIP(ipStr); ip == nil {
					return fmt.Errorf("invalid next hop ips: %s", item.NextHopIP)
				}
			}
		}
	}

	for _, item := range vpc.Spec.VpcPeerings {
		if err := CheckCidrs(item.LocalConnectIP); err != nil {
			klog.Error(err)
			return fmt.Errorf("invalid cidr %s", item.LocalConnectIP)
		}
	}

	return nil
}
