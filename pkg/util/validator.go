package util

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
)

func ValidateSubnet(subnet kubeovnv1.Subnet) error {
	cidrStr := subnet.Spec.CIDRBlock
	if cidrStr == "" {
		return fmt.Errorf("cidr is required for logical switch")
	}
	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return fmt.Errorf("%s is a invalid cidr %v", cidrStr, err)
	}

	gatewayStr := subnet.Spec.Gateway
	if gatewayStr == "" {
		return fmt.Errorf("gateway is required for logical switch")
	}
	gateway := net.ParseIP(gatewayStr)
	if gateway == nil {
		return fmt.Errorf("%s  is not a valid gateway", gatewayStr)
	}

	if !cidr.Contains(gateway) {
		return fmt.Errorf("gateway address %s not in cidr range", gatewayStr)
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
