package controllerruntime

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type recordingVlanOVN struct {
	tags map[string]int
}

func (o *recordingVlanOVN) SetLogicalSwitchPortVlanTag(name string, vlanID int) error {
	if o.tags == nil {
		o.tags = make(map[string]int)
	}
	o.tags[name] = vlanID
	return nil
}

func newRuntimeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme, err := NewScheme()
	require.NoError(t, err)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&kubeovnv1.Vlan{}, &kubeovnv1.ProviderNetwork{}).
		WithObjects(objects...).
		Build()
}

func TestVlanReconcilerUpdatesStatusAndLocalnetTag(t *testing.T) {
	vlan := &kubeovnv1.Vlan{
		Name: "vlan-a",
		Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"},
	}
	providerNetwork := &kubeovnv1.ProviderNetwork{Name: "provider-a"}
	subnet := &kubeovnv1.Subnet{
		Name: "subnet-a",
		Spec: kubeovnv1.SubnetSpec{Vlan: vlan.Name},
	}
	ovn := &recordingVlanOVN{}
	reconciler := &VlanReconciler{
		Client: newRuntimeClient(t, vlan, providerNetwork, subnet),
		OVN:    ovn,
	}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
	require.NoError(t, err)
	require.Equal(t, controllerRuntimeStatusRetry, result.RequeueAfter)
	require.Equal(t, map[string]int{ovs.GetLocalnetName(subnet.Name): vlan.Spec.ID}, ovn.tags)

	updated := &kubeovnv1.Vlan{}
	require.NoError(t, reconciler.Get(t.Context(), types.NamespacedName{Name: vlan.Name}, updated))
	require.Equal(t, []string{subnet.Name}, updated.Status.Subnets)
	require.False(t, updated.Status.Conflict)
}

func TestVlanReconcilerDoesNotTagConflictingVlan(t *testing.T) {
	vlan := &kubeovnv1.Vlan{
		Name: "vlan-a",
		Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"},
	}
	conflictingVlan := &kubeovnv1.Vlan{
		Name: "vlan-b",
		Spec: kubeovnv1.VlanSpec{ID: vlan.Spec.ID, Provider: vlan.Spec.Provider},
	}
	providerNetwork := &kubeovnv1.ProviderNetwork{Name: vlan.Spec.Provider}
	subnet := &kubeovnv1.Subnet{
		Name: "subnet-a",
		Spec: kubeovnv1.SubnetSpec{Vlan: vlan.Name},
	}
	ovn := &recordingVlanOVN{}
	reconciler := &VlanReconciler{
		Client: newRuntimeClient(t, vlan, conflictingVlan, providerNetwork, subnet),
		OVN:    ovn,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
	require.Error(t, err)
	require.Empty(t, ovn.tags)

	updated := &kubeovnv1.Vlan{}
	require.NoError(t, reconciler.Get(t.Context(), types.NamespacedName{Name: vlan.Name}, updated))
	require.True(t, updated.Status.Conflict)
}

func TestMigrateVlanSpec(t *testing.T) {
	vlan := &kubeovnv1.Vlan{Spec: kubeovnv1.VlanSpec{
		VlanID:                100,
		ProviderInterfaceName: "provider-a",
	}}

	require.True(t, migrateVlanSpec(vlan))
	require.Equal(t, 100, vlan.Spec.ID)
	require.Equal(t, "provider-a", vlan.Spec.Provider)
	require.Zero(t, vlan.Spec.VlanID)
	require.Empty(t, vlan.Spec.ProviderInterfaceName)
	require.False(t, migrateVlanSpec(vlan))
}

func TestVlanReconcilerRequestsRelatedConflictPeers(t *testing.T) {
	changed := &kubeovnv1.Vlan{Name: "changed", Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"}}
	reconciler := &VlanReconciler{Client: newRuntimeClient(t,
		changed,
		&kubeovnv1.Vlan{Name: "peer", Spec: changed.Spec},
		&kubeovnv1.Vlan{Name: "other-id", Spec: kubeovnv1.VlanSpec{ID: 101, Provider: changed.Spec.Provider}},
		&kubeovnv1.Vlan{Name: "other-provider", Spec: kubeovnv1.VlanSpec{ID: changed.Spec.ID, Provider: "provider-b"}},
	)}

	requests := reconciler.relatedVlanRequests(t.Context(), changed)
	require.Equal(t, []ctrl.Request{{Name: "peer"}}, requests)
}

func TestProviderNetworkReconcilerMapsVlanProviderChanges(t *testing.T) {
	reconciler := &ProviderNetworkReconciler{}
	oldVlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{Provider: "provider-old"}}
	newVlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{Provider: "provider-new"}}

	require.Equal(t, []ctrl.Request{{Name: "provider-old"}}, reconciler.requestsForVlanObject(oldVlan))
	require.Equal(t, []ctrl.Request{{Name: "provider-new"}}, reconciler.requestsForVlanObject(newVlan))
}

func TestProviderNetworkReconcilerUpdatesVlanAndNodeStatus(t *testing.T) {
	providerNetwork := &kubeovnv1.ProviderNetwork{Name: "provider-a"}
	vlan := &kubeovnv1.Vlan{
		Name: "vlan-a",
		Spec: kubeovnv1.VlanSpec{ID: 100, Provider: providerNetwork.Name},
		Status: kubeovnv1.VlanStatus{Conditions: []kubeovnv1.Condition{{
			Type:               kubeovnv1.Ready,
			Status:             corev1.ConditionTrue,
			ObservedGeneration: 0,
		}}},
	}
	node := &corev1.Node{
		Name: "node-a",
		Labels: map[string]string{
			fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetwork.Name): "true",
		},
	}
	reconciler := &ProviderNetworkReconciler{
		Client:       newRuntimeClient(t, providerNetwork, vlan, node),
		PodNamespace: "kube-system",
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: providerNetwork.Name})
	require.NoError(t, err)

	updated := &kubeovnv1.ProviderNetwork{}
	require.NoError(t, reconciler.Get(t.Context(), types.NamespacedName{Name: providerNetwork.Name}, updated))
	require.Equal(t, []string{vlan.Name}, updated.Status.Vlans)
	require.Equal(t, []string{node.Name}, updated.Status.ReadyNodes)
	require.Empty(t, updated.Status.NotReadyNodes)
	require.True(t, updated.Status.Ready)
}

func TestProviderNetworkReconcilerExcludesUnreadyAndConflictingVlans(t *testing.T) {
	providerNetwork := &kubeovnv1.ProviderNetwork{Name: "provider-a"}
	readyVlan := &kubeovnv1.Vlan{
		Name: "vlan-ready",
		Spec: kubeovnv1.VlanSpec{ID: 100, Provider: providerNetwork.Name},
		Status: kubeovnv1.VlanStatus{Conditions: []kubeovnv1.Condition{{
			Type:               kubeovnv1.Ready,
			Status:             corev1.ConditionTrue,
			ObservedGeneration: 0,
		}}},
	}
	unreadyVlan := &kubeovnv1.Vlan{
		Name: "vlan-unready",
		Spec: kubeovnv1.VlanSpec{ID: 101, Provider: providerNetwork.Name},
	}
	conflictingVlan := &kubeovnv1.Vlan{
		Name: "vlan-conflict",
		Spec: kubeovnv1.VlanSpec{ID: 102, Provider: providerNetwork.Name},
		Status: kubeovnv1.VlanStatus{
			Conflict: true,
			Conditions: []kubeovnv1.Condition{{
				Type:               kubeovnv1.Ready,
				Status:             corev1.ConditionTrue,
				ObservedGeneration: 0,
			}},
		},
	}
	reconciler := &ProviderNetworkReconciler{
		Client:       newRuntimeClient(t, providerNetwork, readyVlan, unreadyVlan, conflictingVlan),
		PodNamespace: "kube-system",
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: providerNetwork.Name})
	require.NoError(t, err)

	updated := &kubeovnv1.ProviderNetwork{}
	require.NoError(t, reconciler.Get(t.Context(), types.NamespacedName{Name: providerNetwork.Name}, updated))
	require.Equal(t, []string{readyVlan.Name}, updated.Status.Vlans)
}

func TestProviderNetworkReconcilerRemovesStaleNodeConditions(t *testing.T) {
	providerNetwork := &kubeovnv1.ProviderNetwork{
		Name: "provider-a",
		Status: kubeovnv1.ProviderNetworkStatus{Conditions: []kubeovnv1.ProviderNetworkCondition{
			{Node: "stale-a", Type: kubeovnv1.Ready},
			{Node: "stale-b", Type: kubeovnv1.Ready},
		}},
	}
	reconciler := &ProviderNetworkReconciler{
		Client:       newRuntimeClient(t, providerNetwork),
		PodNamespace: "kube-system",
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: providerNetwork.Name})
	require.NoError(t, err)

	updated := &kubeovnv1.ProviderNetwork{}
	require.NoError(t, reconciler.Get(t.Context(), types.NamespacedName{Name: providerNetwork.Name}, updated))
	require.Empty(t, updated.Status.Conditions)
}
