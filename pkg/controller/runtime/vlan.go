package controllerruntime

import (
	"context"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	crrecorder "sigs.k8s.io/controller-runtime/pkg/recorder"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type vlanOVN interface {
	LogicalSwitchPortExists(name string) (bool, error)
	SetLogicalSwitchPortVlanTag(lspName string, vlanID int) error
}

// VlanReconciler is the first business reconciler in the controller-runtime
// migration. It owns VLAN conflict/status convergence and localnet VLAN tags.
type VlanReconciler struct {
	client.Client
	OVN                 vlanOVN
	DefaultProviderName string
	Recorder            crrecorder.EventRecorder
}

func (r *VlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	specChanged := predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		oldVlan, oldOK := e.ObjectOld.(*kubeovnv1.Vlan)
		newVlan, newOK := e.ObjectNew.(*kubeovnv1.Vlan)
		return oldOK && newOK && !equality.Semantic.DeepEqual(oldVlan.Spec, newVlan.Spec)
	}}
	return ctrl.NewControllerManagedBy(mgr).
		Named("kube-ovn-vlan").
		For(&kubeovnv1.Vlan{}).
		Watches(&kubeovnv1.Vlan{}, handler.Funcs{
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				if !specChanged.Update(e) {
					return
				}
				for _, request := range r.relatedVlanRequests(ctx, e.ObjectOld, e.ObjectNew) {
					q.Add(request)
				}
			},
		}).
		Watches(&kubeovnv1.Subnet{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSubnet)).
		Watches(&kubeovnv1.ProviderNetwork{}, handler.EnqueueRequestsFromMapFunc(r.requestsForProviderNetwork)).
		Complete(r)
}

func (r *VlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	vlan := &kubeovnv1.Vlan{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, vlan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if migrateVlanSpec(vlan) {
		if err := r.Update(ctx, vlan); err != nil {
			return ctrl.Result{}, fmt.Errorf("migrate spec for vlan %s: %w", vlan.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if vlan.Spec.Provider == "" {
		updated := vlan.DeepCopy()
		updated.Spec.Provider = r.DefaultProviderName
		if err := r.Update(ctx, updated); err != nil {
			return ctrl.Result{}, fmt.Errorf("set default provider for vlan %s: %w", vlan.Name, err)
		}
		return ctrl.Result{}, nil
	}

	providerNetwork := &kubeovnv1.ProviderNetwork{}
	if err := r.Get(ctx, types.NamespacedName{Name: vlan.Spec.Provider}, providerNetwork); err != nil {
		return ctrl.Result{}, fmt.Errorf("get provider network %s for vlan %s: %w", vlan.Spec.Provider, vlan.Name, err)
	}

	var vlans kubeovnv1.VlanList
	if err := r.List(ctx, &vlans); err != nil {
		return ctrl.Result{}, fmt.Errorf("list vlans for %s: %w", vlan.Name, err)
	}

	var subnets kubeovnv1.SubnetList
	if err := r.List(ctx, &subnets); err != nil {
		return ctrl.Result{}, fmt.Errorf("list subnets for vlan %s: %w", vlan.Name, err)
	}

	conflict := vlanConflict(vlan, vlans.Items)
	original := vlan.DeepCopy()
	vlan.Status.Conflict = conflict
	vlan.Status.Subnets = subnetNamesForVlan(vlan.Name, subnets.Items)

	if conflict {
		removeVlanReadyCondition(vlan)
		if err := r.patchVlanStatus(ctx, vlan, original); err != nil {
			return ctrl.Result{}, err
		}
		err := fmt.Errorf("vlan %s conflicts with another vlan in provider network %s", vlan.Name, vlan.Spec.Provider)
		r.recordEvent(vlan, corev1.EventTypeWarning, "VlanConflict", err.Error())
		return ctrl.Result{}, err
	}

	if err := r.updateLocalnetTags(vlan, subnets.Items); err != nil {
		removeVlanReadyCondition(vlan)
		if statusErr := r.patchVlanStatus(ctx, vlan, original); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("%w; clear ready status: %w", err, statusErr)
		}
		return ctrl.Result{}, err
	}
	setVlanReadyCondition(vlan)
	if err := r.patchVlanStatus(ctx, vlan, original); err != nil {
		return ctrl.Result{}, err
	}

	if !equality.Semantic.DeepEqual(original.Status, vlan.Status) {
		r.recordEvent(vlan, corev1.EventTypeNormal, "ReconcileSuccess", fmt.Sprintf("Vlan %s reconciled successfully", vlan.Name))
	}

	if !slices.Contains(providerNetwork.Status.Vlans, vlan.Name) {
		// ProviderNetworkReconciler owns ProviderNetwork status. Its Vlan watch
		// will observe this resource and update Status.Vlans without creating two
		// status writers for the same object.
		return ctrl.Result{RequeueAfter: controllerRuntimeStatusRetry}, nil
	}

	return ctrl.Result{}, nil
}

// Subnet reconciliation creates each localnet port with the current VLAN tag.
// A new VLAN must become ready before its Subnets can create those ports, so
// only update ports that already exist here; propagate actual OVN failures.
func (r *VlanReconciler) updateLocalnetTags(vlan *kubeovnv1.Vlan, subnets []kubeovnv1.Subnet) error {
	for _, subnet := range subnets {
		if subnet.Spec.Vlan != vlan.Name {
			continue
		}
		port := ovs.GetLocalnetName(subnet.Name)
		exists, err := r.OVN.LogicalSwitchPortExists(port)
		if err != nil {
			return fmt.Errorf("check localnet port for subnet %s and vlan %s: %w", subnet.Name, vlan.Name, err)
		}
		if !exists {
			continue
		}
		if err := r.OVN.SetLogicalSwitchPortVlanTag(port, vlan.Spec.ID); err != nil {
			return fmt.Errorf("set localnet tag for subnet %s and vlan %s: %w", subnet.Name, vlan.Name, err)
		}
	}
	return nil
}

func migrateVlanSpec(vlan *kubeovnv1.Vlan) bool {
	var changed bool
	if vlan.Spec.VlanID != 0 && vlan.Spec.ID == 0 {
		vlan.Spec.ID = vlan.Spec.VlanID
		vlan.Spec.VlanID = 0
		changed = true
	}
	//nolint:staticcheck // Ignore SA1019 for backward compatibility of deprecated field ProviderInterfaceName.
	if vlan.Spec.ProviderInterfaceName != "" && vlan.Spec.Provider == "" {
		//nolint:staticcheck // Ignore SA1019 for backward compatibility.
		vlan.Spec.Provider = vlan.Spec.ProviderInterfaceName
		//nolint:staticcheck // Ignore SA1019 for backward compatibility.
		vlan.Spec.ProviderInterfaceName = ""
		changed = true
	}
	return changed
}

func (r *VlanReconciler) patchVlanStatus(ctx context.Context, vlan, original *kubeovnv1.Vlan) error {
	if equality.Semantic.DeepEqual(original.Status, vlan.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, vlan, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("update status for vlan %s: %w", vlan.Name, err)
	}
	return nil
}

func (r *VlanReconciler) requestsForSubnet(_ context.Context, obj client.Object) []ctrl.Request {
	subnet, ok := obj.(*kubeovnv1.Subnet)
	if !ok || subnet.Spec.Vlan == "" {
		return nil
	}
	return []ctrl.Request{{Name: subnet.Spec.Vlan}}
}

func (r *VlanReconciler) requestsForProviderNetwork(ctx context.Context, obj client.Object) []ctrl.Request {
	providerNetwork, ok := obj.(*kubeovnv1.ProviderNetwork)
	if !ok {
		return nil
	}

	var vlans kubeovnv1.VlanList
	if err := r.List(ctx, &vlans); err != nil {
		klog.Errorf("list vlans for provider network %s event: %v", providerNetwork.Name, err)
		return nil
	}
	requests := make([]ctrl.Request, 0)
	for _, vlan := range vlans.Items {
		if vlan.Spec.Provider == providerNetwork.Name {
			requests = append(requests, ctrl.Request{Name: vlan.Name})
		}
	}
	return requests
}

func (r *VlanReconciler) relatedVlanRequests(ctx context.Context, objects ...client.Object) []ctrl.Request {
	var vlans kubeovnv1.VlanList
	if err := r.List(ctx, &vlans); err != nil {
		klog.Errorf("list related vlans: %v", err)
		return nil
	}
	requests := make([]ctrl.Request, 0)
	for _, vlan := range vlans.Items {
		for _, object := range objects {
			changed, ok := object.(*kubeovnv1.Vlan)
			if !ok || changed.Name == vlan.Name {
				continue
			}
			if changed.Spec.ID != 0 && changed.Spec.ID == vlan.Spec.ID && changed.Spec.Provider == vlan.Spec.Provider {
				requests = append(requests, ctrl.Request{Name: vlan.Name})
				break
			}
		}
	}
	return requests
}

func (r *VlanReconciler) recordEvent(vlan *kubeovnv1.Vlan, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(vlan, nil, eventType, reason, "Reconcile", message)
	}
}

type ProviderNetworkReconciler struct {
	client.Client
	PodNamespace string
}

func (r *ProviderNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	cniPodPredicate := predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return isKubeOvnCniPod(e.Object) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return isKubeOvnCniPod(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return isKubeOvnCniPod(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isKubeOvnCniPod(e.ObjectNew) || isKubeOvnCniPod(e.ObjectOld)
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("kube-ovn-provider-network").
		For(&kubeovnv1.ProviderNetwork{}).
		Watches(&kubeovnv1.Vlan{}, handler.Funcs{
			CreateFunc: func(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanProvider(q, e.Object)
			},
			UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanProvider(q, e.ObjectOld)
				r.enqueueVlanProvider(q, e.ObjectNew)
			},
			DeleteFunc: func(_ context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanProvider(q, e.Object)
			},
			GenericFunc: func(_ context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanProvider(q, e.Object)
			},
		}).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(r.requestsForAllProviderNetworks)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.requestsForAllProviderNetworks), builder.WithPredicates(cniPodPredicate)).
		Complete(r)
}

func (r *ProviderNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	providerNetwork := &kubeovnv1.ProviderNetwork{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, providerNetwork); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return ctrl.Result{}, fmt.Errorf("list nodes for provider network %s: %w", providerNetwork.Name, err)
	}
	var cniPods corev1.PodList
	if err := r.List(ctx, &cniPods, client.InNamespace(r.PodNamespace), client.MatchingLabels{"app": "kube-ovn-cni"}); err != nil {
		return ctrl.Result{}, fmt.Errorf("list kube-ovn-cni pods for provider network %s: %w", providerNetwork.Name, err)
	}
	var vlans kubeovnv1.VlanList
	if err := r.List(ctx, &vlans); err != nil {
		return ctrl.Result{}, fmt.Errorf("list vlans for provider network %s: %w", providerNetwork.Name, err)
	}

	original := providerNetwork.DeepCopy()
	providerNetwork.Status.Vlans = readyVlanNamesForProviderNetwork(providerNetwork.Name, vlans.Items)
	providerNetwork.Status.ReadyNodes = nil
	providerNetwork.Status.NotReadyNodes = nil

	podsByNode := make(map[string]*corev1.Pod, len(cniPods.Items))
	for i := range cniPods.Items {
		pod := &cniPods.Items[i]
		podsByNode[pod.Spec.NodeName] = pod
	}
	activeNodes := make(map[string]struct{}, len(nodes.Items))
	for i := range nodes.Items {
		node := &nodes.Items[i]
		excluded, err := util.IsNodeExcludedFromProviderNetwork(node, providerNetwork)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("check node %s for provider network %s: %w", node.Name, providerNetwork.Name, err)
		}
		if excluded {
			providerNetwork.Status.RemoveNodeConditions(node.Name)
			continue
		}
		activeNodes[node.Name] = struct{}{}

		readyAnnotation := fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetwork.Name)
		errorAnnotation := fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork.Name)
		if node.Labels[readyAnnotation] == "true" {
			providerNetwork.Status.SetNodeReady(node.Name, "InitOVSBridgeSucceeded", "")
			providerNetwork.Status.ReadyNodes = append(providerNetwork.Status.ReadyNodes, node.Name)
			continue
		}

		errMessage := "unknown error"
		if pod := podsByNode[node.Name]; pod == nil {
			errMessage = fmt.Sprintf("kube-ovn-cni pod on node %s not found", node.Name)
		} else if pod.Annotations[errorAnnotation] != "" {
			errMessage = pod.Annotations[errorAnnotation]
		}
		providerNetwork.Status.SetNodeNotReady(node.Name, "InitOVSBridgeFailed", errMessage)
		providerNetwork.Status.NotReadyNodes = append(providerNetwork.Status.NotReadyNodes, node.Name)
	}
	staleNodes := make([]string, 0, len(providerNetwork.Status.Conditions))
	for _, condition := range providerNetwork.Status.Conditions {
		if _, ok := activeNodes[condition.Node]; !ok {
			staleNodes = append(staleNodes, condition.Node)
		}
	}
	for _, node := range staleNodes {
		providerNetwork.Status.RemoveNodeConditions(node)
	}

	slices.Sort(providerNetwork.Status.ReadyNodes)
	slices.Sort(providerNetwork.Status.NotReadyNodes)
	providerNetwork.Status.Ready = len(providerNetwork.Status.NotReadyNodes) == 0
	if !equality.Semantic.DeepEqual(original.Status, providerNetwork.Status) {
		if err := r.Status().Patch(ctx, providerNetwork, client.MergeFrom(original)); err != nil {
			return ctrl.Result{}, fmt.Errorf("update provider network %s status: %w", providerNetwork.Name, err)
		}
	}
	return ctrl.Result{}, nil
}

func (r *ProviderNetworkReconciler) requestsForVlanObject(obj client.Object) []ctrl.Request {
	vlan, ok := obj.(*kubeovnv1.Vlan)
	if !ok || vlan.Spec.Provider == "" {
		return nil
	}
	return []ctrl.Request{{Name: vlan.Spec.Provider}}
}

func (r *ProviderNetworkReconciler) enqueueVlanProvider(q workqueue.TypedRateLimitingInterface[ctrl.Request], obj client.Object) {
	for _, request := range r.requestsForVlanObject(obj) {
		q.Add(request)
	}
}

func (r *ProviderNetworkReconciler) requestsForAllProviderNetworks(ctx context.Context, _ client.Object) []ctrl.Request {
	var providerNetworks kubeovnv1.ProviderNetworkList
	if err := r.List(ctx, &providerNetworks); err != nil {
		klog.Errorf("list provider networks for dependent event: %v", err)
		return nil
	}
	requests := make([]ctrl.Request, 0, len(providerNetworks.Items))
	for _, providerNetwork := range providerNetworks.Items {
		requests = append(requests, ctrl.Request{Name: providerNetwork.Name})
	}
	return requests
}

func setupVlanReconcilers(mgr ctrl.Manager, config *controller.Configuration) error {
	nbClient, err := ovs.NewOvnNbClient(
		config.OvnNbAddr,
		config.OvnTimeout,
		config.OvsDbConnectTimeout,
		config.OvsDbInactivityTimeout,
		config.OvsDbConnectMaxRetry,
	)
	if err != nil {
		return fmt.Errorf("create OVN NB client for vlan reconcilers: %w", err)
	}

	vlanReconciler := &VlanReconciler{
		Client:              mgr.GetClient(),
		OVN:                 nbClient,
		DefaultProviderName: config.DefaultProviderName,
		Recorder:            mgr.GetEventRecorder("kube-ovn-vlan"),
	}
	if err := vlanReconciler.SetupWithManager(mgr); err != nil {
		nbClient.Close()
		return fmt.Errorf("setup vlan reconciler: %w", err)
	}
	providerNetworkReconciler := &ProviderNetworkReconciler{
		Client:       mgr.GetClient(),
		PodNamespace: config.PodNamespace,
	}
	if err := providerNetworkReconciler.SetupWithManager(mgr); err != nil {
		nbClient.Close()
		return fmt.Errorf("setup provider network reconciler: %w", err)
	}

	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		<-ctx.Done()
		nbClient.Close()
		return nil
	})); err != nil {
		nbClient.Close()
		return fmt.Errorf("register OVN NB client shutdown: %w", err)
	}
	return nil
}

const controllerRuntimeStatusRetry = 5 * time.Second

func vlanConflict(vlan *kubeovnv1.Vlan, vlans []kubeovnv1.Vlan) bool {
	if vlan.Spec.ID == 0 {
		return false
	}
	for _, other := range vlans {
		if other.Name != vlan.Name && other.Spec.Provider == vlan.Spec.Provider && other.Spec.ID == vlan.Spec.ID {
			return true
		}
	}
	return false
}

func subnetNamesForVlan(vlanName string, subnets []kubeovnv1.Subnet) []string {
	names := make([]string, 0)
	for _, subnet := range subnets {
		if subnet.Spec.Vlan == vlanName {
			names = append(names, subnet.Name)
		}
	}
	slices.Sort(names)
	return names
}

func readyVlanNamesForProviderNetwork(providerNetworkName string, vlans []kubeovnv1.Vlan) []string {
	names := make([]string, 0)
	for _, vlan := range vlans {
		if vlan.Spec.Provider == providerNetworkName && vlanIsReady(&vlan) {
			names = append(names, vlan.Name)
		}
	}
	slices.Sort(names)
	return names
}

func vlanIsReady(vlan *kubeovnv1.Vlan) bool {
	if vlan.Status.Conflict {
		return false
	}
	for _, condition := range vlan.Status.Conditions {
		if condition.Type == kubeovnv1.Ready && condition.Status == corev1.ConditionTrue && condition.ObservedGeneration == vlan.Generation {
			return true
		}
	}
	return false
}

func setVlanReadyCondition(vlan *kubeovnv1.Vlan) {
	vlan.Status.SetVlanCondition(kubeovnv1.Ready, "ReconcileSuccess", "")
	for i := range vlan.Status.Conditions {
		if vlan.Status.Conditions[i].Type == kubeovnv1.Ready {
			vlan.Status.Conditions[i].ObservedGeneration = vlan.Generation
		}
	}
}

func removeVlanReadyCondition(vlan *kubeovnv1.Vlan) {
	conditions := vlan.Status.Conditions[:0]
	for _, condition := range vlan.Status.Conditions {
		if condition.Type != kubeovnv1.Ready {
			conditions = append(conditions, condition)
		}
	}
	vlan.Status.Conditions = conditions
}

func isKubeOvnCniPod(obj client.Object) bool {
	pod, ok := obj.(*corev1.Pod)
	return ok && pod.Labels["app"] == "kube-ovn-cni"
}
