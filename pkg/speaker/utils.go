package speaker

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/osrg/gobgp/v4/api"
	corev1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// prefixMap is a map associating an IP family (IPv4 or IPv6) and an IP
type prefixMap map[string][]string

// addExpectedPrefix adds a new prefix to the list of expected prefixes we should be announcing
func addExpectedPrefix(ip string, expectedPrefixes prefixMap) {
	ipFamily := util.CheckProtocol(ip)
	prefix := fmt.Sprintf("%s/%d", ip, maskMap[ipFamily])
	expectedPrefixes[ipFamily] = append(expectedPrefixes[ipFamily], prefix)
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

// routeDiff returns the routes that should be added and the routes that should be deleted
// after receiving the routes we except to advertise versus the route we are advertising
func routeDiff(expected, exists []string) (toAdd, toDel []string) {
	expectedMap, existsMap := map[string]bool{}, map[string]bool{}
	for _, e := range expected {
		expectedMap[e] = true
	}
	for _, e := range exists {
		existsMap[e] = true
	}

	for e := range expectedMap {
		if !existsMap[e] {
			toAdd = append(toAdd, e)
		}
	}

	for e := range existsMap {
		if !expectedMap[e] {
			toDel = append(toDel, e)
		}
	}

	return toAdd, toDel
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

// kubeOvnFamilyToAFI converts an IP family to its associated AFI
func kubeOvnFamilyToAFI(ipFamily string) (api.Family_Afi, error) {
	var family api.Family_Afi
	switch ipFamily {
	case kubeovnv1.ProtocolIPv4:
		family = api.Family_AFI_IP
	case kubeovnv1.ProtocolIPv6:
		family = api.Family_AFI_IP6
	default:
		return api.Family_AFI_UNSPECIFIED, errors.New("ip family is invalid")
	}

	return family, nil
}
