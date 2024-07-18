package controller

import (
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatImage             = ""
	vpcNatGwBgpSpeakerImage = ""
)

func (c *Controller) resyncVpcNatImage() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil {
		err = fmt.Errorf("failed to get ovn-vpc-nat-config, %w", err)
		klog.Error(err)
		return
	}

	image, exist := cm.Data["image"]
	if !exist {
		err = fmt.Errorf("%s should have image field", util.VpcNatConfig)
		klog.Error(err)
		return
	}
	vpcNatImage = image

	vpcNatGwBgpSpeakerImage = cm.Data["bgpSpeakerImage"]
}
