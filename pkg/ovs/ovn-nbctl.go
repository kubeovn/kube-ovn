package ovs

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alauda/kube-ovn/pkg/util"
	"k8s.io/klog"
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

func (c Client) DeletePort(port string) error {
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lsp-del", port)
	if err != nil {
		return fmt.Errorf("failed to delete port %s, %v", port, err)
	}
	return nil
}

func (c Client) CreatePort(ls, port, ip, mac string) (*Nic, error) {
	if ip == "" && mac == "" {
		_, err := c.ovnNbCommand(WaitSb, MayExist, "lsp-add", ls, port,
			"--", "set", "logical_switch_port", port, "addresses=dynamic")
		if err != nil {
			klog.Errorf("create port %s failed %v", port, err)
			return nil, err
		}

		address, err := c.GetLogicalSwitchPortDynamicAddress(port)
		if err != nil {
			klog.Errorf("get port %s dynamic-addresses failed %v", port, err)
			return nil, err
		}
		mac = address[0]
		ip = address[1]
	} else {
		if mac == "" {
			mac = util.GenerateMac()
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
	}
	cidr, err := c.ovnNbCommand("get", "logical_switch", ls, "other_config:subnet")
	if err != nil {
		klog.Errorf("get switch %s failed %v", ls, err)
		return nil, err
	}
	mask := strings.Split(cidr, "/")[1]

	gw, err := c.ovnNbCommand("get", "logical_switch", ls, "other_config:gateway")
	if err != nil {
		klog.Errorf("get switch %s failed %v", ls, err)
		return nil, err
	}

	return &Nic{IpAddress: fmt.Sprintf("%s/%s", ip, mask), MacAddress: mac, CIDR: cidr, Gateway: gw}, nil
}

type Nic struct {
	IpAddress  string
	MacAddress string
	CIDR       string
	Gateway    string
}

func (c Client) CreateTransitLogicalSwitch(ls, clusterLr, edgeLr, toClusterIP, toEdgeIP string) error {
	mac := util.GenerateMac()
	edgeToTransit := fmt.Sprintf("%s-%s", edgeLr, ls)
	transitToEdge := fmt.Sprintf("%s-%s", ls, edgeLr)
	_, err := c.ovnNbCommand(WaitSb, MayExist, "ls-add", ls, "--",
		"lrp-add", edgeLr, edgeToTransit, mac, toEdgeIP, "--",
		"lsp-add", ls, transitToEdge, "--",
		"lsp-set-type", transitToEdge, "router", "--",
		"lsp-set-addresses", transitToEdge, mac, "--",
		"lsp-set-options", transitToEdge, fmt.Sprintf("router-port=%s", edgeToTransit))
	if err != nil {
		klog.Errorf("connect edge to transit failed %v", err)
		return err
	}

	mac = util.GenerateMac()
	clusterToTransit := fmt.Sprintf("%s-%s", clusterLr, ls)
	transitToCluster := fmt.Sprintf("%s-%s", ls, clusterLr)
	_, err = c.ovnNbCommand("lrp-add", clusterLr, clusterToTransit, mac, toClusterIP, "--",
		"lsp-add", ls, transitToCluster, "--",
		"lsp-set-type", transitToCluster, "router", "--",
		"lsp-set-addresses", transitToCluster, mac, "--",
		"lsp-set-options", transitToCluster, fmt.Sprintf("router-port=%s", clusterToTransit))
	if err != nil {
		klog.Errorf("connect cluster to transit failed %v", err)
		return err
	}
	return nil
}

func (c Client) CreateOutsideLogicalSwitch(ls, edgeLr, ip, mac string) error {
	// 1. create outside logical switch
	_, err := c.ovnNbCommand(WaitSb, MayExist, "ls-add", ls)
	if err != nil {
		klog.Errorf("create outside ls %s failed, %v", ls, err)
		return err
	}

	// 2. connect outside ls with edge lr
	outsideToEdge := fmt.Sprintf("%s-%s", ls, edgeLr)
	edgeToOutside := fmt.Sprintf("%s-%s", edgeLr, ls)
	_, err = c.ovnNbCommand("lrp-add", edgeLr, edgeToOutside, mac, ip)
	if err != nil {
		klog.Errorf("create lsp on edge failed, %v", err)
		return err
	}

	_, err = c.ovnNbCommand(WaitSb, MayExist, "lsp-add", ls, outsideToEdge, "--",
		"lsp-set-type", outsideToEdge, "router", "--",
		"lsp-set-addresses", outsideToEdge, mac, "--",
		"lsp-set-options", outsideToEdge, fmt.Sprintf("router-port=%s", edgeToOutside))
	if err != nil {
		klog.Errorf("failed to connect outside to edge, %v", err)
		return err
	}

	// 3. create localnet port to connect outside to physic net
	outsideToLocal := fmt.Sprintf("%s-localnet", ls)
	_, err = c.ovnNbCommand(WaitSb, MayExist, "lsp-add", ls, outsideToLocal, "--",
		"lsp-set-addresses", outsideToLocal, "unknown", "--",
		"lsp-set-type", outsideToLocal, "localnet", "--",
		"lsp-set-options", outsideToLocal, "network_name=dataNet")
	if err != nil {
		klog.Errorf("failed to create localnet port %v", err)
		return err
	}
	return nil
}

func (c Client) CreateLogicalSwitch(ls, subnet, gateway, excludeIps string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "ls-add", ls, "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", subnet), "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", excludeIps))
	if err != nil {
		klog.Errorf("create switch %s failed %v", ls, err)
		return err
	}

	mac := util.GenerateMac()
	mask := strings.Split(subnet, "/")[1]
	klog.Infof("create route port for switch %s", ls)
	err = c.CreateRouterPort(ls, c.ClusterRouter, gateway+"/"+mask, mac)
	if err != nil {
		klog.Errorf("failed to connect switch %s to router, %v", ls, err)
		return err
	}
	if ls != c.NodeSwitch {
		// DO NOT add ovn dns/lb to node switch
		err = c.AddLoadBalancerToLogicalSwitch(c.ClusterTcpLoadBalancer, ls)
		if err != nil {
			klog.Errorf("failed to add cluster tcp lb to %s, %v", ls, err)
			return err
		}

		err = c.AddLoadBalancerToLogicalSwitch(c.ClusterUdpLoadBalancer, ls)
		if err != nil {
			klog.Errorf("failed to add cluster udp lb to %s, %v", ls, err)
			return err
		}
	}

	return nil
}

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

func (c Client) CreateGatewayRouter(lr, chassis string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "lr-add", lr, "--",
		"set", "logical_router", lr, fmt.Sprintf("options:chassis=%s", chassis))
	return err
}

func (c Client) CreateLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "lr-add", lr)
	return err
}

func (c Client) CreateRouterPort(ls, lr, ip, mac string) error {
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

func (c Client) AddStaticRouter(policy, cidr, nextHop, router string) error {
	if policy == "" {
		policy = PolicyDstIP
	}
	_, err := c.ovnNbCommand(WaitSb, MayExist, fmt.Sprintf("%s=%s", Policy, policy), "lr-route-add", router, cidr, nextHop)
	return err
}

func (c Client) DeleteStaticRouter(cidr, router string) error {
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lr-route-del", router, cidr)
	return err
}

func (c Client) FindLoadbalancer(lb string) (string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
		"find", "load_balancer", fmt.Sprintf("name=%s", lb))

	// deal with ovn-nbctl daemon format bug
	if strings.Contains(output, " ") {
		part := strings.Split(output, " ")
		output = part[len(part)-1]
	}
	return output, err
}

func (c Client) CreateLoadBalancer(lb, protocol string) error {
	_, err := c.ovnNbCommand("create", "load_balancer",
		fmt.Sprintf("name=%s", lb), fmt.Sprintf("protocol=%s", protocol))
	return err
}

func (c Client) CreateLoadBalancerRule(lb, vip, ips string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "lb-add", lb, vip, ips)
	return err
}

func (c Client) AddLoadBalancerToLogicalSwitch(lb, ls string) error {
	_, err := c.ovnNbCommand(WaitSb, MayExist, "ls-lb-add", ls, lb)
	return err
}

func (c Client) DeleteLoadBalancerVip(vip, lb string) error {
	_, err := c.ovnNbCommand(WaitSb, IfExists, "lb-del", lb, vip)
	return err
}

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

func (c Client) CleanLogicalSwitchAcl(ls string) error {
	_, err := c.ovnNbCommand("acl-del", ls)
	return err
}

func (c Client) SetPrivateLogicalSwitch(ls string, allow []string) error {
	allowArgs := []string{}
	for _, subnet := range allow {
		match := fmt.Sprintf(`"ip4.src == %s"`, strings.TrimSpace(subnet))
		allowArgs = append(allowArgs, "--", "acl-add", ls, "to-lport", util.SubnetAllowPriority, match, "allow-related")
	}
	delArgs := []string{"acl-del", ls}
	dropArgs := []string{"--", "acl-add", ls, "to-lport", util.DefaultDropPriority, fmt.Sprintf(`inport == "%s-%s"`, ls, c.ClusterRouter), "drop"}
	nodeSwitchArgs := []string{"--", "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf(`"ip4.src == %s"`, c.NodeSwitchCIDR), "allow-related"}

	ovnArgs := append(delArgs, dropArgs...)
	ovnArgs = append(ovnArgs, nodeSwitchArgs...)
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
	// currently user may only have one fixed address
	// ["0a:00:00:00:00:0c 10.16.0.13"]
	output = strings.Trim(output, `[]"`)
	mac := strings.Split(output, " ")[0]
	ip := strings.Split(output, " ")[1]
	return []string{mac, ip}, nil
}

func (c Client) GetLogicalSwitchPortDynamicAddress(port string) ([]string, error) {
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

func (c Client) ListLogicalRouterPort() (string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=name,networks",
		"list", "Logical_Router_Port")
	return output, err
}
