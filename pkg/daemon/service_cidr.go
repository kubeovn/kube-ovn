package daemon

import (
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) startServiceCIDRInformer(stopCh <-chan struct{}) {
	informer := c.serviceCIDRInformerFactory.Networking().V1().ServiceCIDRs()
	if _, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onServiceCIDRChange,
		UpdateFunc: func(_, newObj any) { c.onServiceCIDRChange(newObj) },
		DeleteFunc: c.onServiceCIDRDelete,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add ServiceCIDR event handler")
	}
	c.serviceCIDRLister = informer.Lister()
	c.serviceCIDRSynced = informer.Informer().HasSynced

	c.serviceCIDRInformerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, c.serviceCIDRSynced) {
		util.LogFatalAndExit(nil, "failed to wait for ServiceCIDR cache to sync")
	}
	klog.Info("ServiceCIDR informer cache synced")

	scs, err := c.serviceCIDRLister.List(labels.Everything())
	if err != nil {
		klog.Warningf("failed to list ServiceCIDRs after sync: %v", err)
		return
	}
	for _, sc := range scs {
		c.serviceCIDRStore.UpsertFromAPI(sc.Name, util.ReadyServiceCIDRs(sc))
	}
}

func (c *Controller) tryStartServiceCIDRInformer(stopCh <-chan struct{}) bool {
	exists, err := util.APIResourceExists(
		c.config.KubeClient.Discovery(),
		networkingv1.SchemeGroupVersion.String(),
		util.ObjectKind[*networkingv1.ServiceCIDR](),
	)
	if err != nil {
		klog.Warningf("failed to check ServiceCIDR API: %v", err)
		return false
	}
	if !exists {
		return false
	}
	klog.Info("ServiceCIDR API found, starting informer")
	c.startServiceCIDRInformer(stopCh)
	return true
}

// StartServiceCIDRInformerFactory starts the optional ServiceCIDR informer.
// On clusters without the API the merged set stays at flag-derived fallback.
// The 3-second setIPSet/setIptables loop in gateway_linux.go automatically
// picks up the merged set on each tick — no precise enqueue is needed here.
func (c *Controller) StartServiceCIDRInformerFactory(stopCh <-chan struct{}) {
	if c.tryStartServiceCIDRInformer(stopCh) {
		return
	}
	klog.Info("ServiceCIDR API not found at startup, will check periodically in background")
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.tryStartServiceCIDRInformer(stopCh) {
					return
				}
			case <-stopCh:
				return
			}
		}
	}()
}

func (c *Controller) onServiceCIDRChange(obj any) {
	sc, ok := obj.(*networkingv1.ServiceCIDR)
	if !ok {
		return
	}
	c.serviceCIDRStore.UpsertFromAPI(sc.Name, util.ReadyServiceCIDRs(sc))
}

func (c *Controller) onServiceCIDRDelete(obj any) {
	sc, ok := obj.(*networkingv1.ServiceCIDR)
	if !ok {
		tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !isTombstone {
			return
		}
		sc, ok = tombstone.Obj.(*networkingv1.ServiceCIDR)
		if !ok {
			return
		}
	}
	c.serviceCIDRStore.DeleteFromAPI(sc.Name)
}
