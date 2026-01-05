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

// the network-attachment-definition CRD is not required to be installed so
// periodically check and see if we should start the informer.
func (c *Controller) StartNetAttachInformerFactory(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				exists, err := c.isNetAttachCRDInstalled()
				if err != nil {
					klog.Errorf("checking network attachment CRD exists: %v", err)
					continue
				}

				if exists {
					klog.Info("Start attachment informer")

					c.netAttachInformerFactory.Start(ctx.Done())
					if !cache.WaitForCacheSync(ctx.Done(), c.netAttachSynced) {
						util.LogFatalAndExit(nil, "failed to wait for network attachment cache to sync")
					}

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
