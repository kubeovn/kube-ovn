package controller

import (
	"strings"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOrDelIP(obj interface{}) {
	ipObj := obj.(*kubeovnv1.IP)
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	if strings.HasPrefix(ipObj.Name, util.U2OInterconnName[0:19]) {
		return
	}
	if !ipObj.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("delete ip %s", ipObj.Name)
		subnet, err := c.subnetsLister.Get(ipObj.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", ipObj.Spec.Subnet, err)
			return
		}
		portName := ovs.PodNameToPortName(ipObj.Name, ipObj.Spec.Namespace, subnet.Spec.Provider)
		if isOvnSubnet(subnet) {
			port, err := c.OVNNbClient.GetLogicalSwitchPort(portName, true)
			if err != nil {
				klog.Errorf("failed to get logical switch port %s: %v", portName, err)
				return
			}
			if port != nil {
				sgList, err := c.getPortSg(port)
				if err != nil {
					klog.Errorf("get port sg failed, %v", err)
					return
				}
				klog.V(3).Infof("delete ip logical switch port %s from logical switch %s", portName, subnet.Name)
				if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
					klog.Errorf("delete ip logical switch port %s from logical switch %s: %v", portName, subnet.Name, err)
					return
				}
				// refresh sg after delete port
				for _, sgName := range sgList {
					if sgName != "" {
						c.syncSgPortsQueue.Add(sgName)
					}
				}
			}
		}
		klog.V(3).Infof("release ipam for ip %s from subnet %s", ipObj.Name, ipObj.Spec.Subnet)
		c.ipam.ReleaseAddressByPod(ipObj.Name, ipObj.Spec.Subnet)
	}
	c.updateSubnetStatusQueue.Add(ipObj.Spec.Subnet)
	for _, as := range ipObj.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update attach status for subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
}

func (c *Controller) enqueueUpdateIP(_, newObj interface{}) {
	ipObj := newObj.(*kubeovnv1.IP)
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	for _, as := range ipObj.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update status for attach subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
}
