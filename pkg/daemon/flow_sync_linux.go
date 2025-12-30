package daemon

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const flowSyncPeriod = 15 * time.Second

var managedFlowCookieSet = map[uint64]struct{}{
	util.UnderlaySvcLocalOpenFlowCookieV4: {},
	util.UnderlaySvcLocalOpenFlowCookieV6: {},
}

func (c *Controller) requestFlowSync() {
	if c.flowChan == nil {
		return
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
		return
	}

	flowsByBridge := c.storeFlowCache()

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
		managed := flowsByBridge[bridgeName]
		merged := append(preserved, managed...)

		if err := ovs.ReplaceFlows(bridgeName, merged); err != nil {
			klog.Errorf("failed to replace flows for bridge %s: %v", bridgeName, err)
			continue
		}
		if len(managed) == 0 {
			klog.V(5).Infof("no managed flows for bridge %s", bridgeName)
			continue
		}
		klog.V(3).Infof("synced %d managed flows on bridge %s", len(managed), bridgeName)
	}
}

func (c *Controller) storeFlowCache() map[string][]string {
	snapshot := make(map[string][]string)

	c.flowCacheMutex.RLock()
	defer c.flowCacheMutex.RUnlock()

	appendFlowCache(snapshot, c.flowCache)

	return snapshot
}

func appendFlowCache(dst map[string][]string, src map[string]map[string][]string) {
	for bridgeName, entries := range src {
		if len(entries) == 0 {
			if _, ok := dst[bridgeName]; !ok {
				dst[bridgeName] = nil
			}
			continue
		}
		for _, flows := range entries {
			if len(flows) == 0 {
				if _, ok := dst[bridgeName]; !ok {
					dst[bridgeName] = nil
				}
				continue
			}
			dst[bridgeName] = append(dst[bridgeName], flows...)
		}
	}
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
	cookie, ok := extractFlowCookie(flow)
	if !ok {
		return false
	}
	_, exists := managedFlowCookieSet[cookie]
	return exists
}

func extractFlowCookie(flow string) (uint64, bool) {
	idx := strings.Index(flow, "cookie=")
	if idx == -1 {
		return 0, false
	}
	cookieField := flow[idx+len("cookie="):]
	if comma := strings.Index(cookieField, ","); comma != -1 {
		cookieField = cookieField[:comma]
	}
	if slash := strings.Index(cookieField, "/"); slash != -1 {
		cookieField = cookieField[:slash]
	}
	cookieField = strings.TrimSpace(cookieField)
	if cookieField == "" {
		return 0, false
	}

	cookie, err := parseHexUint64(cookieField)
	if err != nil {
		return 0, false
	}
	return cookie, true
}

func parseHexUint64(value string) (uint64, error) {
	if !strings.HasPrefix(value, "0x") {
		value = "0x" + value
	}
	return strconv.ParseUint(value, 0, 64)
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

	entries := cache[bridgeName]
	if entries == nil {
		return
	}
	delete(entries, key)
}
