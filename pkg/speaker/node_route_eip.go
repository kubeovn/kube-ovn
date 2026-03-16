package speaker

// This file implements EIP BGP announcement via node route mode.
//
// Mode: eip-bgp-via-node-route
// This mode runs kube-ovn-speaker in host network mode (like Pod/Service IP announcement)
// instead of running inside the NAT gateway pod. It monitors IptablesEIP resources and
// announces EIP addresses for local node's vpc-nat-gw pods.
//
// Control flow:
//  1. Speaker starts in NodeRouteEIPMode with host network enabled
//  2. On startup: enqueue all existing Ready EIPs for route recovery
//  3. On EIP add/update event: check if the associated vpc-nat-gw pod runs on this node
//  4. If yes and EIP is ready: announce the EIP via BGP
//  5. On EIP delete: withdraw the EIP from BGP announcement
//
// Prerequisites:
//   - Speaker must run in host network mode with NodeRouteEIPMode enabled
//   - NodeName must be configured (via --node-name or NODE_NAME env)
//   - vpc-nat-gw pods must have proper labels for identification

import (
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// initNodeRouteEIPMode initializes the EIP informer handlers for node route mode.
// This should be called during controller initialization when NodeRouteEIPMode is enabled.
func (c *Controller) initNodeRouteEIPMode() {
	if c.eipQueue == nil {
		c.eipQueue = workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: "NodeRouteEIP"},
		)
	}

	// Register EIP event handlers for node route mode
	eipInformer := c.kubeovnInformerFactory.Kubeovn().V1().IptablesEIPs().Informer()
	_, _ = eipInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueAddNodeRouteEIP,
		UpdateFunc: c.enqueueUpdateNodeRouteEIP,
		DeleteFunc: c.enqueueDeleteNodeRouteEIP,
	})
}

// enqueueAddNodeRouteEIP handles add events for IptablesEIP in node route mode.
// This is primarily for speaker restart recovery: when the speaker restarts,
// the informer triggers Add events for all existing resources. Ready EIPs
// need to be re-processed to restore their BGP announcements.
func (c *Controller) enqueueAddNodeRouteEIP(obj any) {
	eip, ok := obj.(*kubeovnv1.IptablesEIP)
	if !ok {
		klog.Errorf("expected IptablesEIP but got %T", obj)
		return
	}

	// Only enqueue ready EIPs - non-ready EIPs will be processed when they become ready
	if !eip.Status.Ready {
		return
	}

	klog.V(3).Infof("enqueue add iptables-eip %s for BGP route recovery", eip.Name)
	c.eipQueue.Add(eip.Name)
}

// enqueueUpdateNodeRouteEIP handles update events for IptablesEIP in node route mode.
// This processes EIPs that become ready, become non-ready, or have their NAT gateway changed.
func (c *Controller) enqueueUpdateNodeRouteEIP(_, newObj any) {
	eip, ok := newObj.(*kubeovnv1.IptablesEIP)
	if !ok {
		klog.Errorf("expected IptablesEIP but got %T", newObj)
		return
	}

	// Skip EIPs that are being deleted
	if eip.DeletionTimestamp != nil {
		return
	}

	// Always enqueue on update to handle:
	// - EIP becomes ready: announce route
	// - EIP becomes non-ready: withdraw route
	// - BGP annotation changed: add/remove route
	// - NatGwDp changed: update route announcement
	klog.V(3).Infof("enqueue update iptables-eip %s", eip.Name)
	c.eipQueue.Add(eip.Name)
}

// enqueueDeleteNodeRouteEIP handles delete events for IptablesEIP in node route mode.
// When an EIP is deleted, we withdraw its BGP announcement immediately (not queued).
func (c *Controller) enqueueDeleteNodeRouteEIP(obj any) {
	var eip *kubeovnv1.IptablesEIP
	switch t := obj.(type) {
	case *kubeovnv1.IptablesEIP:
		eip = t
	case cache.DeletedFinalStateUnknown:
		e, ok := t.Obj.(*kubeovnv1.IptablesEIP)
		if !ok {
			klog.Warningf("unexpected object type in DeletedFinalStateUnknown: %T", t.Obj)
			return
		}
		eip = e
	default:
		klog.Warningf("unexpected object type: %T", obj)
		return
	}

	klog.V(3).Infof("withdrawing routes for deleted iptables-eip %s", eip.Name)
	c.withdrawEIPRoutes(eip)
}

// withdrawEIPRoutes withdraws BGP routes for an EIP.
// This is called when an EIP is deleted, becomes non-ready, loses BGP annotation,
// or when the NAT gateway pod moves to another node.
func (c *Controller) withdrawEIPRoutes(eip *kubeovnv1.IptablesEIP) {
	var errs []error
	var withdrawn []string
	for _, ip := range []string{eip.Spec.V4ip, eip.Spec.V6ip} {
		if ip == "" {
			continue
		}
		if !c.isRouteAnnounced(ip) {
			klog.V(3).Infof("BGP route for EIP %s not announced, skipping withdraw", ip)
			continue
		}
		if err := c.delRoute(ip); err != nil {
			klog.Errorf("failed to withdraw BGP route for EIP %s: %v", ip, err)
			errs = append(errs, err)
		} else {
			withdrawn = append(withdrawn, ip)
		}
	}

	if len(withdrawn) > 0 {
		klog.Infof("withdrawn BGP routes for iptables-eip %s: %v", eip.Name, withdrawn)
	}
	if len(errs) > 0 {
		klog.Errorf("errors withdrawing BGP routes for EIP %s: %v", eip.Name, errors.Join(errs...))
	}
}

// runNodeRouteEIPWorker runs the worker for processing EIP events in node route mode.
func (c *Controller) runNodeRouteEIPWorker() {
	for c.processNextNodeRouteEIPItem() {
	}
}

// processNextNodeRouteEIPItem processes the next EIP work item from the queue.
func (c *Controller) processNextNodeRouteEIPItem() bool {
	key, shutdown := c.eipQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.eipQueue.Done(key)
		if err := c.handleAddOrUpdateNodeRouteEIP(key); err != nil {
			c.eipQueue.AddRateLimited(key)
			return fmt.Errorf("error processing EIP %q: %w, requeuing", key, err)
		}
		c.eipQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		klog.Error(err)
	}
	return true
}

// handleAddOrUpdateNodeRouteEIP processes an EIP add or update event.
// It checks if the associated vpc-nat-gw pod runs on the local node and
// announces the EIP via BGP if so, or withdraws it if conditions are not met.
func (c *Controller) handleAddOrUpdateNodeRouteEIP(eipName string) error {
	klog.V(3).Infof("handling add/update iptables-eip %s in node route mode", eipName)

	eip, err := c.eipLister.Get(eipName)
	if err != nil {
		// EIP was deleted, skip processing
		klog.V(3).Infof("iptables-eip %s not found, may have been deleted", eipName)
		return nil
	}

	// Skip non-ready EIPs - they will be processed when they become ready
	// Periodic reconcile (syncNodeRouteEIPs) will clean up any stale routes
	if !eip.Status.Ready {
		return nil
	}

	// Check if BGP annotation is enabled for this EIP
	if eip.Annotations[util.BgpAnnotation] != "true" {
		klog.V(3).Infof("iptables-eip %s does not have BGP annotation, skipping", eipName)
		// Withdraw any existing routes for this EIP (in case annotation was removed)
		c.withdrawEIPRoutes(eip)
		return nil
	}

	// Check if the NAT gateway pod is running on the local node
	if !c.hasNatGwPodOnLocalNode(eip) {
		klog.V(3).Infof("NAT GW pod for iptables-eip %s not on local node %s, withdrawing routes",
			eipName, c.config.NodeName)
		// Withdraw any existing routes for this EIP (in case pod moved to another node)
		c.withdrawEIPRoutes(eip)
		return nil
	}

	// Announce routes only if not already announced (idempotent)
	var errs []error
	var announced []string
	for _, ip := range []string{eip.Spec.V4ip, eip.Spec.V6ip} {
		if ip == "" {
			continue
		}
		if c.isRouteAnnounced(ip) {
			klog.V(3).Infof("BGP route for EIP %s already announced, skipping", ip)
			continue
		}
		if err := c.addRoute(ip); err != nil {
			klog.Errorf("failed to announce BGP route for EIP %s: %v", ip, err)
			errs = append(errs, err)
		} else {
			announced = append(announced, ip)
		}
	}

	if len(announced) > 0 {
		klog.Infof("announced BGP routes for iptables-eip %s: %v", eipName, announced)
	}

	return errors.Join(errs...)
}

// hasNatGwPodOnLocalNode checks if the NAT gateway pod for this EIP is running on the local node.
// NAT gateway pod name follows the pattern: vpc-nat-gw-{natGwDp}-0
// Returns true only if the pod exists, is Running, and is scheduled on the local node.
func (c *Controller) hasNatGwPodOnLocalNode(eip *kubeovnv1.IptablesEIP) bool {
	if eip.Spec.NatGwDp == "" {
		klog.Errorf("iptables-eip %s has empty NatGwDp field", eip.Name)
		return false
	}

	podName := util.GenNatGwPodName(eip.Spec.NatGwDp)
	// Use gwPodsLister which watches pods in VpcNatGwNamespace
	pod, err := c.gwPodsLister.Pods(c.config.VpcNatGwNamespace).Get(podName)
	if err != nil {
		klog.V(3).Infof("failed to get NAT GW pod %s/%s: %v", c.config.VpcNatGwNamespace, podName, err)
		return false
	}

	// Only announce routes for Running pods on local node
	return pod.Spec.NodeName == c.config.NodeName && pod.Status.Phase == corev1.PodRunning
}

// syncNodeRouteEIPs performs a full reconciliation of all EIPs in node route mode.
// This method finds all EIPs associated with local NAT gateway pods and announces them.
// It also withdraws any EIPs that should no longer be announced.
func (c *Controller) syncNodeRouteEIPs() error {
	expectedPrefixes := make(prefixMap)

	// List all EIPs
	eips, err := c.eipLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list EIPs: %w", err)
	}

	for _, eip := range eips {
		// Only process ready EIPs with BGP annotation
		if eip.Annotations[util.BgpAnnotation] != "true" || !eip.Status.Ready {
			continue
		}

		// Only announce EIPs for local NAT gateway pods
		if !c.hasNatGwPodOnLocalNode(eip) {
			continue
		}

		for _, ip := range []string{eip.Spec.V4ip, eip.Spec.V6ip} {
			if ip != "" {
				addExpectedPrefix(ip, expectedPrefixes)
			}
		}
	}

	return c.reconcileRoutes(expectedPrefixes)
}

// startNodeRouteEIPWorkers starts the worker goroutines for processing EIP events.
// This should be called from the controller's Run method.
func (c *Controller) startNodeRouteEIPWorkers(stopCh <-chan struct{}, workers int) {
	klog.Infof("starting %d node route EIP workers", workers)
	for i := 0; i < workers; i++ {
		go wait.Until(c.runNodeRouteEIPWorker, time.Second, stopCh)
	}
}

// shutdownNodeRouteEIPWorkers shuts down the EIP work queue.
func (c *Controller) shutdownNodeRouteEIPWorkers() {
	if c.eipQueue != nil {
		c.eipQueue.ShutDown()
	}
}

// enqueueAllReadyEIPs enqueues all ready EIPs on speaker startup for route recovery.
// This ensures that after a speaker restart, all local EIPs are re-announced.
func (c *Controller) enqueueAllReadyEIPs() error {
	eips, err := c.eipLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list EIPs for startup recovery: %w", err)
	}

	count := 0
	for _, eip := range eips {
		// Only enqueue ready EIPs with BGP annotation
		if !eip.Status.Ready || eip.Annotations[util.BgpAnnotation] != "true" {
			continue
		}
		klog.V(3).Infof("enqueue ready iptables-eip %s on startup", eip.Name)
		c.eipQueue.Add(eip.Name)
		count++
	}

	klog.Infof("enqueued %d ready EIPs for startup recovery", count)
	return nil
}

// ReconcileNodeRouteEIPs performs periodic reconciliation in node route EIP mode.
// Called by the controller's Reconcile method to ensure BGP state is consistent.
func (c *Controller) ReconcileNodeRouteEIPs() {
	if err := c.syncNodeRouteEIPs(); err != nil {
		klog.Errorf("failed to reconcile node route EIPs: %v", err)
	}
}
