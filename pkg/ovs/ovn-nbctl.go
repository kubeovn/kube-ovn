package ovs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	SgAclIngressDirection = ovnnb.ACLDirectionToLport
	SgAclEgressDirection  = ovnnb.ACLDirectionFromLport
)

func Transact(c client.Client, method string, operations []ovsdb.Operation, timeout int) error {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	results, err := c.Transact(ctx, operations...)
	elapsed := float64((time.Since(start)) / time.Millisecond)

	var dbType string
	switch c.Schema().Name {
	case "OVN_Northbound":
		dbType = "ovn-nb"
	}

	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues(dbType, method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Errorf("error occurred in transact with %s operations: %+v in %vms", dbType, operations, elapsed)
		return err
	}

	if elapsed > 500 {
		klog.Warningf("%s operations took too long: %+v in %vms", dbType, operations, elapsed)
	}

	errors, err := ovsdb.CheckOperationResults(results, operations)
	if err != nil {
		klog.Errorf("error occurred in transact with operations %+v with operation errors %+v: %v", operations, errors, err)
		return err
	}

	return nil
}

func (c Client) ovnNbCommand(cmdArgs ...string) (string, error) {
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

func (c Client) SetAzName(azName string) error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", fmt.Sprintf("name=%s", azName)); err != nil {
		return fmt.Errorf("failed to set az name, %v", err)
	}
	return nil
}

func (c Client) SetUseCtInvMatch() error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", "options:use_ct_inv_match=false"); err != nil {
		return fmt.Errorf("failed to set NB_Global option use_ct_inv_match to false: %v", err)
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

func (c Client) ListLogicalEntity(entity string, args ...string) ([]string, error) {
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

func (c Client) CustomFindEntity(entity string, attris []string, args ...string) (result []map[string][]string, err error) {
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

func (c Client) GetEntityInfo(entity string, index string, attris []string) (result map[string]string, err error) {
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

type StaticRoute struct {
	Policy  string
	CIDR    string
	NextHop string
}

/*
func (c Client) ListStaticRoute() ([]StaticRoute, error) {
	routes := make([]ovnnb.LogicalRouterStaticRoute, 0)
	if err := c.nbClient.WhereCache(func(r *ovnnb.LogicalRouterStaticRoute) bool { return len(r.ExternalIDs) == 0 }); err != nil {
		return nil, fmt.Errorf("failed to list logical router static route without external IDs: %v", err)
	}

	result := make([]StaticRoute, len(routes))
	for i := range routes {
		result[i].CIDR = routes[i].IPPrefix
		result[i].NextHop = routes[i].Nexthop
		if routes[i].Policy != nil {
			result[i].Policy = *routes[i].Policy
		}
	}
	return result, nil
}
*/

// AddStaticRoute add a static route rule in ovn
func (c Client) AddStaticRoute(policy, cidr, nextHop, router, routeType string) error {
	// lrList := make([]ovnnb.LogicalRouter, 0, 1)
	// if err := c.nbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool { return lr.Name == router }).List(context.TODO(), &lrList); err != nil {
	// 	return fmt.Errorf("failed to list logical router with name %s: %v", router, err)
	// }
	// if len(lrList) == 0 {
	// 	return fmt.Errorf("logical router %s does not exist", router)
	// }
	// if len(lrList) != 1 {
	// 	return fmt.Errorf("found multiple logical router with the same name %s", router)
	// }

	// var found bool
	if policy == "" {
		policy = PolicyDstIP
	}
	// for _, r := range lrList[0].StaticRoutes {
	// 	route := &ovnnb.LogicalRouterStaticRoute{UUID: r}
	// 	if err := c.nbClient.Get(route); err != nil {
	// 		return fmt.Errorf("failed to get logical router static route with UUID %s: %v", r, err)
	// 	}

	// 	// if route.IPPrefix == cidr
	// 	// c.nbClient.
	// }

	for _, cidrBlock := range strings.Split(cidr, ",") {
		for _, gw := range strings.Split(nextHop, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(gw) {
				continue
			}
			// route := ovnnb.LogicalRouterStaticRoute{IPPrefix: cidrBlock, Nexthop: gw}

			if routeType == util.EcmpRouteType {
				if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "--ecmp", "lr-route-add", router, cidrBlock, gw); err != nil {
					return err
				}
			} else {
				if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "lr-route-add", router, cidrBlock, gw); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (c Client) GetStaticRouteList(router string) (routeList []*StaticRoute, err error) {
	output, err := c.ovnNbCommand("lr-route-list", router)
	if err != nil {
		klog.Errorf("failed to list logical router route: %v", err)
		return nil, err
	}
	return parseLrRouteListOutput(output)
}

var routeRegexp = regexp.MustCompile(`^\s*((\d+(\.\d+){3})|(([a-f0-9:]*:+)+[a-f0-9]?))(/\d+)?\s+((\d+(\.\d+){3})|(([a-f0-9:]*:+)+[a-f0-9]?))\s+(dst-ip|src-ip)(\s+.+)?$`)

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
			} else {
				_, err = c.ovnNbCommand(MayExist, "lr-nat-add", router, policy, externalIP, logicalIP)
			}
			return err
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

func (c Client) DeleteMatchedStaticRoute(cidr, nexthop, router string) error {
	if cidr == "" || nexthop == "" {
		return nil
	}
	_, err := c.ovnNbCommand(IfExists, "lr-route-del", router, cidr, nexthop)
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

/*
func (c Client) FindLoadbalancer(name string) (*ovnnb.LoadBalancer, error) {
	lbList := make([]ovnnb.LoadBalancer, 0, 1)
	if err := c.nbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == name }).List(context.TODO(), &lbList); err != nil {
		return nil, fmt.Errorf("failed to list load balancer with name %s: %v", name, err)
	}
	switch len(lbList) {
	case 0:
		return nil, nil
	case 1:
		return &lbList[0], nil
	default:
		return nil, fmt.Errorf("found multiple load balancers with the same name %s", name)
	}
}
*/

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
func (c Client) SetPrivateLogicalSwitch(ls, cidr string, allow []string) error {
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

func (c Client) CreateIngressACL(npName, pgName, asIngressName, asExceptName, svcAsName, protocol string, npp []netv1.NetworkPolicyPort) error {
	var allowArgs []string

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	ovnArgs := []string{MayExist, "--type=port-group", "--log", fmt.Sprintf("--name=%s", npName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", pgName, "to-lport", util.IngressDefaultDrop, fmt.Sprintf("outport==@%s && ip", pgName), "drop"}

	if len(npp) == 0 {
		allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == $%s && %s.src != $%s && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, pgName), "allow-related"}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for _, port := range npp {
			if port.Port != nil {
				if port.EndPort != nil {
					allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == $%s && %s.src != $%s && %d <= %s.dst <= %d && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, port.Port.IntVal, strings.ToLower(string(*port.Protocol)), *port.EndPort, pgName), "allow-related"}
				} else {
					allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s.dst == %d && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgName), "allow-related"}
				}
			} else {
				allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), pgName), "allow-related"}
			}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) CreateEgressACL(npName, pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort, portSvcName string) error {
	var allowArgs []string

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	ovnArgs := []string{"--", MayExist, "--type=port-group", "--log", fmt.Sprintf("--name=%s", npName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", pgName, "from-lport", util.EgressDefaultDrop, fmt.Sprintf("inport==@%s && ip", pgName), "drop"}

	if len(npp) == 0 {
		allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, pgName), "allow-related"}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for _, port := range npp {
			if port.Port != nil {
				if port.EndPort != nil {
					allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %d <= %s.dst <= %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, port.Port.IntVal, strings.ToLower(string(*port.Protocol)), *port.EndPort, pgName), "allow-related"}
				} else {
					allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s.dst == %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgName), "allow-related"}
				}
			} else {
				allowArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), pgName), "allow-related"}
			}
			ovnArgs = append(ovnArgs, allowArgs...)
		}
	}
	_, err := c.ovnNbCommand(ovnArgs...)
	return err
}

func (c Client) DeleteACL(pgName, direction string) (err error) {
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

func (c Client) CreateACLForNodePg(pgName, nodeIpStr string) error {
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

func (c Client) DeleteAclForNodePg(pgName string) error {
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
		"--timeout=60",
		"show",
	).CombinedOutput()

	if err != nil {
		klog.Errorf("failed to access ovn-nb from daemon, %q", output)
		return err
	}
	return nil
}

func (c Client) createSgRuleACL(sgName string, direction ovnnb.ACLDirection, rule *kubeovnv1.SgRule, index int) error {
	ipSuffix := "ip4"
	if rule.IPVersion == "ipv6" {
		ipSuffix = "ip6"
	}

	sgPortGroupName := SgPortGroupName(sgName)
	var matchArgs []string
	if rule.RemoteType == kubeovnv1.SgRemoteTypeAddress {
		if direction == SgAclIngressDirection {
			matchArgs = append(matchArgs, fmt.Sprintf("outport==@%s && %s && %s.src==%s", sgPortGroupName, ipSuffix, ipSuffix, rule.RemoteAddress))
		} else {
			matchArgs = append(matchArgs, fmt.Sprintf("inport==@%s && %s && %s.dst==%s", sgPortGroupName, ipSuffix, ipSuffix, rule.RemoteAddress))
		}
	} else {
		if direction == SgAclIngressDirection {
			matchArgs = append(matchArgs, fmt.Sprintf("outport==@%s && %s && %s.src==$%s", sgPortGroupName, ipSuffix, ipSuffix, SgV4AssociatedName(rule.RemoteSecurityGroup)))
		} else {
			matchArgs = append(matchArgs, fmt.Sprintf("inport==@%s && %s && %s.dst==$%s", sgPortGroupName, ipSuffix, ipSuffix, SgV4AssociatedName(rule.RemoteSecurityGroup)))
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

func (c Client) CreateSgDenyAllACL() error {
	portGroupName := SgPortGroupName(util.DenyAllSecurityGroup)
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

func (c Client) UpdateSgACL(sg *kubeovnv1.SecurityGroup, direction ovnnb.ACLDirection) error {
	sgPortGroupName := SgPortGroupName(sg.Name)
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
		v4AsName := SgV4AssociatedName(sg.Name)
		v6AsName := SgV6AssociatedName(sg.Name)
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
func (c Client) AclExists(priority, direction string) (bool, error) {
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

func (c *Client) SetLBCIDR(svccidr string) error {
	if _, err := c.ovnNbCommand("set", "NB_Global", ".", fmt.Sprintf("options:svc_ipv4_cidr=%s", svccidr)); err != nil {
		return fmt.Errorf("failed to set svc cidr for lb, %v", err)
	}
	return nil
}
