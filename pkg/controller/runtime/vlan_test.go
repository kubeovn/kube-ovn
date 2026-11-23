package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type recordingVlanOVN struct {
	tags     map[string]int
	ports    map[string]bool
	readErr  error
	writeErr error
}

func (o *recordingVlanOVN) LogicalSwitchPortExists(name string) (bool, error) {
	if o.readErr != nil {
		return false, o.readErr
	}
	if o.ports == nil {
		return true, nil
	}
	return o.ports[name], nil
}

func (o *recordingVlanOVN) SetLogicalSwitchPortVlanTag(name string, vlanID int) error {
	if o.writeErr != nil {
		return o.writeErr
	}
	if o.ports != nil && !o.ports[name] {
		return fmt.Errorf("logical switch port %s not found", name)
	}
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

func TestVlanReconcilerWaitsForRecoveryGate(t *testing.T) {
	reconciler := &VlanReconciler{RecoveryGate: func(context.Context) error {
		return context.Canceled
	}}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "vlan-a"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestProviderNetworkReconcilerWaitsForRecoveryGate(t *testing.T) {
	reconciler := &ProviderNetworkReconciler{RecoveryGate: func(context.Context) error {
		return context.Canceled
	}}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "provider-a"})
	require.ErrorIs(t, err, context.Canceled)
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
		ProviderInterfaceName: "provider-a", //nolint:staticcheck // Exercise migration from the deprecated field.
	}}

	require.True(t, migrateVlanSpec(vlan))
	require.Equal(t, 100, vlan.Spec.ID)
	require.Equal(t, "provider-a", vlan.Spec.Provider)
	require.Zero(t, vlan.Spec.VlanID)
	require.Empty(t, vlan.Spec.ProviderInterfaceName) //nolint:staticcheck // Verify the deprecated field is cleared.
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

func TestVlanReconcilerRequestsConflictPeersAfterDelete(t *testing.T) {
	deleted := &kubeovnv1.Vlan{Name: "deleted", Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"}}
	reconciler := &VlanReconciler{Client: newRuntimeClient(t,
		&kubeovnv1.Vlan{Name: "peer", Spec: deleted.Spec},
	)}

	requests := reconciler.relatedVlanRequests(t.Context(), deleted)
	require.Equal(t, []ctrl.Request{{Name: "peer"}}, requests)
}

func TestProviderNetworkReconcilerMapsVlanProviderChanges(t *testing.T) {
	reconciler := &ProviderNetworkReconciler{}
	oldVlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{Provider: "provider-old"}}
	newVlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{Provider: "provider-new"}}

	require.Equal(t, []ctrl.Request{{Name: "provider-old"}}, reconciler.requestsForVlanObject(oldVlan))
	require.Equal(t, []ctrl.Request{{Name: "provider-new"}}, reconciler.requestsForVlanObject(newVlan))
}

func TestVlanAndProviderNetworkEventMappers(t *testing.T) {
	vlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{Provider: "provider-a"}}
	otherVlan := &kubeovnv1.Vlan{Name: "vlan-b", Spec: kubeovnv1.VlanSpec{Provider: "provider-b"}}
	client := newRuntimeClient(t, vlan, otherVlan,
		&kubeovnv1.ProviderNetwork{Name: "provider-a"},
		&kubeovnv1.ProviderNetwork{Name: "provider-b"},
	)
	vlanReconciler := &VlanReconciler{Client: client}
	require.Equal(t, []ctrl.Request{{Name: "vlan-a"}}, vlanReconciler.requestsForSubnet(t.Context(), &kubeovnv1.Subnet{
		Spec: kubeovnv1.SubnetSpec{Vlan: "vlan-a"},
	}))
	require.Equal(t, []ctrl.Request{{Name: "vlan-a"}}, vlanReconciler.requestsForProviderNetwork(t.Context(), &kubeovnv1.ProviderNetwork{Name: "provider-a"}))

	providerNetworkReconciler := &ProviderNetworkReconciler{Client: client}
	require.ElementsMatch(t,
		[]ctrl.Request{{Name: "provider-a"}, {Name: "provider-b"}},
		providerNetworkReconciler.requestsForAllProviderNetworks(t.Context(), &corev1.Node{}),
	)
}

func TestIsKubeOvnCniPod(t *testing.T) {
	kubeOvnCniPod := &corev1.Pod{}
	kubeOvnCniPod.Labels = map[string]string{"app": "kube-ovn-cni"}
	otherPod := &corev1.Pod{}
	otherPod.Labels = map[string]string{"app": "other"}
	require.True(t, isKubeOvnCniPod(kubeOvnCniPod))
	require.False(t, isKubeOvnCniPod(otherPod))
	require.False(t, isKubeOvnCniPod(&corev1.Node{}))
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

// Count writes as well as comparing values: a no-op status patch can still
// generate API traffic even if a fake API happens to preserve its version.
type statusCountingClient struct {
	client.Client
	patches int
}

func (c *statusCountingClient) Status() client.SubResourceWriter {
	return &statusCountingWriter{SubResourceWriter: c.Client.Status(), patches: &c.patches}
}

type statusCountingWriter struct {
	client.SubResourceWriter
	patches *int
}

func (w *statusCountingWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	*w.patches++
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}

func TestVlanReconcilerBootstrapsBeforeLocalnetPortExists(t *testing.T) {
	vlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"}}
	pn := &kubeovnv1.ProviderNetwork{Name: vlan.Spec.Provider}
	subnet := &kubeovnv1.Subnet{Name: "subnet-a", Spec: kubeovnv1.SubnetSpec{Vlan: vlan.Name}}
	ovn := &recordingVlanOVN{ports: map[string]bool{}}
	api := newRuntimeClient(t, vlan, pn, subnet)
	r := &VlanReconciler{Client: api, OVN: ovn}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
	require.NoError(t, err, "VLAN readiness must allow the Subnet controller to create its localnet port")
	updated := &kubeovnv1.Vlan{}
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(vlan), updated))
	require.True(t, vlanIsReady(updated))
	require.Empty(t, ovn.tags)

	pnReconciler := &ProviderNetworkReconciler{Client: api, PodNamespace: "kube-system"}
	_, err = pnReconciler.Reconcile(t.Context(), ctrl.Request{Name: pn.Name})
	require.NoError(t, err)
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(pn), pn))
	require.Contains(t, pn.Status.Vlans, vlan.Name)

	ovn.ports[ovs.GetLocalnetName(subnet.Name)] = true
	updated.Spec.ID = 101
	require.NoError(t, api.Update(t.Context(), updated))
	_, err = r.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
	require.NoError(t, err)
	require.Equal(t, 101, ovn.tags[ovs.GetLocalnetName(subnet.Name)])
}

func TestVlanReconcilerSettledStatusDoesNotWriteOrEmitEvents(t *testing.T) {
	vlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"}}
	pn := &kubeovnv1.ProviderNetwork{Name: vlan.Spec.Provider, Status: kubeovnv1.ProviderNetworkStatus{Vlans: []string{vlan.Name}}}
	api := &statusCountingClient{Client: newRuntimeClient(t, vlan, pn)}
	recorder := record.NewFakeRecorder(10)
	r := &VlanReconciler{Client: api, OVN: &recordingVlanOVN{}, Recorder: recorder}
	_, err := r.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
	require.NoError(t, err)
	require.Equal(t, 1, api.patches)
	require.Len(t, recorder.Events, 1)
	<-recorder.Events

	// Force a timestamp older than the current second, so this test cannot
	// accidentally pass because metav1.Time JSON truncates subsecond precision.
	updated := &kubeovnv1.Vlan{}
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(vlan), updated))
	for i := range updated.Status.Conditions {
		updated.Status.Conditions[i].LastUpdateTime = metav1.NewTime(time.Unix(1, 0))
		updated.Status.Conditions[i].LastTransitionTime = metav1.NewTime(time.Unix(1, 0))
	}
	require.NoError(t, api.Client.Status().Update(t.Context(), updated))
	before := updated.DeepCopy()
	_, err = r.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
	require.NoError(t, err)
	require.Equal(t, 1, api.patches, "settled reconciliation must not issue another status patch")
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(vlan), updated))
	require.Equal(t, before.Status, updated.Status)
	require.Empty(t, recorder.Events)
}

func TestVlanReconcilerRetriesOVNFailureWithoutReadyStatus(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			vlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{ID: 100, Provider: "provider-a"}}
			setVlanReadyCondition(vlan)
			pn := &kubeovnv1.ProviderNetwork{Name: vlan.Spec.Provider}
			subnet := &kubeovnv1.Subnet{Name: "subnet-a", Spec: kubeovnv1.SubnetSpec{Vlan: vlan.Name}}
			ovn := &recordingVlanOVN{}
			failure := errors.New("OVN unavailable")
			if operation == "read" {
				ovn.readErr = failure
			} else {
				ovn.writeErr = failure
			}
			r := &VlanReconciler{Client: newRuntimeClient(t, vlan, pn, subnet), OVN: ovn}
			_, err := r.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
			require.ErrorIs(t, err, failure)
			updated := &kubeovnv1.Vlan{}
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(vlan), updated))
			require.False(t, vlanIsReady(updated))
			ovn.readErr, ovn.writeErr = nil, nil
			_, err = r.Reconcile(t.Context(), ctrl.Request{Name: vlan.Name})
			require.NoError(t, err)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(vlan), updated))
			require.True(t, vlanIsReady(updated))
		})
	}
}
