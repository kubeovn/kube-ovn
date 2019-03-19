package ovs

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
	"encoding/json"
	"fmt"
	"k8s.io/klog"
	"os/exec"
	"strings"
)

type Client struct {
	OvnNbAddress           string
	OvnSbAddress           string
	ClusterRouter          string
	ClusterTcpLoadBalancer string
	ClusterUdpLoadBalancer string
}

const (
	OvnNbCtl = "ovn-nbctl"
	MayExist = "--may-exist"
	IfExists = "--if-exists"
	WaitSb   = "--wait=sb"
)

var GlobalDnsTable string
var GlobalTcpLb string
var GlobalUdpLb string

func (c Client) ovnCommand(arg ...string) (string, error) {
	cmdArgs := []string{fmt.Sprintf("--db=%s", c.OvnNbAddress)}
	cmdArgs = append(cmdArgs, arg...)
	raw, err := exec.Command(OvnNbCtl, cmdArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s, %v", string(raw), err)
	}
	return trimCommandOutput(raw), nil
}

func trimCommandOutput(raw []byte) string {
	output := strings.TrimSpace(string(raw))
	return strings.Trim(output, "\"")
}

func NewClient(ovnNbHost string, ovnNbPort int, ovnSbHost string, ovnSbPort int, ClusterRouter, ClusterTcpLoadBalancer, ClusterUdpLoadBalancer string) *Client {
	return &Client{
		OvnNbAddress:           fmt.Sprintf("tcp:%s:%d", ovnNbHost, ovnNbPort),
		OvnSbAddress:           fmt.Sprintf("tcp:%s:%d", ovnSbHost, ovnSbPort),
		ClusterRouter:          ClusterRouter,
		ClusterTcpLoadBalancer: ClusterTcpLoadBalancer,
		ClusterUdpLoadBalancer: ClusterUdpLoadBalancer,
	}
}

func (c Client) DeletePort(port string) error {
	_, err := c.ovnCommand(WaitSb, IfExists, "lsp-del", port)
	if err != nil {
		return fmt.Errorf("failed to delete port %s, %v", port, err)
	}
	return nil
}

func (c Client) CreatePort(ls, port, ip, mac string) (*Nic, error) {
	if ip == "" && mac == "" {
		_, err := c.ovnCommand(WaitSb, MayExist, "lsp-add", ls, port,
			"--", "set", "logical_switch_port", port, "addresses=dynamic")
		if err != nil {
			klog.Errorf("create port %s failed %v", port, err)
			return nil, err
		}

		output, err := c.ovnCommand("get", "logical_switch_port", port, "dynamic-addresses")
		if err != nil {
			klog.Errorf("get port %s addresses failed %v", port, err)
			return nil, err
		}
		mac = strings.Split(output, " ")[0]
		ip = strings.Split(output, " ")[1]
	} else {
		if mac == "" {
			mac = util.GenerateMac()
		}

		// remove mask, and retrieve mask from subnet cidr later
		// in this way we can deal with static ip with/without mask
		ip = strings.Split(ip, "/")[0]

		_, err := c.ovnCommand(WaitSb, MayExist, "lsp-add", ls, port, "--",
			"lsp-set-addresses", port, fmt.Sprintf("%s %s", mac, ip))
		if err != nil {
			klog.Errorf("create port %s failed %v", port, err)
			return nil, err
		}
	}
	cidr, err := c.ovnCommand("get", "logical_switch", ls, "other_config:subnet")
	if err != nil {
		klog.Errorf("get switch %s failed %v", ls, err)
		return nil, err
	}
	mask := strings.Split(cidr, "/")[1]

	gw, err := c.ovnCommand("get", "logical_switch", ls, "other_config:gateway")
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
	_, err := c.ovnCommand(WaitSb, MayExist, "ls-add", ls, "--",
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
	_, err = c.ovnCommand("lrp-add", clusterLr, clusterToTransit, mac, toClusterIP, "--",
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
	_, err := c.ovnCommand(WaitSb, MayExist, "ls-add", ls)
	if err != nil {
		klog.Errorf("create outside ls %s failed, %v", ls, err)
		return err
	}

	// 2. connect outside ls with edge lr
	outsideToEdge := fmt.Sprintf("%s-%s", ls, edgeLr)
	edgeToOutside := fmt.Sprintf("%s-%s", edgeLr, ls)
	_, err = c.ovnCommand("lrp-add", edgeLr, edgeToOutside, mac, ip)
	if err != nil {
		klog.Errorf("create lsp on edge failed, %v", err)
		return err
	}

	_, err = c.ovnCommand(WaitSb, MayExist, "lsp-add", ls, outsideToEdge, "--",
		"lsp-set-type", outsideToEdge, "router", "--",
		"lsp-set-addresses", outsideToEdge, mac, "--",
		"lsp-set-options", outsideToEdge, fmt.Sprintf("router-port=%s", edgeToOutside))
	if err != nil {
		klog.Errorf("failed to connect outside to edge, %v", err)
		return err
	}

	// 3. create localnet port to connect outside to physic net
	outsideToLocal := fmt.Sprintf("%s-localnet", ls)
	_, err = c.ovnCommand(WaitSb, MayExist, "lsp-add", ls, outsideToLocal, "--",
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
	_, err := c.ovnCommand(WaitSb, MayExist, "ls-add", ls, "--",
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

	err = c.AddDnsTableToLogicalSwitch(ls)
	if err != nil {
		klog.Errorf("failed to add cluster dns to %s, %v", ls, err)
		return err
	}
	return err
}

func (c Client) ListLogicalSwitch() ([]string, error) {
	output, err := c.ovnCommand("ls-list")
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
	output, err := c.ovnCommand("lr-list")
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
	_, err := c.ovnCommand(WaitSb, IfExists, "lrp-del", fmt.Sprintf("%s-%s", c.ClusterRouter, ls))
	if err != nil {
		klog.Errorf("failed to del lrp %s-%s, %v", c.ClusterRouter, ls, err)
		return err
	}

	_, err = c.ovnCommand(WaitSb, IfExists, "ls-del", ls)
	if err != nil {
		klog.Errorf("failed to del ls %s, %v", ls, err)
		return err
	}
	return nil
}

func (c Client) CreateGatewayRouter(lr, chassis string) error {
	_, err := c.ovnCommand(WaitSb, MayExist, "lr-add", lr, "--",
		"set", "logical_router", lr, fmt.Sprintf("options:chassis=%s", chassis))
	return err
}

func (c Client) CreateLogicalRouter(lr string) error {
	_, err := c.ovnCommand(WaitSb, MayExist, "lr-add", lr)
	return err
}

func (c Client) CreateRouterPort(ls, lr, ip, mac string) error {
	klog.Infof("add %s to %s with ip: %s, mac :%s", ls, lr, ip, mac)
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)
	_, err := c.ovnCommand(WaitSb, MayExist, "lsp-add", ls, lsTolr, "--",
		"set", "logical_switch_port", lsTolr, "type=router", "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("addresses=\"%s\"", mac), "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("options:router-port=%s", lrTols))
	if err != nil {
		klog.Errorf("failed to create switch router port %s %v", lsTolr, err)
		return err
	}

	_, err = c.ovnCommand(WaitSb, MayExist, "lrp-add", lr, lrTols, mac, ip)
	if err != nil {
		klog.Errorf("failed to create router port %s %v", lrTols, err)
		return err
	}
	return nil
}

func (c Client) AddStaticRouter(cidr, nextHop, router string) error {
	_, err := c.ovnCommand(WaitSb, MayExist, "lr-route-add", router, cidr, nextHop)
	return err
}

func (c Client) DeleteStaticRouter(cidr, router string) error {
	_, err := c.ovnCommand(WaitSb, IfExists, "lr-route-del", router, cidr)
	return err
}

func (c Client) FindLoadbalancer(lb string) (string, error) {
	output, err := c.ovnCommand("--data=bare", "--no-heading", "--columns=_uuid", fmt.Sprintf("--db=%s", c.OvnNbAddress),
		"find", "load_balancer", fmt.Sprintf("name=%s", lb))
	return output, err
}

func (c Client) CreateLoadBalancer(lb, protocol string) error {
	_, err := c.ovnCommand("create", "load_balancer",
		fmt.Sprintf("name=%s", lb), fmt.Sprintf("protocol=%s", protocol))
	return err
}

func (c Client) CreateLoadBalancerRule(lb, vip, ips string) error {
	_, err := c.ovnCommand(WaitSb, MayExist, "lb-add", lb, vip, ips)
	return err
}

func (c Client) AddLoadBalancerToLogicalSwitch(lb, ls string) error {
	_, err := c.ovnCommand(WaitSb, MayExist, "ls-lb-add", ls, lb)
	return err
}

func (c Client) DeleteLoadBalancerVip(vip, lb string) error {
	_, err := c.ovnCommand(WaitSb, IfExists, "lb-del", lb, vip)
	return err
}

func (c Client) GetLoadBalancerVips(lb string) (map[string]string, error) {
	output, err := c.ovnCommand("--data=bare", "--no-heading",
		"get", "load_balancer", lb, "vips")
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	err = json.Unmarshal([]byte(strings.Replace(output, "=", ":", -1)), &result)
	return result, err
}

func (c Client) CreateDnsTable() (string, error) {
	output, err := c.ovnCommand("--data=bare", "--no-heading", "--columns=_uuid", fmt.Sprintf("--db=%s", c.OvnNbAddress),
		"find", "DNS", "external_ids:name=ovn")
	if err != nil {
		return "", nil
	}
	if output != "" {
		return output, nil
	} else {
		_, err := c.ovnCommand("create", "DNS", "external_ids:name=ovn")
		if err != nil {
			return "", nil
		}
		output, err := c.ovnCommand("--data=bare", "--no-heading", "--columns=_uuid", fmt.Sprintf("--db=%s", c.OvnNbAddress),
			"find", "DNS", "external_ids:name=ovn")
		if err != nil {
			return "", nil
		}
		return output, nil
	}
}

func (c Client) AddDnsRecord(domain string, addresses []string) error {
	_, err := c.ovnCommand("set", "DNS", GlobalDnsTable, fmt.Sprintf("records:%s=%s", domain, strings.Join(addresses, ",")))
	return err
}

func (c Client) DeleteDnsRecord(domain string) error {
	_, err := c.ovnCommand(IfExists, "remove", "DNS", GlobalDnsTable, "records", domain)
	return err
}

func (c Client) GetDnsRecords() (map[string]string, error) {
	output, err := c.ovnCommand("--data=bare", "--no-heading",
		"get", "dns", GlobalDnsTable, "records")
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	err = json.Unmarshal([]byte(strings.Replace(output, "=", ":", -1)), &result)
	return result, err
}

func (c Client) AddDnsTableToLogicalSwitch(ls string) error {
	_, err := c.ovnCommand("add", "logical_switch", ls, "dns_records", GlobalDnsTable)
	return err
}
