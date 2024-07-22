package speaker

import (
	"fmt"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	bgpapi "github.com/osrg/gobgp/v3/api"
	v1 "k8s.io/api/core/v1"
	"os"
	"strconv"
	"strings"
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
func isPodAlive(p *v1.Pod) bool {
	if p.Status.Phase == v1.PodSucceeded && p.Spec.RestartPolicy != v1.RestartPolicyAlways {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Spec.RestartPolicy == v1.RestartPolicyNever {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Status.Reason == "Evicted" {
		return false
	}
	return true
}

// isClusterIPService returns whether a Service is of type ClusterIP or not
func isClusterIPService(svc *v1.Service) bool {
	return svc.Spec.Type == v1.ServiceTypeClusterIP &&
		svc.Spec.ClusterIP != v1.ClusterIPNone &&
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
func parseRoute(route string) (string, uint32, error) {
	var prefixLen uint32 = 32
	prefix := route
	if strings.Contains(route, "/") {
		prefix = strings.Split(route, "/")[0]
		strLen := strings.Split(route, "/")[1]
		intLen, err := strconv.Atoi(strLen)
		if err != nil {
			return "", 0, err
		}
		prefixLen = uint32(intLen)
	}
	return prefix, prefixLen, nil
}

// getGatewayName returns the name of the NAT GW hosting this speaker
func getGatewayName() string {
	hostname := os.Getenv(HostnameEnvVariable)
	hostnameSplit := strings.Split(hostname, "-")
	splitLength := len(hostnameSplit)

	// The name of the GW is right before the index in the name of the pod
	// For example: "vpc-nat-gw-gw1-0" is the name of the pod hosting the NAT GW "gw1"
	if splitLength < 2 {
		return ""
	}

	return hostnameSplit[splitLength-2]
}

// kubeOvnFamilyToAFI converts an IP family to its associated AFI
func kubeOvnFamilyToAFI(ipFamily string) (bgpapi.Family_Afi, error) {
	var family bgpapi.Family_Afi
	if ipFamily == kubeovnv1.ProtocolIPv6 {
		family = bgpapi.Family_AFI_IP6
	} else if ipFamily == kubeovnv1.ProtocolIPv4 {
		family = bgpapi.Family_AFI_IP
	} else {
		return bgpapi.Family_AFI_UNKNOWN, fmt.Errorf("ip family is invalid")
	}

	return family, nil
}
