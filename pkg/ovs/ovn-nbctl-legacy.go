package ovs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/slices"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func (c LegacyClient) GetVersion() (string, error) {
	if c.Version != "" {
		return c.Version, nil
	}
	output, err := c.ovnNbCommand("--version")
	if err != nil {
		return "", fmt.Errorf("failed to get version,%v", err)
	}
	lines := strings.Split(output, "\n")
	if len(lines) > 0 {
		c.Version = strings.Split(lines[0], " ")[1]
	}
	return c.Version, nil
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

type StaticRoute struct {
	Policy   string
	CIDR     string
	NextHop  string
	ECMPMode string
	BfdId    string
}

// AddStaticRoute add a static route rule in ovn
func (c LegacyClient) AddStaticRoute(policy, cidr, nextHop, ecmp, bfdId, router string, routeType string) error {
	// route require policy, cidr, nextHop, ecmp
	// in default, ecmp route with bfd id use ecmp-symmetric-reply mode
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
				if ecmp == util.StaicRouteBfdEcmp {
					if bfdId == "" {
						err := fmt.Errorf("bfd id should not be empty")
						klog.Error(err)
						return err
					}
					ecmpSymmetric := fmt.Sprintf("--%s", util.StaicRouteBfdEcmp)
					bfd := fmt.Sprintf("--bfd=%s", bfdId)
					if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), bfd, ecmpSymmetric, "lr-route-add", router, cidrBlock, gw); err != nil {
						return err
					}
				} else {
					if _, err := c.ovnNbCommand(MayExist, fmt.Sprintf("%s=%s", Policy, policy), "--ecmp", "lr-route-add", router, cidrBlock, gw); err != nil {
						return err
					}
				}
			} else {
				if !strings.ContainsRune(cidrBlock, '/') {
					filter := []string{fmt.Sprintf("policy=%s", policy), fmt.Sprintf(`ip_prefix="%s"`, cidrBlock), fmt.Sprintf(`nexthop!="%s"`, gw)}
					result, err := c.CustomFindEntity("Logical_Router_Static_Route", []string{"_uuid"}, filter...)
					if err != nil {
						return err
					}

					for _, route := range result {
						if util.ContainsString(existingRoutes, route["_uuid"][0]) {
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
	consistent, err := c.CheckPolicyRouteNexthopConsistent(match, nextHop, priority)
	if err != nil {
		return err
	}
	if consistent {
		return nil
	}

	klog.Infof("remove inconsistent policy route from router %s: match %s", router, match)
	if err := c.DeletePolicyRoute(router, priority, match); err != nil {
		klog.Errorf("failed to delete policy route: %v", err)
		return err
	}

	// lr-policy-add ROUTER PRIORITY MATCH ACTION [NEXTHOP]
	args := []string{MayExist, "lr-policy-add", router, strconv.Itoa(int(priority)), match, action}
	if nextHop != "" {
		args = append(args, nextHop)
	}
	klog.Infof("add policy route for router %s: priority %d, match %s, nextHop %s", router, priority, match, nextHop)
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
	klog.Infof("remove policy route from router %s: match %s", router, match)
	_, err = c.ovnNbCommand(args...)
	return err
}

func (c LegacyClient) CleanPolicyRoute(router string) error {
	// lr-policy-del ROUTER
	klog.Infof("clean all policy route for route %s", router)
	var args = []string{"lr-policy-del", router}
	_, err := c.ovnNbCommand(args...)
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
	klog.Infof("delete policy route for router: %s, priority: %d, match %s", router, priority, output)
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
			return nil, fmt.Errorf("found unexpected policy priority %s, please check", sm[1])
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
		// <PREFIX> <NEXT_HOP> <POLICY> [OUTPUT_PORT] [(learned)] [ecmp] [ecmp-symmetric-reply] [bfd]
		fields := strings.Fields(l)
		if len(fields) < 3 {
			continue
		}
		if !routeRegexp.MatchString(l) {
			continue
		}

		var learned, ecmpSymmetricReply bool
		idx := len(fields) - 1
		for ; !learned && idx > 2; idx-- {
			switch fields[idx] {
			case "(learned)":
				learned = true
			case util.StaicRouteBfdEcmp:
				ecmpSymmetricReply = true
			}
		}
		if learned {
			continue
		}

		route := &StaticRoute{
			Policy:  fields[2],
			CIDR:    fields[0],
			NextHop: fields[1],
		}
		if ecmpSymmetricReply {
			route.ECMPMode = util.StaicRouteBfdEcmp
		}

		routeList = append(routeList, route)
	}
	return routeList, nil
}

func (c LegacyClient) UpdateNatRule(policy, logicalIP, externalIP, router, logicalMac, port string) error {
	// when dual protocol pod has eip or snat, will add nat for all dual addresses.
	// will fail when logicalIP externalIP is different protocol.
	if externalIP != "" && util.CheckProtocol(logicalIP) != util.CheckProtocol(externalIP) {
		return nil
	}

	if policy == "snat" {
		if externalIP == "" {
			_, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP)
			return err
		}
		if _, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, "snat", logicalIP); err != nil {
			return err
		}
		_, err := c.ovnNbCommand(MayExist, "lr-nat-add", router, policy, externalIP, logicalIP)
		return err
	} else {
		output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", strings.ReplaceAll(logicalIP, ":", "\\:")), "type=dnat_and_snat")
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
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=type,external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", strings.ReplaceAll(logicalIP, ":", "\\:")))
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
	results, err := c.CustomFindEntity("NAT", []string{"external_ip"}, fmt.Sprintf("logical_ip=%s", strings.ReplaceAll(logicalIP, ":", "\\:")))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return false, err
	}
	if len(results) == 0 {
		return false, nil
	}
	return true, nil
}

func (c LegacyClient) AddFipRule(router, eip, logicalIP, logicalMac, port string) error {
	// failed if logicalIP externalIP(eip) is different protocol.
	if util.CheckProtocol(logicalIP) != util.CheckProtocol(eip) {
		return nil
	}
	var err error
	fip := "dnat_and_snat"
	if eip != "" && logicalIP != "" && logicalMac != "" {
		if c.ExternalGatewayType == "distributed" {
			_, err = c.ovnNbCommand(MayExist, "--stateless", "lr-nat-add", router, fip, eip, logicalIP, port, logicalMac)
		} else {
			_, err = c.ovnNbCommand(MayExist, "lr-nat-add", router, fip, eip, logicalIP)
		}
		return err
	} else {
		return fmt.Errorf("logical ip, external ip and logical mac must be provided to add fip rule")
	}
}

func (c LegacyClient) DeleteFipRule(router, eip, logicalIP string) error {
	fip := "dnat_and_snat"
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
		if externalIP == eip && policy == fip {
			if _, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, fip, externalIP); err != nil {
				klog.Errorf("failed to delete fip rule, %v", err)
				return err
			}
		}
	}
	return err
}

func (c *LegacyClient) FipRuleExists(eip, logicalIP string) (bool, error) {
	fip := "dnat_and_snat"
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=type,external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", logicalIP))
	if err != nil {
		klog.Errorf("failed to list nat rules, %v", err)
		return false, err
	}
	rules := strings.Split(output, "\n")
	for _, rule := range rules {
		if len(strings.Split(rule, ",")) != 2 {
			continue
		}
		policy, externalIP := strings.Split(rule, ",")[0], strings.Split(rule, ",")[1]
		if externalIP == eip && policy == fip {
			return true, nil
		}
	}
	return false, fmt.Errorf("fip rule not exist")
}

func (c LegacyClient) AddSnatRule(router, eip, ipCidr string) error {
	// failed if logicalIP externalIP(eip) is different protocol.
	if util.CheckProtocol(ipCidr) != util.CheckProtocol(eip) {
		return nil
	}
	snat := "snat"
	if eip != "" && ipCidr != "" {
		_, err := c.ovnNbCommand(MayExist, "lr-nat-add", router, snat, eip, ipCidr)
		return err
	} else {
		return fmt.Errorf("logical ip, external ip and logical mac must be provided to add snat rule")
	}
}

func (c LegacyClient) DeleteSnatRule(router, eip, ipCidr string) error {
	snat := "snat"
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=type,external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", ipCidr))
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
		if externalIP == eip && policy == snat {
			if _, err := c.ovnNbCommand(IfExists, "lr-nat-del", router, snat, ipCidr); err != nil {
				klog.Errorf("failed to delete snat rule, %v", err)
				return err
			}
		}
	}
	return err
}

func (c *LegacyClient) SnatRuleExists(eip, ipCidr string) (bool, error) {
	snat := "snat"
	output, err := c.ovnNbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=type,external_ip", "find", "NAT", fmt.Sprintf("logical_ip=%s", ipCidr))
	if err != nil {
		klog.Errorf("failed to list nat rules, %v", err)
		return false, err
	}
	rules := strings.Split(output, "\n")
	for _, rule := range rules {
		if len(strings.Split(rule, ",")) != 2 {
			continue
		}
		policy, externalIP := strings.Split(rule, ",")[0], strings.Split(rule, ",")[1]
		if externalIP == eip && policy == snat {
			return true, nil
		}
	}
	return false, fmt.Errorf("snat rule not exist")
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
	for _, cidrBlock := range strings.Split(cidr, ",") {
		if _, err := c.ovnNbCommand(IfExists, "lr-route-del", router, cidrBlock); err != nil {
			klog.Errorf("fail to delete static route %s from %s, %v", cidrBlock, router, err)
			return err
		}
	}

	return nil
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
	trimName := ls
	if len(ls) > 63 {
		trimName = ls[:63]
	}
	dropArgs := []string{"--", "--log", fmt.Sprintf("--name=%s", trimName), fmt.Sprintf("--severity=%s", "warning"), "acl-add", ls, "to-lport", util.DefaultDropPriority, "ip", "drop"}
	ovnArgs = append(ovnArgs, dropArgs...)

	for _, cidrBlock := range strings.Split(cidr, ",") {
		allowArgs := []string{}
		protocol := util.CheckProtocol(cidrBlock)
		if protocol == kubeovnv1.ProtocolIPv4 {
			allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.SubnetAllowPriority, fmt.Sprintf(`ip4.src==%s && ip4.dst==%s`, cidrBlock, cidrBlock), "allow-related")
		} else if protocol == kubeovnv1.ProtocolIPv6 {
			allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.SubnetAllowPriority, fmt.Sprintf(`ip6.src==%s && ip6.dst==%s`, cidrBlock, cidrBlock), "allow-related")
		} else {
			klog.Errorf("the cidrBlock: %s format is error in subnet: %s", cidrBlock, ls)
			continue
		}

		for _, nodeCidrBlock := range strings.Split(c.NodeSwitchCIDR, ",") {
			if protocol != util.CheckProtocol(nodeCidrBlock) {
				continue
			}

			if protocol == kubeovnv1.ProtocolIPv4 {
				allowArgs = append(allowArgs, "--", MayExist, "acl-add", ls, "to-lport", util.NodeAllowPriority, fmt.Sprintf("ip4.src==%s", nodeCidrBlock), "allow-related")
			} else if protocol == kubeovnv1.ProtocolIPv6 {
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

func (c LegacyClient) CombineIngressACLCmd(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort, logEnable bool, aclCmds []string, index int, namedPortMap map[string]*util.NamedPortInfo) []string {
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
					if port.Port.Type == intstr.Int {
						allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=to-lport", fmt.Sprintf("priority=%s", util.IngressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s.dst == %d && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
					} else {
						var portId int32 = 0
						if namedPortMap != nil {
							_, ok := namedPortMap[port.Port.StrVal]
							if !ok {
								// for cyclonus network policy test case 'should allow ingress access on one named port'
								// this case expect all-deny if no named port defined
								klog.Errorf("no named port with name %s found ", port.Port.StrVal)
							} else {
								portId = namedPortMap[port.Port.StrVal].PortId
							}
						}
						allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=to-lport", fmt.Sprintf("priority=%s", util.IngressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s.dst == %d && outport==@%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), portId, pgName)), "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
					}
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

func (c LegacyClient) CombineEgressACLCmd(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort, logEnable bool, aclCmds []string, index int, namedPortMap map[string]*util.NamedPortInfo) []string {
	var allowArgs, ovnArgs []string

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	id := pgName + "_" + ipSuffix

	if logEnable {
		ovnArgs = []string{"--", fmt.Sprintf("--id=@%s.drop.%d", id, index), "create", "acl", "action=drop", "direction=from-lport", "log=true", "severity=warning", fmt.Sprintf("priority=%s", util.EgressDefaultDrop), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("inport==@%s && ip", pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.drop.%d", id, index)}
	} else {
		ovnArgs = []string{"--", fmt.Sprintf("--id=@%s.drop.%d", id, index), "create", "acl", "action=drop", "direction=from-lport", "log=false", fmt.Sprintf("priority=%s", util.EgressDefaultDrop), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("inport==@%s && ip", pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.drop.%d", id, index)}
	}

	if len(npp) == 0 {
		allowArgs = []string{"--", fmt.Sprintf("--id=@%s.noport.%d", id, index), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.noport.%d", id, index)}
		ovnArgs = append(ovnArgs, allowArgs...)
	} else {
		for pidx, port := range npp {
			if port.Port != nil {
				if port.EndPort != nil {
					allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %d <= %s.dst <= %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, port.Port.IntVal, strings.ToLower(string(*port.Protocol)), *port.EndPort, pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
				} else {
					if port.Port.Type == intstr.Int {
						allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s.dst == %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), port.Port.IntVal, pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
					} else {
						var portId int32 = 0
						if namedPortMap != nil {
							_, ok := namedPortMap[port.Port.StrVal]
							if !ok {
								klog.Errorf("no named port with name %s found ", port.Port.StrVal)
							} else {
								portId = namedPortMap[port.Port.StrVal].PortId
							}
						}
						allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s.dst == %d && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), portId, pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
					}
				}
			} else {
				allowArgs = []string{"--", fmt.Sprintf("--id=@%s.%d.port.%d", id, index, pidx), "create", "acl", "action=allow-related", "direction=from-lport", fmt.Sprintf("priority=%s", util.EgressAllowPriority), fmt.Sprintf("match=\"%s\"", fmt.Sprintf("%s.dst == $%s && %s.dst != $%s && %s && inport==@%s && ip", ipSuffix, asEgressName, ipSuffix, asExceptName, strings.ToLower(string(*port.Protocol)), pgName)), "options={apply-after-lb=\"true\"}", "--", "add", "port-group", pgName, "acls", fmt.Sprintf("@%s.%d.port.%d", id, index, pidx)}
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

func (c LegacyClient) CreateGatewayACL(ls, pgName, gateway, cidr string) error {
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

			var ingressArgs, egressArgs []string
			if pgName != "" {
				ingressArgs = []string{"--", MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.IngressAllowPriority, fmt.Sprintf("%s.src == %s", ipSuffix, gw), "allow-related"}
				egressArgs = []string{"--", MayExist, "--type=port-group", "--apply-after-lb", "acl-add", pgName, "from-lport", util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s", ipSuffix, gw), "allow-related"}
				if ipSuffix == "ip6" {
					egressArgs = append(egressArgs, []string{"--", "--type=port-group", MayExist, "--apply-after-lb", "acl-add", pgName, "from-lport", util.EgressAllowPriority, `nd || nd_ra || nd_rs`, "allow-related"}...)
				}
			} else if ls != "" {
				ingressArgs = []string{"--", MayExist, "acl-add", ls, "to-lport", util.IngressAllowPriority, fmt.Sprintf(`%s.src == %s`, ipSuffix, gw), "allow-related"}
				egressArgs = []string{"--", MayExist, "--apply-after-lb", "acl-add", ls, "from-lport", util.EgressAllowPriority, fmt.Sprintf(`%s.dst == %s`, ipSuffix, gw), "allow-related"}
				if ipSuffix == "ip6" {
					egressArgs = append(egressArgs, []string{"--", MayExist, "--apply-after-lb", "acl-add", ls, "from-lport", util.EgressAllowPriority, `nd || nd_ra || nd_rs`, "allow-related"}...)
				}
			}

			ovnArgs := append(ingressArgs, egressArgs...)
			if _, err := c.ovnNbCommand(ovnArgs...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c LegacyClient) CreateACLForNodePg(pgName, nodeIpStr, joinIpStr string) error {
	nodeIPs := strings.Split(nodeIpStr, ",")
	for _, nodeIp := range nodeIPs {
		protocol := util.CheckProtocol(nodeIp)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		ingressArgs := []string{MayExist, "--type=port-group", "acl-add", pgName, "to-lport", util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, nodeIp, ipSuffix, pgAs), "allow-related"}
		egressArgs := []string{"--", MayExist, "--type=port-group", "--apply-after-lb", "acl-add", pgName, "from-lport", util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, nodeIp, ipSuffix, pgAs), "allow-related"}
		ovnArgs := append(ingressArgs, egressArgs...)
		if _, err := c.ovnNbCommand(ovnArgs...); err != nil {
			klog.Errorf("failed to add node port-group acl: %v", err)
			return err
		}
	}
	for _, joinIp := range strings.Split(joinIpStr, ",") {
		if util.ContainsString(nodeIPs, joinIp) {
			continue
		}

		protocol := util.CheckProtocol(joinIp)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		ingressArgs := []string{"acl-del", pgName, "to-lport", util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, joinIp, ipSuffix, pgAs)}
		egressArgs := []string{"--", "acl-del", pgName, "from-lport", util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, joinIp, ipSuffix, pgAs)}
		ovnArgs := append(ingressArgs, egressArgs...)
		if _, err := c.ovnNbCommand(ovnArgs...); err != nil {
			klog.Errorf("failed to delete node port-group acl: %v", err)
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

func GetSgPortGroupName(sgName string) string {
	return strings.Replace(fmt.Sprintf("ovn.sg.%s", sgName), "-", ".", -1)
}

func GetSgV4AssociatedName(sgName string) string {
	return strings.Replace(fmt.Sprintf("ovn.sg.%s.associated.v4", sgName), "-", ".", -1)
}

func GetSgV6AssociatedName(sgName string) string {
	return strings.Replace(fmt.Sprintf("ovn.sg.%s.associated.v6", sgName), "-", ".", -1)
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

func (c LegacyClient) createSgRuleACL(sgName string, direction AclDirection, rule *kubeovnv1.SgRule) error {
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
		remotePgName := GetSgV4AssociatedName(rule.RemoteSecurityGroup)
		if rule.IPVersion == "ipv6" {
			remotePgName = GetSgV6AssociatedName(rule.RemoteSecurityGroup)
		}
		if direction == SgAclIngressDirection {
			matchArgs = append(matchArgs, fmt.Sprintf("outport==@%s && %s && %s.src==$%s", sgPortGroupName, ipSuffix, ipSuffix, remotePgName))
		} else {
			matchArgs = append(matchArgs, fmt.Sprintf("inport==@%s && %s && %s.dst==$%s", sgPortGroupName, ipSuffix, ipSuffix, remotePgName))
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

func (c LegacyClient) CreateSgBaseEgressACL(sgName string) error {
	portGroupName := GetSgPortGroupName(sgName)
	klog.Infof("add base egress acl, sg: %s", portGroupName)
	// allow arp
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclEgressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("inport==@%s && arp", portGroupName), "allow-related"); err != nil {
		return err
	}

	// icmpv6
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclEgressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("inport==@%s && icmp6.type=={130, 133, 135, 136} && icmp6.code == 0 && ip.ttl == 255", portGroupName), "allow-related"); err != nil {
		return err
	}

	// dhcpv4 res
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclEgressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("inport==@%s && udp.src==68 && udp.dst==67 && ip4", portGroupName), "allow-related"); err != nil {
		return err
	}

	// dhcpv6 res
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclEgressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("inport==@%s && udp.src==546 && udp.dst==547 && ip6", portGroupName), "allow-related"); err != nil {
		return err
	}
	return nil
}

func (c LegacyClient) CreateSgBaseIngressACL(sgName string) error {
	portGroupName := GetSgPortGroupName(sgName)
	klog.Infof("add base ingress acl, sg: %s", portGroupName)
	// allow arp
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclIngressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("outport==@%s && arp", portGroupName), "allow-related"); err != nil {
		return err
	}

	// icmpv6
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclIngressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("outport==@%s && icmp6.type=={130, 134, 135, 136} && icmp6.code == 0 && ip.ttl == 255", portGroupName), "allow-related"); err != nil {
		return err
	}

	// dhcpv4 offer
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclIngressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("outport==@%s && udp.src==67 && udp.dst==68 && ip4", portGroupName), "allow-related"); err != nil {
		return err
	}

	// dhcpv6 offer
	if _, err := c.ovnNbCommand(MayExist, "--type=port-group", "acl-add", portGroupName, string(SgAclIngressDirection), util.SecurityGroupBasePriority,
		fmt.Sprintf("outport==@%s && udp.src==547 && udp.dst==546 && ip6", portGroupName), "allow-related"); err != nil {
		return err
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
	for _, rule := range sgRules {
		if err = c.createSgRuleACL(sg.Name, direction, rule); err != nil {
			return err
		}
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

func (c *LegacyClient) VpcHasPolicyRoute(vpc string, nextHops []string, priority int32) (bool, error) {
	// get all policies by vpc
	outPolicies, err := c.ovnNbCommand("--data=bare", "--no-heading",
		"--columns=policies", "find", "Logical_Router", fmt.Sprintf("name=%s", vpc))
	if err != nil {
		klog.Errorf("failed to find Logical_Router_Policy %s: %v, %q", vpc, err, outPolicies)
		return false, err
	}
	if outPolicies == "" {
		klog.V(3).Infof("vpc %s has no policy routes", vpc)
		return false, nil
	}

	strRoutes := strings.Split(outPolicies, "\n")[0]
	strPriority := fmt.Sprint(priority)
	routes := strings.Fields(strRoutes)
	// check if policie already exist
	for _, r := range routes {
		outPriorityNexthops, err := c.ovnNbCommand("--data=bare", "--no-heading", "--format=csv", "--columns=priority,nexthops", "list", "Logical_Router_Policy", r)
		if err != nil {
			klog.Errorf("failed to show Logical_Router_Policy %s: %v, %q", r, err, outPriorityNexthops)
			return false, err
		}
		if outPriorityNexthops == "" {
			return false, nil
		}
		priorityNexthops := strings.Split(outPriorityNexthops, "\n")[0]
		result := strings.Split(priorityNexthops, ",")
		if len(result) == 2 {
			routePriority := result[0]
			strNodeIPs := result[1]
			nodeIPs := strings.Fields(strNodeIPs)
			sort.Strings(nodeIPs)
			if routePriority == strPriority && slices.Equal(nextHops, nodeIPs) {
				// make sure priority, nexthops is just the same
				return true, nil
			}
		}
	}
	return false, nil
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

func (c LegacyClient) CheckPolicyRouteNexthopConsistent(match, nexthop string, priority int32) (bool, error) {
	exist, err := c.PolicyRouteExists(priority, match)
	if err != nil {
		return false, err
	}
	if !exist {
		return false, nil
	}

	dbNextHops, _, err := c.GetPolicyRouteParas(priority, match)
	if err != nil {
		klog.Errorf("failed to get policy route paras, %v", err)
		return false, err
	}
	cfgNextHops := strings.Split(nexthop, ",")

	sort.Strings(dbNextHops)
	sort.Strings(cfgNextHops)
	if slices.Equal(dbNextHops, cfgNextHops) {
		return true, nil
	}
	return false, nil
}

type dhcpOptions struct {
	UUID        string
	CIDR        string
	ExternalIds map[string]string
	options     map[string]string
}

func (c LegacyClient) ListDHCPOptions(needVendorFilter bool, ls string, protocol string) ([]dhcpOptions, error) {
	cmds := []string{"--format=csv", "--no-heading", "--data=bare", "--columns=_uuid,cidr,external_ids,options", "find", "dhcp_options"}
	if needVendorFilter {
		cmds = append(cmds, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
	}
	if len(ls) != 0 {
		cmds = append(cmds, fmt.Sprintf("external_ids:ls=%s", ls))
	}
	if len(protocol) != 0 && protocol != kubeovnv1.ProtocolDual {
		cmds = append(cmds, fmt.Sprintf("external_ids:protocol=%s", protocol))
	}

	output, err := c.ovnNbCommand(cmds...)
	if err != nil {
		klog.Errorf("failed to find dhcp options, %v", err)
		return nil, err
	}
	entries := strings.Split(output, "\n")
	dhcpOptionsList := make([]dhcpOptions, 0, len(entries))
	for _, entry := range strings.Split(output, "\n") {
		if len(strings.Split(entry, ",")) == 4 {
			t := strings.Split(entry, ",")

			externalIdsMap := map[string]string{}
			for _, ex := range strings.Split(t[2], " ") {
				ids := strings.Split(strings.TrimSpace(ex), "=")
				if len(ids) == 2 {
					externalIdsMap[ids[0]] = ids[1]
				}
			}

			optionsMap := map[string]string{}
			for _, op := range strings.Split(t[3], " ") {
				kv := strings.Split(strings.TrimSpace(op), "=")
				if len(kv) == 2 {
					optionsMap[kv[0]] = kv[1]
				}
			}

			dhcpOptionsList = append(dhcpOptionsList,
				dhcpOptions{UUID: strings.TrimSpace(t[0]), CIDR: strings.TrimSpace(t[1]), ExternalIds: externalIdsMap, options: optionsMap})
		}
	}
	return dhcpOptionsList, nil
}

func (c *LegacyClient) createDHCPOptions(ls, cidr, optionsStr string) (dhcpOptionsUuid string, err error) {
	klog.Infof("create dhcp options ls:%s, cidr:%s, optionStr:[%s]", ls, cidr, optionsStr)

	protocol := util.CheckProtocol(cidr)
	output, err := c.ovnNbCommand("create", "dhcp_options",
		fmt.Sprintf("cidr=%s", strings.ReplaceAll(cidr, ":", "\\:")),
		fmt.Sprintf("options=%s", strings.ReplaceAll(optionsStr, ":", "\\:")),
		fmt.Sprintf("external_ids=ls=%s,protocol=%s,vendor=%s", ls, protocol, util.CniTypeName))
	if err != nil {
		klog.Errorf("create dhcp options %s for switch %s failed: %v", cidr, ls, err)
		return "", err
	}
	dhcpOptionsUuid = strings.Split(output, "\n")[0]

	return dhcpOptionsUuid, nil
}

func (c *LegacyClient) updateDHCPv4Options(ls, v4CIDR, v4Gateway, dhcpV4OptionsStr string) (dhcpV4OptionsUuid string, err error) {
	dhcpV4OptionsStr = strings.ReplaceAll(dhcpV4OptionsStr, " ", "")
	dhcpV4Options, err := c.ListDHCPOptions(true, ls, kubeovnv1.ProtocolIPv4)
	if err != nil {
		klog.Errorf("list dhcp options for switch %s protocol %s failed: %v", ls, kubeovnv1.ProtocolIPv4, err)
		return "", err
	}

	if len(v4CIDR) > 0 {
		if len(dhcpV4Options) == 0 {
			// create
			mac := util.GenerateMac()
			if len(dhcpV4OptionsStr) == 0 {
				// default dhcp v4 options
				dhcpV4OptionsStr = fmt.Sprintf("lease_time=%d,router=%s,server_id=%s,server_mac=%s", 3600, v4Gateway, "169.254.0.254", mac)
			}
			dhcpV4OptionsUuid, err = c.createDHCPOptions(ls, v4CIDR, dhcpV4OptionsStr)
			if err != nil {
				klog.Errorf("create dhcp options for switch %s failed: %v", ls, err)
				return "", err
			}
		} else {
			// update
			v4Options := dhcpV4Options[0]
			if len(dhcpV4OptionsStr) == 0 {
				mac := v4Options.options["server_mac"]
				if len(mac) == 0 {
					mac = util.GenerateMac()
				}
				dhcpV4OptionsStr = fmt.Sprintf("lease_time=%d,router=%s,server_id=%s,server_mac=%s", 3600, v4Gateway, "169.254.0.254", mac)
			}
			_, err = c.ovnNbCommand("set", "dhcp_options", v4Options.UUID, fmt.Sprintf("cidr=%s", v4CIDR),
				fmt.Sprintf("options=%s", strings.ReplaceAll(dhcpV4OptionsStr, ":", "\\:")))
			if err != nil {
				klog.Errorf("set cidr and options for dhcp v4 options %s failed: %v", v4Options.UUID, err)
				return "", err
			}
			dhcpV4OptionsUuid = v4Options.UUID
		}
	} else if len(dhcpV4Options) > 0 {
		// delete
		if err = c.DeleteDHCPOptions(ls, kubeovnv1.ProtocolIPv4); err != nil {
			klog.Errorf("delete dhcp options for switch %s protocol %s failed: %v", ls, kubeovnv1.ProtocolIPv4, err)
			return "", err
		}
	}

	return
}

func (c *LegacyClient) updateDHCPv6Options(ls, v6CIDR, dhcpV6OptionsStr string) (dhcpV6OptionsUuid string, err error) {
	dhcpV6OptionsStr = strings.ReplaceAll(dhcpV6OptionsStr, " ", "")
	dhcpV6Options, err := c.ListDHCPOptions(true, ls, kubeovnv1.ProtocolIPv6)
	if err != nil {
		klog.Errorf("list dhcp options for switch %s protocol %s failed: %v", ls, kubeovnv1.ProtocolIPv6, err)
		return "", err
	}

	if len(v6CIDR) > 0 {
		if len(dhcpV6Options) == 0 {
			// create
			if len(dhcpV6OptionsStr) == 0 {
				mac := util.GenerateMac()
				dhcpV6OptionsStr = fmt.Sprintf("server_id=%s", mac)
			}
			dhcpV6OptionsUuid, err = c.createDHCPOptions(ls, v6CIDR, dhcpV6OptionsStr)
			if err != nil {
				klog.Errorf("create dhcp options for switch %s failed: %v", ls, err)
				return "", err
			}
		} else {
			// update
			v6Options := dhcpV6Options[0]
			if len(dhcpV6OptionsStr) == 0 {
				mac := v6Options.options["server_id"]
				if len(mac) == 0 {
					mac = util.GenerateMac()
				}
				dhcpV6OptionsStr = fmt.Sprintf("server_id=%s", mac)
			}
			_, err = c.ovnNbCommand("set", "dhcp_options", v6Options.UUID, fmt.Sprintf("cidr=%s", strings.ReplaceAll(v6CIDR, ":", "\\:")),
				fmt.Sprintf("options=%s", strings.ReplaceAll(dhcpV6OptionsStr, ":", "\\:")))
			if err != nil {
				klog.Errorf("set cidr and options for dhcp v6 options %s failed: %v", v6Options.UUID, err)
				return "", err
			}
			dhcpV6OptionsUuid = v6Options.UUID
		}
	} else if len(dhcpV6Options) > 0 {
		// delete
		if err = c.DeleteDHCPOptions(ls, kubeovnv1.ProtocolIPv6); err != nil {
			klog.Errorf("delete dhcp options for switch %s protocol %s failed: %v", ls, kubeovnv1.ProtocolIPv6, err)
			return "", err
		}
	}

	return
}

func (c *LegacyClient) UpdateDHCPOptions(ls, cidrBlock, gateway, dhcpV4OptionsStr, dhcpV6OptionsStr string, enableDHCP bool) (dhcpOptionsUUIDs *DHCPOptionsUUIDs, err error) {
	dhcpOptionsUUIDs = &DHCPOptionsUUIDs{}
	if enableDHCP {
		var v4CIDR, v6CIDR string
		var v4Gateway string
		switch util.CheckProtocol(cidrBlock) {
		case kubeovnv1.ProtocolIPv4:
			v4CIDR = cidrBlock
			v4Gateway = gateway
		case kubeovnv1.ProtocolIPv6:
			v6CIDR = cidrBlock
		case kubeovnv1.ProtocolDual:
			cidrBlocks := strings.Split(cidrBlock, ",")
			gateways := strings.Split(gateway, ",")
			v4CIDR, v6CIDR = cidrBlocks[0], cidrBlocks[1]
			v4Gateway = gateways[0]
		}

		dhcpOptionsUUIDs.DHCPv4OptionsUUID, err = c.updateDHCPv4Options(ls, v4CIDR, v4Gateway, dhcpV4OptionsStr)
		if err != nil {
			klog.Errorf("update dhcp options for switch %s failed: %v", ls, err)
			return nil, err
		}
		dhcpOptionsUUIDs.DHCPv6OptionsUUID, err = c.updateDHCPv6Options(ls, v6CIDR, dhcpV6OptionsStr)
		if err != nil {
			klog.Errorf("update dhcp options for switch %s failed: %v", ls, err)
			return nil, err
		}

	} else {
		if err = c.DeleteDHCPOptions(ls, kubeovnv1.ProtocolDual); err != nil {
			klog.Errorf("delete dhcp options for switch %s failed: %v", ls, err)
			return nil, err
		}
	}
	return dhcpOptionsUUIDs, nil
}

func (c *LegacyClient) DeleteDHCPOptionsByUUIDs(uuidList []string) (err error) {
	for _, uuid := range uuidList {
		_, err = c.ovnNbCommand("dhcp-options-del", uuid)
		if err != nil {
			klog.Errorf("delete dhcp options %s failed: %v", uuid, err)
			return err
		}
	}
	return nil
}

func (c *LegacyClient) DeleteDHCPOptions(ls string, protocol string) error {
	klog.Infof("delete dhcp options for switch %s protocol %s", ls, protocol)
	dhcpOptionsList, err := c.ListDHCPOptions(true, ls, protocol)
	if err != nil {
		klog.Errorf("find dhcp options failed, %v", err)
		return err
	}
	uuidToDeleteList := []string{}
	for _, item := range dhcpOptionsList {
		uuidToDeleteList = append(uuidToDeleteList, item.UUID)
	}

	return c.DeleteDHCPOptionsByUUIDs(uuidToDeleteList)
}

func (c LegacyClient) DeleteSubnetACL(ls string) error {
	results, err := c.CustomFindEntity("acl", []string{"direction", "priority", "match"}, fmt.Sprintf("external_ids:subnet=\"%s\"", ls))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return err
	}
	if len(results) == 0 {
		return nil
	}

	for _, result := range results {
		aclArgs := []string{"acl-del", ls}
		aclArgs = append(aclArgs, result["direction"][0], result["priority"][0], result["match"][0])

		_, err := c.ovnNbCommand(aclArgs...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c LegacyClient) UpdateSubnetACL(ls string, acls []kubeovnv1.Acl) error {
	if err := c.DeleteSubnetACL(ls); err != nil {
		klog.Errorf("failed to delete acls for subnet %s, %v", ls, err)
		return err
	}
	if len(acls) == 0 {
		return nil
	}

	for _, acl := range acls {
		aclArgs := []string{}
		aclArgs = append(aclArgs, "--", MayExist, "acl-add", ls, acl.Direction, strconv.Itoa(acl.Priority), acl.Match, acl.Action)
		_, err := c.ovnNbCommand(aclArgs...)
		if err != nil {
			klog.Errorf("failed to create acl for subnet %s, %v", ls, err)
			return err
		}

		results, err := c.CustomFindEntity("acl", []string{"_uuid"}, fmt.Sprintf("priority=%d", acl.Priority), fmt.Sprintf("direction=%s", acl.Direction), fmt.Sprintf("match=\"%s\"", acl.Match))
		if err != nil {
			klog.Errorf("customFindEntity failed, %v", err)
			return err
		}
		if len(results) == 0 {
			return nil
		}

		uuid := results[0]["_uuid"][0]
		ovnCmd := []string{"set", "acl", uuid}
		ovnCmd = append(ovnCmd, fmt.Sprintf("external_ids:subnet=\"%s\"", ls))

		if _, err := c.ovnNbCommand(ovnCmd...); err != nil {
			return fmt.Errorf("failed to set acl externalIds for subnet %s, %v", ls, err)
		}
	}
	return nil
}

func (c *LegacyClient) GetLspExternalIds(lsp string) map[string]string {
	result, err := c.CustomFindEntity("Logical_Switch_Port", []string{"external_ids"}, fmt.Sprintf("name=%s", lsp))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return nil
	}
	if len(result) == 0 {
		return nil
	}

	nameNsMap := make(map[string]string, 1)
	for _, l := range result[0]["external_ids"] {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), "=")
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != "pod" {
			continue
		}

		podInfo := strings.Split(strings.TrimSpace(parts[1]), "/")
		if len(podInfo) != 2 {
			continue
		}
		podNs := podInfo[0]
		podName := podInfo[1]
		nameNsMap[podName] = podNs
	}

	return nameNsMap
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

func (c *LegacyClient) GetNatIPInfo(uuid string) (string, error) {
	var logical_ip string

	output, err := c.ovnNbCommand("--data=bare", "--format=csv", "--no-heading", "--columns=logical_ip", "list", "nat", uuid)
	if err != nil {
		klog.Errorf("failed to list nat, %v", err)
		return logical_ip, err
	}
	lines := strings.Split(output, "\n")

	if len(lines) > 0 {
		logical_ip = strings.TrimSpace(lines[0])
	}
	return logical_ip, nil
}

// FindBfd find ovn bfd uuid by lrp name and dst ip
func (c LegacyClient) FindBfd(lrp, dstIp string) (string, error) {
	var err error
	var output string
	if dstIp != "" {
		output, err = c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
			"find", "bfd", fmt.Sprintf("logical_port=%s", lrp), fmt.Sprintf("dst_ip=%s", dstIp))
	} else {
		output, err = c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
			"find", "bfd", fmt.Sprintf("logical_port=%s", lrp))
	}
	if err != nil {
		klog.Errorf("faild to find bfd by lrp %s dst ip %s, %v", lrp, dstIp, err)
		return "", err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		result = append(result, strings.TrimSpace(l))
	}

	if len(result) == 0 {
		err := fmt.Errorf("not found bfd entries by lrp %s, dstIp %s", lrp, dstIp)
		klog.Error(err)
		return "", err
	}

	if len(result) > 1 {
		err := fmt.Errorf("found too many bfd entries, %v", result)
		klog.Error(err)
		return "", err
	}

	return result[0], nil
}

// CreateBfd find ovn bfd uuid by lrp name and dst ip
func (c LegacyClient) CreateBfd(lrp, dstIp string, minTx, minRx, detectMult int) (string, error) {
	var err error
	var output string
	if bfdId, err := c.FindBfd(lrp, dstIp); err == nil {
		return bfdId, nil
	}
	if output, err = c.ovnNbCommand("create", "bfd", fmt.Sprintf("logical_port=%s", lrp), fmt.Sprintf("dst_ip=%s", dstIp),
		fmt.Sprintf("min_tx=%d ", minTx), fmt.Sprintf("min_rx=%d", minRx), fmt.Sprintf("detect_mult=%d", detectMult)); err != nil {
		klog.Errorf("faild to create bfd for lrp %s dst ip %s, output %s, %v", lrp, dstIp, output, err)
		return "", err
	}
	if bfdId, err := c.FindBfd(lrp, dstIp); err == nil {
		return bfdId, nil
	} else {
		klog.Errorf("faild to create bfd for lrp %s dst ip %s, output %s, %v", lrp, dstIp, output, err)
		return "", err
	}
}

// DeleteBfd delete ovn bfd uuid by lrp name, dstIp
func (c LegacyClient) DeleteBfd(lrp, dstIp string) error {
	var err error
	var output string
	if dstIp != "" {
		// delete one specific bfd with lrp and dst ip
		output, err = c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
			"find", "bfd", fmt.Sprintf("logical_port=%s", lrp), fmt.Sprintf("dst_ip=%s", dstIp))
	} else {
		// delete all bfds with specific lrp
		output, err = c.ovnNbCommand("--data=bare", "--no-heading", "--columns=_uuid",
			"find", "bfd", fmt.Sprintf("logical_port=%s", lrp))
	}
	if err != nil {
		klog.Errorf("no bfd about lrp %s dst ip %s, %v", lrp, dstIp, err)
		return err
	}
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		bfdId := strings.TrimSpace(l)
		if len(bfdId) == 0 {
			continue
		}
		if _, err = c.ovnNbCommand("destroy", "bfd", bfdId); err != nil {
			klog.Errorf("faild to destroy bfd %s, %v", output, err)
			return err
		}
	}
	return nil
}
