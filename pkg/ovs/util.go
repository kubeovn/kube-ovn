package ovs

import (
	"fmt"
	"regexp"
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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

func LogicalRouterPortName(lr, ls string) string {
	return fmt.Sprintf("%s-%s", lr, ls)
}

func LogicalSwitchPortName(lr, ls string) string {
	return fmt.Sprintf("%s-%s", ls, lr)
}

// parseIpv6RaConfigs parses the ipv6 ra config,
// return default Ipv6RaConfigs when raw="",
// the raw config's format is: address_mode=dhcpv6_stateful,max_interval=30,min_interval=5,send_periodic=true
func parseIpv6RaConfigs(raw string) map[string]string {
	// return default Ipv6RaConfigs
	if len(raw) == 0 {
		return map[string]string{
			"address_mode":  "dhcpv6_stateful",
			"max_interval":  "30",
			"min_interval":  "5",
			"send_periodic": "true",
		}
	}

	Ipv6RaConfigs := make(map[string]string)

	// trim blank
	raw = strings.ReplaceAll(raw, " ", "")
	options := strings.Split(raw, ",")
	for _, option := range options {
		kv := strings.Split(option, "=")
		// TODO: ignore invalidate option, maybe need further validation
		if len(kv) != 2 || len(kv[0]) == 0 || len(kv[1]) == 0 {
			continue
		}
		Ipv6RaConfigs[kv[0]] = kv[1]
	}

	return Ipv6RaConfigs
}

// getIpv6Prefix get ipv6 prefix from networks
func getIpv6Prefix(networks []string) []string {
	ipv6Prefix := make([]string, 0, len(networks))
	for _, network := range networks {
		if kubeovnv1.ProtocolIPv6 == util.CheckProtocol(network) {
			ipv6Prefix = append(ipv6Prefix, strings.Split(network, "/")[1])
		}
	}

	return ipv6Prefix
}

func matchAddressSetName(asName string) (bool, error) {
	matched, err := regexp.MatchString(`^[a-zA-Z_.][a-zA-Z_.0-9]*$`, asName)
	if err != nil {
		return false, err
	}

	if !matched {
		return false, fmt.Errorf("address set %s must match `[a-zA-Z_.][a-zA-Z_.0-9]*`", asName)
	}

	return true, nil
}
