package controllerruntime

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesSubnetIPAMReceiptStoreUsesCASRecords(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	store, err := newKubernetesSubnetIPAMReceiptStore(client.CoreV1().ConfigMaps("kube-system"))
	require.NoError(t, err)
	allocation := testDurableSubnetIPAMAllocation()

	require.NoError(t, store.Remember(t.Context(), allocation))
	stored, found, err := store.Lookup(t.Context(), allocation.key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, allocation, stored)

	require.NoError(t, store.Remember(t.Context(), allocation), "replaying the same receipt is idempotent")
	changed := allocation
	changed.token++
	require.ErrorIs(t, store.Remember(t.Context(), changed), ErrSubnetIPAMReceiptStale)
	require.ErrorIs(t, store.Delete(t.Context(), changed), ErrSubnetIPAMReceiptChanged)

	allocations, err := store.List(t.Context(), allocation.key.subnet)
	require.NoError(t, err)
	require.Equal(t, []subnetIPAMAllocation{allocation}, allocations)

	require.NoError(t, store.Delete(t.Context(), allocation))
	_, found, err = store.Lookup(t.Context(), allocation.key)
	require.NoError(t, err)
	require.False(t, found)
	require.ErrorIs(t, store.Delete(t.Context(), allocation), ErrSubnetIPAMReceiptChanged)
}

func TestKubernetesSubnetIPAMReceiptStoreRebindsWithCAS(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	store, err := newKubernetesSubnetIPAMReceiptStore(client.CoreV1().ConfigMaps("kube-system"))
	require.NoError(t, err)
	previous := testDurableSubnetIPAMAllocation()
	replacement := previous
	replacement.token++
	replacement.identity.WriterEpoch = "epoch-b"

	require.NoError(t, store.Remember(t.Context(), previous))
	require.NoError(t, store.Rebind(t.Context(), previous, replacement))
	require.ErrorIs(t, store.Delete(t.Context(), previous), ErrSubnetIPAMReceiptChanged)
	current, found, err := store.Lookup(t.Context(), previous.key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, replacement, current)
	require.ErrorIs(t, store.Rebind(t.Context(), previous, testDurableSubnetIPAMAllocation()), ErrSubnetIPAMReceiptChanged)
	require.NoError(t, store.Delete(t.Context(), replacement))
}

func TestSubnetIPAMReceiptClaimCASAndRetry(t *testing.T) {
	store := newMemorySubnetIPAMReceiptStore()
	allocation := testDurableSubnetIPAMAllocation()
	require.NoError(t, store.Remember(t.Context(), allocation))
	claimed, err := store.Claim(t.Context(), allocation)
	require.NoError(t, err)
	require.Equal(t, subnetIPAMReceiptPhaseReleasing, claimed.phase)
	oldEpoch := allocation
	oldEpoch.identity.WriterEpoch = "old-epoch"
	_, err = store.Claim(t.Context(), oldEpoch)
	require.ErrorIs(t, err, ErrSubnetIPAMReceiptChanged)
	_, err = store.Claim(t.Context(), allocation)
	require.ErrorIs(t, err, ErrSubnetIPAMReceiptChanged)
	retry, err := store.Claim(t.Context(), claimed)
	require.ErrorIs(t, err, ErrSubnetIPAMReceiptChanged)
	require.Empty(t, retry)
}

func TestSubnetIPAMReceiptJSONPhaseRoundTripAndLegacyDefault(t *testing.T) {
	allocation := testDurableSubnetIPAMAllocation()
	allocation.phase = subnetIPAMReceiptPhaseReleasing
	record, err := marshalSubnetIPAMAllocation(allocation)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(record, &raw))
	require.Equal(t, "releasing", raw["phase"])
	decoded, err := unmarshalSubnetIPAMAllocation(string(record))
	require.NoError(t, err)
	require.Equal(t, allocation, decoded)
	allocation.phase = subnetIPAMReceiptPhaseActive
	record, err = marshalSubnetIPAMAllocation(allocation)
	require.NoError(t, err)
	var activeRaw map[string]any
	require.NoError(t, json.Unmarshal(record, &activeRaw))
	require.Equal(t, "active", activeRaw["phase"])
	legacy := string(record)
	var legacyRaw map[string]any
	require.NoError(t, json.Unmarshal([]byte(legacy), &legacyRaw))
	delete(legacyRaw, "phase")
	legacyBytes, err := json.Marshal(legacyRaw)
	require.NoError(t, err)
	decoded, err = unmarshalSubnetIPAMAllocation(string(legacyBytes))
	require.NoError(t, err)
	require.Equal(t, subnetIPAMReceiptPhaseActive, decoded.phase)
}

func TestKubernetesSubnetIPAMReceiptStoreRejectsMalformedRecords(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&corev1.ConfigMap{
		Name:      "malformed",
		Namespace: "kube-system",
		Labels:    map[string]string{subnetIPAMReceiptLabel: "true"},
		Data:      map[string]string{subnetIPAMReceiptDataKey: "{}"},
	})
	store, err := newKubernetesSubnetIPAMReceiptStore(client.CoreV1().ConfigMaps("kube-system"))
	require.NoError(t, err)
	_, err = store.List(t.Context(), "subnet-a")
	require.ErrorIs(t, err, errSubnetIPAMReceiptInvalid)
}

func TestKubernetesSubnetIPAMReceiptStoreRequiresClient(t *testing.T) {
	_, err := newKubernetesSubnetIPAMReceiptStore(nil)
	require.EqualError(t, err, "subnet IPAM receipt store requires a ConfigMap client")
}

func testDurableSubnetIPAMAllocation() subnetIPAMAllocation {
	return subnetIPAMAllocation{
		key:     subnetIPAMKey{subnet: "subnet-a", pod: "pod-a", nic: "nic-a"},
		token:   7,
		uid:     "subnet-uid",
		address: subnetIPAMAddress{v4: "192.0.2.10", v6: "2001:db8::10", mac: "00:11:22:33:44:55"},
		identity: SubnetMutationIdentity{
			Name:            "subnet-a",
			UID:             types.UID("subnet-uid"),
			Generation:      3,
			ResourceVersion: "9",
			Operation:       SubnetIPAMAddressAllocateOperation,
			WriterEpoch:     "epoch-a",
		},
	}
}
