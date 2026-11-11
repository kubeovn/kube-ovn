package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
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
	provider := testProvider("provider-a", true, "vlan-a")
	reconciler := &SubnetShadowReconciler{Reader: newRuntimeClient(t, subnet, vlan, provider)}

	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForVlan(t.Context(), vlan))
	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForProviderNetwork(t.Context(), provider))
}

func TestSubnetShadowReconcilerRetriesDependencyList(t *testing.T) {
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	provider := testProvider("provider-a", true, "vlan-a")
	reader := &transientSubnetMapperReader{
		Reader:   newRuntimeClient(t, subnet, vlan, provider),
		failures: map[string]int{"subnets": 1, "vlans": 1},
	}
	reconciler := &SubnetShadowReconciler{Reader: reader}

	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForVlanWithRetry(t.Context(), vlan))
	require.Equal(t, []ctrl.Request{{Name: subnet.Name}},
		reconciler.requestsForProviderNetworkWithRetry(t.Context(), provider))
	require.Equal(t, map[string]int{"subnets": 0, "vlans": 0}, reader.failures)
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

func (r *transientSubnetMapperReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	resource := ""
	switch list.(type) {
	case *kubeovnv1.SubnetList:
		resource = "subnets"
	case *kubeovnv1.VlanList:
		resource = "vlans"
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
