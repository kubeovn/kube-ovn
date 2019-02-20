package ovs

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
	"fmt"
	"k8s.io/klog"
	"os/exec"
	"strings"
	"time"
)

const (
	ovsCommandTimeout = 15
)

type Client struct {
	OvnNbAddress  string
	ClusterRouter string
}

func NewClient(ovnNbHost string, ovnNbPort int, clusterRouter string) *Client {
	return &Client{
		OvnNbAddress:  fmt.Sprintf("tcp:%s:%d", ovnNbHost, ovnNbPort),
		ClusterRouter: clusterRouter,
	}
}

func (c Client) DeletePort(port string) error {
	output, err := exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lsp-del", port).CombinedOutput()
	if err == nil {
		return nil
	}

	if strings.Contains(string(output), "not found") {
		return nil
	}
	return fmt.Errorf("failed to delete port %s, %s", port, string(output))
}

func (c Client) CreatePort(ls, port, ip, mac string) (*Nic, error) {
	// TODO
	// 1. If port exists, return it directly
	// 2. Use annotated ip and mac to replace dynamic addresses
	raw, err := exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lsp-add", ls, port,
		"--", "set", "logical_switch_port", port, "addresses=dynamic").CombinedOutput()
	if err != nil && !strings.Contains(string(raw), "already exists") {
		return nil, fmt.Errorf("create port %s failed %s, %v", port, string(raw), err)
	}

	// wait dynamic addresses
	time.Sleep(1 * time.Second)

	raw, err = exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "get", "logical_switch_port", port, "dynamic-addresses").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("get port %s failed %s, %v", port, string(raw), err)
	}
	output := trimCommandOutput(raw)
	mac = strings.Split(output, " ")[0]
	ip = strings.Split(output, " ")[1]

	raw, err = exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "get", "logical_switch", ls, "other_config:subnet").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("get switch %s failed %s, %v", ls, string(raw), err)
	}
	cidr := trimCommandOutput(raw)
	mask := strings.Split(cidr, "/")[1]

	raw, err = exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "get", "logical_switch", ls, "other_config:gateway").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("get switch %s failed %s, %v", ls, string(raw), err)
	}
	gw := trimCommandOutput(raw)

	return &Nic{IpAddress: fmt.Sprintf("%s/%s", ip, mask), MacAddress: mac, CIDR: cidr, Gateway: gw}, nil
}

type Nic struct {
	IpAddress  string
	MacAddress string
	CIDR       string
	Gateway    string
}

func trimCommandOutput(raw []byte) string {
	output := strings.TrimSpace(string(raw))
	return strings.Trim(output, "\"")
}

func (c Client) CreateLogicalSwitch(ls, subnet, gateway, excludeIps string) error {
	// TODO: should check if ls exists first
	raw, err := exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "ls-add", ls, "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:subnet=%s", subnet), "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:gateway=%s", gateway), "--",
		"set", "logical_switch", ls, fmt.Sprintf("other_config:exclude_ips=%s", excludeIps)).CombinedOutput()
	if err != nil && !strings.Contains(string(raw), "already exists") {
		klog.Errorf("create switch %s failed %s", ls, string(raw))
		return fmt.Errorf(string(raw))
	}

	mac := util.GenerateMac()
	mask := strings.Split(subnet, "/")[1]
	err = c.CreateRouterPort(ls, c.ClusterRouter, gateway+"/"+mask, mac)
	return err
}

func (c Client) ListLogicalSwitch() ([]string, error) {
	raw, err := exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "ls-list").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to list logical switch %s", string(raw))
		return nil, fmt.Errorf(string(raw))
	}
	output := trimCommandOutput(raw)
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		tmp := strings.Split(l, " ")[1]
		tmp = strings.Trim(tmp, "()")
		result = append(result, tmp)
	}
	return result, nil
}

func (c Client) DeleteLogicalSwitch(ls string) error {
	raw, err := exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "ls-del", ls).CombinedOutput()
	if err == nil || strings.Contains(string(raw), "not found") {
		return nil
	}
	raw, err = exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lrp-del", fmt.Sprintf("%s-%s", c.ClusterRouter, ls)).CombinedOutput()
	if err == nil || strings.Contains(string(raw), "not found") {
		return nil
	}
	return fmt.Errorf(string(raw))
}

func (c Client) CreateLogicalRouter(lr string) error {
	raw, err := exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lr-add", lr).CombinedOutput()
	if err == nil || strings.Contains(string(raw), "already exists") {
		klog.Infof("router %s ready", lr)
		return nil
	}
	klog.Errorf("create router %s failed %s", lr, string(raw))
	return fmt.Errorf(string(raw))
}

func (c Client) CreateRouterPort(ls, lr, ip, mac string) error {
	klog.Infof("add %s to %s with ip: %s, mac :%s", ls, lr, ip, mac)
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)
	raw, err := exec.Command(
		"ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lsp-add", ls, lsTolr, "--",
		"set", "logical_switch_port", lsTolr, "type=router", "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("addresses=\"%s\"", mac), "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("options:router-port=%s", lrTols)).CombinedOutput()
	if err != nil && !strings.Contains(string(raw), "already exists") {
		klog.Errorf("failed to create switch router port %s %s", lsTolr, string(raw))
		return fmt.Errorf(string(raw))
	}

	raw, err = exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lrp-add", lr, lrTols, mac, ip).CombinedOutput()
	if err != nil && !strings.Contains(string(raw), "already exists") {
		klog.Errorf("failed to create router port %s %s", lrTols, string(raw))
		return fmt.Errorf(string(raw))
	}
	return nil
}
