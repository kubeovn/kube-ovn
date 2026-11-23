package controllerruntime

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var (
	subnetShadowCacheReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_subnet_shadow_cache_ready",
			Help: "Whether the Subnet shadow dependency cache has synchronized.",
		}, []string{"resource"},
	)
	subnetShadowCacheSyncDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "kube_ovn_subnet_shadow_cache_sync_duration_seconds",
			Help: "Duration of Subnet shadow dependency cache synchronization in seconds.",
		},
	)
)

func init() {
	registerSubnetShadowMetric(subnetShadowCacheReady)
	registerSubnetShadowMetric(subnetShadowCacheSyncDuration)
}

type subnetShadowCacheReadiness struct {
	resources []string
	syncs     []cache.InformerSynced
	wait      func(<-chan struct{}, ...cache.InformerSynced) bool
}

func (r *subnetShadowCacheReadiness) Start(ctx context.Context) error {
	if r.wait == nil {
		return errors.New("subnet shadow cache readiness wait function is not configured")
	}
	started := time.Now()
	synced := r.wait(ctx.Done(), r.syncs...)
	subnetShadowCacheSyncDuration.Observe(time.Since(started).Seconds())
	if synced {
		for _, resource := range r.resources {
			subnetShadowCacheReady.WithLabelValues(resource).Set(1)
		}
		return nil
	}
	for index, resource := range r.resources {
		ready := 0.0
		if r.syncs[index]() {
			ready = 1
		}
		subnetShadowCacheReady.WithLabelValues(resource).Set(ready)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("subnet shadow dependency cache failed to synchronize")
}

func setupSubnetShadowCacheReadiness(mgr ctrl.Manager) error {
	if mgr == nil {
		return errors.New("controller-runtime manager is nil")
	}
	objects := []struct {
		name string
		obj  client.Object
	}{
		{name: "subnet", obj: &kubeovnv1.Subnet{}},
		{name: "vlan", obj: &kubeovnv1.Vlan{}},
		{name: "provider_network", obj: &kubeovnv1.ProviderNetwork{}},
		{name: "ip", obj: &kubeovnv1.IP{}},
	}
	readiness := &subnetShadowCacheReadiness{
		wait: cache.WaitForCacheSync,
	}
	for _, object := range objects {
		informer, err := mgr.GetCache().GetInformer(context.Background(), object.obj)
		if err != nil {
			return err
		}
		readiness.resources = append(readiness.resources, object.name)
		readiness.syncs = append(readiness.syncs, informer.HasSynced)
	}
	return mgr.Add(readiness)
}
