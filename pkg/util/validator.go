package util

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	V6Multicast = "ff00::/8"
	V4Multicast = "224.0.0.0/4"
	V4Loopback  = "127.0.0.1/8"
	V6Loopback  = "::1/128"
)

func cidrConflict(cidr string) error {
	if CIDRConflict(cidr, V6Multicast) {
		return fmt.Errorf("%s conflict with v6 multicast cidr %s", cidr, V6Multicast)
	}
	if CIDRConflict(cidr, V4Multicast) {
		return fmt.Errorf("%s conflict with v4 multicast cidr %s", cidr, V4Multicast)
	}
	if CIDRConflict(cidr, V6Loopback) {
		return fmt.Errorf("%s conflict with v6 loopback cidr %s", cidr, V6Loopback)
	}
	if CIDRConflict(cidr, V4Loopback) {
		return fmt.Errorf("%s conflict with v4 multicast cidr %s", cidr, V4Loopback)
	}
	return nil
}

func ValidateSubnet(subnet kubeovnv1.Subnet) error {
	if !CIDRContainIP(subnet.Spec.CIDRBlock, subnet.Spec.Gateway) {
		return fmt.Errorf(" gateway %s is not in cidr %s", subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
	}
	if err := cidrConflict(subnet.Spec.CIDRBlock); err != nil {
		return err
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

	if subnet.Spec.Vpc == DefaultVpc {
		k8sApiServer := os.Getenv("KUBERNETES_SERVICE_HOST")
		if k8sApiServer != "" && CIDRContainIP(subnet.Spec.CIDRBlock, k8sApiServer) {
			return fmt.Errorf("subnet %s cidr %s conflicts with k8s apiserver svc ip %s", subnet.Name, subnet.Spec.CIDRBlock, k8sApiServer)
		}
	}
	return nil
}

func ValidatePodNetwork(annotations map[string]string) error {
	if ip := annotations[IpAddressAnnotation]; ip != "" {
		if strings.Contains(ip, "/") {
			_, _, err := net.ParseCIDR(ip)
			if err != nil {
				return fmt.Errorf("%s is not a valid %s", ip, IpAddressAnnotation)
			}
		} else {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("%s is not a valid %s", ip, IpAddressAnnotation)
			}
		}
		if cidr := annotations[CidrAnnotation]; cidr != "" {
			if !CIDRContainIP(cidr, ip) {
				return fmt.Errorf("%s not in cidr %s", ip, cidr)
			}
		}
	}

	mac := annotations[MacAddressAnnotation]
	if mac != "" {
		if _, err := net.ParseMAC(mac); err != nil {
			return fmt.Errorf("%s is not a valid %s", mac, MacAddressAnnotation)
		}
	}

	ipPool := annotations[IpPoolAnnotation]
	if ipPool != "" {
		for _, ip := range strings.Split(ipPool, ",") {
			if net.ParseIP(strings.TrimSpace(ip)) == nil {
				return fmt.Errorf("%s in %s is not a valid address", ip, IpPoolAnnotation)
			}
		}
	}

	ingress := annotations[IngressRateAnnotation]
	if ingress != "" {
		if _, err := strconv.Atoi(ingress); err != nil {
			return fmt.Errorf("%s is not a valid %s", ingress, IngressRateAnnotation)
		}
	}

	egress := annotations[EgressRateAnnotation]
	if egress != "" {
		if _, err := strconv.Atoi(egress); err != nil {
			return fmt.Errorf("%s is not a valid %s", egress, EgressRateAnnotation)
		}
	}

	return nil
}

func ValidatePodCidr(cidr, ip string) error {
	ipStr := IPToString(ip)
	if SubnetBroadCast(cidr) == ipStr {
		return fmt.Errorf("%s is the broadcast ip in cidr %s", ipStr, cidr)
	}
	if SubnetNumber(cidr) == ipStr {
		return fmt.Errorf("%s is the network number ip in cidr %s", ipStr, cidr)
	}

	return nil
}

func ValidateVlan(vlan int, vlanRange string) error {
	vlans := strings.Split(vlanRange, ",")
	if len(vlans) != 2 {
		return fmt.Errorf("the vlan range %s is invalid", vlanRange)
	}

	min, err := strconv.Atoi(vlans[0])
	if err != nil {
		return err
	}

	max, err := strconv.Atoi(vlans[1])
	if err != nil {
		return err
	}

	if vlan == 0 {
		return nil
	}

	if vlan < min || vlan > max {
		return fmt.Errorf("the vlan is not in vlan range %s", vlanRange)
	}

	return nil
}
