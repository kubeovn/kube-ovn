package controllerruntime

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// SubnetShadowReconciler observes Subnet readiness without taking ownership of
// any API status, finalizer, queue, OVN, or IPAM operation. It is intentionally
// unwired from the production manager until the Subnet writer contract is
// complete.
type SubnetShadowReconciler struct {
	client.Reader
}

// Reconcile performs a read-only Subnet observation. Missing objects converge
// as a no-op, while dependency and API failures remain retryable errors.
func (r *SubnetShadowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if _, err := ObserveSubnet(ctx, r.Reader, req.Name); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager describes the future read-only dependency watches. Callers
// must opt in explicitly; constructing or testing this reconciler has no
// production effect.
func (r *SubnetShadowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("kube-ovn-subnet-shadow").
		For(&kubeovnv1.Subnet{}).
		Watches(&kubeovnv1.Vlan{}, handler.EnqueueRequestsFromMapFunc(r.requestsForVlan)).
		Watches(&kubeovnv1.ProviderNetwork{}, handler.EnqueueRequestsFromMapFunc(r.requestsForProviderNetwork)).
		Complete(r)
}

func (r *SubnetShadowReconciler) requestsForVlan(ctx context.Context, obj client.Object) []ctrl.Request {
	vlan, ok := obj.(*kubeovnv1.Vlan)
	if !ok {
		return nil
	}
	var subnets kubeovnv1.SubnetList
	if err := r.List(ctx, &subnets); err != nil {
		log.FromContext(ctx).Error(err, "list subnets for VLAN shadow event", "vlan", vlan.Name)
		return nil
	}
	requests := make([]ctrl.Request, 0)
	for _, subnet := range subnets.Items {
		if subnet.Spec.Vlan == vlan.Name {
			requests = append(requests, ctrl.Request{NamespacedName: types.NamespacedName{Name: subnet.Name}})
		}
	}
	return requests
}

func (r *SubnetShadowReconciler) requestsForProviderNetwork(ctx context.Context, obj client.Object) []ctrl.Request {
	provider, ok := obj.(*kubeovnv1.ProviderNetwork)
	if !ok {
		return nil
	}
	var vlans kubeovnv1.VlanList
	if err := r.List(ctx, &vlans); err != nil {
		log.FromContext(ctx).Error(err, "list VLANs for ProviderNetwork shadow event", "providerNetwork", provider.Name)
		return nil
	}
	providers := make(map[string]struct{})
	for _, vlan := range vlans.Items {
		if vlan.Spec.Provider == provider.Name {
			providers[vlan.Name] = struct{}{}
		}
	}
	if len(providers) == 0 {
		return nil
	}
	var subnets kubeovnv1.SubnetList
	if err := r.List(ctx, &subnets); err != nil {
		log.FromContext(ctx).Error(err, "list subnets for ProviderNetwork shadow event", "providerNetwork", provider.Name)
		return nil
	}
	requests := make([]ctrl.Request, 0)
	for _, subnet := range subnets.Items {
		if _, ok := providers[subnet.Spec.Vlan]; ok {
			requests = append(requests, ctrl.Request{NamespacedName: types.NamespacedName{Name: subnet.Name}})
		}
	}
	return requests
}
