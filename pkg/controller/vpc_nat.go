package controller

import (
	"fmt"
	"os"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func vpcNatConfigName() string {
	if name := strings.TrimSpace(os.Getenv(util.EnvVpcNatConfig)); name != "" {
		return name
	}
	return util.VpcNatConfig
}

var (
	vpcNatImage             = ""
	vpcNatGwBgpSpeakerImage = ""
	vpcNatAPINadProvider    = ""
	vpcNatImagePullSecret   = ""
)

func (c *Controller) resyncVpcNatConfig() {
	configName := vpcNatConfigName()
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(configName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get ovn-vpc-nat-config, %w", err)
			klog.Error(err)
		}
		return
	}

	// Prefix used to generate the name of the StatefulSet/Pods for a NAT gateway
	// By default it is equal to the value contained in 'util.VpcNatGwNamePrefix'
	vpcNatGwNamePrefix := cm.Data["natGwNamePrefix"]
	if vpcNatGwNamePrefix != "" {
		util.VpcNatGwNamePrefix = vpcNatGwNamePrefix
	} else {
		util.VpcNatGwNamePrefix = util.VpcNatGwNameDefaultPrefix
	}

	// Image we're using to provision the NAT gateways
	image, exist := cm.Data["image"]
	if !exist {
		err = fmt.Errorf("%s should have image field", configName)
		klog.Error(err)
		return
	}
	vpcNatImage = image

	// image pull secret for images in private registries, optional
	vpcNatImagePullSecret = cm.Data["imagePullSecret"]

	// Image for the BGP sidecar of the gateway (optional)
	vpcNatGwBgpSpeakerImage = cm.Data["bgpSpeakerImage"]

	// NetworkAttachmentDefinition provider for the BGP speaker to call the API server
	vpcNatAPINadProvider = cm.Data["apiNadProvider"]
}
