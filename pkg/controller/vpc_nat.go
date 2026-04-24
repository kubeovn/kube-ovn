package controller

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func sanitizeNatGwScriptHostPath(rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", nil
	}
	if strings.ContainsAny(trimmedPath, " \t\n\r") {
		return "", errors.New("must not contain whitespace")
	}
	if !filepath.IsAbs(trimmedPath) {
		return "", errors.New("must be an absolute path")
	}

	return filepath.Clean(trimmedPath), nil
}

var vpcNatRuntimeConfigValue atomic.Pointer[vpcNatRuntimeConfig]

type vpcNatRuntimeConfig struct {
	image            string
	bgpSpeakerImage  string
	apiNadProvider   string
	scriptHostPath   string
	scriptPathSynced bool
}

func shouldSyncVpcNatGwScriptHostPath(currentConfig vpcNatRuntimeConfig, scriptHostPath string) bool {
	if currentConfig.scriptHostPath != scriptHostPath {
		return true
	}

	return scriptHostPath != "" && !currentConfig.scriptPathSynced
}

func currentVpcNatRuntimeConfig() vpcNatRuntimeConfig {
	if cfg := vpcNatRuntimeConfigValue.Load(); cfg != nil {
		return *cfg
	}
	return vpcNatRuntimeConfig{}
}

func storeVpcNatRuntimeConfig(cfg vpcNatRuntimeConfig) {
	vpcNatRuntimeConfigValue.Store(&cfg)
}

func (c *Controller) resyncVpcNatConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get ovn-vpc-nat-config, %w", err)
			klog.Error(err)
		}
		return
	}

	// Prefix used to generate the name of the StatefulSet/Pods for a NAT gateway.
	vpcNatGwNamePrefix := cm.Data["natGwNamePrefix"]
	if vpcNatGwNamePrefix != "" {
		util.SetVpcNatGwNamePrefix(vpcNatGwNamePrefix)
	} else {
		util.SetVpcNatGwNamePrefix(util.VpcNatGwNameDefaultPrefix)
	}

	// Image we're using to provision the NAT gateways
	image, exist := cm.Data["image"]
	if !exist {
		err = fmt.Errorf("%s should have image field", util.VpcNatConfig)
		klog.Error(err)
		return
	}
	// Host path for NAT gateway scripts (optional)
	// When configured, the NAT gateway pod will mount scripts from this host path
	// instead of using the scripts embedded in the container image.
	// This allows updating scripts without rebuilding the image; changes take effect immediately
	// as the script is read on each invocation.
	// NOTE: The host path directory must contain nat-gateway.sh as this mount will overlay
	// the entire /kube-ovn directory in the container.
	// NOTE: Existing NAT gateways need to be recreated to pick up this config change.
	rawScriptHostPath := cm.Data["natGwScriptHostPath"]
	scriptHostPath, err := sanitizeNatGwScriptHostPath(rawScriptHostPath)
	if err != nil {
		klog.Warningf("ignore invalid natGwScriptHostPath %q in %s: %v", rawScriptHostPath, util.VpcNatConfig, err)
		scriptHostPath = ""
	}

	currentConfig := currentVpcNatRuntimeConfig()
	// Image for the BGP sidecar of the gateway (optional)
	bgpSpeakerImage := cm.Data["bgpSpeakerImage"]

	// NetworkAttachmentDefinition provider for the BGP speaker to call the API server
	apiNadProvider := cm.Data["apiNadProvider"]

	newConfig := vpcNatRuntimeConfig{
		image:            image,
		bgpSpeakerImage:  bgpSpeakerImage,
		apiNadProvider:   apiNadProvider,
		scriptHostPath:   scriptHostPath,
		scriptPathSynced: !shouldSyncVpcNatGwScriptHostPath(currentConfig, scriptHostPath),
	}

	// Publish the new config before enqueuing gateways so workers always observe
	// the latest script host path when they reconcile queued items.
	storeVpcNatRuntimeConfig(newConfig)

	if !newConfig.scriptPathSynced {
		gws, err := c.vpcNatGatewayLister.List(labels.Everything())
		if err != nil {
			err = fmt.Errorf("failed to list vpc nat gateways when applying natGwScriptHostPath change, %w", err)
			klog.Error(err)
			return
		}

		for _, gw := range gws {
			c.addOrUpdateVpcNatGatewayQueue.Add(gw.Name)
		}
		newConfig.scriptPathSynced = true
		storeVpcNatRuntimeConfig(newConfig)
		klog.Infof("natGwScriptHostPath changed, enqueued %d vpc nat gateways", len(gws))
	}
}
