package kind

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestWaitLinkToDisappearReturnsTimeout(t *testing.T) {
	node := Node{container.Summary{Names: []string{"/kind-worker"}}}
	var err error
	require.NotPanics(t, func() {
		err = node.WaitLinkToDisappear("br-provider", time.Second, time.Now().Add(-time.Second))
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "br-provider")
	require.ErrorContains(t, err, "kind-worker")
}
