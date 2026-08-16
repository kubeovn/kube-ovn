package controller

import (
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var vpcIPsecImage = ""

func (c *Controller) resyncVpcIPsecConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcIPsecConfig)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get %s: %v", util.VpcIPsecConfig, err)
		}
		return
	}

	image, exist := cm.Data["image"]
	if !exist {
		klog.Errorf("%s should have image field", util.VpcIPsecConfig)
		return
	}
	vpcIPsecImage = image

	if prefix := cm.Data["ipsecGwNamePrefix"]; prefix != "" {
		util.VpcIPsecGwNamePrefix = prefix
	} else {
		util.VpcIPsecGwNamePrefix = util.VpcIPsecGwNameDefaultPrefix
	}
}
