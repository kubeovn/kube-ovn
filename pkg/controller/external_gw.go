package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	exGwEnabled = "unknown"
	lastExGwCM  map[string]string

	externalGatewayNodeSelector = labels.Set{util.ExGatewayLabel: "true"}.AsSelector()
)

func (c *Controller) resyncExternalGateway() {
	// check if default vpc only use extra external subnet
	defaultVPC, err := c.vpcsLister.Get(c.config.ClusterRouter)
	if err != nil {
		klog.Errorf("failed to get vpc %s, %v", c.config.ClusterRouter, err)
		return
	}

	if defaultVPC.Spec.ExtraExternalSubnets != nil || defaultVPC.Status.ExtraExternalSubnets != nil {
		// vpc controller will handle
		klog.Infof("default vpc only use extra external subnets: %v", defaultVPC.Spec.ExtraExternalSubnets)
		return
	}

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

	if exGwEnabled == "true" && lastExGwCM != nil && maps.Equal(cm.Data, lastExGwCM) {
		return
	}
	klog.Infof("last external gw configmap: %v", lastExGwCM)
	if (lastExGwCM["type"] == kubeovnv1.GWDistributedType && cm.Data["type"] == kubeovnv1.GWCentralizedType) ||
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
	nodes, err := c.nodesLister.List(externalGatewayNodeSelector)
	if err != nil {
		klog.Errorf("failed to list external gw nodes, %v", err)
		return err
	}
	for _, node := range nodes {
		patch := util.KVPatch{util.ExGatewayLabel: "false"}
		if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
			klog.Errorf("failed to patch external gw node %s: %v", node.Name, err)
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
		// provider network, underlay vlan control the external gateway switch
		klog.Infof("should keep provider network underlay vlan external gateway switch %s", c.config.ExternalGatewaySwitch)
		lrpName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, c.config.ExternalGatewaySwitch)
		lspName := fmt.Sprintf("%s-%s", c.config.ExternalGatewaySwitch, c.config.ClusterRouter)
		klog.Infof("delete logical patch port lsp %s lrp %s", lspName, lrpName)
		if err := c.OVNNbClient.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
			klog.Errorf("failed to remove logical patch port %s/%s, %v", lspName, lrpName, err)
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
		klog.Errorf("failed to create external gateway switch %s: %v", c.config.ExternalGatewaySwitch, err)
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
			err = fmt.Errorf("lrp %q ip or mac should not be empty", lrpEipName)
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
		if err := c.createOrUpdateOvnEipCR(lrpEipName, c.config.ExternalGatewaySwitch, v4ip, v6ip, mac, util.OvnEipTypeLRP); err != nil {
			klog.Errorf("failed to create ovn lrp eip %s, %v", lrpEipName, err)
			return "", "", err
		}
	}
	v4ipCidr, err := util.GetIPAddrWithMask(v4ip, cachedSubnet.Spec.CIDRBlock)
	if err != nil {
		klog.Errorf("failed to get ip %s with mask %s, %v", v4ip, cachedSubnet.Spec.CIDRBlock, err)
		return "", "", err
	}
	return v4ipCidr, mac, nil
}

func (c *Controller) getGatewayChassis(config map[string]string) ([]string, error) {
	chassises := []string{}
	nodes, err := c.nodesLister.List(externalGatewayNodeSelector)
	if err != nil {
		klog.Errorf("failed to list external gw nodes, %v", err)
		return nil, err
	}
	gwNodes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		gwNodes = append(gwNodes, node.Name)
	}
	if config["type"] != kubeovnv1.GWDistributedType {
		nodeNames := strings.SplitSeq(config["external-gw-nodes"], ",")
		for name := range nodeNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !slices.Contains(gwNodes, name) {
				gwNodes = append(gwNodes, name)
			}
		}
	}
	for _, gw := range gwNodes {
		node, err := c.nodesLister.Get(gw)
		if err != nil {
			klog.Errorf("failed to get gw node %s, %v", gw, err)
			return nil, err
		}
		patch := util.KVPatch{util.ExGatewayLabel: "true"}
		if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
			klog.Errorf("failed to patch annotations of node %s: %v", node.Name, err)
			return nil, err
		}

		annoChassisName := node.Annotations[util.ChassisAnnotation]
		if annoChassisName == "" {
			err := fmt.Errorf("node %s has no chassis annotation, kube-ovn-cni not ready", gw)
			klog.Error(err)
			return nil, err
		}
		klog.Infof("get node %s chassis: %s", gw, annoChassisName)
		chassis, err := c.OVNSbClient.GetChassis(annoChassisName, false)
		if err != nil {
			klog.Errorf("failed to get node %s chassis: %s, %v", node.Name, annoChassisName, err)
			return nil, err
		}
		chassises = append(chassises, chassis.Name)
	}
	if len(chassises) == 0 {
		err := errors.New("no available external gw chassis")
		klog.Error(err)
		return nil, err
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
			err := fmt.Errorf("failed to update vpc enable external %s, %w", vpc.Name, err)
			klog.Error(err)
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
