package ovs

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/alauda/kube-ovn/pkg/util"
)

// PodNameToPortName return the ovn port name for a given pod
func PodNameToPortName(pod, namespace string) string {
	return fmt.Sprintf("%s.%s", pod, namespace)
}

func PodNameToLocalnetName(subnet string) string {
	return fmt.Sprintf("localnet.%s", subnet)
}

func trimCommandOutput(raw []byte) string {
	output := strings.TrimSpace(string(raw))
	return strings.Trim(output, "\"")
}

// ExpandExcludeIPs parse ovn exclude_ips to ip slice
func ExpandExcludeIPs(excludeIPs []string, cidr string) []string {
	rv := []string{}
	for _, excludeIP := range excludeIPs {
		if strings.Contains(excludeIP, "..") {
			for _, cidrBlock := range strings.Split(cidr, ",") {
				subnetNum := util.SubnetNumber(cidrBlock)
				broadcast := util.SubnetBroadCast(cidrBlock)
				parts := strings.Split(excludeIP, "..")
				s := util.Ip2BigInt(parts[0])
				e := util.Ip2BigInt(parts[1])
				for s.Cmp(e) <= 0 {
					ipStr := util.BigInt2Ip(s)
					if ipStr != subnetNum && ipStr != broadcast && util.CIDRContainIP(cidrBlock, ipStr) && !util.ContainsString(rv, ipStr) {
						rv = append(rv, ipStr)
					}
					s.Add(s, big.NewInt(1))
				}
			}
		} else {
			rv = append(rv, excludeIP)
		}
	}
	return rv
}
