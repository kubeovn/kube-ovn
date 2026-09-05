package ovn_ic_controller

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	icEnabled = "unknown"
	lastIcCm  map[string]string
	lastTSs   []string
	curTSs    []string
)

func (c *Controller) disableOVNIC(azName string) error {
	if err := c.removeInterConnection(azName); err != nil {
		klog.Errorf("failed to remove ovn-ic: %v", err)
		return err
	}
	if err := c.delLearnedRoute(); err != nil {
		klog.Errorf("failed to remove learned static routes: %v", err)
		return err
	}

	if err := c.RemoveOldChassisInSbDB(azName); err != nil {
		klog.Errorf("failed to remove remote chassis for az %q: %v", azName, err)
		return err
	}
	return nil
}

func (c *Controller) setAutoRoute(autoRoute bool) error {
	var blackList []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets, %v", err)
		return err
	}
	for _, subnet := range subnets {
		if subnet.Spec.DisableInterConnection || subnet.Name == c.config.NodeSwitch {
			blackList = append(blackList, subnet.Spec.CIDRBlock)
		}
	}
	if autoRoute {
		blackList = append(blackList, c.conflictCIDRList()...)
	} else if c.icConflictCIDRs != nil {
		c.icConflictCIDRs.Clear()
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list node, %v", err)
		return err
	}
	for _, node := range nodes {
		ipv4, ipv6 := util.GetNodeInternalIP(*node)
		if ipv4 != "" {
			blackList = append(blackList, ipv4)
		}
		if ipv6 != "" {
			blackList = append(blackList, ipv6)
		}
	}
	if err := c.setICAutoRouteTable(autoRoute, blackList); err != nil {
		klog.Errorf("failed to config auto route, %v", err)
		return err
	}

	return nil
}

func (c *Controller) DeleteICResources(azName string) error {
	icTSs := make([]string, 0)
	if err := c.deleteICLogicalSwitchPorts(func(lsp *ovnnb.LogicalSwitchPort) bool {
		// add the code below because azName may have multi "-"
		firstIndex := strings.Index(lsp.Name, "-")
		if firstIndex != -1 {
			firstPart := lsp.Name[:firstIndex]
			secondPart := lsp.Name[firstIndex+1:]
			needDelete := secondPart == azName && strings.HasPrefix(firstPart, util.InterconnectionSwitch)
			if needDelete {
				icTSs = append(icTSs, firstPart)
			}
			return needDelete
		}
		return false
	}); err != nil {
		return err
	}

	if err := c.deleteICLogicalRouterPorts(func(lrp *ovnnb.LogicalRouterPort) bool {
		lastIndex := strings.LastIndex(lrp.Name, "-")
		if lastIndex != -1 {
			firstPart := lrp.Name[:lastIndex]
			secondPart := lrp.Name[lastIndex+1:]
			return firstPart == azName && strings.HasPrefix(secondPart, util.InterconnectionSwitch)
		}
		return false
	}); err != nil {
		return err
	}

	for _, icTS := range icTSs {
		if err := c.deleteICLogicalSwitch(icTS); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) getICState(cmData, lastcmData map[string]string) int {
	state := classifyICConfig(icEnabled, lastcmData, cmData)
	if state != icNoAction {
		return state
	}

	c.ovnLegacyClient.OvnICNbAddress = genHostAddress(cmData["ic-db-host"], cmData["ic-nb-port"])
	var err error
	curTSs, err = c.ovnLegacyClient.GetTs()
	if err != nil {
		klog.Errorf("failed to get Transit_Switch, %v", err)
		return icNoAction
	}
	if slices.Equal(lastTSs, curTSs) {
		return icNoAction
	}
	return icConfigChange
}

func (c *Controller) resyncInterConnection() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.InterconnectionConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-ic-config, %v", err)
		return
	}

	if k8serrors.IsNotFound(err) {
		c.disableInterConnection(nil)
		return
	}
	if cm.Data["enable-ic"] == "false" {
		c.disableInterConnection(cm.Data)
		return
	}
	c.reconcileInterConnection(cm.Data)
}

func (c *Controller) disableInterConnection(config map[string]string) {
	if icEnabled == "false" {
		return
	}
	klog.Info("start to remove ovn-ic")
	if config == nil {
		config = lastIcCm
	}
	if config != nil && config["ic-db-host"] != "" {
		c.ovnLegacyClient.OvnICSbAddress = genHostAddress(config["ic-db-host"], config["ic-sb-port"])
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(config["ic-db-host"], config["ic-nb-port"])
	}
	if err := c.setAutoRoute(false); err != nil {
		klog.Errorf("failed to disable auto route: %v", err)
		return
	}
	azName := ""
	if config != nil {
		azName = config["az-name"]
	}
	if err := c.disableOVNIC(azName); err != nil {
		klog.Errorf("Disable az %s OVN IC failed: %v", azName, err)
		return
	}
	icEnabled = "false"
	lastIcCm = nil
	klog.Info("finish removing ovn-ic")
}

func (c *Controller) reconcileInterConnection(config map[string]string) {
	autoRoute := config["auto-route"] == "true"
	if autoRoute {
		if err := c.refreshConflictCIDRs(); err != nil {
			klog.Errorf("failed to refresh conflicting learned routes: %v", err)
			return
		}
	}
	if err := c.setAutoRoute(autoRoute); err != nil {
		klog.Errorf("failed to set auto route: %v", err)
		return
	}

	switch c.getICState(config, lastIcCm) {
	case icNoAction:
		return
	case icFirstEstablish:
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(config["ic-db-host"], config["ic-nb-port"])
		klog.Info("start to establish ovn-ic")
		if err := c.establishInterConnection(config); err != nil {
			klog.Errorf("failed to establish ovn-ic, %v", err)
			return
		}
		curTSs, err := c.ovnLegacyClient.GetTs()
		if err != nil {
			klog.Errorf("failed to get Transit_Switch, %v", err)
			return
		}
		icEnabled = "true"
		lastIcCm = cloneICConfig(config)
		lastTSs = curTSs
		klog.Info("finish establishing ovn-ic")
		return
	case icGatewayChange:
		c.ovnLegacyClient.OvnICSbAddress = genHostAddress(config["ic-db-host"], config["ic-sb-port"])
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(config["ic-db-host"], config["ic-nb-port"])
		klog.Info("start to reconcile ovn-ic gateways")
		if err := c.establishInterConnection(config); err != nil {
			klog.Errorf("failed to reconcile ovn-ic gateways: %v", err)
			return
		}
		icEnabled = "true"
		lastIcCm = cloneICConfig(config)
		klog.Info("finish reconciling ovn-ic gateways")
		return
	case icConfigChange:
		c.ovnLegacyClient.OvnICSbAddress = genHostAddress(lastIcCm["ic-db-host"], config["ic-sb-port"])
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(lastIcCm["ic-db-host"], config["ic-nb-port"])
		err := c.disableOVNIC(lastIcCm["az-name"])
		if err != nil {
			klog.Errorf("Disable az %s OVN IC failed: %v", lastIcCm["az-name"], err)
			return
		}
		klog.Info("start to reestablish ovn-ic")
		if err := c.establishInterConnection(config); err != nil {
			klog.Errorf("failed to reestablish ovn-ic, %v", err)
			return
		}

		icEnabled = "true"
		lastIcCm = cloneICConfig(config)
		lastTSs = curTSs
		klog.Info("finish reestablishing ovn-ic")
		return
	}
}

func (c *Controller) removeInterConnection(azName string) error {
	selector := labels.Set{util.ICGatewayLabel: "true"}.AsSelector()
	nodes, err := c.nodesLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}
	for _, node := range nodes {
		patch := util.KVPatch{util.ICGatewayLabel: "false"}
		if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
			klog.Errorf("failed to patch ic gw node %s: %v", node.Name, err)
			return err
		}
	}

	if err := c.stopOVNIC(); err != nil {
		klog.Errorf("failed to stop ovn-ic, %v", err)
		return err
	}

	if azName != "" {
		if err := c.DeleteICResources(azName); err != nil {
			klog.Errorf("failed to delete ovn-ic resource on az %s , %v", azName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) establishInterConnection(config map[string]string) error {
	if err := c.setICAzNameTable(config["az-name"]); err != nil {
		klog.Errorf("failed to set az name: %v", err)
		return err
	}

	if err := c.startOVNIC(config["ic-db-host"], config["ic-nb-port"], config["ic-sb-port"]); err != nil {
		klog.Errorf("failed to start ovn-ic: %v", err)
		return err
	}

	tsNames, err := c.ovnLegacyClient.GetTs()
	if err != nil {
		klog.Errorf("failed to list ic logical switch: %v", err)
		return err
	}

	sort.Strings(tsNames)

	gwNodes := parseGwNodes(config["gw-nodes"])
	if err := c.syncICGatewayNodeLabels(gwNodes); err != nil {
		return err
	}

	for i, tsName := range tsNames {
		tsPort := fmt.Sprintf("%s-%s", tsName, config["az-name"])
		lrpName := fmt.Sprintf("%s-%s", config["az-name"], tsName)
		exist, err := c.logicalSwitchPortExistsTable(tsPort)
		if err != nil {
			klog.Errorf("failed to check logical switch port %q: %v", tsPort, err)
			return err
		}
		if exist {
			klog.Infof("ts port %s already exists, reconciling gateway chassis", tsPort)
			chassises, err := c.gatewayChassisNames(gwNodes, i)
			if err != nil {
				return err
			}
			if err := c.reconcileICGatewayChassises(lrpName, chassises); err != nil {
				klog.Errorf("failed to reconcile gateway chassis for ic lrp %q: %v", lrpName, err)
				return err
			}
			continue
		}
		if len(gwNodes) == 0 {
			continue
		}

		lrpAddr, err := c.acquireLrpAddress(tsName)
		if err != nil {
			klog.Errorf("failed to acquire lrp address for ts %q: %v", tsName, err)
			return err
		}

		chassises, err := c.gatewayChassisNames(gwNodes, i)
		if err != nil {
			return err
		}
		if err := c.createICLogicalPatchPort(tsName, c.config.ClusterRouter, tsPort, lrpName, lrpAddr, util.GenerateMac(), chassises...); err != nil {
			klog.Errorf("failed to create ovn-ic lrp %q: %v", lrpName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) gatewayChassisNames(gwNodes []string, order int) ([]string, error) {
	ordered := generateNewOrderGwNodes(gwNodes, order)
	chassises := make([]string, len(ordered))
	for i, gw := range ordered {
		chassis, err := c.getICChassisByHost(gw)
		if err != nil {
			klog.Errorf("failed to get gw %q chassis: %v", gw, err)
			return nil, err
		}
		if chassis.Name == "" {
			return nil, fmt.Errorf("no chassis for gw %q", gw)
		}
		chassises[i] = chassis.Name

		node, err := c.nodesLister.Get(gw)
		if err != nil {
			klog.Errorf("failed to get gw node %q: %v", gw, err)
			return nil, err
		}
		patch := util.KVPatch{util.ICGatewayLabel: "true"}
		if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
			klog.Errorf("failed to patch ic gw node %s: %v", node.Name, err)
			return nil, err
		}
	}
	return chassises, nil
}

func (c *Controller) acquireLrpAddress(ts string) (string, error) {
	cidr, err := c.ovnLegacyClient.GetTsSubnet(ts)
	if err != nil {
		klog.Errorf("failed to get ts subnet %s: %v", ts, err)
		return "", err
	}
	existAddress, err := c.listRemoteLogicalSwitchPortAddress()
	if err != nil {
		klog.Errorf("failed to list remote port address: %v", err)
		return "", err
	}

	for {
		var random string
		var ips []string
		v4Cidr, v6Cidr := util.SplitStringIP(cidr)
		if v4Cidr != "" {
			ips = append(ips, util.GenerateRandomIP(v4Cidr))
		}
		if v6Cidr != "" {
			ips = append(ips, util.GenerateRandomIP(v6Cidr))
		}
		random = strings.Join(ips, ",")
		// find a free address
		if !existAddress.Has(random) {
			return random, nil
		}
		klog.Infof("random ip %s already exists", random)
		time.Sleep(time.Second)
	}
}

func (c *Controller) startOVNIC(icHost, icNbPort, icSbPort string) error {
	// #nosec G204
	cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl",
		"--ovn-ic-nb-db="+genHostAddress(icHost, icNbPort),
		"--ovn-ic-sb-db="+genHostAddress(icHost, icSbPort),
		"--ovn-northd-nb-db="+c.config.OvnNbAddr,
		"--ovn-northd-sb-db="+c.config.OvnSbAddr,
		"start_ic")
	if os.Getenv(util.EnvSSLEnabled) == "true" {
		// #nosec G204
		cmd = exec.Command("/usr/share/ovn/scripts/ovn-ctl",
			"--ovn-ic-nb-db="+genHostAddress(icHost, icNbPort),
			"--ovn-ic-sb-db="+genHostAddress(icHost, icSbPort),
			"--ovn-northd-nb-db="+c.config.OvnNbAddr,
			"--ovn-northd-sb-db="+c.config.OvnSbAddr,
			"--ovn-ic-ssl-key=/var/run/tls/key",
			"--ovn-ic-ssl-cert=/var/run/tls/cert",
			"--ovn-ic-ssl-ca-cert=/var/run/tls/cacert",
			"start_ic")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("output: %s, err: %w", output, err)
	}
	return nil
}

func (c *Controller) stopOVNIC() error {
	cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "stop_ic")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("output: %s, err: %w", output, err)
	}
	return nil
}

func (c *Controller) delLearnedRoute() error {
	lrList, err := c.listICLogicalRouters()
	if err != nil {
		klog.Errorf("failed to list logical routers: %v", err)
		return err
	}
	for _, lr := range lrList {
		routes, routeErr := c.listICRoutes(&lr, func(route *ovnnb.LogicalRouterStaticRoute) bool {
			return route.ExternalIDs["ic-learned-route"] != ""
		})
		if routeErr != nil {
			return routeErr
		}
		if err = c.deleteICRoutes(&lr, routes); err != nil {
			klog.Errorf("failed to delete learned static routes on logical router %s: %v", lr.Name, err)
			return err
		}
	}

	klog.V(5).Infof("finish removing learned routes")
	return nil
}

func genHostAddress(host, port string) (hostAddress string) {
	hostList := strings.Split(strings.Trim(host, ","), ",")
	if len(hostList) == 1 {
		hostAddress = fmt.Sprintf("tcp:[%s]:%s", hostList[0], port)
	} else {
		var builder strings.Builder
		i := 0
		for i < len(hostList)-1 {
			fmt.Fprintf(&builder, "tcp:[%s]:%s,", hostList[i], port)
			i++
		}
		fmt.Fprintf(&builder, "tcp:[%s]:%s", hostList[i], port)
		hostAddress = builder.String()
	}
	return hostAddress
}

func (c *Controller) SynRouteToPolicy() {
	c.syncOneRouteToPolicy(util.OvnICKey, util.OvnICConnected)
	c.syncOneRouteToPolicy(util.OvnICKey, util.OvnICStatic)
	// To support the version before kube-ovn v1.9, in which version the option tag is origin=""
	c.syncOneRouteToPolicy(util.OvnICKey, util.OvnICNone)
}

func (c *Controller) RemoveOldChassisInSbDB(azName string) error {
	if azName == "" {
		return nil
	}

	azUUID, err := c.ovnLegacyClient.GetAzUUID(azName)
	if err != nil {
		klog.Errorf("failed to get UUID of AZ %s: %v", lastIcCm["az-name"], err)
		return err
	}

	if azUUID == "" {
		klog.Infof("%s have already been deleted", azName)
		return nil
	}

	gateways, err := c.ovnLegacyClient.GetGatewayUUIDsInOneAZ(azUUID)
	if err != nil {
		klog.Errorf("failed to get gateway UUIDs in AZ %s: %v", azUUID, err)
		return err
	}

	routes, err := c.ovnLegacyClient.GetRouteUUIDsInOneAZ(azUUID)
	if err != nil {
		klog.Errorf("failed to get route UUIDs in AZ %s: %v", azUUID, err)
		return err
	}

	portBindings, err := c.ovnLegacyClient.GetPortBindingUUIDsInOneAZ(azUUID)
	if err != nil {
		klog.Errorf("failed to get Port_Binding UUIDs in AZ %s: %v", azUUID, err)
		return err
	}

	if err := c.ovnLegacyClient.DestroyPortBindings(portBindings); err != nil {
		return err
	}

	if err := c.ovnLegacyClient.DestroyGateways(gateways); err != nil {
		return err
	}

	if err := c.ovnLegacyClient.DestroyRoutes(routes); err != nil {
		return err
	}

	return c.ovnLegacyClient.DestroyChassis(azUUID)
}

func stripPrefix(policyMatch string) (string, error) {
	matches := strings.Split(policyMatch, "==")

	switch {
	case strings.Trim(matches[0], " ") == util.MatchV4Dst:
		return strings.Trim(matches[1], " "), nil
	case strings.Trim(matches[0], " ") == util.MatchV6Dst:
		return strings.Trim(matches[1], " "), nil
	default:
		return "", fmt.Errorf("policy %s is mismatched", policyMatch)
	}
}

func (c *Controller) syncOneRouteToPolicy(key, value string) {
	lr, err := c.getICLogicalRouter(c.config.ClusterRouter)
	if err != nil {
		klog.Infof("logical router %s is not ready at %v", util.DefaultVpc, time.Now())
		return
	}
	lrRouteList, err := c.listICRoutes(lr, func(route *ovnnb.LogicalRouterStaticRoute) bool {
		return route.RouteTable == util.MainRouteTable && route.Options[key] == value
	})
	if err != nil {
		klog.Errorf("failed to list lr ovn-ic route %v", err)
		return
	}

	lrPolicyList, err := c.listICPolicies(lr, func(policy *ovnnb.LogicalRouterPolicy) bool {
		return policy.ExternalIDs[key] == value
	})
	if err != nil {
		klog.Errorf("failed to list ovn-ic lr policy: %v", err)
		return
	}

	if len(lrRouteList) == 0 {
		klog.V(5).Info("lr ovn-ic route does not exist")
		err := c.deleteICPolicies(lr, util.OvnICPolicyPriority, key, value)
		if err != nil {
			klog.Errorf("failed to delete ovn-ic lr policy: %v", err)
			return
		}
		return
	}

	policyMap := map[string]string{}

	for _, lrPolicy := range lrPolicyList {
		match, err := stripPrefix(lrPolicy.Match)
		if err != nil {
			klog.Errorf("policy match abnormal: %v", err)
			continue
		}
		policyMap[match] = lrPolicy.UUID
	}
	networks := strset.NewWithSize(len(lrRouteList))
	for _, lrRoute := range lrRouteList {
		networks.Add(lrRoute.IPPrefix)
	}

	networks.Each(func(prefix string) bool {
		if _, ok := policyMap[prefix]; ok {
			delete(policyMap, prefix)
			return true
		}
		match := util.MatchV4Dst + " == " + prefix
		if util.CheckProtocol(prefix) == kubeovnv1.ProtocolIPv6 {
			match = util.MatchV6Dst + " == " + prefix
		}

		if err = c.addICPolicy(lr, &ovnnb.LogicalRouterPolicy{Priority: util.OvnICPolicyPriority, Match: match, Action: ovnnb.LogicalRouterPolicyActionAllow, ExternalIDs: map[string]string{key: value, "vendor": util.CniTypeName}}); err != nil {
			klog.Errorf("failed to add router policy: %v", err)
		}

		return true
	})
	for _, uuid := range policyMap {
		if err := c.deleteICPolicyUUID(lr, uuid); err != nil {
			klog.Errorf("deleting router policy failed %v", err)
		}
	}
}

func (c *Controller) listRemoteLogicalSwitchPortAddress() (*strset.Set, error) {
	lsps, err := c.listICRemoteLogicalSwitchPortAddresses()
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list remote logical switch ports: %w", err)
	}

	existAddress := strset.NewWithSize(len(lsps))
	for _, lsp := range lsps {
		if len(lsp.Addresses) == 0 {
			continue
		}

		fields := strings.Fields(lsp.Addresses[0])
		if len(fields) != 2 {
			continue
		}

		existAddress.Add(fields[1])
	}

	return existAddress, nil
}

func generateNewOrderGwNodes(arr []string, order int) []string {
	if len(arr) == 0 {
		return nil
	}
	if order >= len(arr) {
		order %= len(arr)
	}

	return append(arr[order:], arr[:order]...)
}

func (c *Controller) conflictCIDRList() []string {
	if c == nil || c.icConflictCIDRs == nil {
		return nil
	}
	list := c.icConflictCIDRs.List()
	sort.Strings(list)
	return list
}

func (c *Controller) refreshConflictCIDRs() error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets, %v", err)
		return err
	}
	localCIDRs := subnetCIDRs(subnets)
	persisted, err := c.persistedConflictCIDRs(localCIDRs)
	if err != nil {
		return err
	}

	lrList, err := c.listICLogicalRouters()
	if err != nil {
		klog.Errorf("failed to list logical routers: %v", err)
		return err
	}

	learnedPrefixes := make([]string, 0)
	type learnedRoute struct {
		lrName string
		uuid   string
		prefix string
	}
	var learned []learnedRoute
	for _, lr := range lrList {
		routeList, err := c.listICRoutes(&lr, func(route *ovnnb.LogicalRouterStaticRoute) bool {
			return route.ExternalIDs["ic-learned-route"] != ""
		})
		if err != nil {
			klog.Errorf("failed to list learned static routes on logical router %s: %v", lr.Name, err)
			return err
		}
		for _, route := range routeList {
			learnedPrefixes = append(learnedPrefixes, route.IPPrefix)
			learned = append(learned, learnedRoute{lrName: lr.Name, uuid: route.UUID, prefix: route.IPPrefix})
		}
	}

	sticky := append(c.conflictCIDRList(), persisted...)
	conflicts := mergeConflictCIDRs(localCIDRs, learnedPrefixes, sticky)
	if c.icConflictCIDRs == nil {
		c.icConflictCIDRs = strset.New()
	}
	c.icConflictCIDRs.Clear()
	if len(conflicts) > 0 {
		c.icConflictCIDRs.Add(conflicts...)
		klog.Infof("detected ovn-ic cidr conflicts %v, adding them to ic-route-blacklist", conflicts)
	}

	conflictSet := strset.New(conflicts...)
	for _, route := range learned {
		if !conflictSet.Has(route.prefix) {
			matched := false
			for _, cidr := range conflicts {
				if util.CIDROverlap(cidr, route.prefix) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		lr, getErr := c.getICLogicalRouter(route.lrName)
		if getErr != nil {
			return getErr
		}
		if err := c.deleteICRoutes(lr, []*ovnnb.LogicalRouterStaticRoute{{UUID: route.uuid}}); err != nil {
			klog.Errorf("failed to delete conflicting learned route %s on logical router %s: %v", route.prefix, route.lrName, err)
			return err
		}
		klog.Infof("deleted conflicting learned route %s on logical router %s", route.prefix, route.lrName)
	}
	return nil
}

func (c *Controller) persistedConflictCIDRs(localCIDRs []string) ([]string, error) {
	nbGlobal, err := c.getNBGlobalTable()
	if err != nil {
		klog.Errorf("failed to get NB_Global while restoring IC conflicts: %v", err)
		return nil, err
	}

	return filterPersistedConflictCIDRs(localCIDRs, nbGlobal.Options["ic-route-blacklist"]), nil
}

func (c *Controller) syncICGatewayNodeLabels(gwNodes []string) error {
	desired := strset.New(gwNodes...)
	selector := labels.Set{util.ICGatewayLabel: "true"}.AsSelector()
	nodes, err := c.nodesLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list ic gateway nodes, %v", err)
		return err
	}
	for _, node := range nodes {
		if desired.Has(node.Name) {
			continue
		}
		patch := util.KVPatch{util.ICGatewayLabel: "false"}
		if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
			klog.Errorf("failed to patch ic gw node %s: %v", node.Name, err)
			return err
		}
	}
	return nil
}
