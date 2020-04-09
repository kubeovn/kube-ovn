package util

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
)

func ValidateSubnet(subnet kubeovnv1.Subnet) error {
	if !CIDRContainIP(subnet.Spec.CIDRBlock, subnet.Spec.Gateway) {
		return fmt.Errorf(" gateway %s is not in cidr %s", subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
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

func ValidateVlan(vlan int, vlanRange string) error {
	vlans := strings.SplitN(vlanRange, ",", 2)
	if len(vlans) != 2 {
		return fmt.Errorf("the vlan range %s is in valid", vlanRange)
	}

	min, err := strconv.Atoi(vlans[0])
	if err != nil {
		return err
	}

	max, err := strconv.Atoi(vlans[1])
	if err != nil {
		return err
	}

	if vlan < min || vlan > max {
		return fmt.Errorf("the vlan is not in vlan range %s", vlanRange)
	}

	return nil
}
