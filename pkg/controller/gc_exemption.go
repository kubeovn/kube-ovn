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

// chassisGCExemptionSnapshot is an immutable, parsed view of the chassis GC
// exemption ConfigMap. It is swapped atomically so that gcChassis (reachable
// from both the periodic GC ticker and the reactive UpdateChassisTag call)
// always observes a consistent exempt set.
//
// This exemption is scoped to chassis only: it exists to stop GC from
// deleting an externally-managed (non-k8s) gateway chassis, e.g. a hybrid
// cluster where a non-k8s node's chassis would otherwise be removed every
// time the controller restarts. LSP GC does not need an equivalent
// exemption list: the vendor filter already applied to LSP/LS/LR GC (see
// gcLogicalSwitch and friends, gated by --enable-external-vpc) keeps that
// GC off non-Kube-OVN-owned objects, so a second, independent exemption
// mechanism for LSPs would just be unused complexity.
type chassisGCExemptionSnapshot struct {
	// raw is the last-seen ConfigMap data, used only to detect changes
	// between ticks without reparsing. nil means the ConfigMap was absent.
	raw     map[string]string
	chassis *strset.Set
}

func emptyChassisGCExemptionSnapshot() *chassisGCExemptionSnapshot {
	return &chassisGCExemptionSnapshot{chassis: strset.New()}
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

// resyncChassisGCExemptions refreshes the controller's cached chassis GC
// exemption list from the ovn-gc-exemption-config ConfigMap in the
// controller's own namespace. It is called once per GC tick, right before
// gcChassis, so a chassis removed from the exemption list via `kubectl-ko
// gc-exemption --remove-chassis-exemption` is picked up on the very next
// tick without requiring a controller restart.
//
// It fails open: a missing ConfigMap means no exemptions, and any other
// read error leaves the previously cached snapshot untouched rather than
// aborting the GC cycle or wiping out exemptions due to a transient error.
func (c *Controller) resyncChassisGCExemptions() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.GCExemptionConfig)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get %s, %v", util.GCExemptionConfig, err)
			return
		}
		if prev := c.chassisGCExemptions.Load(); prev == nil || prev.raw != nil {
			c.chassisGCExemptions.Store(emptyChassisGCExemptionSnapshot())
		}
		return
	}

	if prev := c.chassisGCExemptions.Load(); prev != nil && maps.Equal(cm.Data, prev.raw) {
		return
	}

	c.chassisGCExemptions.Store(&chassisGCExemptionSnapshot{
		raw:     maps.Clone(cm.Data),
		chassis: parseGCExemptionNames(cm.Data[gcexempt.ChassisKey]),
	})
}

// isChassisExempt reports whether the named chassis is currently exempt
// from GC. A nil snapshot (before the first tick has run) means no
// exemptions, never "everything exempt".
func (c *Controller) isChassisExempt(name string) bool {
	snap := c.chassisGCExemptions.Load()
	return snap != nil && snap.chassis.Has(name)
}
