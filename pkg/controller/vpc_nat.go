package controller

import (
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatImage = ""
)

func (c *Controller) resyncVpcNatConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-vpc-nat-config, %v", err)
		return
	}
	vpcNatImage = cm.Data["image"]
}
