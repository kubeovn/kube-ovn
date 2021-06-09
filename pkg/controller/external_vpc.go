package controller

import (
	"context"
	"fmt"
	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

func (c *Controller) syncExternalVpc() {
	logicalRouters := c.getRouterStatus()
	klog.V(4).Infof("sync over with %s", logicalRouters)

	vpcs, err := c.vpcsLister.List(labels.SelectorFromSet(labels.Set{util.VpcExternalLabel: "true"}))
	if err != nil {
		klog.Errorf("failed to list vpc, %v", err)
		return
	}
	vpcMaps := make(map[string]*v1.Vpc)
	for _, vpc := range vpcs {
		vpcMaps[vpc.Name] = vpc
	}
	for vpcName, vpc := range vpcMaps {
		if _, ok := logicalRouters[vpcName]; ok {
			vpc.Status.Subnets = []string{}
			for _, asw := range logicalRouters[vpcName].LogicalSwitches {
				vpc.Status.Subnets = append(vpc.Status.Subnets, asw.Name)
			}
			_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().UpdateStatus(context.Background(), vpc, metav1.UpdateOptions{})
			if err != nil {
				klog.V(4).Infof("update vpc %s status failed", vpcName)
				continue
			}
			delete(logicalRouters, vpcName)
			klog.V(4).Infof("patch vpc %s", vpcName)
		} else {
			err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Delete(context.Background(), vpcName, metav1.DeleteOptions{})
			if err != nil {
				klog.V(4).Infof("delete vpc %s failed", vpcName)
				continue
			}
			klog.V(4).Infof("delete vpc %s ", vpcName)
		}
	}
	if len(logicalRouters) != 0 {
		// routerName, logicalRouter
		for routerName, logicalRouter := range logicalRouters {
			vpc := &v1.Vpc{}
			vpc.Name = routerName
			vpc.Labels = map[string]string{util.VpcExternalLabel: "true"}
			vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("init vpc %s failed %v", routerName, err)
				return
			}

			for _, logicalSwitch := range logicalRouter.LogicalSwitches {
				vpc.Status.Subnets = append(vpc.Status.Subnets, logicalSwitch.Name)
			}
			vpc.Status.Subnets = []string{}
			vpc.Status.DefaultLogicalSwitch = ""
			vpc.Status.Router = routerName
			vpc.Status.Standby = true
			vpc.Status.Default = false

			_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().UpdateStatus(context.Background(), vpc, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("update vpc status failed %v", err)
				return
			}
			klog.V(4).Infof("add vpc %s ", routerName)
		}
	}
}

func (c *Controller) getRouterStatus() (logicalRouters map[string]util.LogicalRouter) {
	logicalRouters = make(map[string]util.LogicalRouter)
	externalOvnRouters, err := c.ovnClient.CustomFindEntity("logical_router", []string{"name", "port"}, fmt.Sprintf("external_ids{!=}vendor=%s", util.CniTypeName))
	if err != nil {
		klog.Errorf("failed to list external logical router, %v", err)
		return
	}
	if len(externalOvnRouters) == 0 {
		klog.V(4).Info("sync over, no external vpc")
		return
	}

	for _, aExternalRouter := range externalOvnRouters {
		var aLogicalRouter util.LogicalRouter
		aLogicalRouter.Name = aExternalRouter["name"][0]
		var ports []util.Port
		for _, portUUId := range aExternalRouter["port"] {
			portName, err := c.ovnClient.GetEntityInfo("logical_router_port", portUUId, []string{"name"})
			if err != nil {
				klog.Info("get port error")
				continue
			}
			aPort := util.Port{
				Name:   portName["name"],
				Subnet: "",
			}
			ports = append(ports, aPort)
		}
		aLogicalRouter.Ports = ports
		logicalRouters[aLogicalRouter.Name] = aLogicalRouter
	}
	UUID := "_uuid"
	for routerName, logicalRouter := range logicalRouters {
		tmpRouter := logicalRouter
		for _, port := range logicalRouter.Ports {
			peerPorts, err := c.ovnClient.CustomFindEntity("logical_switch_port", []string{UUID}, fmt.Sprintf("options:router-port=%s", port.Name))
			if err != nil || len(peerPorts) > 1 {
				klog.Errorf("failed to list peer port of %s, %v", port, err)
				continue
			}
			if len(peerPorts) == 0 {
				continue
			}
			switches, err := c.ovnClient.CustomFindEntity("logical_switch", []string{"name"}, fmt.Sprintf("ports{>=}%s", peerPorts[0][UUID][0]))
			if err != nil || len(switches) > 1 {
				klog.Errorf("failed to list peer switch of %s, %v", peerPorts, err)
				continue
			}
			var aLogicalSwitch util.LogicalSwitch
			aLogicalSwitch.Name = switches[0]["name"][0]
			tmpRouter.LogicalSwitches = append(tmpRouter.LogicalSwitches, aLogicalSwitch)
		}
		logicalRouters[routerName] = tmpRouter
	}
	return
}
