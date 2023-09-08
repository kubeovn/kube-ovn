package ovs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	netv1 "k8s.io/api/networking/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type AclDirection string

const (
	SgAclIngressDirection AclDirection = "to-lport"
	SgAclEgressDirection  AclDirection = "from-lport"
)

var nbctlDaemonSocketRegexp = regexp.MustCompile(`^/var/run/ovn/ovn-nbctl\.[0-9]+\.ctl$`)

func (c LegacyClient) ovnNbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout), "--no-wait"}, cmdArgs...)
	raw, err := exec.Command(OvnNbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms, output %q", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed, raw)
	method := ""
	for _, arg := range cmdArgs {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}
	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovn-nb", method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovn-nbctl command error: %s %s in %vms", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed)
		return "", fmt.Errorf("%s, %q", raw, err)
	} else if elapsed > 500 {
		klog.Warningf("ovn-nbctl command took too long: %s %s in %vms", OvnNbCtl, strings.Join(cmdArgs, " "), elapsed)
	}
	return trimCommandOutput(raw), nil
}

func (c LegacyClient) SetAzName(azName string) error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", fmt.Sprintf("name=%s", azName)); err != nil {
		return fmt.Errorf("failed to set az name, %v", err)
	}
	return nil
}

func (c LegacyClient) SetUseCtInvMatch() error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", "options:use_ct_inv_match=false"); err != nil {
		return fmt.Errorf("failed to set NB_Global option use_ct_inv_match to false: %v", err)
	}
	return nil
}

func (c LegacyClient) SetICAutoRoute(enable bool, blackList []string) error {
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
func (c LegacyClient) DeleteLogicalSwitchPort(port string) error {
	if _, err := c.ovnNbCommand(IfExists, "lsp-del", port); err != nil {
		return fmt.Errorf("failed to delete logical switch port %s, %v", port, err)
	}
	return nil
}

// DeleteLogicalRouterPort delete logical switch port in ovn
func (c LegacyClient) DeleteLogicalRouterPort(port string) error {
	if _, err := c.ovnNbCommand(IfExists, "lrp-del", port); err != nil {
		return fmt.Errorf("failed to delete logical router port %s, %v", port, err)
	}
	return nil
}

func (c LegacyClient) CreateICLogicalRouterPort(az, mac, subnet string, chassises []string) error {
	if _, err := c.ovnNbCommand(MayExist, "lrp-add", c.ClusterRouter, fmt.Sprintf("%s-ts", az), mac, subnet); err != nil {
		return fmt.Errorf("failed to crate ovn-ic lrp, %v", err)
	}
	if _, err := c.ovnNbCommand(MayExist, "lsp-add", util.InterconnectionSwitch, fmt.Sprintf("ts-%s", az), "--",
		"lsp-set-addresses", fmt.Sprintf("ts-%s", az), "router", "--",
		"lsp-set-type", fmt.Sprintf("ts-%s", az), "router", "--",
		"lsp-set-options", fmt.Sprintf("ts-%s", az), fmt.Sprintf("router-port=%s-ts", az), "--",
		"set", "logical_switch_port", fmt.Sprintf("ts-%s", az), fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)); err != nil {
		return fmt.Errorf("failed to create ovn-ic lsp, %v", err)
	}
	for index, chassis := range chassises {
		if _, err := c.ovnNbCommand("lrp-set-gateway-chassis", fmt.Sprintf("%s-ts", az), chassis, fmt.Sprintf("%d", 100-index)); err != nil {
			return fmt.Errorf("failed to set gateway chassis, %v", err)
		}
	}
	return nil
}

func (c LegacyClient) DeleteICLogicalRouterPort(az string) error {
	if err := c.DeleteLogicalRouterPort(fmt.Sprintf("%s-ts", az)); err != nil {
		return fmt.Errorf("failed to delete ovn-ic logical router port: %v", err)
	}
	if err := c.DeleteLogicalSwitchPort(fmt.Sprintf("ts-%s", az)); err != nil {
		return fmt.Errorf("failed to delete ovn-ic logical switch port: %v", err)
	}
	return nil
}

func (c LegacyClient) SetPortAddress(port, mac, ip string) error {
	rets, err := c.ListLogicalEntity("logical_switch_port", fmt.Sprintf("name=%s", port))
	if err != nil {
		return fmt.Errorf("failed to find port %s: %v", port, err)
	}
	if len(rets) == 0 {
		return nil
	}

	var addresses []string
	addresses = append(addresses, mac)
	addresses = append(addresses, strings.Split(ip, ",")...)
	if _, err := c.ovnNbCommand("lsp-set-addresses", port, strings.Join(addresses, " ")); err != nil {
		klog.Errorf("set port %s addresses failed, %v", port, err)
		return err
	}
	return nil
}

func (c LegacyClient) SetPortExternalIds(port, key, value string) error {
	rets, err := c.ListLogicalEntity("logical_switch_port", fmt.Sprintf("name=%s", port))
	if err != nil {
		return fmt.Errorf("failed to find port %s: %v", port, err)
	}
	if len(rets) == 0 {
		return nil
	}

	if _, err := c.ovnNbCommand("set", "logical_switch_port", port, fmt.Sprintf("external_ids:%s=\"%s\"", key, value)); err != nil {
		klog.Errorf("set port %s external_ids failed: %v", port, err)
		return err
	}
	return nil
}

func (c LegacyClient) SetPortSecurity(portSecurity bool, port, mac, ipStr, vips string) error {
	var addresses []string
	ovnCommand := []string{"lsp-set-port-security", port}
	if portSecurity {
		addresses = append(addresses, mac)
		addresses = append(addresses, strings.Split(ipStr, ",")...)
		if vips != "" {
			addresses = append(addresses, strings.Split(vips, ",")...)
		}
		ovnCommand = append(ovnCommand, strings.Join(addresses, " "))
	}

	if _, err := c.ovnNbCommand(ovnCommand...); err != nil {
		klog.Errorf("set port %s security failed: %v", port, err)
		return err
	}
	return nil
}

// CreatePort create logical switch port in ovn
func (c LegacyClient) CreatePort(ls, port, ip, mac, pod, namespace string, portSecurity bool, securityGroups string, vips string, liveMigration bool, hasUnknown bool) error {
	var ovnCommand []string
	var addresses []string
	addresses = append(addresses, mac)
	addresses = append(addresses, strings.Split(ip, ",")...)
	ovnCommand = []string{MayExist, "lsp-add", ls, port}
	isAddrConflict := false
	if liveMigration {
		// add external_id info as the filter of 'live Migration vm port'
		ovnCommand = append(ovnCommand,
			"--", "set", "logical_switch_port", port, fmt.Sprintf("external_ids:ls=%s", ls),
			"--", "set", "logical_switch_port", port, fmt.Sprintf("external_ids:ip=%s", strings.ReplaceAll(ip, ",", "/")))

		ports, err := c.ListLogicalEntity("logical_switch_port",
			fmt.Sprintf("external_ids:ls=%s", ls),
			fmt.Sprintf("external_ids:ip=\"%s\"", strings.ReplaceAll(ip, ",", "/")))
		if err != nil {
			klog.Errorf("list logical entity failed: %v", err)
			return err
		}
		if len(ports) > 0 {
			isAddrConflict = true
		}
	}

	if isAddrConflict {
		// only set mac, and set flag 'liveMigration'
		ovnCommand = append(ovnCommand, "--", "lsp-set-addresses", port, mac, "--",
			"set", "logical_switch_port", port, "external_ids:liveMigration=1")
	} else {
		// set mac and ip
		ovnCommand = append(ovnCommand,
			"--", "lsp-set-addresses", port, strings.Join(addresses, " "))

		if hasUnknown {
			ovnCommand = append(ovnCommand, "unknown")
		}
	}

	if portSecurity {
		if vips != "" {
			addresses = append(addresses, strings.Split(vips, ",")...)
		}
		ovnCommand = append(ovnCommand,
			"--", "lsp-set-port-security", port, strings.Join(addresses, " "))

		if securityGroups != "" {
			sgList := strings.Split(securityGroups, ",")
			ovnCommand = append(ovnCommand,
				"--", "set", "logical_switch_port", port, fmt.Sprintf("external_ids:security_groups=%s", strings.ReplaceAll(securityGroups, ",", "/")))
			for _, sg := range sgList {
				ovnCommand = append(ovnCommand,
					"--", "set", "logical_switch_port", port, fmt.Sprintf("external_ids:associated_sg_%s=true", sg))
			}
		}
	}

	if pod != "" && namespace != "" {
		ovnCommand = append(ovnCommand,
			"--", "set", "logical_switch_port", port, fmt.Sprintf("external_ids:pod=%s/%s", namespace, pod), fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	} else {
		ovnCommand = append(ovnCommand,
			"--", "set", "logical_switch_port", port, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	}

	if _, err := c.ovnNbCommand(ovnCommand...); err != nil {
		klog.Errorf("create port %s failed: %v", port, err)
		return err
	}
	return nil
}

func (c LegacyClient) SetPortTag(name string, vlanID int) error {
	output, err := c.ovnNbCommand("get", "logical_switch_port", name, "tag")
	if err != nil {
		klog.Errorf("failed to get tag of logical switch port %s: %v, %q", name, err, output)
		return err
	}

	var newTag string
	if vlanID > 0 && vlanID < 4096 {
		newTag = strconv.Itoa(vlanID)
	}
	oldTag := strings.Trim(output, "[]")
	if oldTag == newTag {
		return nil
	}

	if oldTag != "" {
		if output, err = c.ovnNbCommand("remove", "logical_switch_port", name, "tag", oldTag); err != nil {
			klog.Errorf("failed to remove tag of logical switch port %s: %v, %q", name, err, output)
			return err
		}
	}
	if newTag != "" {
		if output, err = c.ovnNbCommand("add", "logical_switch_port", name, "tag", newTag); err != nil {
			klog.Errorf("failed to get tag of logical switch port %s: %v, %q", name, err, output)
			return err
		}
	}

	return nil
}

func (c LegacyClient) ListPodLogicalSwitchPorts(pod, namespace string) ([]string, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", "logical_switch_port", fmt.Sprintf("external_ids:pod=%s/%s", namespace, pod))
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

func (c LegacyClient) SetLogicalSwitchConfig(ls, lr, protocol, subnet, gateway string, excludeIps []string, needRouter, needMulticastSnoop bool) error {
	var err error
	cidrBlocks := strings.Split(subnet, ",")
	mask := strings.Split(cidrBlocks[0], "/")[1]

	var cmd []string
	var networks string
	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		networks = fmt.Sprintf("%s/%s", gateway, mask)
		cmd = []string{MayExist, "ls-add", ls}
	case kubeovnv1.ProtocolIPv6:
		gateway := strings.ReplaceAll(gateway, ":", "\\:")
		networks = fmt.Sprintf("%s/%s", gateway, mask)
		cmd = []string{MayExist, "ls-add", ls}
	case kubeovnv1.ProtocolDual:
		gws := strings.Split(gateway, ",")
		v6Mask := strings.Split(cidrBlocks[1], "/")[1]
		gwStr := gws[0] + "/" + mask + "," + gws[1] + "/" + v6Mask
		networks = strings.ReplaceAll(strings.Join(strings.Split(gwStr, ","), " "), ":", "\\:")

		cmd = []string{MayExist, "ls-add", ls}
	}
	if needRouter {
		lsTolr := fmt.Sprintf("%s-%s", ls, lr)
		lrTols := fmt.Sprintf("%s-%s", lr, ls)

		exist, err := c.LogicalSwitchPortExists(lsTolr)
		if err != nil {
			klog.Errorf("failed to get logical switch port %s to router, %v", lsTolr, err)
			return err
		}
		if !exist {
			cmd = append(cmd, []string{"--", MayExist, "lsp-add", ls, lsTolr, "--",
				"set", "logical_switch_port", lsTolr, "type=router", "--",
				"lsp-set-addresses", lsTolr, "router", "--",
				"set", "logical_switch_port", lsTolr, fmt.Sprintf("options:router-port=%s", lrTols), "--",
				"set", "logical_switch_port", lsTolr, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)}...)
		}

		// check router port exist
		results, err := c.ListLogicalEntity("logical_router_port", fmt.Sprintf("name=%s", lrTols))
		if err != nil {
			klog.Errorf("failed to list router port %s, %v", lrTols, err)
			return err
		}
		if len(results) == 0 {
			// v6address no need add \ when use lrp-add
			networks = strings.ReplaceAll(networks, "\\:", ":")
			networkList := strings.Split(networks, " ")
			cmd = append(cmd, []string{"--", MayExist, "lrp-add", lr, lrTols, util.GenerateMac()}...)
			cmd = append(cmd, networkList...)
		} else {
			cmd = append(cmd, []string{"--",
				"set", "logical_router_port", fmt.Sprintf("%s-%s", lr, ls), fmt.Sprintf("networks=%s", networks)}...)
		}
	}
	cmd = append(cmd, []string{"--",
		"set", "logical_switch", ls, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)}...)

	if needMulticastSnoop {
		cmd = append(cmd, []string{"--",
			"set", "logical_switch", ls, "other_config:mcast_snoop=true", "other_config:mcast_querier=false"}...)
	} else {
		cmd = append(cmd, []string{"--",
			"remove", "logical_switch", ls, "other_config", "mcast_snoop=true", "mcast_querier=false"}...)
	}

	_, err = c.ovnNbCommand(cmd...)
	if err != nil {
		klog.Errorf("set switch config for %s failed: %v", ls, err)
		return err
	}
	return nil
}

// CreateLogicalSwitch create logical switch in ovn, connect it to router and apply tcp/udp lb rules
func (c LegacyClient) CreateLogicalSwitch(ls, lr, subnet, gateway string, needRouter, needMulticastSnoop bool) error {
	cmd := []string{MayExist, "ls-add", ls, "--", "set", "logical_switch", ls, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)}

	if needMulticastSnoop {
		cmd = append(cmd, []string{"--",
			"set", "logical_switch", ls, "other_config:mcast_snoop=true", "other_config:mcast_querier=false"}...)
	}

	_, err := c.ovnNbCommand(cmd...)
	if err != nil {
		klog.Errorf("create switch %s failed: %v", ls, err)
		return err
	}

	if needRouter {
		if err := c.createRouterPort(ls, lr); err != nil {
			klog.Errorf("failed to connect switch %s to router, %v", ls, err)
			return err
		}
	}
	return nil
}

func (c LegacyClient) AddLbToLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error {
	if err := c.addLoadBalancerToLogicalSwitch(tcpLb, ls); err != nil {
		klog.Errorf("failed to add tcp lb to %s, %v", ls, err)
		return err
	}

	if err := c.addLoadBalancerToLogicalSwitch(udpLb, ls); err != nil {
		klog.Errorf("failed to add udp lb to %s, %v", ls, err)
		return err
	}

	if err := c.addLoadBalancerToLogicalSwitch(tcpSessLb, ls); err != nil {
		klog.Errorf("failed to add tcp session lb to %s, %v", ls, err)
		return err
	}

	if err := c.addLoadBalancerToLogicalSwitch(udpSessLb, ls); err != nil {
		klog.Errorf("failed to add udp session lb to %s, %v", ls, err)
		return err
	}

	return nil
}

func (c LegacyClient) RemoveLbFromLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error {
	if err := c.removeLoadBalancerFromLogicalSwitch(tcpLb, ls); err != nil {
		klog.Errorf("failed to remove tcp lb from %s, %v", ls, err)
		return err
	}

	if err := c.removeLoadBalancerFromLogicalSwitch(udpLb, ls); err != nil {
		klog.Errorf("failed to remove udp lb from %s, %v", ls, err)
		return err
	}

	if err := c.removeLoadBalancerFromLogicalSwitch(tcpSessLb, ls); err != nil {
		klog.Errorf("failed to remove tcp session lb from %s, %v", ls, err)
		return err
	}

	if err := c.removeLoadBalancerFromLogicalSwitch(udpSessLb, ls); err != nil {
		klog.Errorf("failed to remove udp session lb from %s, %v", ls, err)
		return err
	}

	return nil
}

// DeleteLoadBalancer delete loadbalancer in ovn
func (c LegacyClient) DeleteLoadBalancer(lbs ...string) error {
	for _, lb := range lbs {
		lbid, err := c.FindLoadbalancer(lb)
		if err != nil {
			klog.Warningf("failed to find load_balancer '%s', %v", lb, err)
			continue
		}
		if _, err := c.ovnNbCommand(IfExists, "destroy", "load_balancer", lbid); err != nil {
			return err
		}
	}
	return nil
}

// ListLoadBalancer list loadbalancer names
func (c LegacyClient) ListLoadBalancer() ([]string, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", "load_balancer")
	if err != nil {
		klog.Errorf("failed to list load balancer: %v", err)
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

func (c LegacyClient) CreateGatewaySwitch(name, externalgatewaynet string, externalgatewayvlanid int, ip, mac string, chassises []string) error {
	lsTolr := fmt.Sprintf("%s-%s", name, c.ClusterRouter)
	lrTols := fmt.Sprintf("%s-%s", c.ClusterRouter, name)
	localnetPort := fmt.Sprintf("ln-%s", name)
	portOptions := fmt.Sprintf("network_name=%s", externalgatewaynet)
	_, err := c.ovnNbCommand(
		MayExist, "ls-add", name, "--",
		"set", "logical_switch", name, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName), "--",
		MayExist, "lsp-add", name, localnetPort, "--",
		"lsp-set-type", localnetPort, "localnet", "--",
		"lsp-set-addresses", localnetPort, "unknown", "--",
		"lsp-set-options", localnetPort, portOptions, "--",
		MayExist, "lrp-add", c.ClusterRouter, lrTols, mac, ip, "--",
		MayExist, "lsp-add", name, lsTolr, "--",
		"lsp-set-type", lsTolr, "router", "--",
		"lsp-set-addresses", lsTolr, "router", "--",
		"lsp-set-options", lsTolr, fmt.Sprintf("router-port=%s", lrTols),
	)
	if err != nil {
		return fmt.Errorf("failed to create external gateway switch, %v", err)
	}

	if externalgatewayvlanid > 0 {
		portVlanId := fmt.Sprintf("tag=%d", externalgatewayvlanid)
		_, err := c.ovnNbCommand("set", "logical_switch_port", localnetPort, portVlanId)
		if err != nil {
			return fmt.Errorf("failed to set vlanId for ,%s, %v", localnetPort, err)
		}
	}

	for index, chassis := range chassises {
		if _, err := c.ovnNbCommand("lrp-set-gateway-chassis", lrTols, chassis, fmt.Sprintf("%d", 100-index)); err != nil {
			return fmt.Errorf("failed to set gateway chassis, %v", err)
		}
	}
	return nil
}

func (c LegacyClient) DeleteGatewaySwitch(name string) error {
	lrTols := fmt.Sprintf("%s-%s", c.ClusterRouter, name)
	_, err := c.ovnNbCommand(
		IfExists, "ls-del", name, "--",
		IfExists, "lrp-del", lrTols,
	)
	return err
}

// ListLogicalSwitch list logical switch names
func (c LegacyClient) ListLogicalSwitch(needVendorFilter bool, args ...string) ([]string, error) {
	if needVendorFilter {
		args = append(args, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	}
	return c.ListLogicalEntity("logical_switch", args...)
}

func (c LegacyClient) ListLogicalEntity(entity string, args ...string) ([]string, error) {
	cmd := []string{"--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", entity}
	cmd = append(cmd, args...)
	output, err := c.ovnNbCommand(cmd...)
	if err != nil {
		klog.Errorf("failed to list logical %s: %v", entity, err)
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

func (c LegacyClient) CustomFindEntity(entity string, attris []string, args ...string) (result []map[string][]string, err error) {
	result = []map[string][]string{}
	var attrStr strings.Builder
	for _, e := range attris {
		attrStr.WriteString(e)
		attrStr.WriteString(",")
	}
	// Assuming that the order of the elements in attris does not change
	cmd := []string{"--format=csv", "--data=bare", "--no-heading", fmt.Sprintf("--columns=%s", attrStr.String()), "find", entity}
	cmd = append(cmd, args...)
	output, err := c.ovnNbCommand(cmd...)
	if err != nil {
		klog.Errorf("failed to customized list logical %s: %v", entity, err)
		return nil, err
	}
	if output == "" {
		return result, nil
	}
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		aResult := make(map[string][]string)
		parts := strings.Split(strings.TrimSpace(l), ",")
		for i, e := range attris {
			if aResult[e] = strings.Fields(parts[i]); aResult[e] == nil {
				aResult[e] = []string{}
			}
		}
		result = append(result, aResult)
	}
	return result, nil
}

func (c LegacyClient) GetEntityInfo(entity string, index string, attris []string) (result map[string]string, err error) {
	var attrstr strings.Builder
	for _, e := range attris {
		attrstr.WriteString(e)
		attrstr.WriteString(" ")
	}
	cmd := []string{"get", entity, index, strings.TrimSpace(attrstr.String())}
	output, err := c.ovnNbCommand(cmd...)
	if err != nil {
		klog.Errorf("failed to get attributes from %s %s: %v", entity, index, err)
		return nil, err
	}
	result = make(map[string]string)
	if output == "" {
		return result, nil
	}
	lines := strings.Split(output, "\n")
	if len(lines) != len(attris) {
		klog.Errorf("failed to get attributes from %s %s %s", entity, index, attris)
		return nil, errors.New("length abnormal")
	}
	for i, l := range lines {
		result[attris[i]] = l
	}
	return result, nil
}

func (c LegacyClient) LogicalSwitchExists(logicalSwitch string, needVendorFilter bool, args ...string) (bool, error) {
	lss, err := c.ListLogicalSwitch(needVendorFilter, args...)
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

func (c LegacyClient) ListLogicalSwitchPort(needVendorFilter bool) ([]string, error) {
	cmdArg := []string{"--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", "logical_switch_port", "type=\"\""}
	if needVendorFilter {
		cmdArg = append(cmdArg, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	}
	output, err := c.ovnNbCommand(cmdArg...)
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

func (c LegacyClient) LogicalSwitchPortExists(port string) (bool, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=name", "find", "logical_switch_port", fmt.Sprintf("name=%s", port))
	if err != nil {
		klog.Errorf("failed to find port %s: %v, %q", port, err, output)
		return false, err
	}

	if output != "" {
		return true, nil
	}
	return false, nil
}

func (c LegacyClient) ListRemoteLogicalSwitchPortAddress() ([]string, error) {
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
		fields := strings.Fields(l)
		if len(fields) != 2 {
			continue
		}
		cidr := fields[1]
		result = append(result, strings.TrimSpace(cidr))
	}
	return result, nil
}

// ListLogicalRouter list logical router names
func (c LegacyClient) ListLogicalRouter(needVendorFilter bool, args ...string) ([]string, error) {
	if needVendorFilter {
		args = append(args, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	}
	return c.ListLogicalEntity("logical_router", args...)
}

// DeleteLogicalSwitch delete logical switch
func (c LegacyClient) DeleteLogicalSwitch(ls string) error {
	if _, err := c.ovnNbCommand(IfExists, "ls-del", ls); err != nil {
		klog.Errorf("failed to del ls %s, %v", ls, err)
		return err
	}
	return nil
}

// CreateLogicalRouter delete logical router in ovn
func (c LegacyClient) CreateLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(MayExist, "lr-add", lr, "--",
		"set", "Logical_Router", lr, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	return err
}

// DeleteLogicalRouter create logical router in ovn
func (c LegacyClient) DeleteLogicalRouter(lr string) error {
	_, err := c.ovnNbCommand(IfExists, "lr-del", lr)
	return err
}

func (c LegacyClient) RemoveRouterPort(ls, lr string) error {
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

func (c LegacyClient) createRouterPort(ls, lr string) error {
	klog.Infof("add %s to %s", ls, lr)
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)
	_, err := c.ovnNbCommand(MayExist, "lsp-add", ls, lsTolr, "--",
		"set", "logical_switch_port", lsTolr, "type=router", "--",
		"lsp-set-addresses", lsTolr, "router", "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("options:router-port=%s", lrTols), "--",
		"set", "logical_switch_port", lsTolr, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	if err != nil {
		klog.Errorf("failed to create switch router port %s: %v", lsTolr, err)
		return err
	}
	return nil
}

func (c LegacyClient) CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error {
	localRouterPort := fmt.Sprintf("%s-%s", localRouter, remoteRouter)
	remoteRouterPort := fmt.Sprintf("%s-%s", remoteRouter, localRouter)

	// check router port exist, because '--may-exist' may not work for router port
	results, err := c.ListLogicalEntity("logical_router_port", fmt.Sprintf("name=%s", localRouterPort))
	if err != nil {
		klog.Errorf("failed to list router port %s, %v", localRouterPort, err)
		return err
	}
	if len(results) == 0 {
		ipStr := strings.Split(localRouterPortIP, ",")
		if len(ipStr) == 2 {
			_, err = c.ovnNbCommand(MayExist, "lrp-add", localRouter, localRouterPort, util.GenerateMac(), ipStr[0], ipStr[1], "--",
				"set", "logical_router_port", localRouterPort, fmt.Sprintf("peer=%s", remoteRouterPort))
		} else {
			_, err = c.ovnNbCommand(MayExist, "lrp-add", localRouter, localRouterPort, util.GenerateMac(), ipStr[0], "--",
				"set", "logical_router_port", localRouterPort, fmt.Sprintf("peer=%s", remoteRouterPort))
		}
		if err != nil {
			klog.Errorf("failed to create router port %s: %v", localRouterPort, err)
			return err
		}
	}

	_, err = c.ovnNbCommand("set", "logical_router_port", localRouterPort,
		fmt.Sprintf("networks=\"%s\"", strings.ReplaceAll(localRouterPortIP, ",", " ")))

	if err != nil {
		klog.Errorf("failed to set router port %s: %v", localRouterPort, err)
		return err
	}
	return nil
}

type StaticRoute struct {
	Policy  string
	CIDR    string
	NextHop string
}

func (c LegacyClient) ListStaticRoute() ([]StaticRoute, error) {
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
func (c LegacyClient) AddStaticRoute(policy, cidr, nextHop, router string, routeType string, isMigrate bool) error {
	if policy == "" {
		policy = PolicyDstIP
	}

	var existingRoutes []string
	if routeType != util.EcmpRouteType {
		result, err := c.CustomFindEntity("Logical_Router", []string{"static_routes"}, fmt.Sprintf("name=%s", router))
		if err != nil {
			return err
		}
		if len(result) > 1 {
			return fmt.Errorf("unexpected error: found %d logical router with name %s", len(result), router)
		}
		if len(result) != 0 {
			existingRoutes = result[0]["static_routes"]
		}
	}

	for _, cidrBlock := range strings.Split(cidr, ",") {
		for _, gw := range strings.Split(nextHop, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(gw) {
				continue
			}
			if routeType == util.EcmpRouteType {
				if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "--ecmp", "lr-route-add", router, cidrBlock, gw); err != nil {
					return err
				}
			} else {
				if !strings.ContainsRune(cidrBlock, '/') {
					filter := []string{fmt.Sprintf("policy=%s", policy), fmt.Sprintf(`ip_prefix="%s"`, cidrBlock), fmt.Sprintf(`nexthop!="%s"`, gw)}
					result, err := c.CustomFindEntity("Logical_Router_Static_Route", []string{"_uuid"}, filter...)
					if err != nil {
						return err
					}

					for _, route := range result {
						if util.ContainsString(existingRoutes, route["_uuid"][0]) && !isMigrate {
							return fmt.Errorf(`static route "policy=%s ip_prefix=%s" with different nexthop already exists on logical router %s`, policy, cidrBlock, router)
						}
					}
				}

				if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "lr-route-add", router, cidrBlock, gw); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// AddPolicyRoute add a policy route rule in ovn
func (c LegacyClient) AddPolicyRoute(router string, priority int32, match, action, nextHop string, externalIDs map[string]string) error {
	consistent, err := c.CheckPolicyRouteNexthopConsistent(router, match, nextHop, priority)
	if err != nil {
		return err
	}
	if !consistent {
		if err := c.DeletePolicyRoute(router, priority, match); err != nil {
			klog.Errorf("failed to delete policy route: %v", err)
			return err
		}
	}

	// lr-policy-add ROUTER PRIORITY MATCH ACTION [NEXTHOP]
	args := []string{MayExist, "lr-policy-add", router, strconv.Itoa(int(priority)), match, action}
	if nextHop != "" {
		args = append(args, nextHop)
	}
	if _, err := c.ovnNbCommand(args...); err != nil {
		return err
	}

	if len(externalIDs) == 0 {
		return nil
	}

	result, err := c.CustomFindEntity("logical_router_policy", []string{"_uuid"}, fmt.Sprintf("priority=%d", priority), fmt.Sprintf(`match="%s"`, match))
	if err != nil {
		klog.Errorf("failed to get logical router policy UUID: %v", err)
		return err
	}
	for _, policy := range result {
		args := make([]string, 0, len(externalIDs)+3)
		args = append(args, "set", "logical_router_policy", policy["_uuid"][0])
		for k, v := range externalIDs {
			args = append(args, fmt.Sprintf("external-ids:%s=%v", k, v))
		}
		if _, err = c.ovnNbCommand(args...); err != nil {
			return fmt.Errorf("failed to set external ids of logical router policy %s: %v", policy["_uuid"][0], err)
		}
	}

	return nil
}

// DeletePolicyRoute delete a policy route rule in ovn
func (c LegacyClient) DeletePolicyRoute(router string, priority int32, match string) error {
	exist, err := c.IsPolicyRouteExist(router, priority, match)
	if err != nil {
		return err
	}
	if !exist {
		return nil
	}
	var args = []string{"lr-policy-del", router}
	// lr-policy-del ROUTER [PRIORITY [MATCH]]
	if priority > 0 {
		args = append(args, strconv.Itoa(int(priority)))
		if match != "" {
			args = append(args, match)
		}
	}
	_, err = c.ovnNbCommand(args...)
	return err
}

func (c LegacyClient) IsPolicyRouteExist(router string, priority int32, match string) (bool, error) {
	existPolicyRoute, err := c.GetPolicyRouteList(router)
	if err != nil {
		return false, err
	}
	for _, rule := range existPolicyRoute {
		if rule.Priority != priority {
			continue
		}
		if match == "" || rule.Match == match {
			return true, nil
		}
	}
	return false, nil
}

func (c LegacyClient) DeletePolicyRouteByNexthop(router string, priority int32, nexthop string) error {
	args := []string{
		"--no-heading", "--data=bare", "--columns=match", "find", "Logical_Router_Policy",
		fmt.Sprintf("priority=%d", priority),
		fmt.Sprintf(`nexthops{=}%s`, strings.ReplaceAll(nexthop, ":", `\:`)),
	}
	output, err := c.ovnNbCommand(args...)
	if err != nil {
		klog.Errorf("failed to list router policy by nexthop %s: %v", nexthop, err)
		return err
	}
	if output == "" {
		return nil
	}

	return c.DeletePolicyRoute(router, priority, output)
}

type PolicyRoute struct {
	Priority  int32
	Match     string
	Action    string
	NextHopIP string
}

func (c LegacyClient) GetPolicyRouteList(router string) (routeList []*PolicyRoute, err error) {
	output, err := c.ovnNbCommand("lr-policy-list", router)
	if err != nil {
		klog.Errorf("failed to list logical router policy route: %v", err)
		return nil, err
	}
	return parseLrPolicyRouteListOutput(output)
}

var policyRouteRegexp = regexp.MustCompile(`^\s*(\d+)\s+(.*)\b\s+(allow|drop|reroute)\s*(.*)?$`)

func parseLrPolicyRouteListOutput(output string) (routeList []*PolicyRoute, err error) {
	lines := strings.Split(output, "\n")
	routeList = make([]*PolicyRoute, 0, len(lines))
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		sm := policyRouteRegexp.FindStringSubmatch(l)
		if len(sm) != 5 {
			continue
		}
		priority, err := strconv.ParseInt(sm[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("found unexpeted policy priority %s, please check", sm[1])
		}
		routeList = append(routeList, &PolicyRoute{
			Priority:  int32(priority),
			Match:     sm[2],
			Action:    sm[3],
			NextHopIP: sm[4],
		})
	}
	return routeList, nil
}

func (c LegacyClient) GetStaticRouteList(router string) (routeList []*StaticRoute, err error) {
	output, err := c.ovnNbCommand("lr-route-list", router)
	if err != nil {
		klog.Errorf("failed to list logical router route: %v", err)
		return nil, err
	}
	return parseLrRouteListOutput(output)
}

var routeRegexp = regexp.MustCompile(`^\s*((\d+(\.\d+){3})|(([a-f0-9:]*:+)+[a-f0-9]*?))(/\d+)?\s+((\d+(\.\d+){3})|(([a-f0-9:]*:+)+[a-f0-9]*?))\s+(dst-ip|src-ip)(\s+.+)?$`)

func parseLrRouteListOutput(output string) (routeList []*StaticRoute, err error) {
	lines := strings.Split(output, "\n")
	routeList = make([]*StaticRoute, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(l, "learned") {
			continue
		}

		if len(l) == 0 {
			continue
		}

		if !routeRegexp.MatchString(l) {
			continue
		}

		fields := strings.Fields(l)
		routeList = append(routeList, &StaticRoute{
			Policy:  fields[2],
			CIDR:    fields[0],
			NextHop: fields[1],
		})
	}
	return routeList, nil
}

func (c LegacyClient) UpdateNatRule(policy, logicalIP, externalIP, router, logicalMac, port string) error {
	if policy == "snat" {
		if externalIP == "" {
			_, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP)
			return err
		}
		_, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP, "--",
			"lr-nat-add", router, policy, externalIP, logicalIP)
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
			} else {
				_, err = c.ovnNbCommand(MayExist, "lr-nat-add", router, policy, externalIP, logicalIP)
			}
			return err
		}
	}
	return nil
}

func (c LegacyClient) DeleteNatRule(logicalIP, router string) error {
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

func (c *LegacyClient) NatRuleExists(logicalIP string) (bool, error) {
	results, err := c.CustomFindEntity("NAT", []string{"external_ip"}, fmt.Sprintf("logical_ip=%s", logicalIP))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return false, err
	}
	if len(results) == 0 {
		return false, nil
	}
	return true, nil
}

func (c LegacyClient) DeleteMatchedStaticRoute(cidr, nexthop, router string) error {
	if cidr == "" || nexthop == "" {
		return nil
	}
	_, err := c.ovnNbCommand(IfExists, "lr-route-del", router, cidr, nexthop)
	return err
}

// DeleteStaticRoute delete a static route rule in ovn
func (c LegacyClient) DeleteStaticRoute(cidr, router string) error {
	if cidr == "" {
		return nil
	}
	_, err := c.ovnNbCommand(IfExists, "lr-route-del", router, cidr)
	return err
}

func (c LegacyClient) DeleteStaticRouteByNextHop(nextHop string) error {
	if strings.TrimSpace(nextHop) == "" {
		return nil
	}
	if util.CheckProtocol(nextHop) == kubeovnv1.ProtocolIPv6 {
		nextHop = strings.ReplaceAll(nextHop, ":", "\\:")
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
func (c LegacyClient) FindLoadbalancer(lb string) (string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
		"find", "load_balancer", fmt.Sprintf("name=%s", lb))
	count := len(strings.FieldsFunc(output, func(c rune) bool { return c == '\n' }))
	if count > 1 {
		klog.Errorf("%s has %d lb entries", lb, count)
		return "", fmt.Errorf("%s has %d lb entries", lb, count)
	}
	return output, err
}

// CreateLoadBalancer create loadbalancer in ovn
func (c LegacyClient) CreateLoadBalancer(lb, protocol, selectFields string) error {
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
func (c LegacyClient) CreateLoadBalancerRule(lb, vip, ips, protocol string) error {
	_, err := c.ovnNbCommand(MayExist, "lb-add", lb, vip, ips, strings.ToLower(protocol))
	return err
}

func (c LegacyClient) addLoadBalancerToLogicalSwitch(lb, ls string) error {
	_, err := c.ovnNbCommand(MayExist, "ls-lb-add", ls, lb)
	return err
}

func (c LegacyClient) removeLoadBalancerFromLogicalSwitch(lb, ls string) error {
	if lb == "" {
		return nil
	}
	lbUuid, err := c.FindLoadbalancer(lb)
	if err != nil {
		return err
	}
	if lbUuid == "" {
		return nil
	}

	_, err = c.ovnNbCommand(IfExists, "ls-lb-del", ls, lb)
	return err
}

// DeleteLoadBalancerVip delete a vip rule from loadbalancer
func (c LegacyClient) DeleteLoadBalancerVip(vip, lb string) error {
	lbUuid, err := c.FindLoadbalancer(lb)
	if err != nil {
		klog.Errorf("failed to get lb: %v", err)
		return err
	}

	existVips, err := c.GetLoadBalancerVips(lbUuid)
	if err != nil {
		klog.Errorf("failed to list lb %s vips: %v", lb, err)
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
func (c LegacyClient) GetLoadBalancerVips(lb string) (map[string]string, error) {
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
func (c LegacyClient) CleanLogicalSwitchAcl(ls string) error {
	_, err := c.ovnNbCommand("acl-del", ls)
	return err
}

// ResetLogicalSwitchAcl reset acl of a switch
func (c LegacyClient) ResetLogicalSwitchAcl(ls string) error {
	_, err := c.ovnNbCommand("acl-del", ls)
	return err
}

// SetPrivateLogicalSwitch will drop all ingress traffic except allow subnets
func (c LegacyClient) SetPrivateLogicalSwitch(ls, cidr string, allow []string) error {
	ovnArgs := []string{"acl-del", ls}
	dropArgs := []string{"--", "--log", fmt.Sprintf("--name=%s", ls), fmt.Sprintf("--severity=%s", "warning"), "acl-add", ls, "to-lport", util.DefaultDropPriority, "ip", "drop"}
	ovnArgs = append(ovnArgs, dropArgs...)

	for _, cidrBlock := range strings.Split(cidr, ",") {
		allowArgs := []string{}
		protocol := util.CheckProtocol(cidrBlock)
		if protocol == kubeovnv1.ProtocolIPv4 {
			allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.SubnetAllowPriority, fmt.Sprintf(`ip4.src==%s && ip4.dst==%s`, cidrBlock, cidrBlock), "allow-related")
		} else {
			allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.SubnetAllowPriority, fmt.Sprintf(`ip6.src==%s && ip6.dst==%s`, cidrBlock, cidrBlock), "allow-related")
		}

		for _, nodeCidrBlock := range strings.Split(c.NodeSwitchCIDR, ",") {
			if protocol != util.CheckProtocol(nodeCidrBlock) {
				continue
			}

			if protocol == kubeovnv1.ProtocolIPv4 {
				allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", nodeCidrBlock), "allow-related")
			} else {
				allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip6.src==%s", nodeCidrBlock), "allow-related")
			}
		}

		for _, subnet := range allow {
			if strings.TrimSpace(subnet) != "" {
				allowProtocol := util.CheckProtocol(strings.TrimSpace(subnet))
				if allowProtocol != protocol {
					continue
				}

				var match string
				switch protocol {
				case kubeovnv1.ProtocolIPv4:
					match = fmt.Sprintf("(ip4.src==%s && ip4.dst==%s) || (ip4.src==%s && ip4.dst==%s)", strings.TrimSpace(subnet), cidrBlock, cidrBlock, strings.TrimSpace(subnet))
				case kubeovnv1.ProtocolIPv6:
					match = fmt.Sprintf("(ip6.src==%s && ip6.dst==%s) || (ip6.src==%s && ip6.dst==%s)", strings.TrimSpace(subnet), cidrBlock, cidrBlock, strings.TrimSpace(subnet))
				}

				allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.SubnetAllowPriority, match, "allow-related")
			}
		}
		ovnArgs = append(ovnArgs, allowArgs...)
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c LegacyClient) GetLogicalSwitchPortAddress(port string) ([]string, error) {
	output, err := c.ovnNbCommand("get", "logical_switch_port", port, "addresses")
	if err != nil {
		klog.Errorf("get port %s addresses failed: %v", port, err)
		return nil, err
	}
	if strings.Contains(output, "dynamic") {
		// [dynamic]
		return nil, nil
	}
	output = strings.Trim(output, `[]"`)
	fields := strings.Fields(output)
	if len(fields) != 2 {
		return nil, nil
	}

	// currently user may only have one fixed address
	// ["0a:00:00:00:00:0c 10.16.0.13"]
	return fields, nil
}

func (c LegacyClient) GetLogicalSwitchPortDynamicAddress(port string) ([]string, error) {
	output, err := c.ovnNbCommand("wait-until", "logical_switch_port", port, "dynamic_addresses!=[]", "--",
		"get", "logical_switch_port", port, "dynamic-addresses")
	if err != nil {
		klog.Errorf("get port %s dynamic_addresses failed: %v", port, err)
		return nil, err
	}
	if output == "[]" {
		return nil, ErrNoAddr
	}
	output = strings.Trim(output, `"`)
	// "0a:00:00:00:00:02"
	fields := strings.Fields(output)
	if len(fields) != 2 {
		klog.Error("Subnet address space has been exhausted")
		return nil, ErrNoAddr
	}
	// "0a:00:00:00:00:02 100.64.0.3"
	return fields, nil
}

// GetPortAddr return port [mac, ip]
func (c LegacyClient) GetPortAddr(port string) ([]string, error) {
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

func (c LegacyClient) CreateNpPortGroup(pgName, npNs, npName string) error {
	output, err := c.ovnNbCommand(
		"--data=bare", "--no-heading", "--columns=_uuid", "find", "port_group", fmt.Sprintf("name=%s", pgName))
	if err != nil {
		klog.Errorf("failed to find port_group %s: %v, %q", pgName, err, output)
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

func (c LegacyClient) DeletePortGroup(pgName string) error {
	output, err := c.ovnNbCommand(
		"--data=bare", "--no-heading", "--columns=_uuid", "find", "port_group", fmt.Sprintf("name=%s", pgName))
	if err != nil {
		klog.Errorf("failed to find port_group %s: %v, %q", pgName, err, output)
		return err
	}
	if output == "" {
		return nil
	}

	_, err = c.ovnNbCommand("pg-del", pgName)
	return err
}

type portGroup struct {
	Name        string
	NpName      string
	NpNamespace string
}

func (c LegacyClient) ListNpPortGroup() ([]portGroup, error) {
	output, err := c.ovnNbCommand("--data=bare", "--format=csv", "--no-heading", "--columns=name,external_ids", "find", "port_group", "external_ids:np!=[]")
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

func (c LegacyClient) CreateAddressSet(name string) error {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", name))
	if err != nil {
		klog.Errorf("failed to find address_set %s: %v, %q", name, err, output)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand("create", "address_set", fmt.Sprintf("name=%s", name), fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	return err
}

func (c LegacyClient) CreateAddressSetWithAddresses(name string, addresses ...string) error {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", name))
	if err != nil {
		klog.Errorf("failed to find address_set %s: %v, %q", name, err, output)
		return err
	}

	var args []string
	argAddrs := strings.ReplaceAll(strings.Join(addresses, ","), ":", `\:`)
	if output == "" {
		args = []string{"create", "address_set", fmt.Sprintf("name=%s", name), fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)}
		if argAddrs != "" {
			args = append(args, fmt.Sprintf("addresses=%s", argAddrs))
		}
	} else {
		args = []string{"clear", "address_set", name, "addresses"}
		if argAddrs != "" {
			args = append(args, "--", "set", "address_set", name, "addresses", argAddrs)
		}
	}

	_, err = c.ovnNbCommand(args...)
	return err
}

func (c LegacyClient) AddAddressSetAddresses(name string, address string) error {
	output, err := c.ovnNbCommand("add", "address_set", name, "addresses", strings.ReplaceAll(address, ":", `\:`))
	if err != nil {
		klog.Errorf("failed to add address %s to address_set %s: %v, %q", address, name, err, output)
		return err
	}
	return nil
}

func (c LegacyClient) RemoveAddressSetAddresses(name string, address string) error {
	output, err := c.ovnNbCommand("remove", "address_set", name, "addresses", strings.ReplaceAll(address, ":", `\:`))
	if err != nil {
		klog.Errorf("failed to remove address %s from address_set %s: %v, %q", address, name, err, output)
		return err
	}
	return nil
}

func (c LegacyClient) DeleteAddressSet(name string) error {
	_, err := c.ovnNbCommand(IfExists, "destroy", "address_set", name)
	return err
}

func (c LegacyClient) ListNpAddressSet(npNamespace, npName, direction string) ([]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=name", "find", "address_set", fmt.Sprintf("external_ids:np=%s/%s/%s", npNamespace, npName, direction))
	if err != nil {
		klog.Errorf("failed to list address_set of %s/%s/%s: %v, %q", npNamespace, npName, direction, err, output)
		return nil, err
	}
	return strings.Split(output, "\n"), nil
}

func (c LegacyClient) ListAddressesByName(addressSetName string) ([]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=addresses", "find", "address_set", fmt.Sprintf("name=%s", addressSetName))
	if err != nil {
		klog.Errorf("failed to list address_set of %s, error %v", addressSetName, err)
		return nil, err
	}

	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		result = append(result, strings.Fields(l)...)
	}
	return result, nil
}

func (c LegacyClient) CreateNpAddressSet(asName, npNamespace, npName, direction string) error {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", asName))
	if err != nil {
		klog.Errorf("failed to find address_set %s: %v, %q", asName, err, output)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand("create", "address_set", fmt.Sprintf("name=%s", asName), fmt.Sprintf("external_ids:np=%s/%s/%s", npNamespace, npName, direction))
	return err
}

func (c LegacyClient) CombineIngressACLCmd(pgName, asIngressName, asExceptName, svcAsName, protocol string, npp []netv1.NetworkPolicyPort, logEnable bool, aclCmds []string, index int) []string {
	var allowArgs, ovnArgs []string

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	id := pgName + "_" + ipSuffix

	if logEnable {
		ovnArgs = []string{"--", fmt.Sprintf("--id=@%s.drop.%d", id, index), "create", "acl", "action=drop", "direction=to-lport", "log=true", "severity=warning", fmt.Sprintf("priority=%s", util.IngressDefaultDrop), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("outport==@%s && ip", pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.drop.%d", id, index)}
	} else {
		ovnArgs = []string{"--", fmt.Sprintf("--id=@%s.drop.%d", id, index), "create", "acl", "action=drop", "direction=to-lport", "log=false", fmt.Sprintf("priority=%s", util.IngressDefaultDrop), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("outport==@%s && ip", pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.drop.%d", id, index)}
	}

	if len(npp) == 0 {
		allowArgs = []string{"--", fmt.Sprintf("--id=@%s.noport.%d", id, index), "create", "acl", "action=allow-related", "direction=to-lport", fmt.Sprintf("priority=%s", util.IngressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.src == $%s && %s.src != $%s && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.noport.%d", id, index)}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for pidx, port := range npp {
			if port.Port != nil {
				if port.EndPort != nil {
					allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=to-lport", fmt.Sprintf("priority=%s", util.IngressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.src == $%s && %s.src != $%s && %d <= %s.dst <= %d && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, port.Port.IntVal, strings.ToLower(string(*port.Protocol)), *port.EndPort, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
				} else {
					allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=to-lport", fmt.Sprintf("priority=%s", util.IngressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s.dst == %d && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
				}
			} else {
				allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=to-lport", fmt.Sprintf("priority=%s", util.IngressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
			}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	aclCmds = append(aclCmds, ovnArgs...)
	return aclCmds
}

func (c LegacyClient) CreateACL(aclCmds []string) error {
	_, err := c.ovnNbCommand(aclCmds...)
	return err
}

func (c LegacyClient) CombineEgressACLCmd(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort, portSvcName string, logEnable bool, aclCmds []string, index int) []string {
	var allowArgs, ovnArgs []string

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	id := pgName + "_" + ipSuffix

	if logEnable {
		ovnArgs = []string{"--", fmt.Sprintf("--id=@%s.drop.%d", id, index), "create", "acl", "action=drop", "direction=from-lport", "log=true", "severity=warning", fmt.Sprintf("priority=%s", util.EgressDefaultDrop), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("inport==@%s && ip", pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.drop.%d", id, index)}
	} else {
		ovnArgs = []string{"--", fmt.Sprintf("--id=@%s.drop.%d", id, index), "create", "acl", "action=drop", "direction=from-lport", "log=false", fmt.Sprintf("priority=%s", util.EgressDefaultDrop), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("inport==@%s && ip", pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.drop.%d", id, index)}
	}

	if len(npp) == 0 {
		allowArgs = []string{"--", fmt.Sprintf("--id=@%s.noport.%d", id, index), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.noport.%d", id, index)}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for pidx, port := range npp {
			if port.Port != nil {
				if port.EndPort != nil {
					allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %d <= %s.dst <= %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, port.Port.IntVal, strings.ToLower(string(*port.Protocol)), *port.EndPort, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
				} else {
					allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s.dst == %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
				}
			} else {
				allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
			}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	aclCmds = append(aclCmds, ovnArgs...)
	return aclCmds
}

func (c LegacyClient) DeleteACL(pgName, direction string) (err error) {
	if _, err := c.ovnNbCommand("get", "port_group", pgName, "_uuid"); err != nil {
		if strings.Contains(err.Error(), "no row") {
			return nil
		}
		klog.Errorf("failed to get pg %s, %v", pgName, err)
		return err
	}

	if direction != "" {
		_, err = c.ovnNbCommand("--type=port-group", "acl-del", pgName, direction)
	} else {
		_, err = c.ovnNbCommand("--type=port-group", "acl-del", pgName)
	}
	return
}

func (c LegacyClient) CreateGatewayACL(pgName, gateway, cidr string) error {
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
			ingressArgs := []string{MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == %s", ipSuffix, gw), "allow-related"}
			egressArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s", ipSuffix, gw), "allow-related"}
			ovnArgs := append(ingressArgs, egressArgs...)
			if _, err := c.ovnNbCommand(ovnArgs...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c LegacyClient) CreateACLForNodePg(pgName, nodeIpStr string) error {
	for _, nodeIp := range strings.Split(nodeIpStr, ",") {
		protocol := util.CheckProtocol(nodeIp)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		ingressArgs := []string{MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, nodeIp, ipSuffix, pgAs), "allow-related"}
		egressArgs := []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, nodeIp, ipSuffix, pgAs), "allow-related"}
		ovnArgs := append(ingressArgs, egressArgs...)
		if _, err := c.ovnNbCommand(ovnArgs...); err != nil {
			klog.Errorf("failed to add node port-group acl: %v", err)
			return err
		}
	}

	return nil
}

func (c LegacyClient) DeleteAclForNodePg(pgName string) error {
	ingressArgs := []string{"acl-del", pgName, "to-lport"}
	if _, err := c.ovnNbCommand(ingressArgs...); err != nil {
		klog.Errorf("failed to delete node port-group ingress acl: %v", err)
		return err
	}

	egressArgs := []string{"acl-del", pgName, "from-lport"}
	if _, err := c.ovnNbCommand(egressArgs...); err != nil {
		klog.Errorf("failed to delete node port-group egress acl: %v", err)
		return err
	}

	return nil
}

func (c LegacyClient) ListPgPorts(pgName string) ([]string, error) {
	output, err := c.ovnNbCommand("--format=csv", "--data=bare", "--no-heading", "--columns=ports", "find", "port_group", fmt.Sprintf("name=%s", pgName))
	if err != nil {
		klog.Errorf("failed to list port-group ports, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		result = append(result, strings.Fields(l)...)
	}
	return result, nil
}

func (c LegacyClient) ListLspForNodePortgroup() (map[string]string, map[string]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--format=csv", "--no-heading", "--columns=name,_uuid", "list", "logical_switch_port")
	if err != nil {
		klog.Errorf("failed to list logical-switch-port, %v", err)
		return nil, nil, err
	}
	lines := strings.Split(output, "\n")
	nameIdMap := make(map[string]string, len(lines))
	idNameMap := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		uuid := strings.TrimSpace(parts[1])
		nameIdMap[name] = uuid
		idNameMap[uuid] = name
	}
	return nameIdMap, idNameMap, nil
}

func (c LegacyClient) ListPgPortsForNodePortgroup() (map[string][]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--format=csv", "--no-heading", "--columns=name,ports", "list", "port_group")
	if err != nil {
		klog.Errorf("failed to list port_group, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	namePortsMap := make(map[string][]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		ports := strings.Fields(parts[1])
		namePortsMap[name] = ports
	}

	return namePortsMap, nil
}

func (c LegacyClient) SetPortsToPortGroup(portGroup string, portNames []string) error {
	ovnArgs := []string{"clear", "port_group", portGroup, "ports"}
	if len(portNames) > 0 {
		ovnArgs = []string{"pg-set-ports", portGroup}
		ovnArgs = append(ovnArgs, portNames...)
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c LegacyClient) SetAddressesToAddressSet(addresses []string, as string) error {
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

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("ovn-nbctl", command...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err = cmd.Run(); err != nil {
		klog.Errorf("failed to start ovn-nbctl daemon: %v, %s, %s", err, stdout.String(), stderr.String())
		return err
	}

	daemonSocket := strings.TrimSpace(stdout.String())
	if !nbctlDaemonSocketRegexp.MatchString(daemonSocket) {
		err = fmt.Errorf("invalid nbctl daemon socket: %q", daemonSocket)
		klog.Error(err)
		return err
	}

	_ = os.Unsetenv("OVN_NB_DAEMON")
	if err := os.Setenv("OVN_NB_DAEMON", daemonSocket); err != nil {
		klog.Errorf("failed to set env OVN_NB_DAEMON, %v", err)
		return err
	}
	return nil
}

// CheckAlive check if kube-ovn-controller can access ovn-nb from nbctl-daemon
func CheckAlive() error {
	var stderr bytes.Buffer
	cmd := exec.Command("ovn-nbctl", "--timeout=60", "show")
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		klog.Errorf("failed to access ovn-nb from daemon: %v, %s", err, stderr.String())
		return err
	}
	return nil
}

// GetLogicalSwitchExcludeIPS get a logical switch exclude ips
// ovn-nbctl get logical_switch ovn-default other_config:exclude_ips => "10.17.0.1 10.17.0.2 10.17.0.3..10.17.0.5"
func (c LegacyClient) GetLogicalSwitchExcludeIPS(logicalSwitch string) ([]string, error) {
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
func (c LegacyClient) SetLogicalSwitchExcludeIPS(logicalSwitch string, excludeIPS []string) error {
	_, err := c.ovnNbCommand("set", "logical_switch", logicalSwitch,
		fmt.Sprintf(`other_config:exclude_ips="%s"`, strings.Join(excludeIPS, " ")))
	return err
}

func (c LegacyClient) GetLogicalSwitchPortByLogicalSwitch(logicalSwitch string) ([]string, error) {
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

func (c LegacyClient) CreateLocalnetPort(ls, port, provider string, vlanID int) error {
	cmdArg := []string{
		MayExist, "lsp-add", ls, port, "--",
		"lsp-set-addresses", port, "unknown", "--",
		"lsp-set-type", port, "localnet", "--",
		"lsp-set-options", port, fmt.Sprintf("network_name=%s", provider), "--",
		"set", "logical_switch_port", port, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName),
	}
	if vlanID > 0 && vlanID < 4096 {
		cmdArg = append(cmdArg,
			"--", "set", "logical_switch_port", port, fmt.Sprintf("tag=%d", vlanID))
	}

	if _, err := c.ovnNbCommand(cmdArg...); err != nil {
		klog.Errorf("create localnet port %s failed, %v", port, err)
		return err
	}

	return nil
}

func GetSgPortGroupName(sgName string) string {
	return strings.Replace(fmt.Sprintf("ovn.sg.%s", sgName), "-", ".", -1)
}

func GetSgV4AssociatedName(sgName string) string {
	return strings.Replace(fmt.Sprintf("ovn.sg.%s.associated.v4", sgName), "-", ".", -1)
}

func GetSgV6AssociatedName(sgName string) string {
	return strings.Replace(fmt.Sprintf("ovn.sg.%s.associated.v6", sgName), "-", ".", -1)
}

func (c LegacyClient) CreateSgPortGroup(sgName string) error {
	sgPortGroupName := GetSgPortGroupName(sgName)
	output, err := c.ovnNbCommand(
		"--data=bare", "--no-heading", "--columns=_uuid", "find", "port_group", fmt.Sprintf("name=%s", sgPortGroupName))
	if err != nil {
		klog.Errorf("failed to find port_group of sg %s: %v", sgPortGroupName, err)
		return err
	}
	if output != "" {
		return nil
	}
	_, err = c.ovnNbCommand(
		"pg-add", sgPortGroupName,
		"--", "set", "port_group", sgPortGroupName, "external_ids:type=security_group",
		fmt.Sprintf("external_ids:sg=%s", sgName),
		fmt.Sprintf("external_ids:name=%s", sgPortGroupName))
	return err
}

func (c LegacyClient) DeleteSgPortGroup(sgName string) error {
	sgPortGroupName := GetSgPortGroupName(sgName)
	// delete acl
	if err := c.DeleteACL(sgPortGroupName, ""); err != nil {
		return err
	}

	// delete address_set
	asList, err := c.ListSgRuleAddressSet(sgName, "")
	if err != nil {
		return err
	}
	for _, as := range asList {
		if err = c.DeleteAddressSet(as); err != nil {
			return err
		}
	}

	// delete pg
	err = c.DeletePortGroup(sgPortGroupName)
	if err != nil {
		return err
	}
	return nil
}

func (c LegacyClient) CreateSgAssociatedAddressSet(sgName string) error {
	v4AsName := GetSgV4AssociatedName(sgName)
	v6AsName := GetSgV6AssociatedName(sgName)
	outputV4, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", v4AsName))
	if err != nil {
		klog.Errorf("failed to find address_set for sg %s: %v", sgName, err)
		return err
	}
	outputV6, err := c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid", "find", "address_set", fmt.Sprintf("name=%s", v6AsName))
	if err != nil {
		klog.Errorf("failed to find address_set for sg %s: %v", sgName, err)
		return err
	}

	if outputV4 == "" {
		_, err = c.ovnNbCommand("create", "address_set", fmt.Sprintf("name=%s", v4AsName), fmt.Sprintf("external_ids:sg=%s", sgName))
		if err != nil {
			klog.Errorf("failed to create v4 address_set for sg %s: %v", sgName, err)
			return err
		}
	}
	if outputV6 == "" {
		_, err = c.ovnNbCommand("create", "address_set", fmt.Sprintf("name=%s", v6AsName), fmt.Sprintf("external_ids:sg=%s", sgName))
		if err != nil {
			klog.Errorf("failed to create v6 address_set for sg %s: %v", sgName, err)
			return err
		}
	}
	return nil
}

func (c LegacyClient) ListSgRuleAddressSet(sgName string, direction AclDirection) ([]string, error) {
	ovnCmd := []string{"--data=bare", "--no-heading", "--columns=name", "find", "address_set", fmt.Sprintf("external_ids:sg=%s", sgName)}
	if direction != "" {
		ovnCmd = append(ovnCmd, fmt.Sprintf("external_ids:direction=%s", direction))
	}
	output, err := c.ovnNbCommand(ovnCmd...)
	if err != nil {
		klog.Errorf("failed to list sg address_set of %s, direction %s: %v, %q", sgName, direction, err, output)
		return nil, err
	}
	return strings.Split(output, "\n"), nil
}

func (c LegacyClient) createSgRuleACL(sgName string, direction AclDirection, rule *kubeovnv1.SgRule, index int) error {
	ipSuffix := "ip4"
	if rule.IPVersion == "ipv6" {
		ipSuffix = "ip6"
	}

	sgPortGroupName := GetSgPortGroupName(sgName)
	var matchArgs []string
	if rule.RemoteType == kubeovnv1.SgRemoteTypeAddress {
		if direction == SgAclIngressDirection {
			matchArgs = append(matchArgs, fmt.Sprintf("outport==@%s && %s && %s.src==%s", sgPortGroupName, ipSuffix, ipSuffix, rule.RemoteAddress))
		} else {
			matchArgs = append(matchArgs, fmt.Sprintf("inport==@%s && %s && %s.dst==%s", sgPortGroupName, ipSuffix, ipSuffix, rule.RemoteAddress))
		}
	} else {
		if direction == SgAclIngressDirection {
			matchArgs = append(matchArgs, fmt.Sprintf("outport==@%s && %s && %s.src==$%s", sgPortGroupName, ipSuffix, ipSuffix, GetSgV4AssociatedName(rule.RemoteSecurityGroup)))
		} else {
			matchArgs = append(matchArgs, fmt.Sprintf("inport==@%s && %s && %s.dst==$%s", sgPortGroupName, ipSuffix, ipSuffix, GetSgV4AssociatedName(rule.RemoteSecurityGroup)))
		}
	}

	if rule.Protocol == kubeovnv1.ProtocolICMP {
		if ipSuffix == "ip4" {
			matchArgs = append(matchArgs, "icmp4")
		} else {
			matchArgs = append(matchArgs, "icmp6")
		}
	} else if rule.Protocol == kubeovnv1.ProtocolTCP || rule.Protocol == kubeovnv1.ProtocolUDP {
		matchArgs = append(matchArgs, fmt.Sprintf("%d<=%s.dst<=%d", rule.PortRangeMin, rule.Protocol, rule.PortRangeMax))
	}

	matchStr := strings.Join(matchArgs, " && ")
	action := "drop"
	if rule.Policy == kubeovnv1.PolicyAllow {
		action = "allow-related"
	}
	highestPriority, err := strconv.Atoi(util.SecurityGroupHighestPriority)
	if err != nil {
		return err
	}
	_, err = c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", sgPortGroupName, string(direction), strconv.Itoa(highestPriority-rule.Priority), matchStr, action)
	return err
}

func (c LegacyClient) CreateSgDenyAllACL() error {
	portGroupName := GetSgPortGroupName(util.DenyAllSecurityGroup)
	exist, err := c.AclExists(util.SecurityGroupDropPriority, string(SgAclIngressDirection))
	if err != nil {
		return err
	}
	if !exist {
		if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclIngressDirection), util.SecurityGroupDropPriority,
			fmt.Sprintf("outport==@%s && ip", portGroupName), "drop"); err != nil {
			return err
		}
	}
	exist, err = c.AclExists(util.SecurityGroupDropPriority, string(SgAclEgressDirection))
	if err != nil {
		return err
	}
	if !exist {
		if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclEgressDirection), util.SecurityGroupDropPriority,
			fmt.Sprintf("inport==@%s && ip", portGroupName), "drop"); err != nil {
			return err
		}
	}
	return nil
}

func (c LegacyClient) UpdateSgACL(sg *kubeovnv1.SecurityGroup, direction AclDirection) error {
	sgPortGroupName := GetSgPortGroupName(sg.Name)
	// clear acl
	if err := c.DeleteACL(sgPortGroupName, string(direction)); err != nil {
		return err
	}

	// clear rule address_set
	asList, err := c.ListSgRuleAddressSet(sg.Name, direction)
	if err != nil {
		return err
	}
	for _, as := range asList {
		if err = c.DeleteAddressSet(as); err != nil {
			return err
		}
	}

	// create port_group associated acl
	if sg.Spec.AllowSameGroupTraffic {
		v4AsName := GetSgV4AssociatedName(sg.Name)
		v6AsName := GetSgV6AssociatedName(sg.Name)
		if direction == SgAclIngressDirection {
			if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", sgPortGroupName, "to-lport", util.SecurityGroupAllowPriority,
				fmt.Sprintf("outport==@%s && ip4 && ip4.src==$%s", sgPortGroupName, v4AsName), "allow-related"); err != nil {
				return err
			}
			if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", sgPortGroupName, "to-lport", util.SecurityGroupAllowPriority,
				fmt.Sprintf("outport==@%s && ip6 && ip6.src==$%s", sgPortGroupName, v6AsName), "allow-related"); err != nil {
				return err
			}
		} else {
			if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", sgPortGroupName, "from-lport", util.SecurityGroupAllowPriority,
				fmt.Sprintf("inport==@%s && ip4 && ip4.dst==$%s", sgPortGroupName, v4AsName), "allow-related"); err != nil {
				return err
			}
			if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", sgPortGroupName, "from-lport", util.SecurityGroupAllowPriority,
				fmt.Sprintf("inport==@%s && ip6 && ip6.dst==$%s", sgPortGroupName, v6AsName), "allow-related"); err != nil {
				return err
			}
		}
	}

	// recreate rule ACL
	var sgRules []*kubeovnv1.SgRule
	if direction == SgAclIngressDirection {
		sgRules = sg.Spec.IngressRules
	} else {
		sgRules = sg.Spec.EgressRules
	}
	for index, rule := range sgRules {
		if err = c.createSgRuleACL(sg.Name, direction, rule, index); err != nil {
			return err
		}
	}
	return nil
}
func (c LegacyClient) OvnGet(table, record, column, key string) (string, error) {
	var columnVal string
	if key == "" {
		columnVal = column
	} else {
		columnVal = column + ":" + key
	}
	args := []string{"get", table, record, columnVal}
	return c.ovnNbCommand(args...)
}

func (c LegacyClient) SetLspExternalIds(name string, externalIDs map[string]string) error {
	if len(externalIDs) == 0 {
		return nil
	}

	cmd := make([]string, len(externalIDs)+3)
	cmd = append(cmd, "set", "logical_switch_port", name)
	for k, v := range externalIDs {
		cmd = append(cmd, fmt.Sprintf(`external-ids:%s="%s"`, k, v))
	}

	if _, err := c.ovnNbCommand(cmd...); err != nil {
		return fmt.Errorf("failed to set external-ids for logical switch port %s: %v", name, err)
	}
	return nil
}

func (c *LegacyClient) AclExists(priority, direction string) (bool, error) {
	priorityVal, _ := strconv.Atoi(priority)
	results, err := c.CustomFindEntity("acl", []string{"match"}, fmt.Sprintf("priority=%d", priorityVal), fmt.Sprintf("direction=%s", direction))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return false, err
	}
	if len(results) == 0 {
		return false, nil
	}
	return true, nil
}

func (c *LegacyClient) SetLBCIDR(svccidr string) error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", fmt.Sprintf("options:svc_ipv4_cidr=%s", svccidr)); err != nil {
		return fmt.Errorf("failed to set svc cidr for lb, %v", err)
	}
	return nil
}
func (c *LegacyClient) PortGroupExists(pgName string) (bool, error) {
	results, err := c.CustomFindEntity("port_group", []string{"_uuid"}, fmt.Sprintf("name=%s", pgName))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return false, err
	}
	if len(results) == 0 {
		return false, nil
	}
	return true, nil
}

func (c *LegacyClient) PolicyRouteExists(priority int32, match string) (bool, error) {
	results, err := c.CustomFindEntity("Logical_Router_Policy", []string{"_uuid"}, fmt.Sprintf("priority=%d", priority), fmt.Sprintf("match=\"%s\"", match))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return false, err
	}
	if len(results) == 0 {
		return false, nil
	}
	return true, nil
}

func (c *LegacyClient) DeletePolicyRouteByUUID(router string, uuids []string) error {
	if len(uuids) == 0 {
		return nil
	}
	for _, uuid := range uuids {
		var args []string
		args = append(args, []string{"lr-policy-del", router, uuid}...)
		if _, err := c.ovnNbCommand(args...); err != nil {
			klog.Errorf("failed to delete router %s policy route: %v", router, err)
			return err
		}
	}
	return nil
}

func (c *LegacyClient) GetPolicyRouteParas(priority int32, match string) ([]string, map[string]string, error) {
	result, err := c.CustomFindEntity("Logical_Router_Policy", []string{"nexthops", "external_ids"}, fmt.Sprintf("priority=%d", priority), fmt.Sprintf(`match="%s"`, match))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return nil, nil, err
	}
	if len(result) == 0 {
		return nil, nil, nil
	}

	nameIpMap := make(map[string]string, len(result[0]["external_ids"]))
	for _, l := range result[0]["external_ids"] {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), "=")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		ip := strings.TrimSpace(parts[1])
		nameIpMap[name] = ip
	}

	return result[0]["nexthops"], nameIpMap, nil
}

func (c LegacyClient) SetPolicyRouteExternalIds(priority int32, match string, nameIpMaps map[string]string) error {
	result, err := c.CustomFindEntity("Logical_Router_Policy", []string{"_uuid"}, fmt.Sprintf("priority=%d", priority), fmt.Sprintf("match=\"%s\"", match))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return err
	}
	if len(result) == 0 {
		return nil
	}

	uuid := result[0]["_uuid"][0]
	ovnCmd := []string{"set", "logical-router-policy", uuid}
	for nodeName, nodeIP := range nameIpMaps {
		ovnCmd = append(ovnCmd, fmt.Sprintf("external_ids:%s=\"%s\"", nodeName, nodeIP))
	}

	if _, err := c.ovnNbCommand(ovnCmd...); err != nil {
		return fmt.Errorf("failed to set logical-router-policy externalIds, %v", err)
	}
	return nil
}

func (c LegacyClient) CheckPolicyRouteNexthopConsistent(router, match, nexthop string, priority int32) (bool, error) {
	exist, err := c.PolicyRouteExists(priority, match)
	if err != nil {
		return false, err
	}
	if !exist {
		return false, nil
	}

	nextHops, _, err := c.GetPolicyRouteParas(priority, match)
	if err != nil {
		klog.Errorf("failed to get policy route paras, %v", err)
		return false, err
	}
	for _, next := range nextHops {
		if next == nexthop {
			return true, nil
		}
	}
	return false, nil
}

func (c LegacyClient) SetAclLog(pgName string, logEnable, isIngress bool) error {
	var direction, match string
	if isIngress {
		direction = "to-lport"
		match = fmt.Sprintf("outport==@%s && ip", pgName)
	} else {
		direction = "from-lport"
		match = fmt.Sprintf("inport==@%s && ip", pgName)
	}

	priority, _ := strconv.Atoi(util.IngressDefaultDrop)
	result, err := c.CustomFindEntity("acl", []string{"_uuid"}, fmt.Sprintf("priority=%d", priority), fmt.Sprintf(`match="%s"`, match), fmt.Sprintf("direction=%s", direction), "action=drop")
	if err != nil {
		klog.Errorf("failed to get acl UUID: %v", err)
		return err
	}

	if len(result) == 0 {
		return nil
	}

	uuid := result[0]["_uuid"][0]
	ovnCmd := []string{"set", "acl", uuid, fmt.Sprintf("log=%v", logEnable)}

	if _, err := c.ovnNbCommand(ovnCmd...); err != nil {
		return fmt.Errorf("failed to set acl log, %v", err)
	}

	return nil
}
