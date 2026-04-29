package daemon

import (
	"context"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) isServiceCIDRAPIInstalled() (bool, error) {
	return util.APIResourceExists(
		c.config.KubeClient.Discovery(),
		networkingv1.SchemeGroupVersion.String(),
		"ServiceCIDR",
	)
}

func (c *Controller) startServiceCIDRInformer(stopCh <-chan struct{}) {
	informer := c.serviceCIDRInformerFactory.Networking().V1().ServiceCIDRs()
	if _, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onServiceCIDRAdd,
		UpdateFunc: c.onServiceCIDRUpdate,
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
		c.serviceCIDRStore.UpsertFromAPI(sc.Name, readyServiceCIDRs(sc))
	}
}

func (c *Controller) tryStartServiceCIDRInformer(stopCh <-chan struct{}) bool {
	exists, err := c.isServiceCIDRAPIInstalled()
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
	c.serviceCIDRInformerFactory = informers.NewSharedInformerFactoryWithOptions(c.config.KubeClient, 0,
		informers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}),
	)
	if c.tryStartServiceCIDRInformer(stopCh) {
		return
	}
	klog.Info("ServiceCIDR API not found at startup, will check periodically in background")
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-stopCh
			cancel()
		}()
		for {
			select {
			case <-ticker.C:
				if c.tryStartServiceCIDRInformer(stopCh) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Controller) onServiceCIDRAdd(obj any) {
	sc, ok := obj.(*networkingv1.ServiceCIDR)
	if !ok {
		return
	}
	c.serviceCIDRStore.UpsertFromAPI(sc.Name, readyServiceCIDRs(sc))
}

func (c *Controller) onServiceCIDRUpdate(_, newObj any) {
	sc, ok := newObj.(*networkingv1.ServiceCIDR)
	if !ok {
		return
	}
	c.serviceCIDRStore.UpsertFromAPI(sc.Name, readyServiceCIDRs(sc))
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

func readyServiceCIDRs(sc *networkingv1.ServiceCIDR) []string {
	if sc == nil {
		return nil
	}
	ready := false
	for _, cond := range sc.Status.Conditions {
		if cond.Type == networkingv1.ServiceCIDRConditionReady {
			ready = cond.Status == metav1.ConditionTrue
			break
		}
	}
	if !ready {
		return nil
	}
	return sc.Spec.CIDRs
}
