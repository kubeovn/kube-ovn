package controller

import (
	"errors"
	"net"
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// SubnetStatusIPProjectionInput contains the read-only inputs required to
// calculate the IP-related portion of Subnet status. Callers own collection
// of IP, VIP, and EIP objects; this function performs no API or IPAM writes.
type SubnetStatusIPProjectionInput struct {
	UsingIPs           internal.BigInt
	V4UsingIPRange     string
	V6UsingIPRange     string
	V4AvailableIPRange string
	V6AvailableIPRange string
}

// ProjectSubnetStatusIP calculates IP counters and ranges on a copy of the
// Subnet. The legacy controller uses this projection before its existing
// targeted status patch, while future controller-runtime writers can consume
// the same pure result without taking ownership of the API write.
func ProjectSubnetStatusIP(subnet *kubeovnv1.Subnet, input SubnetStatusIPProjectionInput) (*kubeovnv1.Subnet, error) {
	if subnet == nil {
		return nil, errors.New("subnet status projection requires a Subnet")
	}
	if err := util.CheckCidrs(subnet.Spec.CIDRBlock); err != nil {
		return nil, err
	}
	cidrBlocks := strings.Split(subnet.Spec.CIDRBlock, ",")
	parsedCIDRs, err := validateSubnetStatusProjectionCIDRs(cidrBlocks, subnet.Spec.Protocol)
	if err != nil {
		return nil, err
	}

	v4AvailableIPs, v6AvailableIPs := internal.BigInt{}, internal.BigInt{}
	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolDual:
		v4ExcludeIPs, v6ExcludeIPs := util.SplitIpsByProtocol(subnet.Spec.ExcludeIps)
		v4toSubIPs := util.ExpandExcludeIPs(v4ExcludeIPs, cidrBlocks[0])
		v6toSubIPs := util.ExpandExcludeIPs(v6ExcludeIPs, cidrBlocks[1])
		v4CIDR, v6CIDR := parsedCIDRs[0], parsedCIDRs[1]
		v4AvailableIPs = util.AddressCountBigInt(v4CIDR).Sub(util.CountIPNumsBigInt(v4toSubIPs)).Sub(input.UsingIPs)
		v6AvailableIPs = util.AddressCountBigInt(v6CIDR).Sub(util.CountIPNumsBigInt(v6toSubIPs)).Sub(input.UsingIPs)
	case kubeovnv1.ProtocolIPv4:
		cidr := parsedCIDRs[0]
		toSubIPs := util.ExpandExcludeIPs(subnet.Spec.ExcludeIps, subnet.Spec.CIDRBlock)
		v4AvailableIPs = util.AddressCountBigInt(cidr).Sub(util.CountIPNumsBigInt(toSubIPs)).Sub(input.UsingIPs)
	case kubeovnv1.ProtocolIPv6:
		cidr := parsedCIDRs[0]
		toSubIPs := util.ExpandExcludeIPs(subnet.Spec.ExcludeIps, subnet.Spec.CIDRBlock)
		v6AvailableIPs = util.AddressCountBigInt(cidr).Sub(util.CountIPNumsBigInt(toSubIPs)).Sub(input.UsingIPs)
	}

	if v4AvailableIPs.Sign() < 0 {
		v4AvailableIPs = internal.BigInt{}
	}
	if v6AvailableIPs.Sign() < 0 {
		v6AvailableIPs = internal.BigInt{}
	}

	v4UsingIPs, v6UsingIPs := input.UsingIPs.Clone(), input.UsingIPs.Clone()
	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolIPv4:
		v6UsingIPs = internal.BigInt{}
	case kubeovnv1.ProtocolIPv6:
		v4UsingIPs = internal.BigInt{}
	}

	projected := subnet.DeepCopy()
	projected.Status.V4AvailableIPs = v4AvailableIPs
	projected.Status.V6AvailableIPs = v6AvailableIPs
	projected.Status.V4UsingIPs = v4UsingIPs
	projected.Status.V6UsingIPs = v6UsingIPs
	projected.Status.V4UsingIPRange = input.V4UsingIPRange
	projected.Status.V6UsingIPRange = input.V6UsingIPRange
	projected.Status.V4AvailableIPRange = input.V4AvailableIPRange
	projected.Status.V6AvailableIPRange = input.V6AvailableIPRange
	return projected, nil
}

func validateSubnetStatusProjectionCIDRs(cidrBlocks []string, protocol string) ([]*net.IPNet, error) {
	expected := 0
	switch protocol {
	case kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6:
		expected = 1
	case kubeovnv1.ProtocolDual:
		expected = 2
	default:
		return nil, nil
	}
	if len(cidrBlocks) != expected {
		return nil, errors.New("subnet status projection CIDR count does not match protocol")
	}

	parsed := make([]*net.IPNet, len(cidrBlocks))
	for i, cidrBlock := range cidrBlocks {
		ip, cidr, err := net.ParseCIDR(cidrBlock)
		if err != nil {
			return nil, err
		}
		if (protocol == kubeovnv1.ProtocolIPv4 && ip.To4() == nil) ||
			(protocol == kubeovnv1.ProtocolIPv6 && ip.To4() != nil) ||
			(protocol == kubeovnv1.ProtocolDual && ((i == 0 && ip.To4() == nil) || (i == 1 && ip.To4() != nil))) {
			return nil, errors.New("subnet status projection CIDR family does not match protocol")
		}
		parsed[i] = cidr
	}
	return parsed, nil
}
