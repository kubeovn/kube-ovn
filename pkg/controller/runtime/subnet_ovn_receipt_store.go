package controllerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	subnetOVNReceiptConfigMapPrefix = "kube-ovn-subnet-ovn-"
	subnetOVNReceiptLabel           = "kube-ovn.io/subnet-ovn-receipt"
	subnetOVNReceiptDataKey         = "mutation.json"
)

var (
	ErrSubnetOVNReceiptInvalid = errors.New("invalid Subnet OVN receipt")
	ErrSubnetOVNReceiptStale   = errors.New("subnet OVN receipt is stale or owned by another writer")
	ErrSubnetOVNReceiptChanged = errors.New("subnet OVN receipt owner changed")
)

// SubnetOVNMutationKey identifies one intended OVN mutation. IntentHash
// prevents two operations with the same Subnet and operation from sharing a
// compensation record when their data-plane intent differs.
type SubnetOVNMutationKey struct {
	Name       string               `json:"name"`
	UID        types.UID            `json:"uid"`
	Operation  SubnetWriteOperation `json:"operation"`
	IntentHash string               `json:"intentHash"`
}

// SubnetOVNReceiptPhase records whether a mutation is available for
// compensation or has been claimed for release. The zero value is active.
type SubnetOVNReceiptPhase string

const (
	SubnetOVNReceiptPhaseActive    SubnetOVNReceiptPhase = ""
	SubnetOVNReceiptPhaseReleasing SubnetOVNReceiptPhase = "releasing"
)

// SubnetOVNMutationReceipt is the durable ownership record for one OVN
// mutation. It is intentionally independent from OVSDB marker state; the two
// stores form a retryable saga rather than a two-phase commit.
type SubnetOVNMutationReceipt struct {
	Key             SubnetOVNMutationKey  `json:"key"`
	Token           uint64                `json:"token"`
	Generation      int64                 `json:"generation"`
	ResourceVersion string                `json:"resourceVersion"`
	WriterEpoch     string                `json:"writerEpoch"`
	Phase           SubnetOVNReceiptPhase `json:"phase"`
}

func (r SubnetOVNMutationReceipt) valid() error {
	if r.Key.Name == "" || r.Key.UID == "" || r.Key.IntentHash == "" || r.Token == 0 ||
		r.ResourceVersion == "" || r.WriterEpoch == "" {
		return ErrSubnetOVNReceiptInvalid
	}
	if r.Key.Operation != SubnetOVNAllocateOperation && r.Key.Operation != SubnetOVNCleanupOperation {
		return ErrSubnetOVNReceiptInvalid
	}
	if r.Phase != SubnetOVNReceiptPhaseActive && r.Phase != SubnetOVNReceiptPhaseReleasing {
		return ErrSubnetOVNReceiptInvalid
	}
	return nil
}

func (r SubnetOVNMutationReceipt) ownerEqual(other SubnetOVNMutationReceipt) bool {
	return r.Key == other.Key && r.Token == other.Token && r.Generation == other.Generation &&
		r.ResourceVersion == other.ResourceVersion && r.WriterEpoch == other.WriterEpoch
}

type subnetOVNMutationReceiptStore interface {
	Remember(context.Context, SubnetOVNMutationReceipt) error
	Rebind(context.Context, SubnetOVNMutationReceipt, SubnetOVNMutationReceipt) error
	Lookup(context.Context, SubnetOVNMutationKey) (SubnetOVNMutationReceipt, bool, error)
	Claim(context.Context, SubnetOVNMutationReceipt) (SubnetOVNMutationReceipt, error)
	Delete(context.Context, SubnetOVNMutationReceipt) error
}

type subnetOVNMutationReceiptLister interface {
	subnetOVNMutationReceiptStore
	List(context.Context, string) ([]SubnetOVNMutationReceipt, error)
}

type memorySubnetOVNMutationReceiptStore struct {
	mu       sync.Mutex
	receipts map[SubnetOVNMutationKey]SubnetOVNMutationReceipt
}

func newMemorySubnetOVNMutationReceiptStore() *memorySubnetOVNMutationReceiptStore {
	return &memorySubnetOVNMutationReceiptStore{receipts: make(map[SubnetOVNMutationKey]SubnetOVNMutationReceipt)}
}

func (s *memorySubnetOVNMutationReceiptStore) Remember(ctx context.Context, receipt SubnetOVNMutationReceipt) error {
	if err := receipt.valid(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.receipts[receipt.Key]; ok && current != receipt {
		return ErrSubnetOVNReceiptStale
	}
	s.receipts[receipt.Key] = receipt
	return nil
}

func (s *memorySubnetOVNMutationReceiptStore) Rebind(ctx context.Context, previous, replacement SubnetOVNMutationReceipt) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := previous.valid(); err != nil {
		return err
	}
	if err := replacement.valid(); err != nil {
		return err
	}
	if previous.Key != replacement.Key {
		return ErrSubnetOVNReceiptChanged
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.receipts[previous.Key]; !ok || current != previous {
		return ErrSubnetOVNReceiptChanged
	}
	s.receipts[previous.Key] = replacement
	return nil
}

func (s *memorySubnetOVNMutationReceiptStore) Lookup(ctx context.Context, key SubnetOVNMutationKey) (SubnetOVNMutationReceipt, bool, error) {
	if err := contextError(ctx); err != nil {
		return SubnetOVNMutationReceipt{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, ok := s.receipts[key]
	return receipt, ok, nil
}

func (s *memorySubnetOVNMutationReceiptStore) Claim(ctx context.Context, receipt SubnetOVNMutationReceipt) (SubnetOVNMutationReceipt, error) {
	if err := contextError(ctx); err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	if err := receipt.valid(); err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.receipts[receipt.Key]
	if !ok || !current.ownerEqual(receipt) || current.Phase != SubnetOVNReceiptPhaseActive || receipt.Phase != SubnetOVNReceiptPhaseActive {
		return SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptChanged
	}
	current.Phase = SubnetOVNReceiptPhaseReleasing
	s.receipts[receipt.Key] = current
	return current, nil
}

func (s *memorySubnetOVNMutationReceiptStore) Delete(ctx context.Context, receipt SubnetOVNMutationReceipt) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := receipt.valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.receipts[receipt.Key]; !ok || current != receipt {
		return ErrSubnetOVNReceiptChanged
	}
	delete(s.receipts, receipt.Key)
	return nil
}

func (s *memorySubnetOVNMutationReceiptStore) List(ctx context.Context, subnetName string) ([]SubnetOVNMutationReceipt, error) {
	if subnetName == "" {
		return nil, fmt.Errorf("%w: missing Subnet name", ErrSubnetOVNReceiptInvalid)
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	receipts := make([]SubnetOVNMutationReceipt, 0)
	for _, receipt := range s.receipts {
		if receipt.Key.Name == subnetName {
			receipts = append(receipts, receipt)
		}
	}
	return receipts, nil
}

var _ subnetOVNMutationReceiptLister = (*memorySubnetOVNMutationReceiptStore)(nil)

type kubernetesSubnetOVNMutationReceiptStore struct {
	configMaps coreclient.ConfigMapInterface
}

func newKubernetesSubnetOVNMutationReceiptStore(configMaps coreclient.ConfigMapInterface) (*kubernetesSubnetOVNMutationReceiptStore, error) {
	if configMaps == nil {
		return nil, errors.New("subnet OVN receipt store requires a ConfigMap client")
	}
	return &kubernetesSubnetOVNMutationReceiptStore{configMaps: configMaps}, nil
}

func (s *kubernetesSubnetOVNMutationReceiptStore) Remember(ctx context.Context, receipt SubnetOVNMutationReceipt) error {
	if err := receipt.valid(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	record, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		Name:   subnetOVNReceiptConfigMapName(receipt.Key),
		Labels: map[string]string{subnetOVNReceiptLabel: "true"},
		Data:   map[string]string{subnetOVNReceiptDataKey: string(record)},
	}
	_, err = s.configMaps.Create(ctx, cm, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	current, found, err := s.lookup(ctx, receipt.Key)
	if err != nil {
		return err
	}
	if !found || current != receipt {
		return ErrSubnetOVNReceiptStale
	}
	return nil
}

func (s *kubernetesSubnetOVNMutationReceiptStore) Rebind(ctx context.Context, previous, replacement SubnetOVNMutationReceipt) error {
	if err := previous.valid(); err != nil {
		return err
	}
	if err := replacement.valid(); err != nil {
		return err
	}
	if previous.Key != replacement.Key {
		return ErrSubnetOVNReceiptChanged
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	cm, found, err := s.lookupConfigMap(ctx, previous.Key)
	if err != nil {
		return err
	}
	if !found {
		return ErrSubnetOVNReceiptChanged
	}
	current, err := unmarshalSubnetOVNMutationReceipt(cm.Data[subnetOVNReceiptDataKey])
	if err != nil {
		return err
	}
	if current != previous {
		return ErrSubnetOVNReceiptChanged
	}
	record, err := json.Marshal(replacement)
	if err != nil {
		return err
	}
	updated := cm.DeepCopy()
	if updated.Data == nil {
		updated.Data = make(map[string]string)
	}
	updated.Data[subnetOVNReceiptDataKey] = string(record)
	if _, err = s.configMaps.Update(ctx, updated, metav1.UpdateOptions{}); apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return ErrSubnetOVNReceiptChanged
	}
	return err
}

func (s *kubernetesSubnetOVNMutationReceiptStore) Lookup(ctx context.Context, key SubnetOVNMutationKey) (SubnetOVNMutationReceipt, bool, error) {
	if err := contextError(ctx); err != nil {
		return SubnetOVNMutationReceipt{}, false, err
	}
	return s.lookup(ctx, key)
}

func (s *kubernetesSubnetOVNMutationReceiptStore) Claim(ctx context.Context, receipt SubnetOVNMutationReceipt) (SubnetOVNMutationReceipt, error) {
	if err := receipt.valid(); err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	if err := contextError(ctx); err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	cm, found, err := s.lookupConfigMap(ctx, receipt.Key)
	if err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	if !found {
		return SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptChanged
	}
	current, err := unmarshalSubnetOVNMutationReceipt(cm.Data[subnetOVNReceiptDataKey])
	if err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	if !current.ownerEqual(receipt) || current.Phase != SubnetOVNReceiptPhaseActive || receipt.Phase != SubnetOVNReceiptPhaseActive {
		return SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptChanged
	}
	claimed := current
	claimed.Phase = SubnetOVNReceiptPhaseReleasing
	record, err := json.Marshal(claimed)
	if err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	updated := cm.DeepCopy()
	if updated.Data == nil {
		updated.Data = make(map[string]string)
	}
	updated.Data[subnetOVNReceiptDataKey] = string(record)
	if _, err = s.configMaps.Update(ctx, updated, metav1.UpdateOptions{}); apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptChanged
	} else if err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	return claimed, nil
}

func (s *kubernetesSubnetOVNMutationReceiptStore) Delete(ctx context.Context, receipt SubnetOVNMutationReceipt) error {
	if err := receipt.valid(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	cm, found, err := s.lookupConfigMap(ctx, receipt.Key)
	if err != nil {
		return err
	}
	if !found {
		return ErrSubnetOVNReceiptChanged
	}
	current, err := unmarshalSubnetOVNMutationReceipt(cm.Data[subnetOVNReceiptDataKey])
	if err != nil {
		return err
	}
	if current != receipt {
		return ErrSubnetOVNReceiptChanged
	}
	preconditions := &metav1.Preconditions{UID: &cm.UID, ResourceVersion: &cm.ResourceVersion}
	err = s.configMaps.Delete(ctx, cm.Name, metav1.DeleteOptions{Preconditions: preconditions})
	if apierrors.IsNotFound(err) {
		return ErrSubnetOVNReceiptChanged
	}
	return err
}

func (s *kubernetesSubnetOVNMutationReceiptStore) List(ctx context.Context, subnetName string) ([]SubnetOVNMutationReceipt, error) {
	if subnetName == "" {
		return nil, fmt.Errorf("%w: missing Subnet name", ErrSubnetOVNReceiptInvalid)
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	list, err := s.configMaps.List(ctx, metav1.ListOptions{LabelSelector: labels.Set{subnetOVNReceiptLabel: "true"}.AsSelector().String()})
	if err != nil {
		return nil, err
	}
	receipts := make([]SubnetOVNMutationReceipt, 0, len(list.Items))
	for i := range list.Items {
		receipt, err := unmarshalSubnetOVNMutationReceipt(list.Items[i].Data[subnetOVNReceiptDataKey])
		if err != nil {
			return nil, err
		}
		if receipt.Key.Name == subnetName {
			receipts = append(receipts, receipt)
		}
	}
	return receipts, nil
}

func (s *kubernetesSubnetOVNMutationReceiptStore) lookup(ctx context.Context, key SubnetOVNMutationKey) (SubnetOVNMutationReceipt, bool, error) {
	cm, found, err := s.lookupConfigMap(ctx, key)
	if err != nil || !found {
		return SubnetOVNMutationReceipt{}, found, err
	}
	receipt, err := unmarshalSubnetOVNMutationReceipt(cm.Data[subnetOVNReceiptDataKey])
	if err != nil {
		return SubnetOVNMutationReceipt{}, false, err
	}
	if receipt.Key != key {
		return SubnetOVNMutationReceipt{}, false, ErrSubnetOVNReceiptInvalid
	}
	return receipt, true, nil
}

func (s *kubernetesSubnetOVNMutationReceiptStore) lookupConfigMap(ctx context.Context, key SubnetOVNMutationKey) (*corev1.ConfigMap, bool, error) {
	cm, err := s.configMaps.Get(ctx, subnetOVNReceiptConfigMapName(key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return cm, true, nil
}

func unmarshalSubnetOVNMutationReceipt(data string) (SubnetOVNMutationReceipt, error) {
	if data == "" {
		return SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptInvalid
	}
	var receipt SubnetOVNMutationReceipt
	if err := json.Unmarshal([]byte(data), &receipt); err != nil {
		return SubnetOVNMutationReceipt{}, fmt.Errorf("decode Subnet OVN receipt: %w", err)
	}
	if err := receipt.valid(); err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	return receipt, nil
}

func subnetOVNReceiptConfigMapName(key SubnetOVNMutationKey) string {
	hash := sha256.Sum256([]byte(string(key.UID) + "\x00" + key.Name + "\x00" + string(key.Operation) + "\x00" + key.IntentHash))
	return subnetOVNReceiptConfigMapPrefix + hex.EncodeToString(hash[:16])
}
