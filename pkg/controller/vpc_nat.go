package controller

import (
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"k8s.io/klog/v2"
)

var (
	vpcNatImage              = ""
	vpcNatGwEnableBgpSpeaker = false
	vpcNatGwBgpSpeakerImage  = ""
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

	// Check BGP is enabled on the NAT GW, if yes, verify required parameters are present
	enableBgpSpeaker, exist := cm.Data["enableBgpSpeaker"]
	if exist && enableBgpSpeaker == "true" {
		vpcNatGwEnableBgpSpeaker = true

		vpcNatGwBgpSpeakerImage, exist = cm.Data["bgpSpeakerImage"]
		if !exist {
			err = fmt.Errorf("%s should have bgp speaker image field if bgp enabled", util.VpcNatConfig)
			klog.Error(err)
			return
		}
	}
}
