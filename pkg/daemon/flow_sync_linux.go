package daemon

import (
	"time"

	openvswitch "github.com/digitalocean/go-openvswitch/ovs"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const flowSyncPeriod = 15 * time.Second

var managedFlowCookieSet = sets.New[uint64](
	util.UnderlaySvcLocalOpenFlowCookieV4,
	util.UnderlaySvcLocalOpenFlowCookieV6,
)

func (c *Controller) requestFlowSync() {
	if c.flowChan == nil {
		util.LogFatalAndExit(nil, "flowChan is not initialized")
	}

	select {
	case c.flowChan <- struct{}{}:
		klog.V(5).Info("OpenFlow sync requested")
	default:
		klog.V(5).Info("OpenFlow sync already requested")
	}
}

func (c *Controller) syncFlows() {
	if c.ovsClient == nil {
		util.LogFatalAndExit(nil, "ovsClient is not initialized")
	}

	flowCacheByBridge := c.storeFlowCache()

	bridges, err := ovs.Bridges()
	if err != nil {
		klog.Errorf("failed to list bridges: %v", err)
		return
	}

	for _, bridgeName := range bridges {
		existing, err := ovs.DumpFlows(c.ovsClient, bridgeName)
		if err != nil {
			klog.Errorf("failed to dump flows for bridge %s: %v", bridgeName, err)
			continue
		}

		preserved := filterUnmanagedFlows(existing)
		cachedFlows := flowCacheByBridge[bridgeName]
		finalFlows := append(preserved, cachedFlows...)

		if err := ovs.ReplaceFlows(bridgeName, finalFlows); err != nil {
			klog.Errorf("failed to replace flows for bridge %s: %v", bridgeName, err)
			continue
		}
		if len(cachedFlows) == 0 {
			klog.V(5).Infof("no cached flows for bridge %s", bridgeName)
			continue
		}
		klog.V(3).Infof("synced %d cached flows on bridge %s", len(cachedFlows), bridgeName)
	}
}

func (c *Controller) storeFlowCache() map[string][]string {
	snapshot := make(map[string][]string)

	c.flowCacheMutex.RLock()
	defer c.flowCacheMutex.RUnlock()

	for bridgeName, entries := range c.flowCache {
		for _, flows := range entries {
			snapshot[bridgeName] = append(snapshot[bridgeName], flows...)
		}
	}

	return snapshot
}

func filterUnmanagedFlows(flows []string) []string {
	filtered := make([]string, 0, len(flows))
	for _, flow := range flows {
		if isManagedFlow(flow) {
			continue
		}
		filtered = append(filtered, flow)
	}
	return filtered
}

func isManagedFlow(flow string) bool {
	var f openvswitch.Flow
	if err := f.UnmarshalText([]byte(flow)); err != nil {
		return false
	}
	return managedFlowCookieSet.Has(f.Cookie)
}

func (c *Controller) runFlowSync(stopCh <-chan struct{}) {
	klog.Info("Starting OpenFlow sync loop")

	ticker := time.NewTicker(flowSyncPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.syncFlows()
		case <-c.flowChan:
			klog.V(5).Info("Immediate OpenFlow sync triggered")
			c.syncFlows()
			ticker.Reset(flowSyncPeriod)
		case <-stopCh:
			klog.Info("Stopping OpenFlow sync loop")
			return
		}
	}
}

func (c *Controller) setFlowCache(cache map[string]map[string][]string, bridgeName, key string, flows []string) {
	c.flowCacheMutex.Lock()
	defer c.flowCacheMutex.Unlock()

	if cache[bridgeName] == nil {
		cache[bridgeName] = make(map[string][]string)
	}
	cache[bridgeName][key] = flows
}

func (c *Controller) deleteFlowCache(cache map[string]map[string][]string, bridgeName, key string) {
	c.flowCacheMutex.Lock()
	defer c.flowCacheMutex.Unlock()
	delete(cache[bridgeName], key)
}
