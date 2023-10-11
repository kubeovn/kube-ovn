package ovs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

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

func GetLocalnetName(subnet string) string {
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

func GetSgPortGroupName(sgName string) string {
	return strings.ReplaceAll(fmt.Sprintf("ovn.sg.%s", sgName), "-", ".")
}

func GetSgV4AssociatedName(sgName string) string {
	return strings.ReplaceAll(fmt.Sprintf("ovn.sg.%s.associated.v4", sgName), "-", ".")
}

func GetSgV6AssociatedName(sgName string) string {
	return strings.ReplaceAll(fmt.Sprintf("ovn.sg.%s.associated.v6", sgName), "-", ".")
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

// parseDHCPOptions parses dhcp options,
// the raw option's format is: server_id=192.168.123.50,server_mac=00:00:00:08:0a:11
func parseDHCPOptions(raw string) map[string]string {
	// return default Ipv6RaConfigs
	if len(raw) == 0 {
		return nil
	}

	dhcpOpt := make(map[string]string)

	// trim blank
	raw = strings.ReplaceAll(raw, " ", "")
	options := strings.Split(raw, ",")
	for _, option := range options {
		kv := strings.Split(option, "=")
		// TODO: ignore invalidate option, maybe need further validation
		if len(kv) != 2 || len(kv[0]) == 0 || len(kv[1]) == 0 {
			continue
		}
		dhcpOpt[kv[0]] = kv[1]
	}

	return dhcpOpt
}

func matchAddressSetName(asName string) bool {
	return addressSetNameRegex.MatchString(asName)
}

type ACLMatch interface {
	Match() (string, error)
	String() string
}

type AndACLMatch struct {
	matches []ACLMatch
}

func NewAndACLMatch(matches ...ACLMatch) ACLMatch {
	return AndACLMatch{
		matches: matches,
	}
}

// Rule generate acl match like 'ip4.src == $test.allow.as && ip4.src != $test.except.as && 12345 <= tcp.dst <= 12500 && outport == @ovn.sg.test_sg && ip'
func (m AndACLMatch) Match() (string, error) {
	var matches []string
	for _, r := range m.matches {
		match, err := r.Match()
		if err != nil {
			return "", fmt.Errorf("generate match %s: %v", match, err)
		}
		matches = append(matches, match)
	}

	return strings.Join(matches, " && "), nil
}

func (m AndACLMatch) String() string {
	match, _ := m.Match()
	return match
}

type OrACLMatch struct {
	matches []ACLMatch
}

func NewOrACLMatch(matches ...ACLMatch) ACLMatch {
	return OrACLMatch{
		matches: matches,
	}
}

// Match generate acl match like '(ip4.src==10.250.0.0/16 && ip4.dst==10.244.0.0/16) || (ip4.src==10.244.0.0/16 && ip4.dst==10.250.0.0/16)'
func (m OrACLMatch) Match() (string, error) {
	var matches []string
	for _, specification := range m.matches {
		match, err := specification.Match()
		if err != nil {
			return "", fmt.Errorf("generate match %s: %v", match, err)
		}

		// has more then one rule
		if strings.Contains(match, "&&") {
			match = "(" + match + ")"
		}

		matches = append(matches, match)
	}

	return strings.Join(matches, " || "), nil
}

func (m OrACLMatch) String() string {
	match, _ := m.Match()
	return match
}

type aclMatch struct {
	key      string
	value    string
	maxValue string
	effect   string
}

func NewACLMatch(key, effect, value, maxValue string) ACLMatch {
	return aclMatch{
		key:      key,
		effect:   effect,
		value:    value,
		maxValue: maxValue,
	}
}

// Match generate acl match like
// 'ip4.src == $test.allow.as'
// or 'ip4.src != $test.except.as'
// or '12345 <= tcp.dst <= 12500'
// or 'tcp.dst == 13500'
// or 'outport == @ovn.sg.test_sg && ip'
func (m aclMatch) Match() (string, error) {
	// key must exist at least
	if len(m.key) == 0 {
		return "", fmt.Errorf("acl rule key is required")
	}

	// like 'ip'
	if len(m.effect) == 0 || len(m.value) == 0 {
		return m.key, nil
	}

	// like 'tcp.dst == 13500' or 'ip4.src == $test.allow.as'
	if len(m.maxValue) == 0 {
		return fmt.Sprintf("%s %s %s", m.key, m.effect, m.value), nil
	}

	// like '12345 <= tcp.dst <= 12500'
	return fmt.Sprintf("%s %s %s %s %s", m.value, m.effect, m.key, m.effect, m.maxValue), nil
}

func (m aclMatch) String() string {
	rule, _ := m.Match()
	return rule
}

type Limiter struct {
	limit   int32
	current int32
}

func (l *Limiter) Limit() int32 {
	return l.limit
}

func (l *Limiter) Current() int32 {
	return atomic.LoadInt32(&l.current)
}

func (l *Limiter) Update(limit int32) {
	l.limit = limit
}

func (l *Limiter) Wait(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled by timeout")
		default:
			if l.limit == 0 {
				atomic.AddInt32(&l.current, 1)
				return nil
			}

			if atomic.LoadInt32(&l.current) < l.limit {
				atomic.AddInt32(&l.current, 1)
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (l *Limiter) Done() {
	atomic.AddInt32(&l.current, -1)
}
