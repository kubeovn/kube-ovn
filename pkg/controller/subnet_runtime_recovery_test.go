package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubnetRuntimeRecoveryWaitsForStartup(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	controller.beginSubnetRuntimeRecovery()
	result := make(chan error, 1)
	go func() { result <- controller.WaitForSubnetRuntimeRecovery(context.Background()) }()

	select {
	case err := <-result:
		t.Fatalf("recovery gate opened before startup completed: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	controller.finishSubnetRuntimeRecovery()
	require.NoError(t, <-result)
	require.NoError(t, controller.WaitForSubnetRuntimeRecovery(t.Context()))
}

func TestSubnetRuntimeRecoveryHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	controller.beginSubnetRuntimeRecovery()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, controller.WaitForSubnetRuntimeRecovery(ctx), context.Canceled)
}

func TestSubnetRuntimeRecoveryFinishIsIdempotent(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	controller.beginSubnetRuntimeRecovery()
	controller.finishSubnetRuntimeRecovery()
	controller.finishSubnetRuntimeRecovery()
	require.NoError(t, controller.WaitForSubnetRuntimeRecovery(t.Context()))
}
