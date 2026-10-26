package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) resyncProviderNetworkStatus() {
	klog.V(3).Infof("start to sync ProviderNetwork status")
	pns, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list provider network: %v", err)
		return
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return
	}

	pods, err := c.podsLister.Pods(c.config.PodNamespace).List(labels.Set{"app": "kube-ovn-cni"}.AsSelector())
	if err != nil {
		klog.Errorf("failed to list kube-ovn-cni pods: %v", err)
		return
	}

	podMap := make(map[string]*corev1.Pod, len(pods))
	for _, pod := range pods {
		podMap[pod.Spec.NodeName] = pod
	}

	for _, cachedPn := range pns {
		c.syncProviderNetworkStatus(cachedPn.DeepCopy(), nodes, podMap)
	}
}

func (c *Controller) syncProviderNetworkStatus(pn *kubeovnv1.ProviderNetwork, nodes []*corev1.Node, podMap map[string]*corev1.Pod) {
	if err := c.ensureProviderNetworkFinalizer(pn); err != nil {
		klog.Errorf("failed to ensure finalizer of provider network %s: %v", pn.Name, err)
		return
	}
	addressesUpdated, err := c.reconcileProviderNetworkNodeAddresses(pn, nodes)
	if err != nil {
		klog.Errorf("failed to reconcile VLAN subinterface addresses for provider network %s: %v", pn.Name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if !pn.DeletionTimestamp.IsZero() {
		pn.Status.Ready = false
		if _, updateErr := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(ctx, pn, metav1.UpdateOptions{}); updateErr != nil {
			klog.Errorf("failed to update deleting provider network %s: %v", pn.Name, updateErr)
			return
		}
		if err == nil && len(pn.Status.VlanSubinterfaceAddresses) == 0 {
			if err := c.removeProviderNetworkFinalizer(pn); err != nil {
				klog.Errorf("failed to remove finalizer of provider network %s: %v", pn.Name, err)
			}
		}
		return
	}
	var readyNodes, notReadyNodes, expectNodes []string
	pnReadyAnnotation := fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)
	pnErrMsgAnnotation := fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, pn.Name)

	var conditionsUpdated bool
	for _, node := range nodes {
		excluded, err := util.IsNodeExcludedFromProviderNetwork(node, pn)
		if err != nil {
			klog.Errorf("failed to check if node %s is excluded from provider network %s: %v", node.Name, pn.Name, err)
			continue
		}
		if excluded {
			if pn.Status.RemoveNodeConditions(node.Name) {
				conditionsUpdated = true
			}
			continue
		}
		if node.Labels[pnReadyAnnotation] == "true" {
			if pn.Status.SetNodeReady(node.Name, "InitOVSBridgeSucceeded", "") {
				conditionsUpdated = true
			}
			readyNodes = append(readyNodes, node.Name)
		} else {
			var errMsg string
			if pod := podMap[node.Name]; pod == nil {
				errMsg = fmt.Sprintf("kube-ovn-cni pod on node %s not found", node.Name)
				klog.Error(errMsg)
			} else {
				if len(pod.Annotations) != 0 {
					errMsg = pod.Annotations[pnErrMsgAnnotation]
				}
				if errMsg == "" {
					errMsg = "unknown error"
				}
			}

			if pn.Status.SetNodeNotReady(node.Name, "InitOVSBridgeFailed", errMsg) {
				conditionsUpdated = true
			}
			notReadyNodes = append(notReadyNodes, node.Name)
		}
	}

	expectNodes = append(readyNodes, notReadyNodes...)
	expectNodeSet := make(map[string]struct{}, len(expectNodes))
	for _, n := range expectNodes {
		expectNodeSet[n] = struct{}{}
	}
	var staleNodes []string
	for _, cond := range pn.Status.Conditions {
		if _, ok := expectNodeSet[cond.Node]; !ok {
			staleNodes = append(staleNodes, cond.Node)
		}
	}
	for _, node := range staleNodes {
		if pn.Status.RemoveNodeConditions(node) {
			conditionsUpdated = true
		}
	}

	if addressesUpdated || conditionsUpdated || len(util.DiffStringSlice(pn.Status.ReadyNodes, readyNodes)) != 0 ||
		len(util.DiffStringSlice(pn.Status.NotReadyNodes, notReadyNodes)) != 0 {
		pn.Status.ReadyNodes = readyNodes
		pn.Status.NotReadyNodes = notReadyNodes
		pn.Status.Ready = (len(notReadyNodes) == 0)
		if ctx.Err() != nil {
			return
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(ctx, pn, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
		}
	}
}

func (c *Controller) reconcileProviderNetworkNodeAddresses(pn *kubeovnv1.ProviderNetwork, nodes []*corev1.Node) (bool, error) {
	currentAllocationsByNode, err := c.providerNetworkAddresses(pn)
	if err != nil {
		return false, err
	}

	var errs []error
	desiredAllocations := make([]kubeovnv1.ProviderNetworkNodeAddress, 0, len(nodes))
	for _, node := range nodes {
		if !pn.DeletionTimestamp.IsZero() {
			break
		}
		excluded, err := util.IsNodeExcludedFromProviderNetwork(node, pn)
		if err != nil {
			// Unknown eligibility must not trigger cleanup of an existing IP CR.
			delete(currentAllocationsByNode, node.Name)
			errs = append(errs, fmt.Errorf("failed to check provider network eligibility for node %s: %w", node.Name, err))
			continue
		}
		if excluded || pn.Spec.DefaultInterfaceSubnet == "" || !pn.Spec.AutoCreateVlanSubinterfaces ||
			!strings.Contains(pn.Spec.DefaultInterface, ".") || !util.ProviderNetworkUsesDefaultInterface(pn, node.Name) {
			continue
		}

		// A desired node with an allocation error must not be mistaken for a cleanup target.
		delete(currentAllocationsByNode, node.Name)
		address, err := c.reconcileProviderNetworkNodeAddress(pn, node.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to reconcile address for node %s: %w", node.Name, err))
			continue
		}
		if address != nil {
			desiredAllocations = append(desiredAllocations, *address)
		}
	}

	// Current allocations that were not reused are no longer part of the desired state.
	for node := range currentAllocationsByNode {
		address, err := c.reconcileProviderNetworkAddressCleanup(pn, node)
		if address != nil {
			desiredAllocations = append(desiredAllocations, *address)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	compareByNode := func(a, b kubeovnv1.ProviderNetworkNodeAddress) int {
		return strings.Compare(a.Node, b.Node)
	}
	slices.SortFunc(desiredAllocations, compareByNode)
	currentAllocations := slices.Clone(pn.Status.VlanSubinterfaceAddresses)
	slices.SortFunc(currentAllocations, compareByNode)
	updated := !slices.Equal(desiredAllocations, currentAllocations)
	pn.Status.VlanSubinterfaceAddresses = desiredAllocations
	return updated, errors.Join(errs...)
}

func (c *Controller) ensureProviderNetworkFinalizer(pn *kubeovnv1.ProviderNetwork) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if !pn.DeletionTimestamp.IsZero() || slices.Contains(pn.Finalizers, util.ProviderNetworkCleanupFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(pn, util.ProviderNetworkCleanupFinalizer)
	updated, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Update(ctx, pn, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*pn = *updated
	return nil
}

func (c *Controller) removeProviderNetworkFinalizer(expected *kubeovnv1.ProviderNetwork) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pns := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks()
	pn, err := pns.Get(ctx, expected.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if pn.UID != expected.UID || pn.DeletionTimestamp.IsZero() || len(pn.Status.VlanSubinterfaceAddresses) != 0 {
		return fmt.Errorf("provider network %s changed before finalizer removal", expected.Name)
	}
	// The informer may not have observed a recently created allocation yet.
	// Confirm that none remain before letting the owner disappear.
	ips, err := c.config.KubeOvnClient.KubeovnV1().IPs().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range ips.Items {
		ip := &ips.Items[i]
		if providerNetworkOwnsNodeIP(pn, ip.Spec.NodeName, ip) {
			return fmt.Errorf("waiting for provider network IP %s to be deleted", ip.Name)
		}
	}
	controllerutil.RemoveFinalizer(pn, util.ProviderNetworkCleanupFinalizer)
	_, err = pns.Update(ctx, pn, metav1.UpdateOptions{})
	return err
}

func providerNetworkOwnsNodeIP(pn *kubeovnv1.ProviderNetwork, node string, ip *kubeovnv1.IP) bool {
	return ip.Spec.PodType == util.KindProviderNetwork && ip.Spec.NodeName == node &&
		slices.ContainsFunc(ip.OwnerReferences, func(owner metav1.OwnerReference) bool {
			return owner.APIVersion == kubeovnv1.SchemeGroupVersion.String() &&
				owner.Kind == util.KindProviderNetwork && owner.Name == pn.Name && owner.UID == pn.UID
		})
}
