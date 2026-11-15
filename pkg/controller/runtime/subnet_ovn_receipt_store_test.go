package controllerruntime

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestMemorySubnetOVNMutationReceiptStoreCAS(t *testing.T) {
	store := newMemorySubnetOVNMutationReceiptStore()
	receipt := testSubnetOVNMutationReceipt()
	require.NoError(t, store.Remember(t.Context(), receipt))
	require.NoError(t, store.Remember(t.Context(), receipt))
	current, found, err := store.Lookup(t.Context(), receipt.Key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receipt, current)

	changed := receipt
	changed.Token++
	require.ErrorIs(t, store.Remember(t.Context(), changed), ErrSubnetOVNReceiptStale)
	require.ErrorIs(t, store.Delete(t.Context(), changed), ErrSubnetOVNReceiptChanged)

	claimed, err := store.Claim(t.Context(), receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNReceiptPhaseReleasing, claimed.Phase)
	_, err = store.Claim(t.Context(), receipt)
	require.ErrorIs(t, err, ErrSubnetOVNReceiptChanged)
	require.ErrorIs(t, store.Delete(t.Context(), receipt), ErrSubnetOVNReceiptChanged)
	require.NoError(t, store.Delete(t.Context(), claimed))
	_, found, err = store.Lookup(t.Context(), receipt.Key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestMemorySubnetOVNMutationReceiptStoreRebindsOwner(t *testing.T) {
	store := newMemorySubnetOVNMutationReceiptStore()
	previous := testSubnetOVNMutationReceipt()
	replacement := previous
	replacement.Token++
	replacement.WriterEpoch = "epoch-b"
	require.NoError(t, store.Remember(t.Context(), previous))
	require.NoError(t, store.Rebind(t.Context(), previous, replacement))
	require.ErrorIs(t, store.Delete(t.Context(), previous), ErrSubnetOVNReceiptChanged)
	current, found, err := store.Lookup(t.Context(), previous.Key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, replacement, current)
	require.ErrorIs(t, store.Rebind(t.Context(), previous, testSubnetOVNMutationReceipt()), ErrSubnetOVNReceiptChanged)
}

func TestKubernetesSubnetOVNMutationReceiptStoreCAS(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	store, err := newKubernetesSubnetOVNMutationReceiptStore(client.CoreV1().ConfigMaps("kube-system"))
	require.NoError(t, err)
	receipt := testSubnetOVNMutationReceipt()
	require.NoError(t, store.Remember(t.Context(), receipt))
	require.NoError(t, store.Remember(t.Context(), receipt))

	claimed, err := store.Claim(t.Context(), receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNReceiptPhaseReleasing, claimed.Phase)
	require.ErrorIs(t, store.Delete(t.Context(), receipt), ErrSubnetOVNReceiptChanged)
	require.NoError(t, store.Delete(t.Context(), claimed))
	_, found, err := store.Lookup(t.Context(), receipt.Key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestKubernetesSubnetOVNMutationReceiptStoreRebindAndList(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	store, err := newKubernetesSubnetOVNMutationReceiptStore(client.CoreV1().ConfigMaps("kube-system"))
	require.NoError(t, err)
	previous := testSubnetOVNMutationReceipt()
	replacement := previous
	replacement.Token++
	replacement.WriterEpoch = "epoch-b"
	require.NoError(t, store.Remember(t.Context(), previous))
	require.NoError(t, store.Rebind(t.Context(), previous, replacement))
	list, err := store.List(t.Context(), previous.Key.Name)
	require.NoError(t, err)
	require.Equal(t, []SubnetOVNMutationReceipt{replacement}, list)
	require.ErrorIs(t, store.Delete(t.Context(), previous), ErrSubnetOVNReceiptChanged)
	require.NoError(t, store.Delete(t.Context(), replacement))
}

func TestKubernetesSubnetOVNMutationReceiptStoreRejectsMalformedRecord(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&corev1.ConfigMap{
		Name:      subnetOVNReceiptConfigMapName(testSubnetOVNMutationReceipt().Key),
		Namespace: "kube-system",
		Labels:    map[string]string{subnetOVNReceiptLabel: "true"},
		Data:      map[string]string{subnetOVNReceiptDataKey: "{}"},
	})
	store, err := newKubernetesSubnetOVNMutationReceiptStore(client.CoreV1().ConfigMaps("kube-system"))
	require.NoError(t, err)
	_, _, err = store.Lookup(t.Context(), testSubnetOVNMutationReceipt().Key)
	require.ErrorIs(t, err, ErrSubnetOVNReceiptInvalid)
}

func TestSubnetOVNMutationReceiptJSONRoundTrip(t *testing.T) {
	receipt := testSubnetOVNMutationReceipt()
	receipt.Phase = SubnetOVNReceiptPhaseReleasing
	record, err := json.Marshal(receipt)
	require.NoError(t, err)
	decoded, err := unmarshalSubnetOVNMutationReceipt(string(record))
	require.NoError(t, err)
	require.Equal(t, receipt, decoded)
}

func TestSubnetOVNMutationReceiptStoreRequiresClient(t *testing.T) {
	_, err := newKubernetesSubnetOVNMutationReceiptStore(nil)
	require.EqualError(t, err, "subnet OVN receipt store requires a ConfigMap client")
}

func testSubnetOVNMutationReceipt() SubnetOVNMutationReceipt {
	return SubnetOVNMutationReceipt{
		Key: SubnetOVNMutationKey{
			Name:       "subnet-a",
			UID:        types.UID("subnet-uid"),
			Operation:  SubnetOVNAllocateOperation,
			IntentHash: "intent-a",
		},
		Token:           7,
		Generation:      3,
		ResourceVersion: "9",
		WriterEpoch:     "epoch-a",
	}
}
