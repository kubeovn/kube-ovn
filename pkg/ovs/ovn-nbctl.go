package ovs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/klog"
)

func (c Client) ovnNbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout)}, cmdArgs...)
	raw, err := exec.Command(OvnNbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.Infof("%s command %s in %vms", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed)
	if err != nil {
		return "", fmt.Errorf("%s, %q", raw, err)
	}
	return trimCommandOutput(raw), nil
}

// DeletePort delete logical switch port in ovn
func (c Client) DeletePort(port string) error {
	if _, err := c.ovnNbCommand(IfExists, "lsp-del", port); err != nil {
		return fmt.Errorf("failed to delete port %s, %v", port, err)
	}
	return nil
}

// CreatePort create logical switch port in ovn
func (c Client) CreatePort(ls, port, ip, cidr, mac string) error {
	ovnCommand := []string{MayExist, "lsp-add", ls, port, "--",
		"lsp-set-addresses", port, fmt.Sprintf("%s %s", mac, ip), "--"}

	if ls != c.NodeSwitch {
		ovnCommand = append(ovnCommand,
			"lsp-set-port-security", port, fmt.Sprintf("%s %s/%s", mac, ip, strings.Split(cidr, "/")[1]))
	}

	if _, err := c.ovnNbCommand(ovnCommand...); err != nil {
		klog.Errorf("create port %s failed %v", port, err)
		return err
	}
	return nil
}

// CreateLogicalSwitch create logical switch in ovn, connect it to router and apply tcp/udp lb rules
func (c Client) CreateLogicalSwitch(ls, protocol, subnet, gateway string, excludeIps []string) error {
	var err error
	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		_, err = c.ovnNbCommand(MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", subnet), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " ")), "--",
			"acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", c.NodeSwitchCIDR), "allow-related")
	case kubeovnv1.ProtocolIPv6:
		_, err = c.ovnNbCommand(MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:ipv6_prefix=%s", strings.Split(subnet, "/")[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " ")), "--",
			"acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip6.src==%s", c.NodeSwitchCIDR), "allow-related")
	}

	if err != nil {
		klog.Errorf("create switch %s failed %v", ls, err)
		return err
	}

	mac := util.GenerateMac()
	mask := strings.Split(subnet, "/")[1]
	klog.Infof("create route port for switch %s", ls)
	if err := c.createRouterPort(ls, c.ClusterRouter, gateway+"/"+mask, mac); err != nil {
		klog.Errorf("failed to connect switch %s to router, %v", ls, err)
		return err
	}
	if ls != c.NodeSwitch {
		// DO NOT add ovn dns/lb to node switch
		if err := c.addLoadBalancerToLogicalSwitch(c.ClusterTcpLoadBalancer, ls); err != nil {
			klog.Errorf("failed to add cluster tcp lb to %s, %v", ls, err)
			return err
		}

		if err := c.addLoadBalancerToLogicalSwitch(c.ClusterUdpLoadBalancer, ls); err != nil {
			klog.Errorf("failed to add cluster udp lb to %s, %v", ls, err)
			return err
		}
	}

	return nil
}

// ListLogicalSwitch list logical switch names
func (c Client) ListLogicalSwitch() ([]string, error) {
	output, err := c.ovnNbCommand("ls-list")
	if err != nil {
		klog.Errorf("failed to list logical switch %v", err)
		return nil, err
	}

	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(l) == 0 || !strings.Contains(l, " ") {
			continue
		}
		tmp := strings.Split(l, " ")[1]
		tmp = strings.Trim(tmp, "()")
		result = append(result, tmp)
	}
	return result, nil
}

func (c Client) LogicalSwitchExists(logicalSwitch string) (bool, error) {
	lss, err := c.ListLogicalSwitch()
	if err != nil {
		return false, err
	}
	for _, ls := range lss {
		if ls == logicalSwitch {
			return true, nil
		}
	}
	return false, nil
}

func (c Client) ListLogicalSwitchPort() ([]string, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=name", "list", "logical_switch_port")
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		result = append(result, strings.TrimSpace(l))
	}
	return result, nil
}

// ListLogicalRouter list logical router names
func (c Client) ListLogicalRouter() ([]string, error) {
	output, err := c.ovnNbCommand("lr-list")
	if err != nil {
		klog.Errorf("failed to list logical router %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(l) == 0 || !strings.Contains(l, " ") {
			continue
		}
		tmp := strings.Split(l, " ")[1]
		tmp = strings.Trim(tmp, "()")
		result = append(result, tmp)
	}
	return result, nil
}

// DeleteLogicalSwitch delete logical switch and related router port
func (c Client) DeleteLogicalSwitch(ls string) error {
	if _, err := c.ovnNbCommand(IfExists, "lrp-del", fmt.Sprintf("%s-%s", c.ClusterRouter, ls)); err != nil {
		klog.Errorf("failed to del lrp %s-%s, %v", c.ClusterRouter, ls, err)
		return err
	}

	if _, err := c.ovnNbCommand(IfExists, "ls-del", ls); err != nil {
		klog.Errorf("failed to del ls %s, %v", ls, err)
		return err
	}
	return nil
}

// CreateLogicalRouter create logical router in ovn
func (c Client) CreateLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(MayExist, "lr-add", lr)
	return err
}

func (c Client) createRouterPort(ls, lr, ip, mac string) error {
	klog.Infof("add %s to %s with ip: %s, mac :%s", ls, lr, ip, mac)
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)
	_, err := c.ovnNbCommand(MayExist, "lsp-add", ls, lsTolr, "--",
		"set", "logical_switch_port", lsTolr, "type=router", "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("addresses=\"%s\"", mac), "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("options:router-port=%s", lrTols))
	if err != nil {
		klog.Errorf("failed to create switch router port %s %v", lsTolr, err)
		return err
	}

	if _, err := c.ovnNbCommand(MayExist, "lrp-add", lr, lrTols, mac, ip); err != nil {
		klog.Errorf("failed to create router port %s %v", lrTols, err)
		return err
	}
	return nil
}

type StaticRoute struct {
	Policy  string
	CIDR    string
	NextHop string
}

func (c Client) ListStaticRoute() ([]StaticRoute, error) {
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=ip_prefix,nexthop,policy", "list", "Logical_Router_Static_Route")
	if err != nil {
		return nil, err
	}
	entries := strings.Split(output, "\n")
	staticRoutes := make([]StaticRoute, 0, len(entries))
	for _, entry := range strings.Split(output, "\n") {
		if len(strings.Split(entry, ",")) == 3 {
			t := strings.Split(entry, ",")
			staticRoutes = append(staticRoutes, StaticRoute{CIDR: t[0], NextHop: t[1], Policy: t[2]})
		}
	}
	return staticRoutes, nil
}

// AddStaticRoute add a static route rule in ovn
func (c Client) AddStaticRoute(policy, cidr, nextHop, router string) error {
	if policy == "" {
		policy = PolicyDstIP
	}
	_, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "lr-route-add", router, cidr, nextHop)
	return err
}

// DeleteStaticRoute delete a static route rule in ovn
func (c Client) DeleteStaticRoute(cidr, router string) error {
	_, err := c.ovnNbCommand(IfExists, "lr-route-del", router, cidr)
	return err
}

func (c Client) DeleteStaticRouteByNextHop(nextHop string) error {
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=ip_prefix", "find", "Logical_Router_Static_Route", fmt.Sprintf("nexthop=%s", nextHop))
	if err != nil {
		klog.Errorf("failed to list static route %s, %v", nextHop, err)
		return err
	}
	ipPrefixes := strings.Split(output, "\n")
	for _, ipPre := range ipPrefixes {
		if strings.TrimSpace(ipPre) == "" {
			continue
		}
		if err := c.DeleteStaticRoute(ipPre, c.ClusterRouter); err != nil {
			klog.Errorf("failed to delete route %s, %v", ipPre, err)
			return err
		}
	}
	return nil
}

// FindLoadbalancer find ovn loadbalancer uuid by name
func (c Client) FindLoadbalancer(lb string) (string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
		"find", "load_balancer", fmt.Sprintf("name=%s", lb))
	count := len(strings.Split(output, "/n"))
	if count > 1 {
		klog.Errorf("%s has %d lb entries", lb, count)
		return "", fmt.Errorf("%s has %d lb entries", lb, count)
	}
	return output, err
}

// CreateLoadBalancer create loadbalancer in ovn
func (c Client) CreateLoadBalancer(lb, protocol string) error {
	_, err := c.ovnNbCommand("create", "load_balancer",
		fmt.Sprintf("name=%s", lb), fmt.Sprintf("protocol=%s", protocol))
	return err
}

// CreateLoadBalancerRule create loadbalancer rul in ovn
func (c Client) CreateLoadBalancerRule(lb, vip, ips, protocol string) error {
	_, err := c.ovnNbCommand(MayExist, "lb-add", lb, vip, ips, strings.ToLower(protocol))
	return err
}

func (c Client) addLoadBalancerToLogicalSwitch(lb, ls string) error {
	_, err := c.ovnNbCommand(MayExist, "ls-lb-add", ls, lb)
	return err
}

// DeleteLoadBalancerVip delete a vip rule from loadbalancer
func (c Client) DeleteLoadBalancerVip(vip, lb string) error {
	lbUuid, err := c.FindLoadbalancer(lb)
	if err != nil {
		klog.Errorf("failed to get lb %v", err)
		return err
	}

	existVips, err := c.GetLoadBalancerVips(lbUuid)
	if err != nil {
		klog.Errorf("failed to list lb %s vips, %v", lb, err)
		return err
	}
	// vip is empty or delete last rule will destroy the loadbalancer
	if vip == "" || len(existVips) == 1 {
		return nil
	}
	_, err = c.ovnNbCommand(IfExists, "lb-del", lb, vip)
	return err
}

// GetLoadBalancerVips return vips of a loadbalancer
func (c Client) GetLoadBalancerVips(lb string) (map[string]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading",
		"get", "load_balancer", lb, "vips")
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	err = json.Unmarshal([]byte(strings.Replace(output, "=", ":", -1)), &result)
	return result, err
}

// CleanLogicalSwitchAcl clean acl of a switch
func (c Client) CleanLogicalSwitchAcl(ls string) error {
	_, err := c.ovnNbCommand("acl-del", ls)
	return err
}

// ResetLogicalSwitchAcl reset acl of a switch
func (c Client) ResetLogicalSwitchAcl(ls, protocol string) error {
	var err error
	if protocol == kubeovnv1.ProtocolIPv6 {
		_, err = c.ovnNbCommand("acl-del", ls, "--",
			"acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip6.src==%s", c.NodeSwitchCIDR), "allow-related")
	} else {
		_, err = c.ovnNbCommand("acl-del", ls, "--",
			"acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", c.NodeSwitchCIDR), "allow-related")
	}
	return err
}

// SetPrivateLogicalSwitch will drop all ingress traffic except allow subnets
func (c Client) SetPrivateLogicalSwitch(ls, protocol, cidr string, allow []string) error {
	delArgs := []string{"acl-del", ls}
	allowArgs := []string{}
	var dropArgs []string
	if protocol == kubeovnv1.ProtocolIPv4 {
		dropArgs = []string{"--", "acl-add", ls, "to-lport", util.DefaultDropPriority, fmt.Sprintf(`ip4.src!=%s || ip4.dst!=%s`, cidr, cidr), "drop"}
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", c.NodeSwitchCIDR), "allow-related")
	} else {
		dropArgs = []string{"--", "acl-add", ls, "to-lport", util.DefaultDropPriority, fmt.Sprintf(`ip6.src!=%s || ip6.dst!=%s`, cidr, cidr), "drop"}
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip6.src==%s", c.NodeSwitchCIDR), "allow-related")
	}
	ovnArgs := append(delArgs, dropArgs...)

	for _, subnet := range allow {
		if strings.TrimSpace(subnet) != "" {
			var match string
			switch protocol {
			case kubeovnv1.ProtocolIPv4:
				match = fmt.Sprintf("ip4.src==%s || ip4.dst==%s", strings.TrimSpace(subnet), strings.TrimSpace(subnet))
			case kubeovnv1.ProtocolIPv6:
				match = fmt.Sprintf("ip6.src==%s || ip6.dst==%s", strings.TrimSpace(subnet), strings.TrimSpace(subnet))
			}

			allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.SubnetAllowPriority, match, "allow-related")
		}
	}
	ovnArgs = append(ovnArgs, allowArgs...)

	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) GetLogicalSwitchPortAddress(port string) ([]string, error) {
	output, err := c.ovnNbCommand("get", "logical_switch_port", port, "addresses")
	if err != nil {
		klog.Errorf("get port %s addresses failed %v", port, err)
		return nil, err
	}
	if strings.Index(output, "dynamic") != -1 {
		// [dynamic]
		return nil, nil
	}
	output = strings.Trim(output, `[]"`)
	if len(strings.Split(output, " ")) != 2 {
		return nil, nil
	}

	// currently user may only have one fixed address
	// ["0a:00:00:00:00:0c 10.16.0.13"]
	mac := strings.Split(output, " ")[0]
	ip := strings.Split(output, " ")[1]
	return []string{mac, ip}, nil
}

func (c Client) GetLogicalSwitchPortDynamicAddress(port string) ([]string, error) {
	output, err := c.ovnNbCommand("wait-until", "logical_switch_port", port, "dynamic_addresses!=[]", "--",
		"get", "logical_switch_port", port, "dynamic-addresses")
	if err != nil {
		klog.Errorf("get port %s dynamic_addresses failed %v", port, err)
		return nil, err
	}
	if output == "[]" {
		return nil, ErrNoAddr
	}
	output = strings.Trim(output, `"`)
	// "0a:00:00:00:00:02"
	if len(strings.Split(output, " ")) != 2 {
		klog.Error("Subnet address space has been exhausted")
		return nil, ErrNoAddr
	}
	// "0a:00:00:00:00:02 100.64.0.3"
	mac := strings.Split(output, " ")[0]
	ip := strings.Split(output, " ")[1]
	return []string{mac, ip}, nil
}

// GetPortAddr return port [mac, ip]
func (c Client) GetPortAddr(port string) ([]string, error) {
	var address []string
	var err error
	address, err = c.GetLogicalSwitchPortAddress(port)
	if err != nil {
		return nil, err
	}
	if address == nil {
		address, err = c.GetLogicalSwitchPortDynamicAddress(port)
		if err != nil {
			return nil, err
		}
	}
	return address, nil
}

func (c Client) CreatePortGroup(pgName, npNs, npName string) error {
	output, err := c.ovnNbCommand(
		"--data=bare", "--no-heading", "--columns=_uuid", "find", "port_group", fmt.Sprintf("name=%s", pgName))
	if err != nil {
		klog.Errorf("failed to find port_group %s", pgName)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand(
		"pg-add", pgName,
		"--", "set", "port_group", pgName, fmt.Sprintf("external_ids:np=%s/%s", npNs, npName),
	)
	return err
}

func (c Client) DeletePortGroup(pgName string) error {
	if _, err := c.ovnNbCommand("get", "port_group", pgName, "_uuid"); err != nil {
		if strings.Contains(err.Error(), "no row") {
			return nil
		}
		klog.Errorf("failed to get pg %s, %v", pgName, err)
		return err
	}
	_, err := c.ovnNbCommand("pg-del", pgName)
	return err
}

type portGroup struct {
	Name        string
	NpName      string
	NpNamespace string
}

func (c Client) ListPortGroup() ([]portGroup, error) {
	output, err := c.ovnNbCommand("--data=bare", "--format=csv", "--no-heading", "--columns=name,external_ids", "list", "port_group")
	if err != nil {
		klog.Errorf("failed to list logical port-group, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]portGroup, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		np := strings.Split(strings.TrimPrefix(strings.TrimSpace(parts[1]), "np="), "/")
		if len(np) != 2 {
			continue
		}
		result = append(result, portGroup{Name: name, NpNamespace: np[0], NpName: np[1]})
	}
	return result, nil
}

func (c Client) CreateAddressSet(asName string) error {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", asName))
	if err != nil {
		klog.Errorf("failed to find address_set %s", asName)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand("create", "address_set", fmt.Sprintf("name=%s", asName))
	return err
}

func (c Client) DeleteAddressSet(asName string) error {
	_, err := c.ovnNbCommand(IfExists, "destroy", "address_set", asName)
	return err
}

func (c Client) CreateIngressACL(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
	delArgs := []string{"--type=port-group", "acl-del", pgName, "to-lport"}
	exceptArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressExceptDropPriority, fmt.Sprintf("%s.src == $%s && %s.dst == $%s", ipSuffix, asExceptName, ipSuffix, pgAs), "drop"}
	defaultArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressDefaultDrop, fmt.Sprintf("%s.dst == $%s", ipSuffix, pgAs), "drop"}
	ovnArgs := append(delArgs, exceptArgs...)
	ovnArgs = append(ovnArgs, defaultArgs...)

	if len(npp) == 0 {
		allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == $%s && %s.dst == $%s", ipSuffix, asIngressName, ipSuffix, pgAs), "allow-related"}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for _, port := range npp {
			allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == $%s && %s.dst == %d && %s.dst == $%s", ipSuffix, asIngressName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, ipSuffix, pgAs), "allow-related"}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) CreateEgressACL(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
	delArgs := []string{"--type=port-group", "acl-del", pgName, "from-lport"}
	exceptArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressExceptDropPriority, fmt.Sprintf("%s.dst == $%s && %s.src == $%s", ipSuffix, asExceptName, ipSuffix, pgAs), "drop"}
	defaultArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressDefaultDrop, fmt.Sprintf("%s.src == $%s", ipSuffix, pgAs), "drop"}
	ovnArgs := append(delArgs, exceptArgs...)
	ovnArgs = append(ovnArgs, defaultArgs...)

	if len(npp) == 0 {
		allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == $%s && %s.src == $%s", ipSuffix, asEgressName, ipSuffix, pgAs), "allow-related"}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for _, port := range npp {
			allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == $%s && %s.dst == %d && %s.src == $%s", ipSuffix, asEgressName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, ipSuffix, pgAs), "allow-related"}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) DeleteACL(pgName, direction string) error {
	_, err := c.ovnNbCommand("--type=port-group", "acl-del", pgName, direction)
	return err
}

func (c Client) SetPortsToPortGroup(portGroup string, portNames []string) error {
	ovnArgs := []string{"clear", "port_group", portGroup, "ports"}
	if len(portNames) > 0 {
		ovnArgs = []string{"pg-set-ports", portGroup}
		ovnArgs = append(ovnArgs, portNames...)
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) SetAddressesToAddressSet(addresses []string, as string) error {
	ovnArgs := []string{"clear", "address_set", as, "addresses"}
	if len(addresses) > 0 {
		ovnArgs = append(ovnArgs, "--", "add", "address_set", as, "addresses")
		ovnArgs = append(ovnArgs, addresses...)
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

// StartOvnNbctlDaemon start a daemon and set OVN_NB_DAEMON env
func StartOvnNbctlDaemon(nbHost string, nbPort int) (string, error) {
	klog.Infof("start ovn-nbctl daemon")
	output, err := exec.Command(
		"pkill",
		"-f",
		"ovn-nbctl",
	).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to kill old ovn-nbctl daemon: %q", output)
		return "", err
	}

	output, err = exec.Command(
		"ovn-nbctl",
		fmt.Sprintf("--db=tcp:%s:%d", nbHost, nbPort),
		"--pidfile",
		"--detach",
		"--overwrite-pidfile",
	).CombinedOutput()
	if err != nil {
		klog.Errorf("start ovn-nbctl daemon failed, %q", output)
		return "", err
	}

	daemonSocket := strings.TrimSpace(string(output))
	os.Setenv("OVN_NB_DAEMON", daemonSocket)
	return daemonSocket, nil
}

// CheckAlive check if kube-ovn-controller can access ovn-nb from nbctl-daemon
func CheckAlive() error {
	output, err := exec.Command(
		"ovn-nbctl",
		"--timeout=10",
		"show",
	).CombinedOutput()

	if err != nil {
		klog.Errorf("failed to access ovn-nb from daemon, %q", output)
		return err
	}
	return nil
}

// GetLogicalSwitchExcludeIPS get a logical switch exclude ips
// ovn-nbctl get logical_switch ovn-default other_config:exclude_ips => "10.17.0.1 10.17.0.2 10.17.0.3..10.17.0.5"
func (c Client) GetLogicalSwitchExcludeIPS(logicalSwitch string) ([]string, error) {
	output, err := c.ovnNbCommand(IfExists, "get", "logical_switch", logicalSwitch, "other_config:exclude_ips")
	if err != nil {
		return nil, err
	}
	output = strings.Trim(output, `"`)
	if output == "" {
		return nil, ErrNoAddr
	}
	return strings.Fields(output), nil
}

func (c Client) UpdateLogicalSwitchExcludeIPs(ls string, excludeIPs []string) error {
	_, err := c.ovnNbCommand("set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIPs, " ")))
	return err
}

// SetLogicalSwitchExcludeIPS set a logical switch exclude ips
// ovn-nbctl set logical_switch ovn-default other_config:exclude_ips="10.17.0.2 10.17.0.1"
func (c Client) SetLogicalSwitchExcludeIPS(logicalSwitch string, excludeIPS []string) error {
	_, err := c.ovnNbCommand("set", "logical_switch", logicalSwitch,
		fmt.Sprintf(`other_config:exclude_ips="%s"`, strings.Join(excludeIPS, " ")))
	return err
}

func (c Client) GetLogicalSwitchPortByLogicalSwitch(logicalSwitch string) ([]string, error) {
	output, err := c.ovnNbCommand("lsp-list", logicalSwitch)
	if err != nil {
		return nil, err
	}
	var rv []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		lsp := strings.Fields(line)[0]
		rv = append(rv, lsp)
	}
	return rv, nil
}

func (c Client) SetLogicSwitchPortTag(port, tag string) error {
	_, err := c.ovnNbCommand("set", "logical_switch_port", port, fmt.Sprintf("tag=%s", tag))
	return err
}

func (c Client) GetLogicSwitchPortTag(port string) (string, error) {
	return c.ovnNbCommand("get", "logical_switch_port", port, "tag")
}

func (c Client) SetLogicalSwitchPortAddress(port, ip, mac string) error {
	_, err := c.ovnNbCommand("lsp-set-addresses", port, fmt.Sprintf("%s %s", mac, ip))
	return err
}

func (c Client) SetLogicalSwitchPortSecurity(port, ip, mac, cidr string) error {
	security := fmt.Sprintf("%s %s/%s", mac, ip, strings.Split(cidr, "/")[1])
	_, err := c.ovnNbCommand("lsp-set-port-security", port, security)
	return err
}

func (c Client) CreateLocalnetPort(ls, port, providerName, vlanID string) error {
	_, err := c.ovnNbCommand(MayExist, "lsp-add", ls, port, "--",
		"lsp-set-addresses", port, "unknown", "--",
		"lsp-set-type", port, "localnet", "--",
		"lsp-set-options", port, fmt.Sprintf("network_name=%s", providerName))
	if err != nil {
		klog.Errorf("create localnet port %s failed, %v", port, err)
		return err
	}

	if err = c.SetLogicSwitchPortTag(port, vlanID); err != nil {
		klog.Errorf("set localnet port vlan %s failed, %v", vlanID, err)
		return err
	}

	return nil
}
