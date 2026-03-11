package controller

import (
	"context"
	"encoding/json"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/logging"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) isNetAttachCRDInstalled() (bool, error) {
	return apiResourceExists(
		c.config.AttachNetClient.Discovery(),
		nadv1.SchemeGroupVersion.String(),
		util.ObjectKind[*nadv1.NetworkAttachmentDefinition](),
	)
}

// startNetAttachInformer starts the NAD informer and waits for cache sync.
func (c *Controller) startNetAttachInformer(ctx context.Context) {
	c.netAttachInformerFactory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), c.netAttachSynced) {
		util.LogFatalAndExit(nil, "failed to wait for network attachment cache to sync")
	}
	klog.Info("Network attachment informer cache synced")
}

// tryStartNetAttachInformer checks if NAD CRD is installed and starts the informer if so.
// Returns true if informer was started, false otherwise.
func (c *Controller) tryStartNetAttachInformer(ctx context.Context) bool {
	exists, err := c.isNetAttachCRDInstalled()
	if err != nil {
		klog.Warningf("failed to check if network attachment CRD exists: %v", err)
		return false
	}
	if !exists {
		return false
	}
	klog.Info("Network attachment CRD found, starting informer")
	c.startNetAttachInformer(ctx)
	return true
}

// StartNetAttachInformerFactory starts the network attachment definition (NAD) informer.
// This MUST be called before other informers that depend on NAD cache (Pod, Subnet, VpcNatGateway, etc.)
//
// The NAD CRD is optional - if installed, we start the informer synchronously and wait for
// cache sync before returning. This ensures NAD cache is ready when other controllers start
// processing resources that reference NADs.
//
// If the CRD is not installed at startup, we start a background goroutine to periodically
// check and start the informer when the CRD becomes available.
func (c *Controller) StartNetAttachInformerFactory(ctx context.Context) {
	if c.tryStartNetAttachInformer(ctx) {
		return
	}

	// CRD not found at startup, start background check loop
	klog.Info("Network attachment CRD not found at startup, will check periodically in background")
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.tryStartNetAttachInformer(ctx) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func loadNetConf(bytes []byte) (*multustypes.DelegateNetConf, error) {
	delegateConf := &multustypes.DelegateNetConf{}
	if err := json.Unmarshal(bytes, &delegateConf.Conf); err != nil {
		return nil, logging.Errorf("LoadDelegateNetConf: error unmarshalling delegate config: %v", err)
	}

	if delegateConf.Conf.Type == "" {
		if err := multustypes.LoadDelegateNetConfList(bytes, delegateConf); err != nil {
			return nil, logging.Errorf("LoadDelegateNetConf: failed with: %v", err)
		}
	}
	return delegateConf, nil
}
