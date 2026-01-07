package speaker

import (
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

// prefixMap is a map associating an BGP address family (IPv4 or IPv6) and an IP set
type prefixMap map[api.Family_Afi]set.Set[string]

// addExpectedPrefix adds a new prefix to the list of expected prefixes we should be announcing
func addExpectedPrefix(ip string, expectedPrefixes prefixMap) {
	prefix, err := parsePrefix(ip)
	if err != nil {
		klog.Errorf("failed to parse prefix of address %q: %v", ip, err)
		return
	}

	if afi := prefixToAFI(prefix); expectedPrefixes[afi] == nil {
		expectedPrefixes[afi] = set.New(prefix.String())
	} else {
		expectedPrefixes[afi].Insert(prefix.String())
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

// parsePrefix returns the prefix by parsing the received ip address or network string
// If the input is an IP address, it converts it to a /32 or /128 prefix
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// getGatewayName returns the name of the NAT GW hosting this speaker
func getGatewayName(config *Configuration) string {
	if config != nil && config.VpcNatGatewayName != "" {
		return config.VpcNatGatewayName
	}
	return os.Getenv(util.EnvGatewayName)
}

// prefixToAFI converts a network prefix to BGP AFI by checking its bit length
func prefixToAFI(prefix netip.Prefix) api.Family_Afi {
	switch prefix.Addr().BitLen() {
	case net.IPv4len * 8:
		return api.Family_AFI_IP
	case net.IPv6len * 8:
		return api.Family_AFI_IP6
	default:
		return api.Family_AFI_UNSPECIFIED
	}
}
