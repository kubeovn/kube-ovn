package controller

import (
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// coalescingDeltaQueue keeps one queued item per policy key while preserving
// the complete set of changes observed before the worker takes the item.
type coalescingDeltaQueue[T comparable] struct {
	queue    workqueue.TypedRateLimitingInterface[string]
	key      func(T) string
	merge    func(T, T)
	pending  map[string]T
	inFlight map[T]string
	mu       sync.Mutex
}

func newCoalescingDeltaQueue[T comparable](name string, key func(T) string, merge func(T, T)) workqueue.TypedRateLimitingInterface[T] {
	return &coalescingDeltaQueue[T]{
		queue:    newTypedRateLimitingQueue[string](name, nil),
		key:      key,
		merge:    merge,
		pending:  make(map[string]T),
		inFlight: make(map[T]string),
	}
}

func (q *coalescingDeltaQueue[T]) Add(item T) {
	key := q.key(item)
	q.mu.Lock()
	if current, ok := q.pending[key]; ok {
		q.merge(current, item)
	} else {
		q.pending[key] = item
	}
	q.mu.Unlock()
	q.queue.Add(key)
}

func (q *coalescingDeltaQueue[T]) AddAfter(item T, duration time.Duration) {
	key := q.key(item)
	q.mu.Lock()
	if current, ok := q.pending[key]; ok {
		q.merge(current, item)
	} else {
		q.pending[key] = item
	}
	q.mu.Unlock()
	q.queue.AddAfter(key, duration)
}

func (q *coalescingDeltaQueue[T]) AddRateLimited(item T) {
	key := q.key(item)
	q.mu.Lock()
	if current, ok := q.pending[key]; ok {
		q.merge(current, item)
	} else {
		q.pending[key] = item
	}
	q.mu.Unlock()
	q.queue.AddRateLimited(key)
}

func (q *coalescingDeltaQueue[T]) Done(item T) {
	q.mu.Lock()
	key, ok := q.inFlight[item]
	delete(q.inFlight, item)
	q.mu.Unlock()
	if !ok {
		key = q.key(item)
	}
	q.queue.Done(key)
}

func (q *coalescingDeltaQueue[T]) Forget(item T) {
	q.mu.Lock()
	key, ok := q.inFlight[item]
	q.mu.Unlock()
	if !ok {
		key = q.key(item)
	}
	q.queue.Forget(key)
}

func (q *coalescingDeltaQueue[T]) Get() (item T, shutdown bool) {
	key, shutdown := q.queue.Get()
	if shutdown {
		return item, true
	}

	q.mu.Lock()
	item = q.pending[key]
	delete(q.pending, key)
	q.inFlight[item] = key
	q.mu.Unlock()
	return item, false
}

func (q *coalescingDeltaQueue[T]) Len() int {
	return q.queue.Len()
}

func (q *coalescingDeltaQueue[T]) NumRequeues(item T) int {
	q.mu.Lock()
	key, ok := q.inFlight[item]
	q.mu.Unlock()
	if !ok {
		key = q.key(item)
	}
	return q.queue.NumRequeues(key)
}

func (q *coalescingDeltaQueue[T]) ShutDown() {
	q.queue.ShutDown()
}

func (q *coalescingDeltaQueue[T]) ShutDownWithDrain() {
	q.queue.ShutDownWithDrain()
}

func (q *coalescingDeltaQueue[T]) ShuttingDown() bool {
	return q.queue.ShuttingDown()
}

type changedFieldMask uint8

const (
	changedFieldSubject changedFieldMask = 1 << iota
	changedFieldIngressRule
	changedFieldEgressRule
)

func fieldMask(field ChangedField) changedFieldMask {
	switch field {
	case ChangedSubject:
		return changedFieldSubject
	case ChangedIngressRule:
		return changedFieldIngressRule
	case ChangedEgressRule:
		return changedFieldEgressRule
	default:
		return 0
	}
}

func (d *AdminNetworkPolicyChangedDelta) fieldsMask() changedFieldMask {
	if d.fields != 0 {
		return d.fields
	}
	return fieldMask(d.field)
}

func (d *AdminNetworkPolicyChangedDelta) normalizeRuleNames() {
	switch d.field {
	case ChangedIngressRule:
		if isZeroChangedNames(d.ingressRuleNames[:]) {
			d.ingressRuleNames = d.ruleNames
		}
	case ChangedEgressRule:
		if isZeroChangedNames(d.egressRuleNames[:]) {
			d.egressRuleNames = d.ruleNames
		}
	}
}

func (d *AdminNetworkPolicyChangedDelta) ruleNamesFor(field ChangedField) *[util.AnpMaxRules]ChangedName {
	d.normalizeRuleNames()
	switch field {
	case ChangedIngressRule:
		return &d.ingressRuleNames
	case ChangedEgressRule:
		return &d.egressRuleNames
	default:
		return &d.ruleNames
	}
}

func (d *ClusterNetworkPolicyChangedDelta) fieldsMask() changedFieldMask {
	if d.fields != 0 {
		return d.fields
	}
	return fieldMask(d.field)
}

func (d *ClusterNetworkPolicyChangedDelta) normalizeRuleNames() {
	switch d.field {
	case ChangedIngressRule:
		if isZeroChangedNames(d.ingressRuleNames[:]) {
			d.ingressRuleNames = d.ruleNames
		}
	case ChangedEgressRule:
		if isZeroChangedNames(d.egressRuleNames[:]) {
			d.egressRuleNames = d.ruleNames
		}
	}
}

func (d *ClusterNetworkPolicyChangedDelta) ruleNamesFor(field ChangedField) *[util.CnpMaxRules]ChangedName {
	d.normalizeRuleNames()
	switch field {
	case ChangedIngressRule:
		return &d.ingressRuleNames
	case ChangedEgressRule:
		return &d.egressRuleNames
	default:
		return &d.ruleNames
	}
}

func isZeroChangedNames(names []ChangedName) bool {
	for _, name := range names {
		if name.isMatch || name.curRuleName != "" {
			return false
		}
	}
	return true
}

func mergeDeltaChangedNames(dst, src []ChangedName) {
	for index := range src {
		if src[index].curRuleName != "" {
			dst[index].curRuleName = src[index].curRuleName
		}
		dst[index].isMatch = dst[index].isMatch || src[index].isMatch
	}
}

func mergeAdminNetworkPolicyDelta(dst, src *AdminNetworkPolicyChangedDelta) {
	dst.normalizeRuleNames()
	src.normalizeRuleNames()
	dst.fields = dst.fieldsMask() | src.fieldsMask()
	dst.field = ""
	mergeDeltaChangedNames(dst.ingressRuleNames[:], src.ingressRuleNames[:])
	mergeDeltaChangedNames(dst.egressRuleNames[:], src.egressRuleNames[:])
	dst.DNSReconcileDone = dst.DNSReconcileDone && src.DNSReconcileDone
}

func mergeClusterNetworkPolicyDelta(dst, src *ClusterNetworkPolicyChangedDelta) {
	dst.normalizeRuleNames()
	src.normalizeRuleNames()
	dst.fields = dst.fieldsMask() | src.fieldsMask()
	dst.field = ""
	mergeDeltaChangedNames(dst.ingressRuleNames[:], src.ingressRuleNames[:])
	mergeDeltaChangedNames(dst.egressRuleNames[:], src.egressRuleNames[:])
	dst.DNSReconcileDone = dst.DNSReconcileDone && src.DNSReconcileDone
}
