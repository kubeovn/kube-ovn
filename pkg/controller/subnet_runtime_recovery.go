package controller

import (
	"context"
	"errors"
)

// ErrSubnetRuntimeAllocationNotAllowed reports a dependency readiness gate
// that must be retried before the active Subnet writer can allocate or mutate
// data-plane state.
var ErrSubnetRuntimeAllocationNotAllowed = errors.New("subnet runtime allocation is not allowed")

// beginSubnetRuntimeRecovery closes the Subnet writer gate until the shared
// IPAM and OVN prerequisites have been restored by controller startup.
func (c *Controller) beginSubnetRuntimeRecovery() {
	if c.subnetRuntimeRecoveryReady == nil {
		c.subnetRuntimeRecoveryReady = make(chan struct{})
	}
}

// finishSubnetRuntimeRecovery publishes the recovered state to the runtime
// writer. It is safe to call more than once from startup and test handoffs.
func (c *Controller) finishSubnetRuntimeRecovery() {
	if c.subnetRuntimeRecoveryReady == nil {
		return
	}
	c.subnetRuntimeRecoveryOnce.Do(func() { close(c.subnetRuntimeRecoveryReady) })
}

func (c *Controller) waitForSubnetRuntimeRecovery(ctx context.Context) error {
	if c.subnetRuntimeRecoveryReady == nil {
		return nil
	}
	select {
	case <-c.subnetRuntimeRecoveryReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitForSubnetRuntimeRecovery exposes the startup barrier to the runtime
// reconciler and keeps standalone legacy controller tests ungated.
func (c *Controller) WaitForSubnetRuntimeRecovery(ctx context.Context) error {
	return c.waitForSubnetRuntimeRecovery(ctx)
}
