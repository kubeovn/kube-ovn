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

	"github.com/alauda/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

var (
	icEnabled                   = "unknown"
	lastICCM  map[string]string = nil
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
		}
		if err := c.removeInterConnection(azName); err != nil {
			klog.Errorf("failed to remove ovn-ic, %v", err)
			return
		}
		icEnabled = "false"
		lastICCM = nil
		klog.Info("finish removing ovn-ic")
		return
	} else {
		if icEnabled == "true" && lastICCM != nil && reflect.DeepEqual(cm.Data, lastICCM) {
			return
		}

		if err := c.removeInterConnection(cm.Data["az-name"]); err != nil {
			klog.Errorf("failed to remove ovn-ic, %v", err)
			return
		}
		c.ovnClient.OVNIcNBAddress = fmt.Sprintf("%s:%s", cm.Data["ic-db-host"], cm.Data["ic-nb-port"])
		klog.Info("start to establish ovn-ic")
		if err := c.establishInterConnection(cm.Data); err != nil {
			klog.Errorf("failed to establish ovn-ic, %v", err)
			return
		}
		icEnabled = "true"
		lastICCM = cm.Data
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
		if err := c.ovnClient.DeleteICLogicalRouterPort(azName); err != nil {
			klog.Errorf("failed to delete ovn-ic lrp, %v", err)
			return err
		}
	}

	if err := c.stopOVNIC(); err != nil {
		klog.Errorf("failed to stop ovn-ic, %v", err)
		return err
	}

	return nil
}

func (c *Controller) establishInterConnection(config map[string]string) error {
	if err := c.startOVNIC(config["ic-db-host"], config["ic-nb-port"], config["ic-sb-port"]); err != nil {
		klog.Errorf("failed to start ovn-ic, %v", err)
		return err
	}

	if err := c.ovnClient.SetAzName(config["az-name"]); err != nil {
		klog.Errorf("failed to set az name. %v", err)
		return err
	}

	autoRoute := false
	if config["auto-route"] == "true" {
		autoRoute = true
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets, %v", err)
		return err
	}
	blackList := []string{}
	for _, subnet := range subnets {
		if subnet.Spec.DisableInterConnection || subnet.Name == c.config.NodeSwitch {
			blackList = append(blackList, subnet.Spec.CIDRBlock)
		}
	}
	if err := c.ovnClient.SetICAutoRoute(autoRoute, blackList); err != nil {
		klog.Errorf("failed to config auto route, %v", err)
		return nil
	}

	chassises := []string{}
	gwNodes := strings.Split(config["gw-nodes"], ",")
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
		node.Labels[util.ICGatewayLabel] = "true"
		raw, _ := json.Marshal(node.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), gw, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
		if err != nil {
			klog.Errorf("patch gw node %s failed %v", gw, err)
			return err
		}
		chassisID, err := c.ovnClient.GetChassis(gw)
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
		return fmt.Errorf("noavailable ic gw")
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

	if err := c.ovnClient.CreateICLogicalRouterPort(config["az-name"], util.GenerateMac(), subnet, chassises); err != nil {
		klog.Errorf("failed to create ovn-ic lrp %v", err)
		return err
	}

	return nil
}

func (c *Controller) acquireLrpAddress(ts string) (string, error) {
	cidr, err := c.ovnClient.GetTsSubnet(ts)
	if err != nil {
		klog.Errorf("failed to get ts subnet, %v", err)
		return "", err
	}
	existAddress, err := c.ovnClient.ListRemoteLogicalSwitchPortAddress()
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
		fmt.Sprintf("--ovn-ic-nb-db=tcp:%s:%s", icHost, icNbPort),
		fmt.Sprintf("--ovn-ic-sb-db=tcp:%s:%s", icHost, icSbPort),
		fmt.Sprintf("--ovn-northd-nb-db=%s", c.config.OvnNbAddr),
		fmt.Sprintf("--ovn-northd-sb-db=%s", c.config.OvnSbAddr),
		"start_ic")
	if os.Getenv("ENABLE_SSL") == "true" {
		cmd = exec.Command("/usr/share/ovn/scripts/ovn-ctl",
			fmt.Sprintf("--ovn-ic-nb-db=tcp:[%s]:%s", icHost, icNbPort),
			fmt.Sprintf("--ovn-ic-sb-db=tcp:[%s]:%s", icHost, icSbPort),
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

func (c *Controller) stopOVNIC() error {
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
		exists, err := c.ovnClient.LogicalSwitchExists(util.InterconnectionSwitch)
		if err != nil {
			klog.Errorf("failed to list logical switch, %v", err)
			return err
		}
		if exists {
			return nil
		}
		klog.Info("wait for ts logical switch ready")
		time.Sleep(5 * time.Second)
		retry = retry - 1
	}
	return fmt.Errorf("timeout to wait ts ready")
}
