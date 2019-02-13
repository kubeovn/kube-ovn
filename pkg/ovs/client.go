package ovs

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	ovsCommandTimeout = 15
)

type Client struct {
	OvnNbAddress string
}

func NewClient(ovnNbHost string, ovnNbPort int) *Client {
	return &Client{OvnNbAddress: fmt.Sprintf("tcp:%s:%d", ovnNbHost, ovnNbPort)}
}

func (c Client) DeletePort(ls, port string) error {
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
	var err error
	defer func() {
		if err != nil {
			exec.Command("ovn-nbctl", fmt.Sprintf("--db=%s", c.OvnNbAddress), "lsp-del", port).CombinedOutput()
		}
	}()
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
