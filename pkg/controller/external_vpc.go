package controller

import (
	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) syncExternalVpc() {
	logicalRouters, err := c.getRouterStatus()
	klog.V(4).Infof("sync over with %s", logicalRouters)
	if err != nil {
		klog.Error("list lr failed", err)
		return
	}
	vpcs, err := c.vpcsLister.List(labels.SelectorFromSet(labels.Set{util.VpcExternalLabel: "true"}))
	if err != nil {
		klog.Errorf("failed to list vpc, %v", err)
		return
	}
	vpcMaps := make(map[string]*v1.Vpc)
	for _, vpc := range vpcs {
		vpcMaps[vpc.Name] = vpc.DeepCopy()
	}
	for vpcName, vpc := range vpcMaps {
		if _, ok := logicalRouters[vpcName]; ok {
			vpc.Status.Subnets = []string{}
			for _, asw := range logicalRouters[vpcName].LogicalSwitches {
				vpc.Status.Subnets = append(vpc.Status.Subnets, asw.Name)
			}
			_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().UpdateStatus(context.Background(), vpc, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("update vpc %s status failed: %v", vpcName, err)
				continue
			}
			delete(logicalRouters, vpcName)
			klog.V(4).Infof("patch vpc %s", vpcName)
		} else {
			err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Delete(context.Background(), vpcName, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("delete vpc %s failed: %v", vpcName, err)
				continue
			}
			klog.Infof("deleted vpc %s", vpcName)
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
			klog.V(4).Infof("add vpc %s", routerName)
		}
	}
}

func (c *Controller) getRouterStatus() (logicalRouters map[string]util.LogicalRouter, err error) {
	logicalRouters = make(map[string]util.LogicalRouter)
	externalOvnRouters, err := c.OVNNbClient.ListLogicalRouter(false, func(lr *ovnnb.LogicalRouter) bool {
		return len(lr.ExternalIDs) == 0 || lr.ExternalIDs["vendor"] != util.CniTypeName
	})
	if err != nil {
		klog.Errorf("failed to list external logical router, %v", err)
		return logicalRouters, err
	}
	if len(externalOvnRouters) == 0 {
		klog.V(4).Info("sync over, no external vpc")
		return logicalRouters, nil
	}

	for _, externalLR := range externalOvnRouters {
		lr := util.LogicalRouter{
			Name:  externalLR.Name,
			Ports: make([]util.Port, 0, len(externalLR.Ports)),
		}
		for _, uuid := range externalLR.Ports {
			lrp, err := c.OVNNbClient.GetLogicalRouterPortByUUID(uuid)
			if err != nil {
				klog.Warningf("failed to get LRP by UUID %s: %v", uuid, err)
				continue
			}
			lr.Ports = append(lr.Ports, util.Port{Name: lrp.Name})
		}
		logicalRouters[lr.Name] = lr
	}
	for routerName, logicalRouter := range logicalRouters {
		tmpRouter := logicalRouter
		for _, port := range logicalRouter.Ports {
			peerPorts, err := c.OVNNbClient.ListLogicalSwitchPorts(false, nil, func(lsp *ovnnb.LogicalSwitchPort) bool {
				return len(lsp.Options) != 0 && lsp.Options["router-port"] == port.Name
			})
			if err != nil || len(peerPorts) > 1 {
				klog.Errorf("failed to list peer port of %s, %v", port, err)
				continue
			}
			if len(peerPorts) == 0 {
				continue
			}
			lsp := peerPorts[0]
			switches, err := c.OVNNbClient.ListLogicalSwitch(false, func(ls *ovnnb.LogicalSwitch) bool {
				return slices.Contains(ls.Ports, lsp.UUID)
			})
			if err != nil || len(switches) > 1 {
				klog.Errorf("failed to get logical switch of LSP %s: %v", lsp.Name, err)
				continue
			}
			var aLogicalSwitch util.LogicalSwitch
			aLogicalSwitch.Name = switches[0].Name
			tmpRouter.LogicalSwitches = append(tmpRouter.LogicalSwitches, aLogicalSwitch)
		}
		logicalRouters[routerName] = tmpRouter
	}
	return logicalRouters, nil
}
