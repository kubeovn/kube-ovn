package ovs

import (
	"fmt"
	"regexp"
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var addressSetNameRegex = regexp.MustCompile(`^[a-zA-Z_.][a-zA-Z_.0-9]*$`)

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

func matchAddressSetName(asName string) bool {
	return addressSetNameRegex.MatchString(asName)
}

type AclMatchRule interface {
	Rule() (string, error)
	String() string
}

type AndAclMatchRule struct {
	rules []AclMatchRule
}

func NewAndAclMatchRule(rules ...AclMatchRule) AclMatchRule {
	return AndAclMatchRule{
		rules: rules,
	}
}

// Rule generate acl match rule like 'ip4.src == $test.allow.as && ip4.src != $test.except.as && 12345 <= tcp.dst <= 12500 && outport == @ovn.sg.test_sg && ip'
func (s AndAclMatchRule) Rule() (string, error) {
	var rules []string
	for _, specification := range s.rules {
		rule, err := specification.Rule()
		if err != nil {
			return "", fmt.Errorf("generate rule %s: %v", rule, err)
		}
		rules = append(rules, rule)
	}

	return strings.Join(rules, " && "), nil
}

func (s AndAclMatchRule) String() string {
	rule, _ := s.Rule()

	return rule
}

type aclRuleKv struct {
	key      string
	value    string
	maxValue string
	effect   string
}

func NewAclRuleKv(key, effect, value, maxValue string) AclMatchRule {
	return aclRuleKv{
		key:      key,
		effect:   effect,
		value:    value,
		maxValue: maxValue,
	}
}

// Rule generate acl match rule like
// 'ip4.src == $test.allow.as'
// or 'ip4.src != $test.except.as'
// or '12345 <= tcp.dst <= 12500'
// or 'tcp.dst == 13500'
// or 'outport == @ovn.sg.test_sg && ip'
func (kv aclRuleKv) Rule() (string, error) {
	// key must exist at least
	if len(kv.key) == 0 {
		return "", fmt.Errorf("acl rule key is required")
	}

	// like 'ip'
	if len(kv.effect) == 0 || len(kv.value) == 0 {
		return kv.key, nil
	}

	// like 'tcp.dst == 13500' or 'ip4.src == $test.allow.as'
	if len(kv.maxValue) == 0 {
		return fmt.Sprintf("%s %s %s", kv.key, kv.effect, kv.value), nil
	}

	// like '12345 <= tcp.dst <= 12500'
	return fmt.Sprintf("%s %s %s %s %s", kv.value, kv.effect, kv.key, kv.effect, kv.maxValue), nil
}

func (kv aclRuleKv) String() string {
	rule, _ := kv.Rule()

	return rule
}
