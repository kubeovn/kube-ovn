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
	"k8s.io/klog"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	exGwEnabled                   = "unknown"
	lastExGwCM  map[string]string = nil
)

func (c *Controller) resyncExternalGateway() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.ExternalGatewayConfig)
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
		exGwEnabled = "false"
		lastExGwCM = nil
		klog.Info("finish remove ovn external gw")
		return
	} else {
		if exGwEnabled == "true" && lastExGwCM != nil && reflect.DeepEqual(cm.Data, lastExGwCM) {
			return
		}
		klog.Info("start to establish ovn external gw")
		if err := c.establishExternalGateway(cm.Data); err != nil {
			klog.Errorf("failed to establish ovn-external-gw, %v", err)
			return
		}
		exGwEnabled = "true"
		lastExGwCM = cm.Data
		c.ovnClient.ExternalGatewayType = cm.Data["type"]
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
	for _, no := range nodes {
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

	if err := c.ovnClient.DeleteGatewaySwitch(util.ExternalGatewaySwitch); err != nil {
		klog.Errorf("failed to delete external gateway switch, %v", err)
		return err
	}
	return nil
}

func (c *Controller) establishExternalGateway(config map[string]string) error {
	chassises := []string{}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
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
		node, err := c.nodesLister.Get(gw)
		if err != nil {
			klog.Errorf("failed to get gw node %s, %v", gw, err)
			return err
		}
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
			return err
		}
		chassisID, err := c.ovnClient.GetChassis(gw)
		if err != nil {
			klog.Errorf("failed to get external gw %s chassisID, %v", gw, err)
			return err
		}
		if chassisID == "" {
			return fmt.Errorf("no chassisID for external gw %s", gw)
		}
		chassises = append(chassises, chassisID)
	}
	if len(chassises) == 0 {
		klog.Error("no available external gw")
		return fmt.Errorf("no available external gw")
	}

	if err := c.ovnClient.CreateGatewaySwitch(util.ExternalGatewaySwitch, config["nic-ip"], config["nic-mac"], chassises); err != nil {
		klog.Errorf("failed to create external gateway switch, %v", err)
		return err
	}

	return nil
}
