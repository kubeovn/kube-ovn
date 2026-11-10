package controllerruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

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
