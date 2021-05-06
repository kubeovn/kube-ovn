package ovs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	netv1 "k8s.io/api/networking/v1"
	"k8s.io/klog"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
)

func (c Client) ovnNbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout)}, cmdArgs...)
	raw, err := exec.Command(OvnNbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed)
	if err != nil || elapsed > 500 {
		klog.Warning("ovn-nbctl command error or took too long")
		klog.Warningf("%s %s in %vms", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed)
	}
	method := ""
	for _, arg := range cmdArgs {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}
	code := "0"
	if err != nil {
		code = "1"
	}
	ovsClientRequestLatency.WithLabelValues("ovn-nb", method, code).Observe(elapsed)
	if err != nil {
		return "", fmt.Errorf("%s, %q", raw, err)
	}
	return trimCommandOutput(raw), nil
}

func (c Client) SetAzName(azName string) error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", fmt.Sprintf("name=%s", azName)); err != nil {
		return fmt.Errorf("failed to set az name, %v", err)
	}
	return nil
}

func (c Client) SetICAutoRoute(enable bool, blackList []string) error {
	if enable {
		if _, err := c.ovnNbCommand("set", "NB_Global", ".", "options:ic-route-adv=true", "options:ic-route-learn=true", fmt.Sprintf("options:ic-route-blacklist=%s", strings.Join(blackList, ","))); err != nil {
			return fmt.Errorf("failed to enable ovn-ic auto route, %v", err)
		}
		return nil
	} else {
		if _, err := c.ovnNbCommand("set", "NB_Global", ".", "options:ic-route-adv=false", "options:ic-route-learn=false"); err != nil {
			return fmt.Errorf("failed to disable ovn-ic auto route, %v", err)
		}
		return nil
	}
}

// DeleteLogicalSwitchPort delete logical switch port in ovn
func (c Client) DeleteLogicalSwitchPort(port string) error {
	if _, err := c.ovnNbCommand(IfExists, "lsp-del", port); err != nil {
		return fmt.Errorf("failed to delete logical switch port %s, %v", port, err)
	}
	return nil
}

// DeleteLogicalRouterPort delete logical switch port in ovn
func (c Client) DeleteLogicalRouterPort(port string) error {
	if _, err := c.ovnNbCommand(IfExists, "lrp-del", port); err != nil {
		return fmt.Errorf("failed to delete logical router port %s, %v", port, err)
	}
	return nil
}

func (c Client) CreateICLogicalRouterPort(az, mac, subnet string, chassises []string) error {
	if _, err := c.ovnNbCommand(MayExist, "lrp-add", c.ClusterRouter, fmt.Sprintf("%s-ts", az), mac, subnet); err != nil {
		return fmt.Errorf("failed to crate ovn-ic lrp, %v", err)
	}
	if _, err := c.ovnNbCommand(MayExist, "lsp-add", util.InterconnectionSwitch, fmt.Sprintf("ts-%s", az), "--",
		"lsp-set-addresses", fmt.Sprintf("ts-%s", az), "router", "--",
		"lsp-set-type", fmt.Sprintf("ts-%s", az), "router", "--",
		"lsp-set-options", fmt.Sprintf("ts-%s", az), fmt.Sprintf("router-port=%s-ts", az)); err != nil {
		return fmt.Errorf("failed to create ovn-ic lsp, %v", err)
	}
	for index, chassis := range chassises {
		if _, err := c.ovnNbCommand("lrp-set-gateway-chassis", fmt.Sprintf("%s-ts", az), chassis, fmt.Sprintf("%d", 100-index)); err != nil {
			return fmt.Errorf("failed to set gateway chassis, %v", err)
		}
	}
	return nil
}

func (c Client) DeleteICLogicalRouterPort(az string) error {
	if err := c.DeleteLogicalRouterPort(fmt.Sprintf("%s-ts", az)); err != nil {
		return fmt.Errorf("failed to delete ovn-ic logical router port. %v", err)
	}
	if err := c.DeleteLogicalSwitchPort(fmt.Sprintf("ts-%s", az)); err != nil {
		return fmt.Errorf("failed to delete ovn-ic logical switch port. %v", err)
	}
	return nil
}

// CreatePort create logical switch port in ovn
func (c Client) CreatePort(ls, port, ip, cidr, mac, tag string, portSecurity bool) error {
	var ovnCommand []string
	if util.CheckProtocol(cidr) == kubeovnv1.ProtocolDual {
		ips := strings.Split(ip, ",")
		ovnCommand = []string{MayExist, "lsp-add", ls, port, "--",
			"lsp-set-addresses", port, fmt.Sprintf("%s %s %s", mac, ips[0], ips[1])}

		ipAddr := util.GetIpAddrWithMask(ip, cidr)
		ipAddrs := strings.Split(ipAddr, ",")
		if portSecurity {
			ovnCommand = append(ovnCommand,
				"--", "lsp-set-port-security", port, fmt.Sprintf("%s %s %s", mac, ipAddrs[0], ipAddrs[1]))
		}
	} else {
		ovnCommand = []string{MayExist, "lsp-add", ls, port, "--",
			"lsp-set-addresses", port, fmt.Sprintf("%s %s", mac, ip)}

		if portSecurity {
			ovnCommand = append(ovnCommand,
				"--", "lsp-set-port-security", port, fmt.Sprintf("%s %s/%s", mac, ip, strings.Split(cidr, "/")[1]))
		}
	}

	if tag != "" && tag != "0" {
		ovnCommand = append(ovnCommand,
			"--", "set", "logical_switch_port", port, fmt.Sprintf("tag=%s", tag))
	}

	if _, err := c.ovnNbCommand(ovnCommand...); err != nil {
		klog.Errorf("create port %s failed %v", port, err)
		return err
	}
	return nil
}

func (c Client) SetLogicalSwitchConfig(ls string, isUnderlayGW bool, lr, protocol, subnet, gateway string, excludeIps []string) error {
	var err error
	cidrBlocks := strings.Split(subnet, ",")
	mask := strings.Split(cidrBlocks[0], "/")[1]

	var cmd []string
	var networks string
	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		networks = fmt.Sprintf("%s/%s", gateway, mask)
		cmd = []string{MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", subnet), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " "))}
	case kubeovnv1.ProtocolIPv6:
		gateway := strings.ReplaceAll(gateway, ":", "\\:")
		networks = fmt.Sprintf("%s/%s", gateway, mask)
		cmd = []string{MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:ipv6_prefix=%s", strings.Split(subnet, "/")[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " "))}
	case kubeovnv1.ProtocolDual:
		gws := strings.Split(gateway, ",")
		v6Mask := strings.Split(cidrBlocks[1], "/")[1]
		gwStr := gws[0] + "/" + mask + "," + gws[1] + "/" + v6Mask
		networks = strings.ReplaceAll(strings.Join(strings.Split(gwStr, ","), " "), ":", "\\:")

		cmd = []string{MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", cidrBlocks[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:ipv6_prefix=%s", strings.Split(cidrBlocks[1], "/")[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " "))}
	}
	if !isUnderlayGW {
		cmd = append(cmd, []string{"--",
			"set", "logical_router_port", fmt.Sprintf("%s-%s", lr, ls), fmt.Sprintf("networks=%s", networks)}...)
	}
	_, err = c.ovnNbCommand(cmd...)
	if err != nil {
		klog.Errorf("set switch config for %s failed %v", ls, err)
		return err
	}
	return nil
}

// CreateLogicalSwitch create logical switch in ovn, connect it to router and apply tcp/udp lb rules
func (c Client) CreateLogicalSwitch(ls, lr, protocol, subnet, gateway string, excludeIps []string, underlayGateway, defaultVpc bool) error {
	var err error
	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		_, err = c.ovnNbCommand(MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", subnet), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " ")))
	case kubeovnv1.ProtocolIPv6:
		_, err = c.ovnNbCommand(MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:ipv6_prefix=%s", strings.Split(subnet, "/")[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " ")))
	case kubeovnv1.ProtocolDual:
		// gateway is not offical column, which is used for private
		cidrBlocks := strings.Split(subnet, ",")
		_, err = c.ovnNbCommand(MayExist, "ls-add", ls, "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", cidrBlocks[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:ipv6_prefix=%s", strings.Split(cidrBlocks[1], "/")[0]), "--",
			"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", strings.Join(excludeIps, " ")))
	}

	if err != nil {
		klog.Errorf("create switch %s failed %v", ls, err)
		return err
	}

	ip := util.GetIpAddrWithMask(gateway, subnet)
	mac := util.GenerateMac()
	if !underlayGateway {
		if err := c.createRouterPort(ls, lr, ip, mac); err != nil {
			klog.Errorf("failed to connect switch %s to router, %v", ls, err)
			return err
		}
	}
	if ls != c.NodeSwitch && defaultVpc {
		// DO NOT add ovn dns/lb to node switch
		// TODO: custom vpc not support dns/lb now
		if err := c.addLoadBalancerToLogicalSwitch(c.ClusterTcpLoadBalancer, ls); err != nil {
			klog.Errorf("failed to add cluster tcp lb to %s, %v", ls, err)
			return err
		}

		if err := c.addLoadBalancerToLogicalSwitch(c.ClusterUdpLoadBalancer, ls); err != nil {
			klog.Errorf("failed to add cluster udp lb to %s, %v", ls, err)
			return err
		}

		if err := c.addLoadBalancerToLogicalSwitch(c.ClusterTcpSessionLoadBalancer, ls); err != nil {
			klog.Errorf("failed to add cluster tcp session lb to %s, %v", ls, err)
			return err
		}

		if err := c.addLoadBalancerToLogicalSwitch(c.ClusterUdpSessionLoadBalancer, ls); err != nil {
			klog.Errorf("failed to add cluster udp lb to %s, %v", ls, err)
			return err
		}
	}

	return nil
}

func (c Client) CreateGatewaySwitch(name, ip, mac string, chassises []string) error {
	lsTolr := fmt.Sprintf("%s-%s", name, c.ClusterRouter)
	lrTols := fmt.Sprintf("%s-%s", c.ClusterRouter, name)
	localnetPort := fmt.Sprintf("ln-%s", name)
	_, err := c.ovnNbCommand(
		MayExist, "ls-add", name, "--",
		MayExist, "lsp-add", name, localnetPort, "--",
		"lsp-set-type", localnetPort, "localnet", "--",
		"lsp-set-addresses", localnetPort, "unknown", "--",
		"lsp-set-options", localnetPort, "network_name=external", "--",
		MayExist, "lrp-add", c.ClusterRouter, lrTols, mac, ip, "--",
		MayExist, "lsp-add", name, lsTolr, "--",
		"lsp-set-type", lsTolr, "router", "--",
		"lsp-set-addresses", lsTolr, "router", "--",
		"lsp-set-options", lsTolr, fmt.Sprintf("router-port=%s", lrTols),
	)
	if err != nil {
		return fmt.Errorf("failed to create external gateway switch, %v", err)
	}

	for index, chassis := range chassises {
		if _, err := c.ovnNbCommand("lrp-set-gateway-chassis", lrTols, chassis, fmt.Sprintf("%d", 100-index)); err != nil {
			return fmt.Errorf("failed to set gateway chassis, %v", err)
		}
	}
	return nil
}

func (c Client) DeleteGatewaySwitch(name string) error {
	lrTols := fmt.Sprintf("%s-%s", c.ClusterRouter, name)
	_, err := c.ovnNbCommand(
		IfExists, "ls-del", name, "--",
		IfExists, "lrp-del", lrTols,
	)
	return err
}

// ListLogicalSwitch list logical switch names
func (c Client) ListLogicalSwitch() ([]string, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", "logical_switch")
	if err != nil {
		klog.Errorf("failed to list logical switch %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {

		l = strings.TrimSpace(l)
		if len(l) > 0 {
			result = append(result, l)
		}
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
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", "logical_switch_port", "type=\"\"")
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

func (c Client) ListRemoteLogicalSwitchPortAddress() ([]string, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=addresses", "find", "logical_switch_port", "type=remote")
	if err != nil {
		return nil, fmt.Errorf("failed to list ic remote addresses, %v", err)
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		if len(strings.Split(l, " ")) != 2 {
			continue
		}
		cidr := strings.Split(l, " ")[1]
		result = append(result, strings.TrimSpace(cidr))
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

// DeleteLogicalSwitch delete logical switch
func (c Client) DeleteLogicalSwitch(ls string) error {
	if _, err := c.ovnNbCommand(IfExists, "ls-del", ls); err != nil {
		klog.Errorf("failed to del ls %s, %v", ls, err)
		return err
	}
	return nil
}

// CreateLogicalRouter delete logical router in ovn
func (c Client) CreateLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(MayExist, "lr-add", lr)
	return err
}

// DeleteLogicalRouter create logical router in ovn
func (c Client) DeleteLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(IfExists, "lr-del", lr)
	return err
}

func (c Client) RemoveRouterPort(ls, lr string) error {
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)
	_, err := c.ovnNbCommand(IfExists, "lsp-del", lsTolr, "--",
		IfExists, "lrp-del", lrTols)
	if err != nil {
		klog.Errorf("failed to remove router port, %v", err)
		return err
	}
	return nil
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

	ipStr := strings.Split(ip, ",")
	if len(ipStr) == 2 {
		_, err = c.ovnNbCommand(MayExist, "lrp-add", lr, lrTols, mac, ipStr[0], ipStr[1])
	} else {
		_, err = c.ovnNbCommand(MayExist, "lrp-add", lr, lrTols, mac, ipStr[0])
	}
	if err != nil {
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
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=ip_prefix,nexthop,policy", "find", "Logical_Router_Static_Route", "external_ids{=}{}")
	if err != nil {
		return nil, err
	}
	entries := strings.Split(output, "\n")
	staticRoutes := make([]StaticRoute, 0, len(entries))
	for _, entry := range strings.Split(output, "\n") {
		if len(strings.Split(entry, ",")) == 3 {
			t := strings.Split(entry, ",")
			staticRoutes = append(staticRoutes,
				StaticRoute{CIDR: strings.TrimSpace(t[0]), NextHop: strings.TrimSpace(t[1]), Policy: strings.TrimSpace(t[2])})
		}
	}
	return staticRoutes, nil
}

// AddStaticRoute add a static route rule in ovn
func (c Client) AddStaticRoute(policy, cidr, nextHop, router string) error {
	if policy == "" {
		policy = PolicyDstIP
	}

	for _, cidrBlock := range strings.Split(cidr, ",") {
		for _, gw := range strings.Split(nextHop, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(gw) {
				continue
			}
			if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "lr-route-add", router, cidrBlock, gw); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c Client) GetStaticRouteList(router string) (routeList []*StaticRoute, err error) {
	output, err := c.ovnNbCommand("lr-route-list", router)
	if err != nil {
		klog.Errorf("failed to list logical router route %v", err)
		return nil, err
	}
	return parseLrRouteListOutput(output)
}

func parseLrRouteListOutput(output string) (routeList []*StaticRoute, err error) {
	lines := strings.Split(output, "\n")
	routeList = make([]*StaticRoute, 0, len(lines))
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		reg := regexp.MustCompile(`(\d+.\d+.\d+.\d+(/\d+)*)\s+(\d+.\d+.\d+.\d+)\s+(dst-ip|src-ip)`)
		sm := reg.FindStringSubmatch(l)
		if len(sm) == 0 {
			continue
		}
		routeList = append(routeList, &StaticRoute{
			Policy:  sm[4],
			CIDR:    sm[1],
			NextHop: sm[3],
		})
	}
	return routeList, nil
}

func (c Client) UpdateNatRule(policy, logicalIP, externalIP, router, logicalMac, port string) error {
	if policy == "snat" {
		if externalIP == "" {
			_, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP)
			return err
		}
		_, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP, "--",
			MayExist, "lr-nat-add", router, policy, externalIP, logicalIP)
		return err
	} else {
		output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", logicalIP), "type=dnat_and_snat")
		if err != nil {
			klog.Errorf("failed to list nat rules, %v", err)
			return err
		}
		eips := strings.Split(output, "\n")
		for _, eip := range eips {
			eip = strings.TrimSpace(eip)
			if eip == "" || eip == externalIP {
				continue
			}
			if _, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "dnat_and_snat", eip); err != nil {
				klog.Errorf("failed to delete nat rule, %v", err)
				return err
			}
		}
		if externalIP != "" {
			if c.ExternalGatewayType == "distributed" {
				_, err = c.ovnNbCommand(MayExist, "--stateless", "lr-nat-add", router, policy, externalIP, logicalIP, port, logicalMac)
				return err
			} else {
				_, err = c.ovnNbCommand(MayExist, "lr-nat-add", router, policy, externalIP, logicalIP)
				return err
			}
		}
	}
	return nil
}

func (c Client) DeleteNatRule(logicalIP, router string) error {
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=type,external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", logicalIP))
	if err != nil {
		klog.Errorf("failed to list nat rules, %v", err)
		return err
	}
	rules := strings.Split(output, "\n")
	for _, rule := range rules {
		if len(strings.Split(rule, ",")) != 2 {
			continue
		}
		policy, externalIP := strings.Split(rule, ",")[0], strings.Split(rule, ",")[1]
		if policy == "snat" {
			if _, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP); err != nil {
				klog.Errorf("failed to delete nat rule, %v", err)
				return err
			}
		} else if policy == "dnat_and_snat" {
			if _, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "dnat_and_snat", externalIP); err != nil {
				klog.Errorf("failed to delete nat rule, %v", err)
				return err
			}
		}
	}

	return err
}

// DeleteStaticRoute delete a static route rule in ovn
func (c Client) DeleteStaticRoute(cidr, router string) error {
	if cidr == "" {
		return nil
	}
	_, err := c.ovnNbCommand(IfExists, "lr-route-del", router, cidr)
	return err
}

func (c Client) DeleteStaticRouteByNextHop(nextHop string) error {
	if strings.TrimSpace(nextHop) == "" {
		return nil
	}
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
	count := len(strings.Split(output, "\n"))
	if count > 1 {
		klog.Errorf("%s has %d lb entries", lb, count)
		return "", fmt.Errorf("%s has %d lb entries", lb, count)
	}
	return output, err
}

// CreateLoadBalancer create loadbalancer in ovn
func (c Client) CreateLoadBalancer(lb, protocol, selectFields string) error {
	var err error
	if selectFields == "" {
		_, err = c.ovnNbCommand("create", "load_balancer",
			fmt.Sprintf("name=%s", lb), fmt.Sprintf("protocol=%s", protocol))
	} else {
		_, err = c.ovnNbCommand("create", "load_balancer",
			fmt.Sprintf("name=%s", lb), fmt.Sprintf("protocol=%s", protocol), fmt.Sprintf("selection_fields=%s", selectFields))
	}

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
func (c Client) ResetLogicalSwitchAcl(ls string) error {
	_, err := c.ovnNbCommand("acl-del", ls)
	return err
}

// SetPrivateLogicalSwitch will drop all ingress traffic except allow subnets
func (c Client) SetPrivateLogicalSwitch(ls, protocol, cidr string, allow []string) error {
	delArgs := []string{"acl-del", ls}
	allowArgs := []string{}
	var dropArgs []string
	if protocol == kubeovnv1.ProtocolIPv4 {
		dropArgs = []string{"--", "--log", fmt.Sprintf("--name=%s", ls), fmt.Sprintf("--severity=%s", "warning"), "acl-add", ls, "to-lport", util.DefaultDropPriority, "ip", "drop"}
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", c.NodeSwitchCIDR), "allow-related")
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.SubnetAllowPriority, fmt.Sprintf(`ip4.src==%s && ip4.dst==%s`, cidr, cidr), "allow-related")
	} else {
		dropArgs = []string{"--", "--log", fmt.Sprintf("--name=%s", ls), fmt.Sprintf("--severity=%s", "warning"), "acl-add", ls, "to-lport", util.DefaultDropPriority, "ip", "drop"}
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip6.src==%s", c.NodeSwitchCIDR), "allow-related")
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.SubnetAllowPriority, fmt.Sprintf(`ip6.src==%s && ip6.dst==%s`, cidr, cidr), "allow-related")
	}
	ovnArgs := append(delArgs, dropArgs...)

	for _, subnet := range allow {
		if strings.TrimSpace(subnet) != "" {
			var match string
			switch protocol {
			case kubeovnv1.ProtocolIPv4:
				match = fmt.Sprintf("(ip4.src==%s && ip4.dst==%s) || (ip4.src==%s && ip4.dst==%s)", strings.TrimSpace(subnet), cidr, cidr, strings.TrimSpace(subnet))
			case kubeovnv1.ProtocolIPv6:
				match = fmt.Sprintf("(ip6.src==%s && ip6.dst==%s) || (ip6.src==%s && ip6.dst==%s)", strings.TrimSpace(subnet), cidr, cidr, strings.TrimSpace(subnet))
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
	if strings.Contains(output, "dynamic") {
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

func (c Client) CreateAddressSet(asName, npNamespace, npName, direction string) error {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", asName))
	if err != nil {
		klog.Errorf("failed to find address_set %s", asName)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand("create", "address_set", fmt.Sprintf("name=%s", asName), fmt.Sprintf("external_ids:np=%s/%s/%s", npNamespace, npName, direction))
	return err
}

func (c Client) ListAddressSet(npNamespace, npName, direction string) ([]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=name", "find", "address_set", fmt.Sprintf("external_ids:np=%s/%s/%s", npNamespace, npName, direction))
	if err != nil {
		klog.Errorf("failed to list address_set of %s/%s/%s", npNamespace, npName, direction)
		return nil, err
	}
	return strings.Split(output, "\n"), nil
}

func (c Client) DeleteAddressSet(asName string) error {
	_, err := c.ovnNbCommand(IfExists, "destroy", "address_set", asName)
	return err
}

func (c Client) CreateIngressACL(npName, pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
	exceptArgs := []string{MayExist, "--type=port-group", "--log", fmt.Sprintf("--name=%s", npName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", pgName, "to-lport", util.IngressExceptDropPriority, fmt.Sprintf("%s.src == $%s && %s.dst == $%s", ipSuffix, asExceptName, ipSuffix, pgAs), "drop"}
	defaultArgs := []string{"--", MayExist, "--type=port-group", "--log", fmt.Sprintf("--name=%s", npName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", pgName, "to-lport", util.IngressDefaultDrop, fmt.Sprintf("%s.dst == $%s", ipSuffix, pgAs), "drop"}
	ovnArgs := append(exceptArgs, defaultArgs...)

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

func (c Client) CreateEgressACL(npName, pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
	exceptArgs := []string{MayExist, "--type=port-group", "--log", fmt.Sprintf("--name=%s", npName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", pgName, "from-lport", util.EgressExceptDropPriority, fmt.Sprintf("%s.dst == $%s && %s.src == $%s", ipSuffix, asExceptName, ipSuffix, pgAs), "drop"}
	defaultArgs := []string{"--", MayExist, "--type=port-group", "--log", fmt.Sprintf("--name=%s", npName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", pgName, "from-lport", util.EgressDefaultDrop, fmt.Sprintf("%s.src == $%s", ipSuffix, pgAs), "drop"}
	ovnArgs := append(exceptArgs, defaultArgs...)

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

func (c Client) CreateGatewayACL(pgName, gateway, cidr string) error {
	for _, cidrBlock := range strings.Split(cidr, ",") {
		for _, gw := range strings.Split(gateway, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(gw) {
				continue
			}
			protocol := util.CheckProtocol(cidrBlock)
			ipSuffix := "ip4"
			if protocol == kubeovnv1.ProtocolIPv6 {
				ipSuffix = "ip6"
			}
			ingressArgs := []string{MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == %s && icmp", ipSuffix, gw), "allow-related"}
			egressArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s && icmp", ipSuffix, gw), "allow-related"}
			ovnArgs := append(ingressArgs, egressArgs...)
			if _, err := c.ovnNbCommand(ovnArgs...); err != nil {
				return err
			}
		}
	}
	return nil
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
		var newAddrs []string
		for _, addr := range addresses {
			if util.CheckProtocol(addr) == kubeovnv1.ProtocolIPv6 {
				newAddr := strings.ReplaceAll(addr, ":", "\\:")
				newAddrs = append(newAddrs, newAddr)
			} else {
				newAddrs = append(newAddrs, addr)
			}
		}
		ovnArgs = append(ovnArgs, "--", "add", "address_set", as, "addresses")
		ovnArgs = append(ovnArgs, newAddrs...)
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

// StartOvnNbctlDaemon start a daemon and set OVN_NB_DAEMON env
func StartOvnNbctlDaemon(ovnNbAddr string) error {
	klog.Infof("start ovn-nbctl daemon")
	output, err := exec.Command(
		"pkill",
		"-f",
		"ovn-nbctl",
	).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to kill old ovn-nbctl daemon: %q", output)
		return err
	}
	command := []string{
		fmt.Sprintf("--db=%s", ovnNbAddr),
		"--pidfile",
		"--detach",
		"--overwrite-pidfile",
	}
	if os.Getenv("ENABLE_SSL") == "true" {
		command = []string{
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert",
			fmt.Sprintf("--db=%s", ovnNbAddr),
			"--pidfile",
			"--detach",
			"--overwrite-pidfile",
		}
	}
	_ = os.Unsetenv("OVN_NB_DAEMON")
	output, err = exec.Command("ovn-nbctl", command...).CombinedOutput()
	if err != nil {
		klog.Errorf("start ovn-nbctl daemon failed, %q", output)
		return err
	}

	daemonSocket := strings.TrimSpace(string(output))
	if err := os.Setenv("OVN_NB_DAEMON", daemonSocket); err != nil {
		klog.Errorf("failed to set env OVN_NB_DAEMON, %v", err)
		return err
	}
	return nil
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

func (c Client) CreateLocalnetPort(ls, port, providerName, vlanID string) error {
	cmdArg := []string{
		MayExist, "lsp-add", ls, port, "--",
		"lsp-set-addresses", port, "unknown", "--",
		"lsp-set-type", port, "localnet", "--",
		"lsp-set-options", port, fmt.Sprintf("network_name=%s", providerName),
	}
	if vlanID != "" && vlanID != "0" {
		cmdArg = append(cmdArg,
			"--", "set", "logical_switch_port", port, fmt.Sprintf("tag=%s", vlanID))
	}

	if _, err := c.ovnNbCommand(cmdArg...); err != nil {
		klog.Errorf("create localnet port %s failed, %v", port, err)
		return err
	}

	return nil
}
