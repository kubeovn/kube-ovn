package controllerruntime

import (
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
