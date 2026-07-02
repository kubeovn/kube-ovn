package util

import (
	"fmt"
	"strings"

	"golang.org/x/sys/unix"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// ProtocolToFamily converts protocol string to netlink family
func ProtocolToFamily(protocol string) (int, error) {
	switch protocol {
	case kubeovnv1.ProtocolDual:
		return unix.AF_UNSPEC, nil
	case kubeovnv1.ProtocolIPv4:
		return unix.AF_INET, nil
	case kubeovnv1.ProtocolIPv6:
		return unix.AF_INET6, nil
	default:
		return -1, fmt.Errorf("invalid protocol: %s", protocol)
	}
}

// NormalizeIPFamily converts annotation values to kube-ovn protocol constants.
// Only lowercase ipv4/ipv6 are accepted by validation; this helper keeps callers
// working with the same protocol strings used by subnets and IPAM.
func NormalizeIPFamily(ipFamily string) string {
	switch ipFamily {
	case strings.ToLower(kubeovnv1.ProtocolIPv4):
		return kubeovnv1.ProtocolIPv4
	case strings.ToLower(kubeovnv1.ProtocolIPv6):
		return kubeovnv1.ProtocolIPv6
	default:
		return ipFamily
	}
}
