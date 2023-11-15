package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	exGwEnabled = "unknown"
	lastExGwCM  map[string]string
)

func (c *Controller) resyncExternalGateway() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.ExternalGatewayConfigNS).Get(util.ExternalGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-external-gw-config, %v", err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-external-gw"] == "false" {
		if exGwEnabled == "false" {
			return
		}
		klog.Info("start to remove ovn external gw")
		if err := c.removeExternalGateway(); err != nil {
			klog.Errorf("failed to remove ovn external gw, %v", err)
			return
		}
		if err := c.updateDefaultVpcExternal(false); err != nil {
			klog.Errorf("failed to update default vpc, %v", err)
			return
		}
		exGwEnabled = "false"
		lastExGwCM = nil
		klog.Info("finish remove ovn external gw")
		return
	}

	if exGwEnabled == "true" && lastExGwCM != nil && reflect.DeepEqual(cm.Data, lastExGwCM) {
		return
	}
	klog.Infof("last external gw configmap: %v", lastExGwCM)
	if (lastExGwCM["type"] == "distributed" && cm.Data["type"] == "centralized") ||
		lastExGwCM != nil && !reflect.DeepEqual(lastExGwCM["external-gw-nodes"], cm.Data["external-gw-nodes"]) {
		klog.Info("external gw nodes list changed, start to remove ovn external gw")
		if err := c.removeExternalGateway(); err != nil {
			klog.Errorf("failed to remove old ovn external gw, %v", err)
			return
		}
	}
	klog.Info("start to establish ovn external gw")
	if err := c.establishExternalGateway(cm.Data); err != nil {
		klog.Errorf("failed to establish ovn-external-gw, %v", err)
		return
	}
	exGwEnabled = "true"
	lastExGwCM = cm.Data
	c.ExternalGatewayType = cm.Data["type"]
	if err := c.updateDefaultVpcExternal(true); err != nil {
		klog.Errorf("failed to update default vpc, %v", err)
		return
	}
	klog.Info("finish establishing ovn external gw")
}

func (c *Controller) removeExternalGateway() error {
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{util.ExGatewayLabel: "true"}})
	nodes, err := c.nodesLister.List(sel)
	if err != nil {
		klog.Errorf("failed to list external gw nodes, %v", err)
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
		no.Labels[util.ExGatewayLabel] = "false"
		raw, _ := json.Marshal(no.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), no.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
		if err != nil {
			klog.Errorf("patch external gw node %s failed %v", no.Name, err)
			return err
		}
	}

	keepExternalSubnet := false
	externalSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get subnet %s, %v", c.config.ExternalGatewaySwitch, err)
			return err
		}
	} else {
		if externalSubnet.Spec.Vlan != "" {
			keepExternalSubnet = true
		}
	}

	if !keepExternalSubnet {
		klog.Infof("delete external gateway switch %s", c.config.ExternalGatewaySwitch)
		if err := c.OVNNbClient.DeleteLogicalGatewaySwitch(util.ExternalGatewaySwitch, c.config.ClusterRouter); err != nil {
			klog.Errorf("delete external gateway switch %s: %v", util.ExternalGatewaySwitch, err)
			return err
		}
	} else {
		klog.Infof("should keep provider network vlan underlay external gateway switch %s", c.config.ExternalGatewaySwitch)
		lrpName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, c.config.ExternalGatewaySwitch)
		klog.Infof("delete logical router port %s", lrpName)
		if err := c.OVNNbClient.DeleteLogicalRouterPort(lrpName); err != nil {
			klog.Errorf("failed to delete lrp %s, %v", lrpName, err)
			return err
		}
	}
	return nil
}

func (c *Controller) establishExternalGateway(config map[string]string) error {
	chassises, err := c.getGatewayChassis(config)
	if err != nil {
		klog.Errorf("failed to get gateway chassis, %v", err)
		return err
	}
	var lrpIP, lrpMac string
	lrpName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, c.config.ExternalGatewaySwitch)
	lrp, err := c.OVNNbClient.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		klog.Errorf("failed to get lrp %s, %v", lrpName, err)
		return err
	}

	switch {
	case lrp != nil:
		klog.Infof("lrp %s already exist", lrpName)
		lrpMac = lrp.MAC
		lrpIP = lrp.Networks[0]
	case config["nic-ip"] == "":
		if lrpIP, lrpMac, err = c.createDefaultVpcLrpEip(); err != nil {
			klog.Errorf("failed to create ovn eip for default vpc lrp: %v", err)
			return err
		}
	default:
		lrpIP = config["nic-ip"]
		lrpMac = config["nic-mac"]
	}

	if err := c.OVNNbClient.CreateGatewayLogicalSwitch(c.config.ExternalGatewaySwitch, c.config.ClusterRouter, c.config.ExternalGatewayNet, lrpIP, lrpMac, c.config.ExternalGatewayVlanID, chassises...); err != nil {
		klog.Errorf("create external gateway switch %s: %v", c.config.ExternalGatewaySwitch, err)
		return err
	}

	return nil
}

func (c *Controller) createDefaultVpcLrpEip() (string, string, error) {
	cachedSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		klog.Errorf("failed to get subnet %s, %v", c.config.ExternalGatewaySwitch, err)
		return "", "", err
	}
	needCreateEip := false
	lrpEipName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, c.config.ExternalGatewaySwitch)
	cachedEip, err := c.ovnEipsLister.Get(lrpEipName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get eip %s, %v", lrpEipName, err)
			return "", "", err
		}
		needCreateEip = true
	}
	var v4ip, mac string
	if !needCreateEip {
		v4ip = cachedEip.Status.V4Ip
		mac = cachedEip.Status.MacAddress
		if v4ip == "" || mac == "" {
			err = fmt.Errorf("lrp '%s' ip or mac should not be empty", lrpEipName)
			klog.Error(err)
			return "", "", err
		}
	} else {
		var v6ip string
		v4ip, v6ip, mac, err = c.acquireIPAddress(c.config.ExternalGatewaySwitch, lrpEipName, lrpEipName)
		if err != nil {
			klog.Errorf("failed to acquire ip address for default vpc lrp %s, %v", lrpEipName, err)
			return "", "", err
		}
		if err := c.createOrUpdateCrdOvnEip(lrpEipName, c.config.ExternalGatewaySwitch, v4ip, v6ip, mac, util.Lrp); err != nil {
			klog.Errorf("failed to create ovn eip cr for lrp %s, %v", lrpEipName, err)
			return "", "", err
		}
	}
	v4ipCidr := util.GetIPAddrWithMask(v4ip, cachedSubnet.Spec.CIDRBlock)
	return v4ipCidr, mac, nil
}

func (c *Controller) getGatewayChassis(config map[string]string) ([]string, error) {
	chassises := []string{}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return chassises, err
	}
	gwNodes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		gwNodes = append(gwNodes, node.Name)
	}
	if config["type"] != "distributed" {
		nodeNames := strings.Split(config["external-gw-nodes"], ",")
		for _, name := range nodeNames {
			name = strings.TrimSpace(name)
			if name != "" && !util.ContainsString(gwNodes, name) {
				gwNodes = append(gwNodes, name)
			}
		}
	}
	for _, gw := range gwNodes {
		cachedNode, err := c.nodesLister.Get(gw)
		if err != nil {
			klog.Errorf("failed to get gw node %s, %v", gw, err)
			return chassises, err
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
		node.Labels[util.ExGatewayLabel] = "true"
		raw, _ := json.Marshal(node.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), gw, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
		if err != nil {
			klog.Errorf("patch external gw node %s failed %v", gw, err)
			return chassises, err
		}
		annoChassisName := node.Annotations[util.ChassisAnnotation]
		if annoChassisName == "" {
			err := fmt.Errorf("node %s has no chassis annotation, kube-ovn-cni not ready", gw)
			klog.Error(err)
			return chassises, err
		}
		klog.Infof("get node %s chassis: %s", gw, annoChassisName)
		chassis, err := c.OVNSbClient.GetChassis(annoChassisName, false)
		if err != nil {
			klog.Errorf("failed to get node %s chassis: %s, %v", node.Name, annoChassisName, err)
			return chassises, err
		}
		chassises = append(chassises, chassis.Name)
	}
	if len(chassises) == 0 {
		klog.Error("no available external gw")
		return chassises, fmt.Errorf("no available external gw")
	}
	return chassises, nil
}

func (c *Controller) updateDefaultVpcExternal(enableExternal bool) error {
	cachedVpc, err := c.vpcsLister.Get(c.config.ClusterRouter)
	if err != nil {
		klog.Errorf("failed to get vpc %s, %v", c.config.ClusterRouter, err)
		return err
	}
	vpc := cachedVpc.DeepCopy()
	if vpc.Spec.EnableExternal != enableExternal {
		vpc.Spec.EnableExternal = enableExternal
		if _, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			errMsg := fmt.Errorf("failed to update vpc enable external %s, %v", vpc.Name, err)
			klog.Error(errMsg)
			return err
		}
	}
	if vpc.Status.EnableExternal != enableExternal {
		vpc.Status.EnableExternal = enableExternal
		bytes, err := vpc.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to get vpc bytes, %v", err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(),
			vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch vpc %s, %v", c.config.ClusterRouter, err)
			return err
		}
	}
	return nil
}

func (c *Controller) l3HA() {
	// maintain router external lrp ha
	// based on the bfd between router external lrp and physical switch gw
	// if bfd down, switch router external lrp to another chassis node

	// get gw chassis
	sel, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{util.ExGatewayLabel: "true"}})
	if err != nil {
		klog.Errorf("failed to convert label selector, %v", err)
		return
	}
	gwNodes, err := c.nodesLister.List(sel)
	if err != nil {
		klog.Errorf("failed to list external gw nodes, %v", err)
		return
	}
	if len(gwNodes) == 0 {
		return
	}

	// get down bfd by external subnet gateway
	externalSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		klog.Errorf("failed to get default external subnet %s, %v", c.config.ExternalGatewaySwitch, err)
		return
	}
	v4ExtGw, _ := util.SplitStringIP(externalSubnet.Spec.Gateway)
	downBfds, err := c.OVNNbClient.ListDownBFDs(v4ExtGw)
	if err != nil {
		klog.Errorf("failed to list bfd, %v", err)
		return
	}
	// no bfd down
	if len(*downBfds) == 0 {
		return
	}

	centralizedGwChassisList := []string{}
	for _, node := range gwNodes {
		if annoChassesName, ok := node.Annotations[util.ChassisAnnotation]; ok {
			centralizedGwChassisList = append(centralizedGwChassisList, annoChassesName)
		}
	}
	if len(centralizedGwChassisList) < 2 {
		klog.Errorf("failed to found alternative chassis for lrp when bfd status is down")
		return
	}

	downBfdLrps := []*ovnnb.LogicalRouterPort{}
	for _, bfd := range *downBfds {
		lrp, err := c.OVNNbClient.GetLogicalRouterPort(bfd.LogicalPort, false)
		if err != nil {
			klog.Errorf("failed to get lrp %s, %v", bfd.LogicalPort, err)
			continue
		}
		downBfdLrps = append(downBfdLrps, lrp)
	}

	badChassisNames := []string{}
	badChassises := []ovnnb.GatewayChassis{}

	// update bad chassis with low priority
	for _, lrp := range downBfdLrps {
		gwChassisList, err := c.OVNNbClient.ListGatewayChassisByLogicalRouterPort(lrp.Name, false)
		if err != nil {
			klog.Errorf("failed to list gateway chassis for lrp %s, %v", lrp.Name, err)
			continue
		}
		if len(*gwChassisList) == 0 {
			klog.Errorf("no gateway chassis for lrp %s", lrp.Name)
			continue
		}
		badChassis := (*gwChassisList)[0]
		klog.Errorf("lrp %s using bad chassis %s", lrp.Name, badChassis.ChassisName)
		if !util.ContainsString(badChassisNames, badChassis.ChassisName) {
			badChassisNames = append(badChassisNames, badChassis.ChassisName)
		}
		badChassises = append(badChassises, badChassis)
	}

	if len(badChassisNames) == 0 {
		return
	}

	if len(badChassisNames) == len(centralizedGwChassisList) {
		// all chassis bfd status are down
		klog.Errorf("all bad external gw chassis %v", badChassisNames)
		return
	}

	// centralized gw chassis node number probably less than 5
	maxGwnum := 5
	// bad chassis use low priority
	for _, badChassis := range badChassises {
		if badChassis.Priority > maxGwnum {
			badChassis.Priority -= maxGwnum
			klog.Warningf("lower bad chassis %s priority to %d", badChassis.Name, badChassis.Priority)
		} else {
			badChassis.Priority = util.GwChassisMaxPriority - maxGwnum
			// in case of priority too low to switch back
			klog.Warningf("refresh bad chassis %s priority to %d", badChassis.Name, badChassis.Priority)
		}
		if err := c.OVNNbClient.UpdateGatewayChassis(&badChassis, &badChassis.Priority); err != nil {
			klog.Errorf("failed to update bad chassis %s, %v", badChassis.Name, err)
			continue
		}
	}

	upBfds, err := c.OVNNbClient.ListUpBFDs(v4ExtGw)
	if err != nil {
		klog.Errorf("failed to list bfd, %v", err)
		return
	}
	// no bfd down
	if len(*upBfds) == 0 {
		return
	}

	upBfdLrps := []*ovnnb.LogicalRouterPort{}
	for _, bfd := range *upBfds {
		lrp, err := c.OVNNbClient.GetLogicalRouterPort(bfd.LogicalPort, false)
		if err != nil {
			klog.Errorf("failed to get lrp %s, %v", bfd.LogicalPort, err)
			continue
		}
		upBfdLrps = append(upBfdLrps, lrp)
	}

	goodChassisNames := []string{}
	goodChassises := []ovnnb.GatewayChassis{}

	// update bad chassis with low priority
	for _, lrp := range upBfdLrps {
		gwChassisList, err := c.OVNNbClient.ListGatewayChassisByLogicalRouterPort(lrp.Name, false)
		if err != nil {
			klog.Errorf("failed to list gateway chassis for lrp %s, %v", lrp.Name, err)
			continue
		}
		if len(*gwChassisList) == 0 {
			klog.Errorf("no gateway chassis for lrp %s", lrp.Name)
			continue
		}
		goodChassis := (*gwChassisList)[0]
		klog.Errorf("lrp %s using good chassis %s", lrp.Name, goodChassis.ChassisName)
		if !util.ContainsString(goodChassisNames, goodChassis.ChassisName) {
			goodChassisNames = append(goodChassisNames, goodChassis.ChassisName)
		}
		goodChassises = append(goodChassises, goodChassis)
	}

	// good chassis use high priority
	for _, goodChassis := range goodChassises {
		if goodChassis.Priority != util.GwChassisMaxPriority {
			goodChassis.Priority = util.GwChassisMaxPriority
			klog.Infof("raise good chassis %s priority to %d", goodChassis.Name, goodChassis.Priority)
			if err := c.OVNNbClient.UpdateGatewayChassis(&goodChassis, &goodChassis.Priority); err != nil {
				klog.Errorf("failed to update good chassis %s, %v", goodChassis.Name, err)
				continue
			}
		}
	}
}
