package controller

import (
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func (c *Controller) enqueueAddOrDelIP(obj interface{}) {

	ipObj := obj.(*kubeovnv1.IP)
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(ipObj.Spec.Subnet)
	for _, as := range ipObj.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update status subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
}

func (c *Controller) enqueueUpdateIP(old, new interface{}) {

	ipObj := new.(*kubeovnv1.IP)
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	for _, as := range ipObj.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update status subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
}
