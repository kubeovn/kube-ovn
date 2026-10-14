package controller

import (
	"maps"
	"strings"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/gcexempt"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// gcExemptionSnapshot is an immutable, fully-parsed view of the GC exemption
// ConfigMap. It is swapped atomically so that markAndCleanLSP (on the GC
// ticker goroutine) and gcChassis (on both the GC ticker and the reactive
// UpdateChassisTag path) always observe one consistent pair of exempt sets.
type gcExemptionSnapshot struct {
	// raw is the last-seen ConfigMap data, used only to detect changes
	// between ticks without reparsing. nil means the ConfigMap was absent.
	raw     map[string]string
	lsps    *strset.Set
	chassis *strset.Set
}

func emptyGCExemptionSnapshot() *gcExemptionSnapshot {
	return &gcExemptionSnapshot{lsps: strset.New(), chassis: strset.New()}
}

// parseGCExemptionNames splits a comma-separated list of names, trimming
// whitespace and dropping empty entries. Duplicates collapse naturally once
// added to a strset.Set.
func parseGCExemptionNames(value string) *strset.Set {
	set := strset.New()
	for name := range strings.SplitSeq(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			set.Add(name)
		}
	}
	return set
}

// resyncGCExemptions refreshes the controller's cached GC exemption lists
// from the ovn-gc-exemption-config ConfigMap in the controller's own
// namespace. It is called once per GC tick so markAndCleanLSP and gcChassis
// share a consistent snapshot for that tick.
//
// It fails open: a missing ConfigMap means no exemptions, and any other
// read error leaves the previously cached snapshot untouched rather than
// aborting the GC cycle or wiping out exemptions due to a transient error.
func (c *Controller) resyncGCExemptions() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.GCExemptionConfig)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get %s, %v", util.GCExemptionConfig, err)
			return
		}
		if prev := c.gcExemptions.Load(); prev == nil || prev.raw != nil {
			c.gcExemptions.Store(emptyGCExemptionSnapshot())
		}
		return
	}

	if prev := c.gcExemptions.Load(); prev != nil && maps.Equal(cm.Data, prev.raw) {
		return
	}

	c.gcExemptions.Store(&gcExemptionSnapshot{
		raw:     maps.Clone(cm.Data),
		lsps:    parseGCExemptionNames(cm.Data[gcexempt.LSPKey]),
		chassis: parseGCExemptionNames(cm.Data[gcexempt.ChassisKey]),
	})
}

// isLSPExempt reports whether the named logical switch port is currently
// exempt from GC. A nil snapshot (before the first tick has run) means no
// exemptions, never "everything exempt".
func (c *Controller) isLSPExempt(name string) bool {
	snap := c.gcExemptions.Load()
	return snap != nil && snap.lsps.Has(name)
}

// isChassisExempt reports whether the named chassis is currently exempt
// from GC.
func (c *Controller) isChassisExempt(name string) bool {
	snap := c.gcExemptions.Load()
	return snap != nil && snap.chassis.Has(name)
}
