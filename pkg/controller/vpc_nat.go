package controller

import (
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatImage = ""
	sslVpnImage = ""
)

func (c *Controller) resyncVpcNatImage() error {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil {
		err = fmt.Errorf("failed to get ovn-vpc-nat-config, %v", err)
		klog.Error(err)
		return err
	}
	image, exist := cm.Data["image"]
	if !exist {
		err = fmt.Errorf("%s should have image field", util.VpcNatConfig)
		klog.Error(err)
		return err
	}
	vpcNatImage = image
	sslVpnImage, exist = cm.Data["ssl-vpn-image"]
	if exist {
		klog.Infof("ssl vpn server image: %s", sslVpnImage)
	}
	return nil
}
