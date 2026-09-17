package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonitorWithTimeout(t *testing.T) {
	err := monitorWithTimeout(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, 10*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
