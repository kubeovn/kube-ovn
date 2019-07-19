package ovs

import (
	"fmt"
	"strings"

	"github.com/alauda/kube-ovn/pkg/util"
)

// PodNameToPortName return the ovn port name for a given pod
func PodNameToPortName(pod, namespace string) string {
	return fmt.Sprintf("%s.%s", pod, namespace)
}

func trimCommandOutput(raw []byte) string {
	output := strings.TrimSpace(string(raw))
	return strings.Trim(output, "\"")
}

// ExpandExcludeIPs parse ovn exclude_ips to ip slice
func ExpandExcludeIPs(excludeIPs []string) []string {
	rv := []string{}
	for _, excludeIP := range excludeIPs {
		if strings.Index(excludeIP, "..") != -1 {
			parts := strings.Split(excludeIP, "..")
			s := util.Ip2Long(parts[0])
			e := util.Ip2Long(parts[1])
			for s <= e {
				rv = append(rv, util.Long2Ip(s))
				s++
			}

		} else {
			rv = append(rv, excludeIP)
		}
	}
	return rv
}
