package ovs

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alauda/kube-ovn/pkg/util"
	"k8s.io/klog"

	netv1 "k8s.io/api/networking/v1"
)

func (c Client) ovnNbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	raw, err := exec.Command(OvnNbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.Infof("%s command %s in %vms", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed)
	if err != nil {
		return "", fmt.Errorf("%s, %v", string(raw), err)
	}
	return trimCommandOutput(raw), nil
}

// DeletePort delete logical switch port in ovn
func (c Client) DeletePort(port string) error {
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lsp-del", port)
	if err != nil {
		return fmt.Errorf("failed to delete port %s, %v", port, err)
	}
	return nil
}

// CreatePort create logical switch port in ovn
func (c Client) CreatePort(ls, port, ip, mac string) (*nic, error) {
	if ip == "" && mac == "" {
		_, err := c.ovnNbCommand(WaitSb, MayExist, "lsp-add", ls, port,
			"--", "set", "logical_switch_port", port, "addresses=dynamic")
		if err != nil {
			klog.Errorf("create port %s failed %v", port, err)
			return nil, err
		}

		address, err := c.getLogicalSwitchPortDynamicAddress(port)
		if err != nil {
			klog.Errorf("get port %s dynamic-addresses failed %v", port, err)
			return nil, err
		}
		mac = address[0]
		ip = address[1]
	} else {
		if mac == "" {
			mac = "dynamic"
		}

		// remove mask, and retrieve mask from subnet cidr later
		// in this way we can deal with static ip with/without mask
		ip = strings.Split(ip, "/")[0]

		_, err := c.ovnNbCommand(WaitSb, MayExist, "lsp-add", ls, port, "--",
			"lsp-set-addresses", port, fmt.Sprintf("%s %s", mac, ip))
		if err != nil {
			klog.Errorf("create port %s failed %v", port, err)
			return nil, err
		}

		if mac == "dynamic" {
			address, err := c.getLogicalSwitchPortDynamicAddress(port)
			if err != nil {
				klog.Errorf("get port %s dynamic-addresses failed %v", port, err)
				return nil, err
			}
			mac = address[0]
		}
	}
	output, err := c.ovnNbCommand("get", "logical_switch", ls, "other_config:subnet", "other_config:gateway")
	if err != nil {
		klog.Errorf("get switch %s failed %v", ls, err)
		return nil, err
	}
	subnet, gw := strings.Trim(strings.Split(output, "\n")[0], "\""), strings.Trim(strings.Split(output, "\n")[1], "\"")
	return &nic{IpAddress: ip, MacAddress: mac, CIDR: subnet, Gateway: gw}, nil
}

type nic struct {
	IpAddress  string
	MacAddress string
	CIDR       string
	Gateway    string
}

// CreateLogicalSwitch create logical switch in ovn, connect it to router and apply tcp/udp lb rules
func (c Client) CreateLogicalSwitch(ls, subnet, gateway string, excludeIps []string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "ls-add", ls, "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", subnet), "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " ")), "--",
		"acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", c.NodeSwitchCIDR), "allow-related")
	if err != nil {
		klog.Errorf("create switch %s failed %v", ls, err)
		return err
	}

	mac := util.GenerateMac()
	mask := strings.Split(subnet, "/")[1]
	klog.Infof("create route port for switch %s", ls)
	err = c.createRouterPort(ls, c.ClusterRouter, gateway+"/"+mask, mac)
	if err != nil {
		klog.Errorf("failed to connect switch %s to router, %v", ls, err)
		return err
	}
	if ls != c.NodeSwitch {
		// DO NOT add ovn dns/lb to node switch
		err = c.addLoadBalancerToLogicalSwitch(c.ClusterTcpLoadBalancer, ls)
		if err != nil {
			klog.Errorf("failed to add cluster tcp lb to %s, %v", ls, err)
			return err
		}

		err = c.addLoadBalancerToLogicalSwitch(c.ClusterUdpLoadBalancer, ls)
		if err != nil {
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
		if len(l) == 0 || !strings.Contains(l, "") {
			continue
		}
		tmp := strings.Split(l, " ")[1]
		tmp = strings.Trim(tmp, "()")
		result = append(result, tmp)
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
		if len(l) == 0 || !strings.Contains(l, "") {
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
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lrp-del", fmt.Sprintf("%s-%s", c.ClusterRouter, ls))
	if err != nil {
		klog.Errorf("failed to del lrp %s-%s, %v", c.ClusterRouter, ls, err)
		return err
	}

	_, err = c.ovnNbCommand(WaitSb, IfExists, "ls-del", ls)
	if err != nil {
		klog.Errorf("failed to del ls %s, %v", ls, err)
		return err
	}
	return nil
}

// CreateLogicalRouter create logical router in ovn
func (c Client) CreateLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "lr-add", lr)
	return err
}

func (c Client) createRouterPort(ls, lr, ip, mac string) error {
	klog.Infof("add %s to %s with ip: %s, mac :%s", ls, lr, ip, mac)
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)
	_, err := c.ovnNbCommand(WaitSb, MayExist, "lsp-add", ls, lsTolr, "--",
		"set", "logical_switch_port", lsTolr, "type=router", "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("addresses=\"%s\"", mac), "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("options:router-port=%s", lrTols))
	if err != nil {
		klog.Errorf("failed to create switch router port %s %v", lsTolr, err)
		return err
	}

	_, err = c.ovnNbCommand(WaitSb, MayExist, "lrp-add", lr, lrTols, mac, ip)
	if err != nil {
		klog.Errorf("failed to create router port %s %v", lrTols, err)
		return err
	}
	return nil
}

// AddStaticRouter add a static route rule in ovn
func (c Client) AddStaticRouter(policy, cidr, nextHop, router string) error {
	if policy == "" {
		policy = PolicyDstIP
	}
	_, err := c.ovnNbCommand(WaitSb, MayExist, fmt.Sprintf("%s=%s", Policy, policy), "lr-route-add", router, cidr, nextHop)
	return err
}

// DeleteStaticRouter delete a static route rule in ovn
func (c Client) DeleteStaticRouter(cidr, router string) error {
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lr-route-del", router, cidr)
	return err
}

// FindLoadbalancer find ovn loadbalancer uuid by name
func (c Client) FindLoadbalancer(lb string) (string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
		"find", "load_balancer", fmt.Sprintf("name=%s", lb))
	return output, err
}

// CreateLoadBalancer create loadbalancer in ovn
func (c Client) CreateLoadBalancer(lb, protocol string) error {
	_, err := c.ovnNbCommand("create", "load_balancer",
		fmt.Sprintf("name=%s", lb), fmt.Sprintf("protocol=%s", protocol))
	return err
}

// CreateLoadBalancerRule create loadbalancer rul in ovn
func (c Client) CreateLoadBalancerRule(lb, vip, ips string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "lb-add", lb, vip, ips)
	return err
}

func (c Client) addLoadBalancerToLogicalSwitch(lb, ls string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "ls-lb-add", ls, lb)
	return err
}

// DeleteLoadBalancerVip delete a vip rule from loadbalancer
func (c Client) DeleteLoadBalancerVip(vip, lb string) error {
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lb-del", lb, vip)
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

// SetPrivateLogicalSwitch will drop all ingress traffic except allow subnets
func (c Client) SetPrivateLogicalSwitch(ls string, allow []string) error {
	delArgs := []string{"acl-del", ls}
	dropArgs := []string{"--", "acl-add", ls, "to-lport", util.DefaultDropPriority, fmt.Sprintf(`inport=="%s-%s"`, ls, c.ClusterRouter), "drop"}
	ovnArgs := append(delArgs, dropArgs...)

	allowArgs := []string{}
	for _, subnet := range allow {
		if strings.TrimSpace(subnet) != "" {
			match := fmt.Sprintf("ip4.src==%s", strings.TrimSpace(subnet))
			allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.SubnetAllowPriority, match, "allow-related")
		}
	}
	ovnArgs = append(ovnArgs, allowArgs...)

	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) getLogicalSwitchPortAddress(port string) ([]string, error) {
	output, err := c.ovnNbCommand("get", "logical_switch_port", port, "addresses")
	if err != nil {
		klog.Errorf("get port %s addresses failed %v", port, err)
		return nil, err
	}
	if strings.Index(output, "dynamic") != -1 {
		// [dynamic]
		return nil, nil
	}
	// currently user may only have one fixed address
	// ["0a:00:00:00:00:0c 10.16.0.13"]
	output = strings.Trim(output, `[]"`)
	mac := strings.Split(output, " ")[0]
	ip := strings.Split(output, " ")[1]
	return []string{mac, ip}, nil
}

func (c Client) getLogicalSwitchPortDynamicAddress(port string) ([]string, error) {
	output, err := c.ovnNbCommand("get", "logical_switch_port", port, "dynamic-addresses")
	if err != nil {
		klog.Errorf("get port %s dynamic_addresses failed %v", port, err)
		return nil, err
	}
	if output == "[]" {
		return nil, ErrNoAddr
	}
	// "0a:00:00:00:00:02 100.64.0.3"
	output = strings.Trim(output, `"`)
	mac := strings.Split(output, " ")[0]
	ip := strings.Split(output, " ")[1]
	return []string{mac, ip}, nil
}

// GetPortAddr return port [mac, ip]
func (c Client) GetPortAddr(port string) ([]string, error) {
	var address []string
	var err error
	address, err = c.getLogicalSwitchPortAddress(port)
	if err != nil {
		return nil, err
	}
	if address == nil {
		address, err = c.getLogicalSwitchPortDynamicAddress(port)
		if err != nil {
			return nil, err
		}
	}
	return address, nil
}

func (c Client) CreatePortGroup(pgName string) error {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "port_group", fmt.Sprintf("name=%s", pgName))
	if err != nil {
		klog.Errorf("failed to find port_group %s", pgName)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand("pg-add", pgName)
	return err
}

func (c Client) DeletePortGroup(pgName string) error {
	_, err := c.ovnNbCommand(IfExists, "destroy", "port_group", pgName)
	return err
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

func (c Client) CreateIngressACL(pgName, asIngressName, asExceptName string, npp []netv1.NetworkPolicyPort) error {
	pgAs := fmt.Sprintf("%s_ip4", pgName)
	delArgs := []string{"--type=port-group", "acl-del", pgName, "to-lport"}
	exceptArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressExceptDropPriority, fmt.Sprintf("ip4.src == $%s && ip4.dst == $%s", asExceptName, pgAs), "drop"}
	defaultArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressDefaultDrop, fmt.Sprintf("ip4.dst == $%s", pgAs), "drop"}
	ovnArgs := append(delArgs, exceptArgs...)
	ovnArgs = append(ovnArgs, defaultArgs...)

	if len(npp) == 0 {
		allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("ip4.src == $%s && ip4.dst == $%s", asIngressName, pgAs), "allow-related"}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for _, port := range npp {
			allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("ip4.src == $%s && %s.dst == %d && ip4.dst == $%s", asIngressName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgAs), "allow-related"}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) CreateEgressACL(pgName, asEgressName, asExceptName string, npp []netv1.NetworkPolicyPort) error {
	pgAs := fmt.Sprintf("%s_ip4", pgName)
	delArgs := []string{"--type=port-group", "acl-del", pgName, "from-lport"}
	exceptArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressExceptDropPriority, fmt.Sprintf("ip4.dst == $%s && ip4.src == $%s", asExceptName, pgAs), "drop"}
	defaultArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressDefaultDrop, fmt.Sprintf("ip4.src == $%s", pgAs), "drop"}
	ovnArgs := append(delArgs, exceptArgs...)
	ovnArgs = append(ovnArgs, defaultArgs...)

	if len(npp) == 0 {
		allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("ip4.dst == $%s && ip4.src == $%s", asEgressName, pgAs), "allow-related"}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for _, port := range npp {
			allowArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("ip4.dst == $%s && %s.dst == %d && ip4.src == $%s", asEgressName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgAs), "allow-related"}
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
	ovnArgs := []string{"pg-set-ports", portGroup}
	ovnArgs = append(ovnArgs, portNames...)
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
