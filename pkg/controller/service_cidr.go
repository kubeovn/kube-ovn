package controller

import (
	"context"
	"strings"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

func (c *Controller) startServiceCIDRInformer(ctx context.Context) {
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

	c.serviceCIDRInformerFactory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), c.serviceCIDRSynced) {
		util.LogFatalAndExit(nil, "failed to wait for ServiceCIDR cache to sync")
	}
	klog.Info("ServiceCIDR informer cache synced")

	c.serviceCIDRStore.OnChange(c.reconcileForServiceCIDRChange)

	// Re-fire after sync so consumers see the merged set even when no events follow.
	scs, err := c.serviceCIDRLister.List(labels.Everything())
	if err != nil {
		klog.Warningf("failed to list ServiceCIDRs after sync: %v", err)
		return
	}
	for _, sc := range scs {
		c.serviceCIDRStore.UpsertFromAPI(sc.Name, readyServiceCIDRs(sc))
	}
}

func (c *Controller) tryStartServiceCIDRInformer(ctx context.Context) bool {
	exists, err := c.isServiceCIDRAPIInstalled()
	if err != nil {
		klog.Warningf("failed to check ServiceCIDR API: %v", err)
		return false
	}
	if !exists {
		return false
	}
	klog.Info("ServiceCIDR API found, starting informer")
	c.startServiceCIDRInformer(ctx)
	return true
}

// StartServiceCIDRInformerFactory wires up the optional networking.k8s.io/v1
// ServiceCIDR informer. On clusters without the API (K8s <1.31, or 1.31/1.32
// with the feature gate disabled) the informer is never started and the
// ServiceCIDRStore stays at its flag-derived fallback.
func (c *Controller) StartServiceCIDRInformerFactory(ctx context.Context) {
	if c.tryStartServiceCIDRInformer(ctx) {
		return
	}
	klog.Info("ServiceCIDR API not found at startup, will check periodically in background")
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.tryStartServiceCIDRInformer(ctx) {
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

// readyServiceCIDRs returns Spec.CIDRs when the object's Ready condition is
// True. ServiceCIDRs that are still being initialized or already terminating
// are skipped, matching the apiserver allocator's own behavior.
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

// reconcileForServiceCIDRChange enqueues every object whose data-plane artifact
// embeds a Service CIDR so that its existing reconciler rebuilds the artifact
// against the freshly merged set.
func (c *Controller) reconcileForServiceCIDRChange() {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
	} else {
		for _, s := range subnets {
			if s.Spec.U2OInterconnection {
				c.addOrUpdateSubnetQueue.Add(s.Name)
			}
		}
	}

	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpcs: %v", err)
	} else {
		for _, v := range vpcs {
			if strings.ToLower(v.Annotations[util.VpcLbAnnotation]) == "on" {
				c.addOrUpdateVpcQueue.Add(v.Name)
			}
		}
	}

	// VpcNatGateways are intentionally NOT re-enqueued here. The route
	// annotation is written into the StatefulSet template by
	// genNatGwStatefulSet, but handleAddOrUpdateVpcNatGw only updates the
	// StatefulSet when isVpcNatGwChanged() returns true (Spec-only diff),
	// and handleInitVpcNatGw bails out as soon as the pod carries
	// VpcNatGatewayInitAnnotation. Newly created NAT gateways still pick up
	// the current store because their add path runs genNatGwStatefulSet
	// against c.serviceCIDRStore; existing NAT gateways keep their original
	// service routes until the pod is recreated by other means.
}
