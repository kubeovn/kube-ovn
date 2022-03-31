package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueUpdateProviderNetwork(_, obj interface{}) {
	if !c.isLeader() {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue update provider network %s", key)
	c.updateProviderNetworkQueue.Add(key)
}

func (c *Controller) runUpdateProviderNetworkWorker() {
	for c.processNextUpdateProviderNetworkWorkItem() {
	}
}

func (c *Controller) processNextUpdateProviderNetworkWorkItem() bool {
	obj, shutdown := c.updateProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateProviderNetworkQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateProviderNetworkQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateProviderNetwork(key); err != nil {
			c.updateProviderNetworkQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateProviderNetworkQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) handleUpdateProviderNetwork(key string) error {
	pn, err := c.providerNetworksLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}

	if providerNetworkIsReady(pn, nodes) != pn.Status.Ready {
		newPn := pn.DeepCopy()
		newPn.Status.Ready = !pn.Status.Ready
		_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), newPn, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) resyncProviderNetworkStatus() {
	nodeList, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get nodes %v", err)
		return
	}
	pnList, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get provider networks %v", err)
		return
	}

	for _, pn := range pnList {
		if providerNetworkIsReady(pn, nodeList) != pn.Status.Ready {
			newPn := pn.DeepCopy()
			newPn.Status.Ready = !newPn.Status.Ready
			_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), newPn, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
			}
		}
	}
}

func providerNetworkIsReady(pn *kubeovnv1.ProviderNetwork, nodes []*corev1.Node) bool {
	for _, node := range nodes {
		if !util.ContainsString(pn.Spec.ExcludeNodes, node.Name) &&
			!util.ContainsString(pn.Status.ReadyNodes, node.Name) {
			return false
		}
	}
	return true
}
