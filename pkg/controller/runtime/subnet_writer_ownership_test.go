package controllerruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubnetWriterOwnershipSerializesLegacyAndRuntime(t *testing.T) {
	ownership := &SubnetWriterOwnership{}
	legacy, err := ownership.Claim(t.Context(), SubnetWriterOwnerLegacy)
	require.NoError(t, err)
	_, err = ownership.Claim(t.Context(), SubnetWriterOwnerRuntime)
	require.ErrorIs(t, err, ErrSubnetWriterAlreadyClaimed)
	require.NoError(t, legacy.Release(t.Context()))

	runtime, err := ownership.Claim(t.Context(), SubnetWriterOwnerRuntime)
	require.NoError(t, err)
	require.NoError(t, runtime.Active(t.Context()))
	require.ErrorIs(t, legacy.Active(t.Context()), ErrSubnetWriterLeaseStale)
	require.NoError(t, runtime.Release(t.Context()))
}

func TestSubnetWriterOwnershipRejectsInvalidClaimsAndContexts(t *testing.T) {
	ownership := &SubnetWriterOwnership{}
	_, err := ownership.Claim(t.Context(), "unknown")
	require.ErrorIs(t, err, ErrSubnetWriterOwnerMissing)
	var nilContext context.Context
	_, err = ownership.Claim(nilContext, SubnetWriterOwnerLegacy)
	require.EqualError(t, err, "subnet writer ownership context is nil")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = ownership.Claim(canceled, SubnetWriterOwnerLegacy)
	require.ErrorIs(t, err, context.Canceled)

	var missing SubnetWriterLease
	require.ErrorIs(t, missing.Active(t.Context()), ErrSubnetWriterLeaseMissing)
	require.ErrorIs(t, missing.Release(t.Context()), ErrSubnetWriterLeaseMissing)
}

func TestSubnetWriterOwnershipRejectsWrongRelease(t *testing.T) {
	ownership := &SubnetWriterOwnership{}
	legacy, err := ownership.Claim(t.Context(), SubnetWriterOwnerLegacy)
	require.NoError(t, err)
	forged := SubnetWriterLease{ownership: ownership, owner: SubnetWriterOwnerRuntime, epoch: legacy.epoch}
	require.ErrorIs(t, forged.Release(t.Context()), ErrSubnetWriterLeaseStale)
	require.NoError(t, legacy.Release(t.Context()))
}

func TestSubnetWriterOwnershipBlocksReleaseDuringActiveOperation(t *testing.T) {
	ownership := &SubnetWriterOwnership{}
	lease, err := ownership.Claim(t.Context(), SubnetWriterOwnerRuntime)
	require.NoError(t, err)
	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- lease.WithActive(t.Context(), func(context.Context) error {
			close(started)
			<-finish
			return nil
		})
	}()
	<-started
	require.ErrorIs(t, lease.Release(t.Context()), ErrSubnetWriterLeaseBusy)
	close(finish)
	require.NoError(t, <-done)
	require.NoError(t, lease.Release(t.Context()))
}
