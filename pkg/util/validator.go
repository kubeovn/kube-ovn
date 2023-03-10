package util

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func ValidateSubnet(subnet kubeovnv1.Subnet) error {
	if subnet.Spec.Gateway != "" && !CIDRContainIP(subnet.Spec.CIDRBlock, subnet.Spec.Gateway) {
		return fmt.Errorf(" gateway %s is not in cidr %s", subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
	}
	if err := CIDRGlobalUnicast(subnet.Spec.CIDRBlock); err != nil {
		return err
	}
	if CheckProtocol(subnet.Spec.CIDRBlock) == "" {
		return fmt.Errorf("CIDRBlock: %s formal error", subnet.Spec.CIDRBlock)
	}
	excludeIps := subnet.Spec.ExcludeIps
	for _, ipr := range excludeIps {
		ips := strings.Split(ipr, "..")
		if len(ips) > 2 {
			return fmt.Errorf("%s in excludeIps is not a valid ip range", ipr)
		}

		if len(ips) == 1 {
			if net.ParseIP(ips[0]) == nil {
				return fmt.Errorf("ip %s in exclude_ips is not a valid address", ips[0])
			}
		}

		if len(ips) == 2 {
			for _, ip := range ips {
				if net.ParseIP(ip) == nil {
					return fmt.Errorf("ip %s in exclude_ips is not a valid address", ip)
				}
			}
			if Ip2BigInt(ips[0]).Cmp(Ip2BigInt(ips[1])) == 1 {
				return fmt.Errorf("%s in excludeIps is not a valid ip range", ipr)
			}
		}
	}

	for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("subnet %s cidr %s is invalid", subnet.Name, cidr)
		}
	}

	allow := subnet.Spec.AllowSubnets
	for _, cidr := range allow {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
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

	if subnet.Spec.Vpc == DefaultVpc {
		k8sApiServer := os.Getenv("KUBERNETES_SERVICE_HOST")
		if k8sApiServer != "" && CIDRContainIP(subnet.Spec.CIDRBlock, k8sApiServer) {
			return fmt.Errorf("subnet %s cidr %s conflicts with k8s apiserver svc ip %s", subnet.Name, subnet.Spec.CIDRBlock, k8sApiServer)
		}
	}

	if egw := subnet.Spec.ExternalEgressGateway; egw != "" {
		if subnet.Spec.NatOutgoing {
			return fmt.Errorf("conflict configuration: natOutgoing and externalEgressGateway")
		}
		ips := strings.Split(egw, ",")
		if len(ips) > 2 {
			return fmt.Errorf("invalid external egress gateway configuration")
		}
		for _, ip := range ips {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("IP %s in externalEgressGateway is not a valid address", ip)
			}
		}
		egwProtocol, cidrProtocol := CheckProtocol(egw), CheckProtocol(subnet.Spec.CIDRBlock)
		if egwProtocol != cidrProtocol && cidrProtocol != kubeovnv1.ProtocolDual {
			return fmt.Errorf("invalid external egress gateway configuration: address family is conflict with CIDR")
		}
	}

	if len(subnet.Spec.Vips) != 0 {
		for _, vip := range subnet.Spec.Vips {
			if !CIDRContainIP(subnet.Spec.CIDRBlock, vip) {
				return fmt.Errorf("vip %s conflicts with subnet %s cidr %s", vip, subnet.Name, subnet.Spec.CIDRBlock)
			}
		}
	}
	return nil
}

func ValidatePodNetwork(annotations map[string]string) error {
	errors := []error{}

	if ipAddress := annotations[IpAddressAnnotation]; ipAddress != "" {
		// The format of IP Annotation in dual-stack is 10.244.0.0/16,fd00:10:244:0:2::/80
		for _, ip := range strings.Split(ipAddress, ",") {
			if strings.Contains(ip, "/") {
				if _, _, err := net.ParseCIDR(ip); err != nil {
					errors = append(errors, fmt.Errorf("%s is not a valid %s", ip, IpAddressAnnotation))
					continue
				}
			} else {
				if net.ParseIP(ip) == nil {
					errors = append(errors, fmt.Errorf("%s is not a valid %s", ip, IpAddressAnnotation))
					continue
				}
			}

			if cidrStr := annotations[CidrAnnotation]; cidrStr != "" {
				if err := CheckCidrs(cidrStr); err != nil {
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
			errors = append(errors, fmt.Errorf("%s is not a valid %s", mac, MacAddressAnnotation))
		}
	}

	ipPool := annotations[IpPoolAnnotation]
	if ipPool != "" {
		for _, ips := range strings.Split(ipPool, ";") {
			if cidrStr := annotations[CidrAnnotation]; cidrStr != "" {
				if !CIDRContainIP(cidrStr, ips) {
					errors = append(errors, fmt.Errorf("%s not in cidr %s", ips, cidrStr))
					continue
				}
			}

			for _, ip := range strings.Split(ips, ",") {
				if net.ParseIP(strings.TrimSpace(ip)) == nil {
					errors = append(errors, fmt.Errorf("%s in %s is not a valid address", ip, IpPoolAnnotation))
				}
			}
		}
	}

	ingress := annotations[IngressRateAnnotation]
	if ingress != "" {
		if _, err := strconv.Atoi(ingress); err != nil {
			errors = append(errors, fmt.Errorf("%s is not a valid %s", ingress, IngressRateAnnotation))
		}
	}

	egress := annotations[EgressRateAnnotation]
	if egress != "" {
		if _, err := strconv.Atoi(egress); err != nil {
			errors = append(errors, fmt.Errorf("%s is not a valid %s", egress, EgressRateAnnotation))
		}
	}

	return utilerrors.NewAggregate(errors)
}

func ValidatePodCidr(cidr, ip string) error {
	for _, cidrBlock := range strings.Split(cidr, ",") {
		for _, ipAddr := range strings.Split(ip, ",") {
			if CheckProtocol(cidrBlock) != CheckProtocol(ipAddr) {
				continue
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
			if ip := net.ParseIP(item.NextHopIP); ip == nil {
				return fmt.Errorf("bad next hop ip: %s", item.NextHopIP)
			}
		}
	}

	for _, item := range vpc.Spec.VpcPeerings {
		if err := CheckCidrs(item.LocalConnectIP); err != nil {
			return fmt.Errorf("invalid cidr %s", item.LocalConnectIP)
		}
	}

	return nil
}
