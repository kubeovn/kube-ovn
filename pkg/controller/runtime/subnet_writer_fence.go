package controllerruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"sync"
	"time"

	coordv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coordinationclient "k8s.io/client-go/kubernetes/typed/coordination/v1"
)

const subnetWriterFenceEpochAnnotation = "kube-ovn.kubernetes.io/subnet-writer-epoch"

var (
	ErrSubnetWriterFenceMissing  = errors.New("subnet writer durable fence is not configured")
	ErrSubnetWriterFenceBusy     = errors.New("subnet writer durable fence is held by another identity")
	ErrSubnetWriterFenceStale    = errors.New("subnet writer durable fence is stale")
	ErrSubnetWriterFenceInactive = errors.New("subnet writer durable fence is inactive")
)

// SubnetWriterFence is checked immediately before each writer operation. The
// implementation must read durable ownership from the API and fail closed
// after another process takes the lease or the lease expires.
type SubnetWriterFence interface {
	Check(context.Context) error
}

// KubernetesSubnetWriterFence owns a dedicated coordination.k8s.io Lease.
// It is independent from the outer kube-ovn-controller election so runtime
// writers can be fenced during a staged handoff.
type KubernetesSubnetWriterFence struct {
	leases   coordinationclient.LeaseInterface
	name     string
	identity string
	duration time.Duration
	now      func() time.Time
	epoch    string

	acquireMu sync.Mutex
	renewMu   sync.Mutex
	mu        sync.RWMutex
	active    bool
	stop      chan struct{}
	cancel    context.CancelFunc
	renewDone chan struct{}
	runID     uint64
}

func NewKubernetesSubnetWriterFence(leases coordinationclient.LeaseInterface, name, identity string, duration time.Duration) (*KubernetesSubnetWriterFence, error) {
	if leases == nil || name == "" || identity == "" || duration <= 0 {
		return nil, ErrSubnetWriterFenceMissing
	}
	epoch, err := newSubnetWriterFenceEpoch()
	if err != nil {
		return nil, err
	}
	return &KubernetesSubnetWriterFence{leases: leases, name: name, identity: identity, duration: duration, now: time.Now, epoch: epoch}, nil
}

func (f *KubernetesSubnetWriterFence) Acquire(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if f == nil || f.leases == nil || f.name == "" || f.identity == "" || f.duration <= 0 {
		return ErrSubnetWriterFenceMissing
	}

	// A stopped or failed run must use a new epoch. Reusing the same holder
	// identity would otherwise let an old process and a restart overlap.
	f.acquireMu.Lock()
	defer f.acquireMu.Unlock()
	f.mu.Lock()
	rotateEpoch := f.runID != 0 && !f.active
	if rotateEpoch {
		epoch, err := newSubnetWriterFenceEpoch()
		if err != nil {
			f.mu.Unlock()
			return err
		}
		f.epoch = epoch
	}
	epoch := f.epoch
	f.mu.Unlock()

	now := metav1.NewMicroTime(f.now())
	lease, err := f.leases.Get(ctx, f.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		seconds := leaseDurationSeconds(f.duration)
		identity := f.identity
		lease = &coordv1.Lease{Name: f.name, Annotations: map[string]string{subnetWriterFenceEpochAnnotation: epoch}, Spec: coordv1.LeaseSpec{
			HolderIdentity: &identity, LeaseDurationSeconds: &seconds, AcquireTime: &now, RenewTime: &now,
		}}
		if _, err = f.leases.Create(ctx, lease, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return ErrSubnetWriterFenceBusy
			}
			return err
		}
		f.markActive()
		return nil
	}
	if err != nil {
		return err
	}
	if !leaseExpired(lease, f.now()) &&
		(lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != "" && *lease.Spec.HolderIdentity != f.identity || leaseEpoch(lease) != epoch) {
		return ErrSubnetWriterFenceBusy
	}
	updated := lease.DeepCopy()
	identity := f.identity
	now = metav1.NewMicroTime(f.now())
	seconds := leaseDurationSeconds(f.duration)
	updated.Spec.HolderIdentity = &identity
	updated.Spec.LeaseDurationSeconds = &seconds
	updated.Spec.AcquireTime = &now
	updated.Spec.RenewTime = &now
	updated.Annotations = maps.Clone(updated.Annotations)
	if updated.Annotations == nil {
		updated.Annotations = make(map[string]string, 1)
	}
	updated.Annotations[subnetWriterFenceEpochAnnotation] = epoch
	if _, err := f.leases.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return ErrSubnetWriterFenceBusy
		}
		return err
	}
	f.markActive()
	return nil
}

func (f *KubernetesSubnetWriterFence) Start(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := f.Acquire(ctx); err != nil {
		return err
	}
	f.mu.Lock()
	if f.stop != nil {
		f.mu.Unlock()
		return nil
	}
	if !f.active {
		f.mu.Unlock()
		return ErrSubnetWriterFenceInactive
	}
	stop := make(chan struct{})
	renewDone := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	f.runID++
	runID := f.runID
	f.stop = stop
	f.cancel = cancel
	f.renewDone = renewDone
	f.mu.Unlock()
	interval := f.duration / 3
	if interval < time.Second {
		interval = time.Second
	}
	go f.renewLoop(runCtx, stop, runID, interval, renewDone)
	return nil
}

// Stop deliberately leaves the Lease in place. The holder remains durable
// until expiry, preventing a restarted writer from silently overlapping it.
func (f *KubernetesSubnetWriterFence) Stop() {
	if f == nil {
		return
	}
	f.mu.Lock()
	stop := f.stop
	cancel := f.cancel
	renewDone := f.renewDone
	if stop != nil {
		close(stop)
		f.stop = nil
	}
	f.cancel = nil
	f.renewDone = nil
	f.runID++
	f.active = false
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Wait for an in-flight renewal to observe cancellation before returning;
	// otherwise a stopped run could extend the durable lease after Stop.
	if renewDone != nil {
		<-renewDone
	}
}

func (f *KubernetesSubnetWriterFence) Check(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if f == nil || f.leases == nil || f.name == "" || f.identity == "" {
		return ErrSubnetWriterFenceMissing
	}
	f.mu.RLock()
	active := f.active
	epoch := f.epoch
	f.mu.RUnlock()
	if epoch == "" {
		return ErrSubnetWriterFenceMissing
	}
	if !active {
		return ErrSubnetWriterFenceInactive
	}
	lease, err := f.leases.Get(ctx, f.name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	f.mu.RLock()
	active = f.active
	currentEpoch := f.epoch
	f.mu.RUnlock()
	if !active {
		return ErrSubnetWriterFenceInactive
	}
	// The run may have been stopped and reacquired while the API read was in
	// flight. Compare against the current epoch after the read so an old
	// response cannot authorize the new run.
	if currentEpoch != epoch {
		return ErrSubnetWriterFenceStale
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != f.identity || leaseEpoch(lease) != currentEpoch {
		return ErrSubnetWriterFenceStale
	}
	if leaseExpired(lease, f.now()) {
		return ErrSubnetWriterFenceStale
	}
	return nil
}

func (f *KubernetesSubnetWriterFence) renewLoop(ctx context.Context, stop <-chan struct{}, runID uint64, interval time.Duration, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			f.deactivate(runID)
			return
		case <-stop:
			return
		case <-ticker.C:
			if err := f.renew(ctx, runID); err != nil {
				f.deactivate(runID)
				return
			}
		}
	}
}

func (f *KubernetesSubnetWriterFence) deactivate(runID uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runID == runID {
		f.active = false
		f.stop = nil
		f.cancel = nil
		f.renewDone = nil
	}
}

func (f *KubernetesSubnetWriterFence) renew(ctx context.Context, runID uint64) error {
	if err := f.checkRun(runID); err != nil {
		return err
	}
	f.renewMu.Lock()
	defer f.renewMu.Unlock()
	if err := f.checkRun(runID); err != nil {
		return err
	}
	f.mu.RLock()
	epoch := f.epoch
	f.mu.RUnlock()
	lease, err := f.leases.Get(ctx, f.name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != f.identity || leaseEpoch(lease) != epoch || leaseExpired(lease, f.now()) {
		return ErrSubnetWriterFenceStale
	}
	if err := f.checkRun(runID); err != nil {
		return err
	}
	updated := lease.DeepCopy()
	now := metav1.NewMicroTime(f.now())
	updated.Spec.RenewTime = &now
	_, err = f.leases.Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

func (f *KubernetesSubnetWriterFence) checkRun(runID uint64) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if !f.active || f.runID != runID {
		return ErrSubnetWriterFenceInactive
	}
	return nil
}

func (f *KubernetesSubnetWriterFence) markActive() {
	f.mu.Lock()
	f.active = true
	f.mu.Unlock()
}

func leaseDurationSeconds(duration time.Duration) int32 {
	seconds64 := int64(duration / time.Second)
	if seconds64 > 1<<31-1 {
		return 1<<31 - 1
	}
	seconds := int32(seconds64) //nolint:gosec // seconds64 is clamped to the int32 range above.
	if seconds < 1 {
		return 1
	}
	return seconds
}

func leaseExpired(lease *coordv1.Lease, now time.Time) bool {
	if lease == nil || lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return true
	}
	return !now.Before(lease.Spec.RenewTime.Add(time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second))
}

func leaseEpoch(lease *coordv1.Lease) string {
	if lease == nil || lease.Annotations == nil {
		return ""
	}
	return lease.Annotations[subnetWriterFenceEpochAnnotation]
}

func newSubnetWriterFenceEpoch() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
