package controller

import (
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	// vpcNatGwScriptMountPath is the path where NAT gateway scripts are mounted inside the container.
	// This path is used both for the default scripts embedded in the image and for hostPath mounted scripts.
	vpcNatGwScriptMountPath    = "/kube-ovn"
	vpcNatGwScriptVolumeName   = "nat-gw-script"
	vpcNatGwScriptName         = "nat-gateway.sh"
	vpcNatGwScriptPath         = vpcNatGwScriptMountPath + "/" + vpcNatGwScriptName
	vpcNatGwContainerName      = "vpc-nat-gw"
	vpcNatGwServiceAccountName = "vpc-nat-gw"
)

var (
	vpcNatImage             = ""
	vpcNatGwBgpSpeakerImage = ""
	vpcNatAPINadProvider    = ""
	vpcNatGwScriptHostPath  = ""
)

func (c *Controller) resyncVpcNatConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
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
		err = fmt.Errorf("%s should have image field", util.VpcNatConfig)
		klog.Error(err)
		return
	}
	vpcNatImage = image

	// Image for the BGP sidecar of the gateway (optional)
	vpcNatGwBgpSpeakerImage = cm.Data["bgpSpeakerImage"]

	// NetworkAttachmentDefinition provider for the BGP speaker to call the API server
	vpcNatAPINadProvider = cm.Data["apiNadProvider"]

	// Host path for NAT gateway scripts (optional)
	// When configured, the NAT gateway pod will mount scripts from this host path
	// instead of using the scripts embedded in the container image.
	// This allows updating scripts without rebuilding the image; changes take effect immediately
	// as the script is read on each invocation.
	// NOTE: The host path directory must contain nat-gateway.sh as this mount will overlay
	// the entire /kube-ovn directory in the container.
	// NOTE: Existing NAT gateways need to be recreated to pick up this config change.
	vpcNatGwScriptHostPath = cm.Data["natGwScriptHostPath"]
}
