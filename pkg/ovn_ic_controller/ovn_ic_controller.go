package ovn_ic_controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	icEnabled = "unknown"
	lastIcCm  map[string]string
	lastTSs   []string
	curTSs    []string
)

const (
	icNoAction = iota
	icFirstEstablish
	icConfigChange
)

func (c *Controller) disableOVNIC(azName string) error {
	if err := c.removeInterConnection(azName); err != nil {
		klog.Errorf("failed to remove ovn-ic, %v", err)
		return err
	}
	if err := c.delLearnedRoute(); err != nil {
		klog.Errorf("failed to remove learned static routes, %v", err)
		return err
	}

	if err := c.RemoveOldChassisInSbDB(azName); err != nil {
		klog.Errorf("failed to remove remote chassis: %v", err)
		return err
	}
	return nil
}

func (c *Controller) setAutoRoute(autoRoute bool) {

	var blackList []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets, %v", err)
		return
	}
	for _, subnet := range subnets {
		if subnet.Spec.DisableInterConnection || subnet.Name == c.config.NodeSwitch {
			blackList = append(blackList, subnet.Spec.CIDRBlock)
		}
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list node, %v", err)
		return
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
	if err := c.ovnLegacyClient.SetICAutoRoute(autoRoute, blackList); err != nil {
		klog.Errorf("failed to config auto route, %v", err)
		return
	}
}

func (c *Controller) getICState(cmData, lastcmData map[string]string) int {
	isCMEqual := reflect.DeepEqual(cmData, lastcmData)
	if icEnabled != "true" && len(lastcmData) == 0 && cmData["enable-ic"] == "true" {
		return icFirstEstablish
	}

	if icEnabled == "true" && lastcmData != nil && isCMEqual {
		var err error
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(cmData["ic-db-host"], cmData["ic-nb-port"])
		curTSs, err = c.ovnLegacyClient.GetTs()
		if err != nil {
			klog.Errorf("failed to get Transit_Switch, %v", err)
			return icNoAction
		}
		isTsEqual := reflect.DeepEqual(lastTSs, curTSs)
		if isTsEqual {
			return icNoAction
		}
	}
	return icConfigChange
}

func (c *Controller) resyncInterConnection() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.InterconnectionConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-ic-config, %v", err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-ic"] == "false" {
		if icEnabled == "false" {
			return
		}
		klog.Info("start to remove ovn-ic")
		var azName, icDBHost, icSBPort, icNBPort string
		if cm != nil {
			azName = cm.Data["az-name"]
			icDBHost = cm.Data["ic-db-host"]
			icSBPort = cm.Data["ic-sb-port"]
			icNBPort = cm.Data["ic-nb-port"]
		} else if lastIcCm != nil {
			azName = lastIcCm["az-name"]
			icDBHost = lastIcCm["ic-db-host"]
			icSBPort = lastIcCm["ic-sb-port"]
			icNBPort = lastIcCm["ic-nb-port"]
		}

		if icDBHost != "" {
			c.ovnLegacyClient.OvnICSbAddress = genHostAddress(icDBHost, icSBPort)
			c.ovnLegacyClient.OvnICNbAddress = genHostAddress(icDBHost, icNBPort)
		}

		err := c.disableOVNIC(azName)
		if err != nil {
			klog.Errorf("Disable az %s OVN IC failed ", azName)
			return
		}
		icEnabled = "false"
		lastIcCm = nil

		klog.Info("finish removing ovn-ic")
		return
	}

	autoRoute := false
	if cm.Data["auto-route"] == "true" {
		autoRoute = true
	}
	c.setAutoRoute(autoRoute)

	switch c.getICState(cm.Data, lastIcCm) {
	case icNoAction:
		return
	case icFirstEstablish:
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(cm.Data["ic-db-host"], cm.Data["ic-nb-port"])
		klog.Info("start to establish ovn-ic")
		if err := c.establishInterConnection(cm.Data); err != nil {
			klog.Errorf("failed to establish ovn-ic, %v", err)
			return
		}
		curTSs, err := c.ovnLegacyClient.GetTs()
		if err != nil {
			klog.Errorf("failed to get Transit_Switch, %v", err)
			return
		}
		icEnabled = "true"
		lastIcCm = cm.Data
		lastTSs = curTSs
		klog.Info("finish establishing ovn-ic")
		return
	case icConfigChange:
		c.ovnLegacyClient.OvnICSbAddress = genHostAddress(lastIcCm["ic-db-host"], cm.Data["ic-sb-port"])
		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(lastIcCm["ic-db-host"], cm.Data["ic-nb-port"])
		err := c.disableOVNIC(lastIcCm["az-name"])
		if err != nil {
			klog.Errorf("Disable az %s OVN IC failed ", lastIcCm["az-name"])
			return
		}
		klog.Info("start to reestablish ovn-ic")
		if err := c.establishInterConnection(cm.Data); err != nil {
			klog.Errorf("failed to reestablish ovn-ic, %v", err)
			return
		}

		icEnabled = "true"
		lastIcCm = cm.Data
		lastTSs = curTSs
		klog.Info("finish reestablishing ovn-ic")
		return
	}
}

func (c *Controller) removeInterConnection(azName string) error {
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{util.ICGatewayLabel: "true"}})
	nodes, err := c.nodesLister.List(sel)
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}
	for _, cachedNode := range nodes {
		no := cachedNode.DeepCopy()
		patchPayloadTemplate := `[{
        "op": "%s",
        "path": "/metadata/labels",
        "value": %s
    	}]`
		op := "replace"
		if len(no.Labels) == 0 {
			op = "add"
		}
		no.Labels[util.ICGatewayLabel] = "false"
		raw, _ := json.Marshal(no.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), no.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
		if err != nil {
			klog.Errorf("patch ic gw node %s failed %v", no.Name, err)
			return err
		}
	}

	if err := c.stopOVNIC(); err != nil {
		klog.Errorf("failed to stop ovn-ic, %v", err)
		return err
	}

	if azName != "" {
		if err := c.ovnLegacyClient.DeleteICResources(azName); err != nil {
			klog.Errorf("failed to delete ovn-ic resource on az %s , %v", azName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) establishInterConnection(config map[string]string) error {
	if err := c.ovnLegacyClient.SetAzName(config["az-name"]); err != nil {
		klog.Errorf("failed to set az name. %v", err)
		return err
	}

	if err := c.startOVNIC(config["ic-db-host"], config["ic-nb-port"], config["ic-sb-port"]); err != nil {
		klog.Errorf("failed to start ovn-ic, %v", err)
		return err
	}

	tsNames, err := c.ovnLegacyClient.GetTs()
	if err != nil {
		klog.Errorf("failed to list ic logical switch. %v ", err)
		return err
	}

	sort.Strings(tsNames)

	gwNodes := strings.Split(config["gw-nodes"], ",")
	chassises := make([]string, len(gwNodes))

	for i, tsName := range tsNames {
		gwNodesOrdered := generateNewOrdergwNodes(gwNodes, i)
		for j, gw := range gwNodesOrdered {
			gw = strings.TrimSpace(gw)
			chassisID, err := c.ovnLegacyClient.GetChassis(gw)
			if err != nil {
				klog.Errorf("failed to get gw %s chassisID: %v", gw, err)
				return err
			}
			if chassisID == "" {
				return fmt.Errorf("no chassisID for gw %s", gw)
			}
			chassises[j] = chassisID

			cachedNode, err := c.nodesLister.Get(gw)
			if err != nil {
				klog.Errorf("failed to get gw node %s, %v", gw, err)
				return err
			}
			node := cachedNode.DeepCopy()
			patchPayloadTemplate := `[{
			"op": "%s",
			"path": "/metadata/labels",
			"value": %s
			}]`
			op := "replace"
			if len(node.Labels) == 0 {
				op = "add"
			}
			node.Labels[util.ICGatewayLabel] = "true"
			raw, _ := json.Marshal(node.Labels)
			patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
			_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), gw, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
			if err != nil {
				klog.Errorf("patch gw node %s failed %v", gw, err)
				return err
			}
		}

		tsPort := fmt.Sprintf("%s-%s", tsName, config["az-name"])
		exist, err := c.ovnLegacyClient.LogicalSwitchPortExists(tsPort)
		if err != nil {
			klog.Errorf("failed to list logical switch ports, %v", err)
			return err
		}
		if exist {
			klog.Infof("ts port %s already exists", tsPort)
			continue
		}

		lrpAddr, err := c.acquireLrpAddress(tsName)
		if err != nil {
			klog.Errorf("failed to acquire lrp address, %v", err)
			return err
		}

		if err = c.ovnLegacyClient.CreateICLogicalRouterPort(config["az-name"], tsName, util.GenerateMac(), lrpAddr, chassises); err != nil {
			klog.Errorf("failed to create ovn-ic lrp %v", err)
			return err
		}
	}

	return nil
}

func (c *Controller) acquireLrpAddress(ts string) (string, error) {
	cidr, err := c.ovnLegacyClient.GetTsSubnet(ts)
	if err != nil {
		klog.Errorf("failed to get ts subnet %s: %v", ts, err)
		return "", err
	}
	existAddress, err := c.ovnLegacyClient.ListRemoteLogicalSwitchPortAddress()
	if err != nil {
		klog.Errorf("failed to list remote port address, %v", err)
		return "", err
	}
	for {
		random := util.GenerateRandomV4IP(cidr)
		if !util.ContainsString(existAddress, random) {
			return random, nil
		}
		klog.Infof("random ip %s already exists", random)
		time.Sleep(1 * time.Second)
	}
}

func (c *Controller) startOVNIC(icHost, icNbPort, icSbPort string) error {
	cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl",
		fmt.Sprintf("--ovn-ic-nb-db=%s", genHostAddress(icHost, icNbPort)),
		fmt.Sprintf("--ovn-ic-sb-db=%s", genHostAddress(icHost, icSbPort)),
		fmt.Sprintf("--ovn-northd-nb-db=%s", c.config.OvnNbAddr),
		fmt.Sprintf("--ovn-northd-sb-db=%s", c.config.OvnSbAddr),
		"start_ic")
	if os.Getenv("ENABLE_SSL") == "true" {
		cmd = exec.Command("/usr/share/ovn/scripts/ovn-ctl",
			fmt.Sprintf("--ovn-ic-nb-db=%s", genHostAddress(icHost, icNbPort)),
			fmt.Sprintf("--ovn-ic-sb-db=%s", genHostAddress(icHost, icSbPort)),
			fmt.Sprintf("--ovn-northd-nb-db=%s", c.config.OvnNbAddr),
			fmt.Sprintf("--ovn-northd-sb-db=%s", c.config.OvnSbAddr),
			"--ovn-ic-ssl-key=/var/run/tls/key",
			"--ovn-ic-ssl-cert=/var/run/tls/cert",
			"--ovn-ic-ssl-ca-cert=/var/run/tls/cacert",
			"start_ic")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("output: %s, err: %v", output, err)
	}
	return nil
}

func (c *Controller) stopOVNIC() error {
	cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "stop_ic")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("output: %s, err: %v", output, err)
	}
	return nil
}

func (c *Controller) delLearnedRoute() error {
	originalPorts, err := c.ovnLegacyClient.CustomFindEntity("Logical_Router_Static_Route", []string{"_uuid", "ip_prefix"})
	if err != nil {
		klog.Errorf("failed to list static routes of logical router, %v", err)
		return err
	}
	filteredPorts, err := c.ovnLegacyClient.CustomFindEntity("Logical_Router_Static_Route", []string{"_uuid", "ip_prefix"}, "external_ids:ic-learned-route{<=}1")
	if err != nil {
		klog.Errorf("failed to filter static routes of logical router, %v", err)
		return err
	}
	learnedPorts := []map[string][]string{}
	for _, aOriPort := range originalPorts {
		isFiltered := false
		for _, aFtPort := range filteredPorts {
			if aFtPort["_uuid"][0] == aOriPort["_uuid"][0] {
				isFiltered = true
			}
		}
		if !isFiltered {
			learnedPorts = append(learnedPorts, aOriPort)
		}
	}
	if len(learnedPorts) != 0 {
		for _, aLdPort := range learnedPorts {
			itsRouter, err := c.ovnLegacyClient.CustomFindEntity("Logical_Router", []string{"name"}, fmt.Sprintf("static_routes{>}%s", aLdPort["_uuid"][0]))
			if err != nil {
				klog.Errorf("failed to list logical router of static route %s, %v", aLdPort["_uuid"][0], err)
				return err
			}

			if len(aLdPort["ip_prefix"]) != 0 && len(itsRouter) != 0 && len(itsRouter[0]["name"]) != 0 {
				if err := c.ovnLegacyClient.DeleteStaticRoute(aLdPort["ip_prefix"][0], itsRouter[0]["name"][0]); err != nil {
					klog.Errorf("failed to delete stale route %s, %v", aLdPort["ip_prefix"][0], err)
					return err
				}
			}
		}
		klog.V(5).Infof("finish removing learned routes")
	}
	return nil
}

func genHostAddress(host, port string) (hostAddress string) {
	hostList := strings.Split(host, ",")
	if len(hostList) == 1 {
		hostAddress = fmt.Sprintf("tcp:[%s]:%s", hostList[0], port)
	} else {
		var builder strings.Builder
		i := 0
		for i < len(hostList)-1 {
			builder.WriteString(fmt.Sprintf("tcp:[%s]:%s,", hostList[i], port))
			i++
		}
		builder.WriteString(fmt.Sprintf("tcp:[%s]:%s", hostList[i], port))
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
	if strings.Trim(matches[0], " ") == util.MatchV4Dst {
		return strings.Trim(matches[1], " "), nil
	} else {
		return "", fmt.Errorf("policy %s is mismatched", policyMatch)
	}
}

func (c *Controller) syncOneRouteToPolicy(key, value string) {
	lr, err := c.ovnClient.GetLogicalRouter(util.DefaultVpc, false)
	if err != nil {
		klog.Infof("logical router %s is not ready at %v", util.DefaultVpc, time.Now())
		return
	}
	lrRouteList, err := c.ovnClient.GetLogicalRouterRouteByOpts(key, value)
	if err != nil {
		klog.Errorf("failed to list lr ovn-ic route %v", err)
		return
	}

	lrPolicyList, err := c.ovnClient.GetLogicalRouterPoliciesByExtID(key, value)
	if err != nil {
		klog.Errorf("failed to list ovn-ic lr policy: %v", err)
		return
	}

	if len(lrRouteList) == 0 {
		klog.V(5).Info(" lr ovn-ic route does not exist")
		for _, lrPolicy := range lrPolicyList {
			if err := c.ovnClient.DeleteRouterPolicy(lr, lrPolicy.UUID); err != nil {
				klog.Errorf("deleting router policy failed %v", err)
			}
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
		if err := c.ovnClient.AddRouterPolicy(lr, match, ovnnb.LogicalRouterPolicyActionAllow,
			nil, map[string]string{key: value, "vendor": util.CniTypeName},
			util.OvnICPolicyPriority); err != nil {
			klog.Errorf("failed to add router policy: %v", err)
		}

		return true
	})
	for _, uuid := range policyMap {
		if err := c.ovnClient.DeleteRouterPolicy(lr, uuid); err != nil {
			klog.Errorf("deleting router policy failed %v", err)
		}
	}
}

func generateNewOrdergwNodes(arr []string, order int) []string {
	if order >= len(arr) {
		order = order % len(arr)
	}

	return append(arr[order:], arr[:order]...)
}
