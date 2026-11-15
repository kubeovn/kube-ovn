package controllerruntime

import "testing"

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
