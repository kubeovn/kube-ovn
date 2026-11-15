package controllerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	subnetIPAMReceiptConfigMapPrefix = "kube-ovn-subnet-ipam-"
	subnetIPAMReceiptLabel           = "kube-ovn.io/subnet-ipam-receipt"
	subnetIPAMReceiptDataKey         = "allocation.json"
)

var errSubnetIPAMReceiptInvalid = errors.New("invalid Subnet IPAM receipt")

// kubernetesSubnetIPAMReceiptStore persists one IPAM allocation receipt per
// ConfigMap. Create-if-absent writes and resource-version guarded deletes make
// receipt publication and compensation safe across adapter recreation.
type kubernetesSubnetIPAMReceiptStore struct {
	configMaps coreclient.ConfigMapInterface
}

var _ subnetIPAMReceiptLister = (*kubernetesSubnetIPAMReceiptStore)(nil)

// newKubernetesSubnetIPAMReceiptStore constructs an API-backed receipt store.
// The caller must pass a namespace-scoped ConfigMap client owned by the
// runtime controller's durable namespace.
func newKubernetesSubnetIPAMReceiptStore(configMaps coreclient.ConfigMapInterface) (*kubernetesSubnetIPAMReceiptStore, error) {
	if configMaps == nil {
		return nil, errors.New("subnet IPAM receipt store requires a ConfigMap client")
	}
	return &kubernetesSubnetIPAMReceiptStore{configMaps: configMaps}, nil
}

func (s *kubernetesSubnetIPAMReceiptStore) Remember(ctx context.Context, allocation subnetIPAMAllocation) error {
	if err := validateSubnetIPAMAllocation(allocation); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	record, err := marshalSubnetIPAMAllocation(allocation)
	if err != nil {
		return err
	}
	name := subnetIPAMReceiptConfigMapName(allocation.key)
	cm := &corev1.ConfigMap{
		Name:   name,
		Labels: map[string]string{subnetIPAMReceiptLabel: "true"},
		Data:   map[string]string{subnetIPAMReceiptDataKey: string(record)},
	}
	_, err = s.configMaps.Create(ctx, cm, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	current, found, err := s.lookup(ctx, allocation.key)
	if err != nil {
		return err
	}
	if !found || current != allocation {
		return ErrSubnetIPAMReceiptStale
	}
	return nil
}

func (s *kubernetesSubnetIPAMReceiptStore) Lookup(ctx context.Context, key subnetIPAMKey) (subnetIPAMAllocation, bool, error) {
	if err := contextError(ctx); err != nil {
		return subnetIPAMAllocation{}, false, err
	}
	return s.lookup(ctx, key)
}

func (s *kubernetesSubnetIPAMReceiptStore) Delete(ctx context.Context, allocation subnetIPAMAllocation) error {
	if err := validateSubnetIPAMAllocation(allocation); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	current, found, err := s.lookupConfigMap(ctx, allocation.key)
	if err != nil {
		return err
	}
	if !found {
		return ErrSubnetIPAMReceiptChanged
	}
	currentAllocation, err := unmarshalSubnetIPAMAllocation(current.Data[subnetIPAMReceiptDataKey])
	if err != nil {
		return err
	}
	if currentAllocation != allocation {
		return ErrSubnetIPAMReceiptChanged
	}
	preconditions := &metav1.Preconditions{UID: &current.UID, ResourceVersion: &current.ResourceVersion}
	err = s.configMaps.Delete(ctx, current.Name, metav1.DeleteOptions{Preconditions: preconditions})
	if apierrors.IsNotFound(err) {
		return ErrSubnetIPAMReceiptChanged
	}
	return err
}

// List returns all durable receipts belonging to one Subnet. It is kept as a
// refinement of subnetIPAMReceiptStore so recovery can enumerate ownership
// without weakening the narrow mutation interface.
func (s *kubernetesSubnetIPAMReceiptStore) List(ctx context.Context, subnetName string) ([]subnetIPAMAllocation, error) {
	if subnetName == "" {
		return nil, fmt.Errorf("%w: missing Subnet name", errSubnetIPAMReceiptInvalid)
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	list, err := s.configMaps.List(ctx, metav1.ListOptions{LabelSelector: labels.Set{subnetIPAMReceiptLabel: "true"}.AsSelector().String()})
	if err != nil {
		return nil, err
	}
	allocations := make([]subnetIPAMAllocation, 0, len(list.Items))
	for i := range list.Items {
		allocation, err := unmarshalSubnetIPAMAllocation(list.Items[i].Data[subnetIPAMReceiptDataKey])
		if err != nil {
			return nil, err
		}
		if allocation.key.subnet == subnetName {
			allocations = append(allocations, allocation)
		}
	}
	return allocations, nil
}

func (s *kubernetesSubnetIPAMReceiptStore) lookup(ctx context.Context, key subnetIPAMKey) (subnetIPAMAllocation, bool, error) {
	cm, found, err := s.lookupConfigMap(ctx, key)
	if err != nil || !found {
		return subnetIPAMAllocation{}, found, err
	}
	allocation, err := unmarshalSubnetIPAMAllocation(cm.Data[subnetIPAMReceiptDataKey])
	if err != nil {
		return subnetIPAMAllocation{}, false, err
	}
	if allocation.key != key {
		return subnetIPAMAllocation{}, false, errSubnetIPAMReceiptInvalid
	}
	return allocation, true, nil
}

func (s *kubernetesSubnetIPAMReceiptStore) lookupConfigMap(ctx context.Context, key subnetIPAMKey) (*corev1.ConfigMap, bool, error) {
	cm, err := s.configMaps.Get(ctx, subnetIPAMReceiptConfigMapName(key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return cm, true, nil
}

type subnetIPAMAllocationRecord struct {
	Key      subnetIPAMKeyRecord          `json:"key"`
	Token    uint64                       `json:"token"`
	UID      string                       `json:"uid"`
	Address  subnetIPAMAddressRecord      `json:"address"`
	Identity subnetMutationIdentityRecord `json:"identity"`
}

type subnetIPAMKeyRecord struct {
	Subnet string `json:"subnet"`
	Pod    string `json:"pod"`
	NIC    string `json:"nic"`
}

type subnetIPAMAddressRecord struct {
	V4  string `json:"v4"`
	V6  string `json:"v6"`
	MAC string `json:"mac"`
}

type subnetMutationIdentityRecord struct {
	Name            string               `json:"name"`
	UID             string               `json:"uid"`
	Generation      int64                `json:"generation"`
	ResourceVersion string               `json:"resourceVersion"`
	Operation       SubnetWriteOperation `json:"operation"`
	WriterEpoch     string               `json:"writerEpoch"`
}

func validateSubnetIPAMAllocation(allocation subnetIPAMAllocation) error {
	if allocation.key.subnet == "" || allocation.key.pod == "" || allocation.key.nic == "" || allocation.uid == "" || allocation.token == 0 ||
		(allocation.address.v4 == "" && allocation.address.v6 == "" && allocation.address.mac == "") {
		return errSubnetIPAMReceiptInvalid
	}
	return allocation.identity.Validate()
}

func marshalSubnetIPAMAllocation(allocation subnetIPAMAllocation) ([]byte, error) {
	record := subnetIPAMAllocationRecord{
		Key:      subnetIPAMKeyRecord{Subnet: allocation.key.subnet, Pod: allocation.key.pod, NIC: allocation.key.nic},
		Token:    allocation.token,
		UID:      allocation.uid,
		Address:  subnetIPAMAddressRecord{V4: allocation.address.v4, V6: allocation.address.v6, MAC: allocation.address.mac},
		Identity: subnetMutationIdentityRecord{Name: allocation.identity.Name, UID: string(allocation.identity.UID), Generation: allocation.identity.Generation, ResourceVersion: allocation.identity.ResourceVersion, Operation: allocation.identity.Operation, WriterEpoch: allocation.identity.WriterEpoch},
	}
	return json.Marshal(record)
}

func unmarshalSubnetIPAMAllocation(data string) (subnetIPAMAllocation, error) {
	if data == "" {
		return subnetIPAMAllocation{}, errSubnetIPAMReceiptInvalid
	}
	var record subnetIPAMAllocationRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return subnetIPAMAllocation{}, fmt.Errorf("decode Subnet IPAM receipt: %w", err)
	}
	allocation := subnetIPAMAllocation{
		key:      subnetIPAMKey{subnet: record.Key.Subnet, pod: record.Key.Pod, nic: record.Key.NIC},
		token:    record.Token,
		uid:      record.UID,
		address:  subnetIPAMAddress{v4: record.Address.V4, v6: record.Address.V6, mac: record.Address.MAC},
		identity: SubnetMutationIdentity{Name: record.Identity.Name, UID: types.UID(record.Identity.UID), Generation: record.Identity.Generation, ResourceVersion: record.Identity.ResourceVersion, Operation: record.Identity.Operation, WriterEpoch: record.Identity.WriterEpoch},
	}
	if err := validateSubnetIPAMAllocation(allocation); err != nil {
		return subnetIPAMAllocation{}, err
	}
	return allocation, nil
}

func subnetIPAMReceiptConfigMapName(key subnetIPAMKey) string {
	hash := sha256.Sum256([]byte(key.subnet + "\x00" + key.pod + "\x00" + key.nic))
	return subnetIPAMReceiptConfigMapPrefix + hex.EncodeToString(hash[:16])
}
