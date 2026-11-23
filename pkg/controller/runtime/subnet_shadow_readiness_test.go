package controllerruntime

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/cache"
)

func TestSubnetShadowCacheReadinessRecordsSynchronizedResources(t *testing.T) {
	resources := []string{"subnet", "vlan", "provider_network", "ip"}
	readiness := &subnetShadowCacheReadiness{
		resources: resources,
		syncs: []cache.InformerSynced{
			func() bool { return true },
			func() bool { return true },
			func() bool { return true },
			func() bool { return true },
		},
		wait: func(<-chan struct{}, ...cache.InformerSynced) bool { return true },
	}

	require.NoError(t, readiness.Start(t.Context()))
	for _, resource := range resources {
		require.Equal(t, float64(1), testutil.ToFloat64(subnetShadowCacheReady.WithLabelValues(resource)))
	}
}

func TestSubnetShadowCacheReadinessFailsClosedOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	readiness := &subnetShadowCacheReadiness{
		resources: []string{"subnet"},
		syncs:     []cache.InformerSynced{func() bool { return false }},
		wait:      func(<-chan struct{}, ...cache.InformerSynced) bool { return false },
	}

	require.ErrorIs(t, readiness.Start(ctx), context.Canceled)
	require.Equal(t, float64(0), testutil.ToFloat64(subnetShadowCacheReady.WithLabelValues("subnet")))
}

func TestSubnetShadowCacheReadinessReportsSyncFailure(t *testing.T) {
	readiness := &subnetShadowCacheReadiness{
		resources: []string{"subnet"},
		syncs:     []cache.InformerSynced{func() bool { return false }},
		wait:      func(<-chan struct{}, ...cache.InformerSynced) bool { return false },
	}

	require.EqualError(t, readiness.Start(t.Context()), "subnet shadow dependency cache failed to synchronize")
	require.Equal(t, float64(0), testutil.ToFloat64(subnetShadowCacheReady.WithLabelValues("subnet")))
}

func TestSubnetShadowCacheReadinessRequiresWaitFunction(t *testing.T) {
	readiness := &subnetShadowCacheReadiness{resources: []string{"subnet"}}
	require.EqualError(t, readiness.Start(t.Context()), "subnet shadow cache readiness wait function is not configured")
}
