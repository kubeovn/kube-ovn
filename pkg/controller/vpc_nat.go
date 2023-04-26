package controller

import (
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatImage = ""
	sslVpnImage = ""
)

func (c *Controller) resyncVpcNatConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return
		}
		klog.Errorf("failed to get ovn-vpc-nat-config, %v", err)
		return
	}
	image, exist := cm.Data["image"]
	if !exist {
		klog.Errorf("failed to get 'image' at ovn-vpc-nat-config")
		return
	}
	vpcNatImage = image
	sslVpnImage, exist = cm.Data["ssl-vpn-image"]
	if exist {
		klog.Infof("enable ssl vpn")
		return
	}
}
