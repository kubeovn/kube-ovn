package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const serviceL2StatusServiceIndex = "service"

func serviceL2StatusServiceKey(status *metallbv1beta1.ServiceL2Status) (string, bool) {
	if status == nil || status.Status.ServiceNamespace == "" || status.Status.ServiceName == "" {
		return "", false
	}
	return status.Status.ServiceNamespace + "/" + status.Status.ServiceName, true
}

func indexServiceL2StatusByService(obj any) ([]string, error) {
	status, ok := obj.(*metallbv1beta1.ServiceL2Status)
	if !ok {
		return nil, fmt.Errorf("expected ServiceL2Status but got %T", obj)
	}
	key, ok := serviceL2StatusServiceKey(status)
	if !ok {
		return nil, nil
	}
	return []string{key}, nil
}

func (c *Controller) getServiceL2StatusNode(namespace, serviceName string) (string, bool, error) {
	c.serviceL2StatusMutex.RLock()
	started := c.serviceL2StatusStarted
	indexer := c.serviceL2StatusIndexer
	synced := c.serviceL2StatusSynced
	c.serviceL2StatusMutex.RUnlock()
	if !started {
		return "", true, nil
	}
	if indexer == nil || synced == nil || !synced() {
		return "", false, nil
	}

	objects, err := indexer.ByIndex(serviceL2StatusServiceIndex, namespace+"/"+serviceName)
	if err != nil {
		return "", true, fmt.Errorf("failed to list ServiceL2Statuses for service %s/%s: %w", namespace, serviceName, err)
	}

	var node string
	for _, obj := range objects {
		status, ok := obj.(*metallbv1beta1.ServiceL2Status)
		if !ok || status.Status.Node == "" {
			continue
		}
		if node != "" && node != status.Status.Node {
			return "", true, fmt.Errorf("multiple announcing nodes found for service %s/%s: %s and %s", namespace, serviceName, node, status.Status.Node)
		}
		node = status.Status.Node
	}
	return node, true, nil
}

func (c *Controller) enqueueServiceL2Status(obj any) {
	status, ok := obj.(*metallbv1beta1.ServiceL2Status)
	if !ok {
		tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !isTombstone {
			return
		}
		status, ok = tombstone.Obj.(*metallbv1beta1.ServiceL2Status)
		if !ok {
			return
		}
	}
	key, ok := serviceL2StatusServiceKey(status)
	if !ok || c.addOrUpdateEndpointSliceQueue == nil {
		return
	}
	c.addOrUpdateEndpointSliceQueue.Add(key)
}

func newMetalLBRESTClient(config *rest.Config) (rest.Interface, error) {
	if config == nil {
		return nil, errors.New("kubernetes REST config is nil")
	}

	scheme := runtime.NewScheme()
	if err := metallbv1beta1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to register MetalLB API types: %w", err)
	}

	metalLBConfig := rest.CopyConfig(config)
	metalLBConfig.GroupVersion = &metallbv1beta1.GroupVersion
	metalLBConfig.APIPath = "/apis"
	metalLBConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme).WithoutConversion()
	client, err := rest.RESTClientFor(metalLBConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create MetalLB REST client: %w", err)
	}
	return client, nil
}

func (c *Controller) tryStartServiceL2StatusInformer(ctx context.Context) bool {
	c.serviceL2StatusMutex.RLock()
	started := c.serviceL2StatusStarted
	c.serviceL2StatusMutex.RUnlock()
	if started {
		return true
	}

	exists, err := util.APIResourceExists(
		c.config.KubeClient.Discovery(),
		metallbv1beta1.GroupVersion.String(),
		util.ObjectKind[*metallbv1beta1.ServiceL2Status](),
	)
	if err != nil {
		klog.Warningf("failed to check ServiceL2Status API: %v", err)
		return false
	}
	if !exists {
		return false
	}

	client, err := newMetalLBRESTClient(c.config.KubeRestConfig)
	if err != nil {
		klog.Warningf("failed to initialize ServiceL2Status informer: %v", err)
		return false
	}
	informer := cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client, "servicel2statuses", metav1.NamespaceAll, fields.Everything()),
		&metallbv1beta1.ServiceL2Status{},
		0,
		cache.Indexers{serviceL2StatusServiceIndex: indexServiceL2StatusByService},
	)
	if _, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueServiceL2Status,
		UpdateFunc: func(_, newObj any) {
			c.enqueueServiceL2Status(newObj)
		},
		DeleteFunc: c.enqueueServiceL2Status,
	}); err != nil {
		klog.Warningf("failed to add ServiceL2Status event handler: %v", err)
		return false
	}

	c.serviceL2StatusMutex.Lock()
	if c.serviceL2StatusStarted {
		c.serviceL2StatusMutex.Unlock()
		return true
	}
	c.serviceL2StatusIndexer = informer.GetIndexer()
	c.serviceL2StatusSynced = informer.HasSynced
	c.serviceL2StatusStarted = true
	c.serviceL2StatusMutex.Unlock()

	go informer.Run(ctx.Done())
	go func() {
		if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
			return
		}
		for _, obj := range informer.GetIndexer().List() {
			c.enqueueServiceL2Status(obj)
		}
	}()
	klog.Info("ServiceL2Status API found, informer started")
	return true
}

func (c *Controller) StartServiceL2StatusInformer(ctx context.Context) {
	if !c.config.EnableLb || !c.config.EnableOVNLBPreferLocal {
		return
	}

	if c.tryStartServiceL2StatusInformer(ctx) {
		return
	}

	klog.Info("ServiceL2Status API not found at startup, will check periodically in background")
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.tryStartServiceL2StatusInformer(ctx) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
