package controllerruntime

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestClassifySubnetDependencyEvent(t *testing.T) {
	tests := []struct {
		name  string
		event SubnetDependencyEvent
		want  SubnetEventPath
	}{
		{name: "subnet spec", event: SubnetDependencyEvent{SourceKind: "Subnet"}, want: SubnetSpecReconcilePath},
		{name: "vlan dependency", event: SubnetDependencyEvent{SourceKind: "Vlan"}, want: SubnetSpecReconcilePath},
		{name: "provider dependency", event: SubnetDependencyEvent{SourceKind: "ProviderNetwork"}, want: SubnetSpecReconcilePath},
		{name: "ip status", event: SubnetDependencyEvent{SourceKind: "IP"}, want: SubnetStatusObservePath},
		{name: "explicit status", event: SubnetDependencyEvent{SourceKind: "Subnet", StatusOnly: true}, want: SubnetStatusObservePath},
		{name: "node belongs to provider network", event: SubnetDependencyEvent{SourceKind: "Node"}, want: SubnetDependencyObservePath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifySubnetDependencyEvent(test.event); got != test.want {
				t.Fatalf("ClassifySubnetDependencyEvent(%+v) = %q, want %q", test.event, got, test.want)
			}
		})
	}
}

func TestSubnetAllocationGate(t *testing.T) {
	vlan := &kubeovnv1.Vlan{Name: "vlan-a"}
	provider := &kubeovnv1.ProviderNetwork{Status: kubeovnv1.ProviderNetworkStatus{Ready: true, Vlans: []string{"vlan-a"}}}
	tests := []struct {
		name     string
		vlan     *kubeovnv1.Vlan
		provider *kubeovnv1.ProviderNetwork
		want     bool
	}{
		{name: "ready", vlan: vlan, provider: provider, want: true},
		{name: "vlan missing", provider: provider, want: false},
		{name: "provider missing", vlan: vlan, want: false},
		{name: "provider not ready", vlan: vlan, provider: &kubeovnv1.ProviderNetwork{Status: kubeovnv1.ProviderNetworkStatus{Vlans: []string{"vlan-a"}}}, want: false},
		{name: "vlan conflict", vlan: &kubeovnv1.Vlan{Name: "vlan-a", Status: kubeovnv1.VlanStatus{Conflict: true}}, provider: provider, want: false},
		{name: "vlan not registered", vlan: vlan, provider: &kubeovnv1.ProviderNetwork{Status: kubeovnv1.ProviderNetworkStatus{Ready: true}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SubnetAllocationGate(test.vlan, test.provider); got != test.want {
				t.Fatalf("SubnetAllocationGate(%+v, %+v) = %t, want %t", test.vlan, test.provider, got, test.want)
			}
		})
	}
}

func TestObserveSubnetHasNoWriteSideEffects(t *testing.T) {
	subnet := &kubeovnv1.Subnet{Name: "subnet-a", Generation: 3, Spec: kubeovnv1.SubnetSpec{Vlan: "vlan-a"}}
	vlan := &kubeovnv1.Vlan{Name: "vlan-a", Spec: kubeovnv1.VlanSpec{Provider: "provider-a"}}
	provider := &kubeovnv1.ProviderNetwork{Name: "provider-a", Status: kubeovnv1.ProviderNetworkStatus{Ready: true, Vlans: []string{"vlan-a"}}}
	api := newRuntimeClient(t, subnet, vlan, provider)

	got, err := ObserveSubnet(t.Context(), api, subnet.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AllocationAllowed || got.Generation != subnet.Generation {
		t.Fatalf("unexpected observation: %+v", got)
	}

	var observed kubeovnv1.Subnet
	if err := api.Get(t.Context(), clientKey(subnet.Name), &observed); err != nil {
		t.Fatal(err)
	}
	if observed.ResourceVersion != subnet.ResourceVersion || observed.Generation != subnet.Generation {
		t.Fatalf("observation mutated subnet metadata: before=%+v after=%+v", subnet.ObjectMeta, observed.ObjectMeta)
	}
}

func TestObserveSubnetReportsDeletionAndFinalizerState(t *testing.T) {
	deletionTimestamp := metav1.Now()
	tests := []struct {
		name                    string
		deleting                bool
		finalizers              []string
		wantControllerFinalizer bool
		wantDeprecatedFinalizer bool
	}{
		{name: "active current finalizer", finalizers: []string{util.KubeOVNControllerFinalizer}, wantControllerFinalizer: true},
		{name: "terminating current finalizer", deleting: true, finalizers: []string{util.KubeOVNControllerFinalizer}, wantControllerFinalizer: true},
		{name: "terminating deprecated finalizer", deleting: true, finalizers: []string{util.DeprecatedFinalizerName}, wantDeprecatedFinalizer: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subnet := testSubnet("")
			subnet.Finalizers = append([]string(nil), test.finalizers...)
			if test.deleting {
				subnet.DeletionTimestamp = &deletionTimestamp
			}
			api := newRuntimeClient(t, subnet)

			got, err := ObserveSubnet(t.Context(), api, subnet.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got.Deleting != test.deleting ||
				got.HasControllerFinalizer != test.wantControllerFinalizer ||
				got.HasDeprecatedFinalizer != test.wantDeprecatedFinalizer {
				t.Fatalf("unexpected observation: %+v", got)
			}

			var observed kubeovnv1.Subnet
			if err := api.Get(t.Context(), clientKey(subnet.Name), &observed); err != nil {
				t.Fatal(err)
			}
			if observed.DeletionTimestamp == nil != (subnet.DeletionTimestamp == nil) ||
				!slices.Equal(observed.Finalizers, subnet.Finalizers) {
				t.Fatalf("observation mutated deletion metadata: before=%+v after=%+v", subnet.ObjectMeta, observed.ObjectMeta)
			}
		})
	}
}

func TestObserveSubnetBlocksMissingOrUnreadyDependencies(t *testing.T) {
	tests := []struct {
		name     string
		objects  []client.Object
		wantGate bool
	}{
		{name: "missing vlan", objects: []client.Object{testSubnet("vlan-a")}},
		{name: "missing provider", objects: []client.Object{testSubnet("vlan-a"), testVlan("vlan-a", "provider-a")}},
		{name: "provider not ready", objects: []client.Object{testSubnet("vlan-a"), testVlan("vlan-a", "provider-a"), testProvider("provider-a", false, "vlan-a")}},
		{name: "vlan conflict", objects: []client.Object{testSubnet("vlan-a"), testVlanConflict("vlan-a", "provider-a"), testProvider("provider-a", true, "vlan-a")}},
		{name: "provider has no vlan", objects: []client.Object{testSubnet("vlan-a"), testVlan("vlan-a", "provider-a"), testProvider("provider-a", true)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := newRuntimeClient(t, test.objects...)
			got, err := ObserveSubnet(t.Context(), api, "subnet-a")
			if err != nil {
				t.Fatal(err)
			}
			if got.AllocationAllowed != test.wantGate {
				t.Fatalf("unexpected observation: %+v", got)
			}
		})
	}
}

func clientKey(name string) client.ObjectKey { return client.ObjectKey{Name: name} }

func testSubnet(vlan string) *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{Name: "subnet-a", Spec: kubeovnv1.SubnetSpec{Vlan: vlan}}
}

func testVlan(name, provider string) *kubeovnv1.Vlan {
	return &kubeovnv1.Vlan{Name: name, Spec: kubeovnv1.VlanSpec{Provider: provider}}
}

func testVlanConflict(name, provider string) *kubeovnv1.Vlan {
	vlan := testVlan(name, provider)
	vlan.Status.Conflict = true
	return vlan
}

func testProvider(name string, ready bool, vlans ...string) *kubeovnv1.ProviderNetwork {
	return &kubeovnv1.ProviderNetwork{Name: name, Status: kubeovnv1.ProviderNetworkStatus{Ready: ready, Vlans: vlans}}
}
