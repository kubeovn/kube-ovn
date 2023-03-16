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

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	exGwEnabled                   = "unknown"
	lastExGwCM  map[string]string = nil
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
			klog.Error("failed to update default vpc, %v", err)
			return
		}
		exGwEnabled = "false"
		lastExGwCM = nil
		klog.Info("finish remove ovn external gw")
		return
	} else {
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
		c.ovnLegacyClient.ExternalGatewayType = cm.Data["type"]
		c.ExternalGatewayType = cm.Data["type"]
		if err := c.updateDefaultVpcExternal(true); err != nil {
			klog.Error("failed to update default vpc, %v", err)
			return
		}
		klog.Info("finish establishing ovn external gw")
	}
}

func (c *Controller) removeExternalGateway() error {
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{util.ExGatewayLabel: "true"}})
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
		if err := c.ovnClient.DeleteLogicalGatewaySwitch(util.ExternalGatewaySwitch, c.config.ClusterRouter); err != nil {
			klog.Errorf("delete external gateway switch %s: %v", util.ExternalGatewaySwitch, err)
			return err
		}
	} else {
		klog.Infof("should keep provider network vlan underlay external gateway switch %s", c.config.ExternalGatewaySwitch)
		lrpName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, c.config.ExternalGatewaySwitch)
		klog.Infof("delete logical router port %s", lrpName)
		if err := c.ovnClient.DeleteLogicalRouterPort(lrpName); err != nil {
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
	var lrpIp, lrpMac string
	if config["nic-ip"] == "" {
		if lrpIp, lrpMac, err = c.createDefaultVpcLrpEip(config); err != nil {
			klog.Errorf("failed to create ovn eip for default vpc lrp: %v", err)
			return err
		}
	} else {
		lrpIp = config["nic-ip"]
		lrpMac = config["nic-mac"]
	}
	lrpName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, c.config.ExternalGatewaySwitch)
	exist, err := c.ovnClient.LogicalRouterPortExists(lrpName)
	if err != nil {
		return err
	}
	if exist {
		klog.Infof("lrp %s exist", lrpName)
		return nil
	}

	if err := c.ovnClient.CreateGatewayLogicalSwitch(c.config.ExternalGatewaySwitch, c.config.ClusterRouter, c.config.ExternalGatewayNet, lrpIp, lrpMac, c.config.ExternalGatewayVlanID, chassises...); err != nil {
		klog.Errorf("create external gateway switch %s: %v", c.config.ExternalGatewaySwitch, err)
		return err
	}

	return nil
}

func (c *Controller) createDefaultVpcLrpEip(config map[string]string) (string, string, error) {
	cachedSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		klog.Errorf("failed to get subnet %s, %v", c.config.ExternalGatewaySwitch, err)
		return "", "", err
	}
	needCreateEip := false
	lrpEipName := fmt.Sprintf("%s-%s", util.DefaultVpc, c.config.ExternalGatewaySwitch)
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
		v4ip, v6ip, mac, err = c.acquireIpAddress(c.config.ExternalGatewaySwitch, lrpEipName, lrpEipName)
		if err != nil {
			klog.Errorf("failed to acquire ip address for default vpc lrp %s, %v", lrpEipName, err)
			return "", "", err
		}
		if err := c.createOrUpdateCrdOvnEip(lrpEipName, c.config.ExternalGatewaySwitch, v4ip, v6ip, mac, util.LrpUsingEip); err != nil {
			klog.Errorf("failed to create ovn eip cr for lrp %s, %v", lrpEipName, err)
			return "", "", err
		}
	}
	v4ipCidr := util.GetIpAddrWithMask(v4ip, cachedSubnet.Spec.CIDRBlock)
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
		gwNodes = strings.Split(config["external-gw-nodes"], ",")
	}
	for _, gw := range gwNodes {
		gw = strings.TrimSpace(gw)
		cachedNode, err := c.nodesLister.Get(gw)
		if err != nil {
			klog.Errorf("failed to get gw node %s, %v", gw, err)
			return chassises, err
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
		node.Labels[util.ExGatewayLabel] = "true"
		raw, _ := json.Marshal(node.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), gw, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
		if err != nil {
			klog.Errorf("patch external gw node %s failed %v", gw, err)
			return chassises, err
		}
		chassisID, err := c.ovnLegacyClient.GetChassis(gw)
		if err != nil {
			klog.Errorf("failed to get external gw %s chassisID, %v", gw, err)
			return chassises, err
		}
		if chassisID == "" {
			return chassises, fmt.Errorf("no chassisID for external gw %s", gw)
		}
		chassises = append(chassises, chassisID)
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
		lrpEipName := fmt.Sprintf("%s-%s", util.DefaultVpc, c.config.ExternalGatewaySwitch)
		if err := c.patchLrpOvnEipEnableBfdLabel(lrpEipName, vpc.Spec.EnableBfd); err != nil {
			klog.Errorf("failed to patch label for lrp %s, %v", lrpEipName, err)
			return err
		}
	}
	return nil
}
