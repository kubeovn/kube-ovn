package controller

import (
	"context"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) isBgpConfCRDInstalled() (bool, error) {
	return apiResourceExists(
		c.config.KubeOvnClient.Discovery(),
		kubeovnv1.SchemeGroupVersion.String(),
		util.ObjectKind[*kubeovnv1.BgpConf](),
	)
}

func (c *Controller) isEvpnConfCRDInstalled() (bool, error) {
	return apiResourceExists(
		c.config.KubeOvnClient.Discovery(),
		kubeovnv1.SchemeGroupVersion.String(),
		util.ObjectKind[*kubeovnv1.EvpnConf](),
	)
}

// startBgpEvpnConfInformer registers the informers for the CRDs that have been
// confirmed present, starts the shared informer factory and waits for the new
// caches to sync. The lister/synced fields are published to the rest of the
// controller only after WaitForCacheSync succeeds, so concurrent workers
// never observe a non-nil lister backed by an empty cache. Returns true when
// every requested informer has synced.
func (c *Controller) startBgpEvpnConfInformer(ctx context.Context, bgpReady, evpnReady bool) bool {
	var (
		syncs   []cache.InformerSynced
		publish []func()
	)

	if bgpReady && c.bgpConfSynced == nil {
		informer := c.kubeovnInformerFactory.Kubeovn().V1().BgpConves()
		syncs = append(syncs, informer.Informer().HasSynced)
		publish = append(publish, func() {
			lister := informer.Lister()
			c.bgpConfLister.Store(&lister)
			c.bgpConfSynced = informer.Informer().HasSynced
		})
	}
	if evpnReady && c.evpnConfSynced == nil {
		informer := c.kubeovnInformerFactory.Kubeovn().V1().EvpnConves()
		syncs = append(syncs, informer.Informer().HasSynced)
		publish = append(publish, func() {
			lister := informer.Lister()
			c.evpnConfLister.Store(&lister)
			c.evpnConfSynced = informer.Informer().HasSynced
		})
	}
	if len(syncs) == 0 {
		return true
	}

	// SharedInformerFactory.Start is idempotent: only informers that have not
	// been started yet will be launched.
	c.kubeovnInformerFactory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), syncs...) {
		klog.Error("failed to wait for BgpConf/EvpnConf caches to sync")
		return false
	}

	for _, p := range publish {
		p()
	}
	klog.Info("BgpConf/EvpnConf informer cache synced")
	return true
}

func (c *Controller) tryStartBgpEvpnConfInformer(ctx context.Context) bool {
	bgp, err := c.isBgpConfCRDInstalled()
	if err != nil {
		klog.Warningf("failed to check if BgpConf CRD exists: %v", err)
	}
	evpn, err := c.isEvpnConfCRDInstalled()
	if err != nil {
		klog.Warningf("failed to check if EvpnConf CRD exists: %v", err)
	}
	if !bgp && !evpn {
		return false
	}
	if !c.startBgpEvpnConfInformer(ctx, bgp, evpn) {
		// cache sync failed (e.g. ctx cancellation, persistent list/watch
		// errors) — keep polling so a follow-up attempt can publish the
		// listers once the API recovers.
		return false
	}
	// Keep polling until both CRDs are available so a partial install
	// (e.g. only one CRD applied) is eventually completed.
	return bgp && evpn
}

// StartBgpEvpnConfInformerFactory starts the optional BgpConf/EvpnConf informers.
//
// Both CRDs were introduced in v1.16.0 to back the BGP/EVPN sub-feature of
// vpc-egress-gateway. They are optional: clusters that don't use BGP/EVPN, or
// that were upgraded from <v1.16 with Helm (which doesn't re-apply the `crds/`
// directory on `helm upgrade`), may run without them. In that case the
// controller must still start its workers; this function performs a best-effort
// start with a background retry loop so the informers are picked up
// automatically once the CRDs become available.
func (c *Controller) StartBgpEvpnConfInformerFactory(ctx context.Context) {
	if c.tryStartBgpEvpnConfInformer(ctx) {
		return
	}
	klog.Info("BgpConf/EvpnConf CRDs not fully installed at startup, will check periodically in background")
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.tryStartBgpEvpnConfInformer(ctx) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
