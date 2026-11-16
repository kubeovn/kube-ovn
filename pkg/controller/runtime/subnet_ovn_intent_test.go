package controllerruntime

import (
	"errors"
	"testing"
)

func TestSubnetOVNMutationIntentHashIsCanonicalAndGenerationScoped(t *testing.T) {
	snapshot := SubnetWriteSnapshot{Name: "subnet-a", Generation: 3}
	intent := subnetOVNMutationIntentForSnapshot(snapshot)
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)

	if got := intent.hash(); got != key.IntentHash {
		t.Fatalf("intent hash %q does not match key hash %q", got, key.IntentHash)
	}
	other := snapshot
	other.Generation++
	if subnetOVNMutationKey(other, SubnetOVNAllocateOperation).IntentHash == key.IntentHash {
		t.Fatal("different Subnet generations must not share an OVN intent hash")
	}
}

func TestSubnetOVNMutationIntentEqualNormalizesNetworks(t *testing.T) {
	left := SubnetOVNMutationIntent{
		LogicalSwitchName:     "subnet-a",
		Generation:            3,
		LogicalRouterName:     "ovn-cluster",
		LogicalRouterPortName: "ovn-cluster-subnet-a",
		LogicalSwitchPortName: "subnet-a-ovn-cluster",
		LogicalRouterNetworks: []string{"fd00::1/64", "10.0.0.1/24"},
		RequirePatchPorts:     true,
	}
	right := left
	right.LogicalRouterNetworks = []string{"10.0.0.1/24", "fd00::1/64"}
	if !left.equal(right) {
		t.Fatal("equivalent router networks must compare equal regardless of order")
	}
}

func TestSubnetOVNMutationIntentRejectsPatchFieldsWithoutRequirement(t *testing.T) {
	fields := map[string]func(*SubnetOVNMutationIntent){
		"router":      func(intent *SubnetOVNMutationIntent) { intent.LogicalRouterName = "ovn-cluster" },
		"router port": func(intent *SubnetOVNMutationIntent) { intent.LogicalRouterPortName = "lrp-subnet-a" },
		"switch port": func(intent *SubnetOVNMutationIntent) { intent.LogicalSwitchPortName = "lsp-subnet-a" },
		"networks":    func(intent *SubnetOVNMutationIntent) { intent.LogicalRouterNetworks = []string{"10.0.0.1/24"} },
		"gateway MAC": func(intent *SubnetOVNMutationIntent) { intent.GatewayMAC = "00:00:00:00:00:01" },
	}
	for name, mutate := range fields {
		t.Run(name, func(t *testing.T) {
			intent := SubnetOVNMutationIntent{LogicalSwitchName: "subnet-a", Generation: 3}
			mutate(&intent)
			if err := intent.valid(); !errors.Is(err, ErrSubnetOVNReceiptInvalid) {
				t.Fatalf("valid() error = %v, want %v", err, ErrSubnetOVNReceiptInvalid)
			}
		})
	}
}
