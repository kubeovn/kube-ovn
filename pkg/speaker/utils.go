package speaker

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/osrg/gobgp/v4/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// prefixMap is a map associating an IP family (IPv4 or IPv6) and an IP
type prefixMap map[int]set.Set[string]

// addExpectedPrefix adds a new prefix to the list of expected prefixes we should be announcing
func addExpectedPrefix(ip string, expectedPrefixes prefixMap) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		klog.Errorf("failed to parse IP address %q: %v", ip, err)
		return
	}

	bitLen := addr.BitLen()
	prefix := netip.PrefixFrom(addr, bitLen).String()
	if expectedPrefixes[bitLen] == nil {
		expectedPrefixes[bitLen] = set.New(prefix)
	} else {
		expectedPrefixes[bitLen].Insert(prefix)
	}
}

// isPodAlive returns whether a Pod is alive or not
func isPodAlive(p *corev1.Pod) bool {
	if p.Status.Phase == corev1.PodSucceeded && p.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		return false
	}

	if p.Status.Phase == corev1.PodFailed && p.Spec.RestartPolicy == corev1.RestartPolicyNever {
		return false
	}

	if p.Status.Phase == corev1.PodFailed && p.Status.Reason == "Evicted" {
		return false
	}
	return true
}

// isClusterIPService returns whether a Service is of type ClusterIP or not
func isClusterIPService(svc *corev1.Service) bool {
	return svc.Spec.Type == corev1.ServiceTypeClusterIP &&
		svc.Spec.ClusterIP != corev1.ClusterIPNone &&
		len(svc.Spec.ClusterIP) != 0
}

// parseRoute returns the prefix and length of the prefix (in bits) by parsing the received route
// If no prefix is mentioned in the route (e.g 1.1.1.1 instead of 1.1.1.1/32), the prefix length
// is assumed to be 32 bits
func parseRoute(route string) (netip.Prefix, error) {
	if strings.Contains(route, "/") {
		return netip.ParsePrefix(route)
	}
	addr, err := netip.ParseAddr(route)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// getGatewayName returns the name of the NAT GW hosting this speaker
func getGatewayName() string {
	return os.Getenv(util.GatewayNameEnv)
}

// bitLenToAFI converts bit length to BGP AFI
func bitLenToAFI(bitLen int) (api.Family_Afi, error) {
	switch bitLen {
	case net.IPv4len * 8:
		return api.Family_AFI_IP, nil
	case net.IPv6len * 8:
		return api.Family_AFI_IP6, nil
	default:
		return api.Family_AFI_UNSPECIFIED, fmt.Errorf("invalid bit length %d", bitLen)
	}
}
