package speaker

import (
	v1 "k8s.io/api/core/v1"
	"strconv"
	"strings"
)

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
