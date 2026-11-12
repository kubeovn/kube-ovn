package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var subnetShadowDependencyMappingFailures = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "kube_ovn_subnet_shadow_dependency_mapping_failures_total",
		Help: "Number of exhausted or canceled Subnet shadow dependency mapping retries.",
	}, []string{"source", "result"},
)

func init() {
	metrics.Registry.MustRegister(subnetShadowDependencyMappingFailures)
}

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
		Watches(&kubeovnv1.Vlan{}, handler.Funcs{
			CreateFunc: func(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanStatusRequests(q, e.Object)
			},
			UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanStatusRequests(q, e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(_ context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueVlanStatusRequests(q, e.Object)
			},
		}).
		Watches(&kubeovnv1.ProviderNetwork{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueProviderNetworkStatusRequests(ctx, q, e.Object)
			},
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueProviderNetworkStatusRequests(ctx, q, e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				r.enqueueProviderNetworkStatusRequests(ctx, q, e.Object)
			},
		}).
		Watches(&kubeovnv1.IP{}, handler.Funcs{
			CreateFunc: func(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				for _, request := range r.requestsForIP(e.Object) {
					q.Add(request)
				}
			},
			UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				for _, request := range r.requestsForIP(e.ObjectOld, e.ObjectNew) {
					q.Add(request)
				}
			},
			DeleteFunc: func(_ context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				for _, request := range r.requestsForIP(e.Object) {
					q.Add(request)
				}
			},
		}).
		Complete(r)
}

func (r *SubnetShadowReconciler) enqueueVlanStatusRequests(q workqueue.TypedRateLimitingInterface[ctrl.Request], objects ...client.Object) {
	for _, request := range r.requestsForVlanStatus(objects...) {
		q.Add(request)
	}
}

func (r *SubnetShadowReconciler) requestsForVlanStatus(objects ...client.Object) []ctrl.Request {
	names := make(map[string]struct{})
	for _, obj := range objects {
		vlan, ok := obj.(*kubeovnv1.Vlan)
		if !ok || vlan == nil {
			continue
		}
		for _, subnet := range vlan.Status.Subnets {
			if subnet != "" {
				names[subnet] = struct{}{}
			}
		}
	}
	return subnetRequestsForNames(names)
}

func (r *SubnetShadowReconciler) requestsForProviderNetwork(ctx context.Context, obj client.Object) []ctrl.Request {
	requests, err := r.requestsForProviderNetworkWithError(ctx, obj)
	if err != nil {
		log.FromContext(ctx).Error(err, "list dependencies for ProviderNetwork shadow event")
		return nil
	}
	return requests
}

func (r *SubnetShadowReconciler) requestsForProviderNetworkWithRetry(ctx context.Context, obj client.Object) []ctrl.Request {
	requests, err := r.providerNetworkStatusRequestsWithFallback(ctx, obj)
	if err != nil {
		subnetShadowDependencyMappingFailures.WithLabelValues("provider_network", subnetShadowRetryResult(err)).Inc()
		log.FromContext(ctx).Error(err, "retry list dependencies for ProviderNetwork shadow event")
		return nil
	}
	return requests
}

func (r *SubnetShadowReconciler) enqueueProviderNetworkStatusRequests(ctx context.Context, q workqueue.TypedRateLimitingInterface[ctrl.Request], objects ...client.Object) {
	requests, err := r.providerNetworkStatusRequestsWithFallback(ctx, objects...)
	if err != nil {
		subnetShadowDependencyMappingFailures.WithLabelValues("provider_network", subnetShadowRetryResult(err)).Inc()
		log.FromContext(ctx).Error(err, "retry list dependencies for ProviderNetwork shadow event")
		return
	}
	for _, request := range requests {
		q.Add(request)
	}
}

func (r *SubnetShadowReconciler) providerNetworkStatusRequestsWithFallback(ctx context.Context, objects ...client.Object) ([]ctrl.Request, error) {
	requests, err := r.retryProviderNetworkStatusRequests(ctx, objects...)
	if err == nil || ctx.Err() != nil {
		return requests, err
	}
	return r.requestsForProviderNetworkBySubnetSpec(ctx, objects...)
}

func (r *SubnetShadowReconciler) retryProviderNetworkStatusRequests(ctx context.Context, objects ...client.Object) ([]ctrl.Request, error) {
	if len(objects) == 0 {
		return nil, nil
	}
	return retrySubnetDependencyMapping(ctx, objects[0], func(ctx context.Context, _ client.Object) ([]ctrl.Request, error) {
		return r.requestsForProviderNetworkStatusWithError(ctx, objects...)
	})
}

func subnetShadowRetryResult(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "canceled"
	}
	return "exhausted"
}

func (r *SubnetShadowReconciler) requestsForProviderNetworkWithError(ctx context.Context, obj client.Object) ([]ctrl.Request, error) {
	return r.requestsForProviderNetworkStatusWithError(ctx, obj)
}

func (r *SubnetShadowReconciler) requestsForProviderNetworkStatusWithError(ctx context.Context, objects ...client.Object) ([]ctrl.Request, error) {
	providerName, vlans := providerNetworkVlanNames(objects...)
	if providerName == "" || len(vlans) == 0 {
		return nil, nil
	}
	names := make(map[string]struct{})
	for vlanName := range vlans {
		var vlan kubeovnv1.Vlan
		if err := r.Get(ctx, types.NamespacedName{Name: vlanName}, &vlan); err != nil {
			return nil, fmt.Errorf("get VLAN %s for ProviderNetwork %s: %w", vlanName, providerName, err)
		}
		for _, subnet := range vlan.Status.Subnets {
			if subnet != "" {
				names[subnet] = struct{}{}
			}
		}
	}
	return subnetRequestsForNames(names), nil
}

func (r *SubnetShadowReconciler) requestsForProviderNetworkBySubnetSpec(ctx context.Context, objects ...client.Object) ([]ctrl.Request, error) {
	_, vlans := providerNetworkVlanNames(objects...)
	if len(vlans) == 0 {
		return nil, nil
	}
	var subnets kubeovnv1.SubnetList
	if err := r.List(ctx, &subnets); err != nil {
		return nil, fmt.Errorf("list Subnets for ProviderNetwork fallback: %w", err)
	}
	names := make(map[string]struct{})
	for _, subnet := range subnets.Items {
		if _, ok := vlans[subnet.Spec.Vlan]; ok {
			names[subnet.Name] = struct{}{}
		}
	}
	return subnetRequestsForNames(names), nil
}

func providerNetworkVlanNames(objects ...client.Object) (string, map[string]struct{}) {
	var providerName string
	vlans := make(map[string]struct{})
	for _, obj := range objects {
		provider, ok := obj.(*kubeovnv1.ProviderNetwork)
		if !ok || provider == nil {
			continue
		}
		providerName = provider.Name
		for _, vlan := range provider.Status.Vlans {
			if vlan != "" {
				vlans[vlan] = struct{}{}
			}
		}
	}
	return providerName, vlans
}

type subnetDependencyMapper func(context.Context, client.Object) ([]ctrl.Request, error)

func retrySubnetDependencyMapping(ctx context.Context, obj client.Object, mapper subnetDependencyMapper) ([]ctrl.Request, error) {
	const attempts = 3
	delays := [...]time.Duration{10 * time.Millisecond, 50 * time.Millisecond}
	var err error
	for attempt := range attempts {
		var requests []ctrl.Request
		requests, err = mapper(ctx, obj)
		if err == nil {
			return requests, nil
		}
		if attempt == attempts-1 {
			break
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	return nil, err
}

func (r *SubnetShadowReconciler) requestsForIP(objects ...client.Object) []ctrl.Request {
	names := make(map[string]struct{})
	for _, obj := range objects {
		ip, ok := obj.(*kubeovnv1.IP)
		if !ok || ip == nil {
			continue
		}
		if ip.Spec.Subnet != "" {
			names[ip.Spec.Subnet] = struct{}{}
		}
		for _, subnet := range ip.Spec.AttachSubnets {
			if subnet != "" {
				names[subnet] = struct{}{}
			}
		}
	}
	return subnetRequestsForNames(names)
}

func subnetRequestsForNames(names map[string]struct{}) []ctrl.Request {
	ordered := slices.Sorted(maps.Keys(names))
	requests := make([]ctrl.Request, 0, len(ordered))
	for _, name := range ordered {
		requests = append(requests, ctrl.Request{Name: name})
	}
	return requests
}
