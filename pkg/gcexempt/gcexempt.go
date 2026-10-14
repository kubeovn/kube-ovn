// Package gcexempt defines the names shared between the kube-ovn-controller
// GC exemption logic and the kubectl-ko CLI that edits it. It has no
// dependencies beyond the standard library so that kubectl-ko, which avoids
// importing pkg/util's heavier dependency set, can still share a single
// source of truth for these names instead of duplicating them.
package gcexempt

const (
	// ConfigMapName is the ConfigMap holding the GC exemption lists, read
	// from the kube-ovn-controller's own namespace.
	ConfigMapName = "ovn-gc-exemption-config"
	// LSPKey is the ConfigMap data key holding a comma-separated list of
	// logical switch port names exempted from GC.
	LSPKey = "exempt-lsps"
	// ChassisKey is the ConfigMap data key holding a comma-separated list
	// of chassis names exempted from GC.
	ChassisKey = "exempt-chassis"
)
