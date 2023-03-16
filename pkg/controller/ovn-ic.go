package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	icEnabled = "unknown"
	lastIcCm  map[string]string
)

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
		azName := ""
		if cm != nil {
			azName = cm.Data["az-name"]
		} else if lastIcCm != nil {
			azName = lastIcCm["az-name"]
		}
		if err := c.removeInterConnection(azName); err != nil {
			klog.Errorf("failed to remove ovn-ic, %v", err)
			return
		}
		if err := c.delLearnedRoute(); err != nil {
			klog.Errorf("failed to remove learned static routes, %v", err)
			return
		}
		icEnabled = "false"
		lastIcCm = nil

		klog.Info("finish removing ovn-ic")
		return
	} else {
		blackList := []string{}
		autoRoute := false
		if cm.Data["auto-route"] == "true" {
			autoRoute = true
		}
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
		if err := c.ovnClient.SetICAutoRoute(autoRoute, blackList); err != nil {
			klog.Errorf("failed to config auto route, %v", err)
			return
		}

		isCMEqual := reflect.DeepEqual(cm.Data, lastIcCm)
		if icEnabled == "true" && lastIcCm != nil && isCMEqual {
			return
		}
		if icEnabled == "true" && lastIcCm != nil && !isCMEqual {
			if err := c.removeInterConnection(lastIcCm["az-name"]); err != nil {
				klog.Errorf("failed to remove ovn-ic, %v", err)
				return
			}
			if err := c.delLearnedRoute(); err != nil {
				klog.Errorf("failed to remove learned static routes, %v", err)
				return
			}
			c.ovnLegacyClient.OvnICSbAddress = genHostAddress(cm.Data["ic-db-host"], cm.Data["ic-sb-port"])

			c.ovnLegacyClient.OvnICNbAddress = genHostAddress(cm.Data["ic-db-host"], cm.Data["ic-nb-port"])
			klog.Info("start to reestablish ovn-ic")
			if err := c.establishInterConnection(cm.Data); err != nil {
				klog.Errorf("failed to reestablish ovn-ic, %v", err)
				return
			}

			if err := c.RemoveOldChassisInSbDB(lastIcCm["az-name"]); err != nil {
				klog.Errorf("failed to remove remote chassis: %v", err)
			}

			icEnabled = "true"
			lastIcCm = cm.Data
			klog.Info("finish reestablishing ovn-ic")
			return
		}

		c.ovnLegacyClient.OvnICNbAddress = genHostAddress(cm.Data["ic-db-host"], cm.Data["ic-nb-port"])
		klog.Info("start to establish ovn-ic")
		if err := c.establishInterConnection(cm.Data); err != nil {
			klog.Errorf("failed to establish ovn-ic, %v", err)
			return
		}
		icEnabled = "true"
		lastIcCm = cm.Data
		klog.Info("finish establishing ovn-ic")
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
		patchPayloadTemplate :=
			`[{
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

	if azName != "" {
		lspName := fmt.Sprintf("ts-%s", azName)
		lrpName := fmt.Sprintf("%s-ts", azName)
		if err := c.ovnClient.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
			klog.Errorf("delete ovn-ic logical port %s and %s: %v", lspName, lrpName, err)
			return err
		}
	}

	if err := c.stopOvnIC(); err != nil {
		klog.Errorf("failed to stop ovn-ic, %v", err)
		return err
	}

	return nil
}

func (c *Controller) establishInterConnection(config map[string]string) error {
	if err := c.startOvnIC(config["ic-db-host"], config["ic-nb-port"], config["ic-sb-port"]); err != nil {
		klog.Errorf("failed to start ovn-ic, %v", err)
		return err
	}

	tsPort := fmt.Sprintf("ts-%s", config["az-name"])
	exist, err := c.ovnClient.LogicalSwitchPortExists(tsPort)
	if err != nil {
		klog.Errorf("failed to list logical switch ports, %v", err)
		return err
	}

	if exist {
		klog.Infof("ts port %s already exists", tsPort)
		return nil
	}

	if err := c.ovnClient.SetAzName(config["az-name"]); err != nil {
		klog.Errorf("failed to set az name. %v", err)
		return err
	}

	chassises := []string{}
	gwNodes := strings.Split(config["gw-nodes"], ",")
	for _, gw := range gwNodes {
		gw = strings.TrimSpace(gw)
		cachedNode, err := c.nodesLister.Get(gw)
		if err != nil {
			klog.Errorf("failed to get gw node %s, %v", gw, err)
			return err
		}
		node := cachedNode.DeepCopy()
		patchPayloadTemplate :=
			`[{
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
		chassisID, err := c.ovnLegacyClient.GetChassis(gw)
		if err != nil {
			klog.Errorf("failed to get gw %s chassisID, %v", gw, err)
			return err
		}
		if chassisID == "" {
			return fmt.Errorf("no chassisID for gw %s", gw)
		}
		chassises = append(chassises, chassisID)
	}
	if len(chassises) == 0 {
		klog.Error("no available ic gw")
		return fmt.Errorf("no available ic gw")
	}
	if err := c.waitTsReady(); err != nil {
		klog.Errorf("failed to wait ts ready, %v", err)
		return err
	}

	subnet, err := c.acquireLrpAddress(util.InterconnectionSwitch)
	if err != nil {
		klog.Errorf("failed to acquire lrp address, %v", err)
		return err
	}

	lrpName := fmt.Sprintf("%s-ts", config["az-name"])
	if err := c.ovnClient.CreateLogicalPatchPort(util.InterconnectionSwitch, c.config.ClusterRouter, tsPort, lrpName, subnet, util.GenerateMac(), chassises...); err != nil {
		klog.Errorf("failed to create ovn-ic lrp %v", err)
		return err
	}

	return nil
}

func (c *Controller) acquireLrpAddress(ts string) (string, error) {
	cidr, err := c.ovnLegacyClient.GetTsSubnet(ts)
	if err != nil {
		klog.Errorf("failed to get ts subnet: %v", err)
		return "", err
	}
	existAddress, err := c.listRemoteLogicalSwitchPortAddress()
	if err != nil {
		klog.Errorf("list remote port address: %v", err)
		return "", err
	}

	for {
		random := util.GenerateRandomV4IP(cidr)

		// find a free address
		if _, ok := existAddress[random]; !ok {
			return random, nil
		}

		klog.Infof("random ip %s already exists", random)
		time.Sleep(1 * time.Second)
	}
}

func (c *Controller) startOvnIC(icHost, icNbPort, icSbPort string) error {
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
		return fmt.Errorf("%s", output)
	}
	return nil
}

func (c *Controller) stopOvnIC() error {
	cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "stop_ic")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", output)
	}
	return nil
}

func (c *Controller) waitTsReady() error {
	retry := 6
	for retry > 0 {
		ready, err := c.allSubnetReady(util.InterconnectionSwitch)
		if err != nil {
			return err
		}

		if ready {
			return nil
		}

		klog.Info("wait for logical switch %s ready", util.InterconnectionSwitch)
		time.Sleep(5 * time.Second)
		retry = retry - 1
	}
	return fmt.Errorf("timeout to wait for logical switch %s ready", util.InterconnectionSwitch)
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
			} else if len(itsRouter) != 1 {
				klog.Errorf("number wrong of logical router for static route %s, %v", aLdPort["_uuid"][0], itsRouter)
				return nil
			}
			if err := c.ovnLegacyClient.DeleteStaticRoute(aLdPort["ip_prefix"][0], itsRouter[0]["name"][0]); err != nil {
				klog.Errorf("failed to delete stale route %s, %v", aLdPort["ip_prefix"][0], err)
				return err
			}
		}
		klog.V(5).Infof("finish removing learned routes")
	}
	return nil
}

func genHostAddress(host string, port string) (hostAddress string) {
	hostList := strings.Split(host, ",")
	if len(hostList) == 1 {
		hostAddress = fmt.Sprintf("tcp:[%s]:%s", hostList[0], port)
	} else {
		var builder strings.Builder
		i := 0
		for i < len(hostList)-1 {
			builder.WriteString(fmt.Sprintf("tcp:[%s]:%s,", hostList[i], port))
			i += 1
		}
		builder.WriteString(fmt.Sprintf("tcp:[%s]:%s", hostList[i], port))
		hostAddress = builder.String()
	}
	return hostAddress
}

func (c *Controller) SynRouteToPolicy() {
	c.syncOneRouteToPolicy(util.OvnICKey, util.OvnICConnected)
	c.syncOneRouteToPolicy(util.OvnICKey, util.OvnICStatic)
}

func (c *Controller) RemoveOldChassisInSbDB(azName string) error {
	azUUID, err := c.ovnLegacyClient.GetAzUUID(azName)
	if err != nil {
		klog.Errorf("failed to get UUID of AZ %s: %v", lastIcCm["az-name"], err)
	}

	gateways, err := c.ovnLegacyClient.GetGatewayUUIDsInOneAZ(azUUID)
	if err != nil {
		klog.Errorf("failed to get gateway UUIDs in AZ %s: %v", azUUID, err)
	}

	routes, err := c.ovnLegacyClient.GetRouteUUIDsInOneAZ(azUUID)
	if err != nil {
		klog.Errorf("failed to get route UUIDs in AZ %s: %v", azUUID, err)
	}

	c.ovnLegacyClient.DestroyGateways(gateways)
	c.ovnLegacyClient.DestroyRoutes(routes)
	if err := c.ovnLegacyClient.DestroyChassis(azUUID); err != nil {
		return err
	}
	return nil
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
		klog.Errorf("logical router does not exist %v at %v", err, time.Now())
		return
	}
	lrRouteList, err := c.ovnClient.GetLogicalRouterRouteByOpts(key, value)
	if err != nil {
		klog.Errorf("failed to list lr ovn-ic route %v", err)
		return
	}
	if len(lrRouteList) == 0 {
		klog.V(5).Info("lr ovn-ic route does not exist")
		err := c.ovnClient.DeleteLogicalRouterPolicies(lr.Name, util.OvnICPolicyPriority, map[string]string{key: value})
		if err != nil {
			klog.Errorf("delete ovn-ic lr policy", err)
			return
		}
		return
	}

	policyMap := map[string]string{}
	lrPolicyList, err := c.ovnClient.ListLogicalRouterPolicies(util.OvnICPolicyPriority, map[string]string{key: value})
	if err != nil {
		klog.Errorf("failed to list ovn-ic lr policy ", err)
		return
	}
	for _, lrPolicy := range lrPolicyList {
		match, err := stripPrefix(lrPolicy.Match)
		if err != nil {
			klog.Errorf("policy match abnormal ", err)
			continue
		}
		policyMap[match] = lrPolicy.UUID
	}
	for _, lrRoute := range lrRouteList {
		if _, ok := policyMap[lrRoute.IPPrefix]; ok {
			delete(policyMap, lrRoute.IPPrefix)
		} else {
			matchFiled := util.MatchV4Dst + " == " + lrRoute.IPPrefix
			if err := c.ovnClient.AddLogicalRouterPolicy(util.DefaultVpc, util.OvnICPolicyPriority, matchFiled, ovnnb.LogicalRouterPolicyActionAllow, nil, map[string]string{key: value, "vendor": util.CniTypeName}); err != nil {
				klog.Errorf("adding router policy failed %v", err)
			}
		}
	}
	for _, uuid := range policyMap {
		if err := c.ovnClient.DeleteLogicalRouterPolicyByUUID(lr.Name, uuid); err != nil {
			klog.Errorf("deleting router policy failed %v", err)
		}
	}
}

func (c *Controller) listRemoteLogicalSwitchPortAddress() (map[string]struct{}, error) {
	lsps, err := c.ovnClient.ListLogicalSwitchPorts(true, nil, func(lsp *ovnnb.LogicalSwitchPort) bool {
		return lsp.Type == "remote"
	})
	if err != nil {
		return nil, fmt.Errorf("list remote logical switch ports: %v", err)
	}

	existAddress := make(map[string]struct{})
	for _, lsp := range lsps {
		if len(lsp.Addresses) == 0 {
			continue
		}

		addresses := lsp.Addresses[0]

		fields := strings.Fields(addresses)
		if len(fields) != 2 {
			continue
		}

		existAddress[fields[1]] = struct{}{}
	}

	return existAddress, nil
}
