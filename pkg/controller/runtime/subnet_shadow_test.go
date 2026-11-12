package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestSubnetShadowReconcilerIsReadOnly(t *testing.T) {
	subnet := testSubnet("vlan-a")
	subnet.Generation = 3
	vlan := testVlan("vlan-a", "provider-a")
	provider := testProvider("provider-a", true, "vlan-a")
	reader := newRuntimeClient(t, subnet, vlan, provider)
	reconciler := &SubnetShadowReconciler{Reader: reader}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: subnet.Name})
	require.NoError(t, err)
	require.Zero(t, result)

	current := &kubeovnv1.Subnet{}
	require.NoError(t, reader.Get(t.Context(), types.NamespacedName{Name: subnet.Name}, current))
	require.Equal(t, subnet.Generation, current.Generation)
	require.Equal(t, subnet.Spec, current.Spec)
}

func TestSubnetShadowReconcilerMapsDependencies(t *testing.T) {
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{subnet.Name}
	provider := testProvider("provider-a", true, "vlan-a")
	reconciler := &SubnetShadowReconciler{Reader: newRuntimeClient(t, subnet, vlan, provider)}

	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForProviderNetwork(t.Context(), provider))
}

func TestSubnetShadowReconcilerMapsVlanStatusAcrossEvents(t *testing.T) {
	reconciler := &SubnetShadowReconciler{}
	oldVlan := &kubeovnv1.Vlan{Status: kubeovnv1.VlanStatus{Subnets: []string{"subnet-b", "subnet-a", ""}}}
	newVlan := &kubeovnv1.Vlan{Status: kubeovnv1.VlanStatus{Subnets: []string{"subnet-c", "subnet-b", "subnet-c"}}}

	require.Equal(t,
		[]ctrl.Request{{Name: "subnet-a"}, {Name: "subnet-b"}, {Name: "subnet-c"}},
		reconciler.requestsForVlanStatus(oldVlan, newVlan))
	require.Equal(t, []ctrl.Request{{Name: "subnet-a"}, {Name: "subnet-b"}}, reconciler.requestsForVlanStatus(oldVlan))
	require.Empty(t, reconciler.requestsForVlanStatus(&kubeovnv1.Subnet{}))
}

func TestSubnetShadowReconcilerRetriesProviderNetworkStatusMapping(t *testing.T) {
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{subnet.Name}
	provider := testProvider("provider-a", true, "vlan-a")
	reader := &transientSubnetMapperReader{
		Reader:   newRuntimeClient(t, subnet, vlan, provider),
		failures: map[string]int{"vlans": 1},
	}
	reconciler := &SubnetShadowReconciler{Reader: reader}

	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForProviderNetworkWithRetry(t.Context(), provider))
	require.Equal(t, map[string]int{"vlans": 0}, reader.failures)
}

func TestSubnetShadowReconcilerRecordsDependencyMappingFailure(t *testing.T) {
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{subnet.Name}
	reader := &transientSubnetMapperReader{
		Reader:   newRuntimeClient(t, subnet, vlan),
		failures: map[string]int{"vlans": 3, "subnets": 3},
	}
	before := testutil.ToFloat64(subnetShadowDependencyMappingFailures.WithLabelValues("provider_network", "exhausted"))
	reconciler := &SubnetShadowReconciler{Reader: reader}

	require.Empty(t, reconciler.requestsForProviderNetworkWithRetry(t.Context(), testProvider("provider-a", true, "vlan-a")))
	require.Equal(t, before+1, testutil.ToFloat64(subnetShadowDependencyMappingFailures.WithLabelValues("provider_network", "exhausted")))
}

func TestSubnetShadowReconcilerFallsBackToSubnetSpecAfterVlanGetFailure(t *testing.T) {
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{subnet.Name}
	provider := testProvider("provider-a", true, "vlan-a")
	reader := &transientSubnetMapperReader{
		Reader:   newRuntimeClient(t, subnet, vlan, provider),
		failures: map[string]int{"vlans": 3},
	}
	reconciler := &SubnetShadowReconciler{Reader: reader}

	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForProviderNetworkWithRetry(t.Context(), provider))
	require.Equal(t, map[string]int{"vlans": 0}, reader.failures)
}

func TestSubnetShadowReconcilerFallbackHandlerEnqueuesSubnet(t *testing.T) {
	subnet := testSubnet("vlan-a")
	provider := testProvider("provider-a", true, "vlan-a")
	reconciler := &SubnetShadowReconciler{Reader: &transientSubnetMapperReader{
		Reader:   newRuntimeClient(t, subnet),
		failures: map[string]int{"vlans": 3},
	}}
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
	t.Cleanup(q.ShutDown)

	reconciler.enqueueProviderNetworkStatusRequests(t.Context(), q, provider)
	require.Equal(t, 1, q.Len())
	request, _ := q.Get()
	q.Done(request)
	require.Equal(t, ctrl.Request{Name: subnet.Name}, request)
}

func TestSubnetShadowReconcilerRecordsCanceledDependencyMapping(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := testutil.ToFloat64(subnetShadowDependencyMappingFailures.WithLabelValues("provider_network", "canceled"))
	reconciler := &SubnetShadowReconciler{Reader: canceledSubnetMapperReader{Reader: newRuntimeClient(t)}}

	require.Empty(t, reconciler.requestsForProviderNetworkWithRetry(ctx, testProvider("provider-a", true, "vlan-a")))
	require.Equal(t, before+1, testutil.ToFloat64(subnetShadowDependencyMappingFailures.WithLabelValues("provider_network", "canceled")))
}

func TestSubnetDependencyMappingRetryExhaustsAttempts(t *testing.T) {
	wantErrors := []error{
		errors.New("first list failure"),
		errors.New("second list failure"),
		errors.New("third list failure"),
	}
	var calls int
	requests, err := retrySubnetDependencyMapping(t.Context(), &kubeovnv1.Vlan{}, func(context.Context, client.Object) ([]ctrl.Request, error) {
		failure := wantErrors[calls]
		calls++
		return nil, failure
	})

	require.ErrorIs(t, err, wantErrors[2])
	require.NotErrorIs(t, err, wantErrors[0])
	require.Nil(t, requests)
	require.Equal(t, 3, calls)
}

func TestSubnetDependencyMappingRetryStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls int
	requests, err := retrySubnetDependencyMapping(ctx, &kubeovnv1.Vlan{}, func(context.Context, client.Object) ([]ctrl.Request, error) {
		calls++
		cancel()
		return nil, errors.New("transient list failure")
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, requests)
	require.Equal(t, 1, calls)
}

type transientSubnetMapperReader struct {
	client.Reader
	failures map[string]int
}

type canceledSubnetMapperReader struct {
	client.Reader
}

func (r canceledSubnetMapperReader) Get(ctx context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return ctx.Err()
}

func (r canceledSubnetMapperReader) List(ctx context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return ctx.Err()
}

func (r *transientSubnetMapperReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	resource := ""
	if _, ok := obj.(*kubeovnv1.Vlan); ok {
		resource = "vlans"
	}
	if r.failures[resource] > 0 {
		r.failures[resource]--
		return errors.New("injected transient get failure")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func (r *transientSubnetMapperReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	resource := ""
	if _, ok := list.(*kubeovnv1.SubnetList); ok {
		resource = "subnets"
	}
	if r.failures[resource] > 0 {
		r.failures[resource]--
		return errors.New("injected transient list failure")
	}
	return r.Reader.List(ctx, list, opts...)
}

func TestSubnetShadowReconcilerMapsIPDependenciesAcrossUpdates(t *testing.T) {
	reconciler := &SubnetShadowReconciler{}
	oldIP := &kubeovnv1.IP{Spec: kubeovnv1.IPSpec{Subnet: "subnet-a", AttachSubnets: []string{"subnet-b", "subnet-a", ""}}}
	newIP := &kubeovnv1.IP{Spec: kubeovnv1.IPSpec{Subnet: "subnet-c", AttachSubnets: []string{"subnet-b"}}}

	require.Equal(t,
		[]ctrl.Request{{Name: "subnet-a"}, {Name: "subnet-b"}, {Name: "subnet-c"}},
		reconciler.requestsForIP(oldIP, newIP))
	require.Equal(t, []ctrl.Request{{Name: "subnet-a"}, {Name: "subnet-b"}}, reconciler.requestsForIP(oldIP))
	require.Empty(t, reconciler.requestsForIP(&kubeovnv1.Vlan{}))
}

func TestSubnetShadowReconcilerIgnoresDeletedSubnet(t *testing.T) {
	reconciler := &SubnetShadowReconciler{Reader: newRuntimeClient(t)}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "missing"})
	require.NoError(t, err)
}
