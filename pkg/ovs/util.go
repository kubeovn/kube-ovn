package ovs

import (
	"fmt"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// PodNameToPortName return the ovn port name for a given pod
func PodNameToPortName(pod, namespace, provider string) string {
	if provider == util.OvnProvider {
		return fmt.Sprintf("%s.%s", pod, namespace)
	}
	return fmt.Sprintf("%s.%s.%s", pod, namespace, provider)
}

func PodNameToLocalnetName(subnet string) string {
	return fmt.Sprintf("localnet.%s", subnet)
}

func trimCommandOutput(raw []byte) string {
	output := strings.TrimSpace(string(raw))
	return strings.Trim(output, "\"")
}
