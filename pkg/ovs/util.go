package ovs

import (
	"fmt"
	"strings"
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
