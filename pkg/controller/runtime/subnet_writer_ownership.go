package controllerruntime

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrSubnetWriterOwnerMissing   = errors.New("subnet writer owner is not configured")
	ErrSubnetWriterAlreadyClaimed = errors.New("subnet writer ownership is already claimed")
	ErrSubnetWriterLeaseMissing   = errors.New("subnet writer ownership lease is not configured")
	ErrSubnetWriterLeaseStale     = errors.New("subnet writer ownership lease is stale")
	ErrSubnetWriterLeaseBusy      = errors.New("subnet writer ownership lease is active")
)

// SubnetWriterOwner identifies the only two ownership domains allowed during
// the migration. The legacy controller remains the production owner until a
// runtime writer claims the lease explicitly.
type SubnetWriterOwner string

const (
	SubnetWriterOwnerLegacy  SubnetWriterOwner = "legacy"
	SubnetWriterOwnerRuntime SubnetWriterOwner = "runtime"
)

// SubnetWriterOwnership serializes the legacy/runtime handoff. It is a
// process-local contract; durable leader fencing remains required before a
// runtime writer is enabled in production.
type SubnetWriterOwnership struct {
	mu     sync.Mutex
	owner  SubnetWriterOwner
	epoch  uint64
	active uint64
}

// SubnetWriterLease is an epoch-bound ownership token. A released token can
// never be reused after another owner claims the lease.
type SubnetWriterLease struct {
	ownership *SubnetWriterOwnership
	owner     SubnetWriterOwner
	epoch     uint64
}

func (o *SubnetWriterOwnership) Claim(ctx context.Context, owner SubnetWriterOwner) (SubnetWriterLease, error) {
	if err := contextError(ctx); err != nil {
		return SubnetWriterLease{}, err
	}
	if owner != SubnetWriterOwnerLegacy && owner != SubnetWriterOwnerRuntime {
		return SubnetWriterLease{}, ErrSubnetWriterOwnerMissing
	}
	if o == nil {
		return SubnetWriterLease{}, ErrSubnetWriterLeaseMissing
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.owner != "" {
		return SubnetWriterLease{}, ErrSubnetWriterAlreadyClaimed
	}
	o.epoch++
	o.owner = owner
	return SubnetWriterLease{ownership: o, owner: owner, epoch: o.epoch}, nil
}

func (l SubnetWriterLease) Active(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if l.ownership == nil {
		return ErrSubnetWriterLeaseMissing
	}
	l.ownership.mu.Lock()
	defer l.ownership.mu.Unlock()
	if l.ownership.owner != l.owner || l.ownership.epoch != l.epoch {
		return ErrSubnetWriterLeaseStale
	}
	return nil
}

// WithActive keeps the lease held for the complete operation. Ownership
// release is rejected while the callback is running, so a handoff cannot
// leave an old writer executing side effects after a new owner claims it.
func (l SubnetWriterLease) WithActive(ctx context.Context, fn func(context.Context) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("subnet writer ownership operation is nil")
	}
	if l.ownership == nil {
		return ErrSubnetWriterLeaseMissing
	}
	l.ownership.mu.Lock()
	if l.ownership.owner != l.owner || l.ownership.epoch != l.epoch {
		l.ownership.mu.Unlock()
		return ErrSubnetWriterLeaseStale
	}
	l.ownership.active++
	l.ownership.mu.Unlock()
	defer func() {
		l.ownership.mu.Lock()
		l.ownership.active--
		l.ownership.mu.Unlock()
	}()
	return fn(ctx)
}

func (l SubnetWriterLease) Release(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if l.ownership == nil {
		return ErrSubnetWriterLeaseMissing
	}
	l.ownership.mu.Lock()
	defer l.ownership.mu.Unlock()
	if l.ownership.owner != l.owner || l.ownership.epoch != l.epoch {
		return ErrSubnetWriterLeaseStale
	}
	if l.ownership.active != 0 {
		return ErrSubnetWriterLeaseBusy
	}
	l.ownership.owner = ""
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("subnet writer ownership context is nil")
	}
	return ctx.Err()
}
