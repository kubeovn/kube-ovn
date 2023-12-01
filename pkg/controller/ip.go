package controller

import (
	"strings"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOrDelIP(obj interface{}) {
	if _, ok := obj.(*kubeovnv1.IP); !ok {
		klog.Errorf("object is not an IP, ignore it")
		return
	}

	ipObj := obj.(*kubeovnv1.IP)
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	if strings.HasPrefix(ipObj.Name, util.U2OInterconnName[0:19]) {
		return
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
