package ko

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/spf13/cobra"
)

type sourceKind string

const (
	sourceKindPod  sourceKind = "pod"
	sourceKindNode sourceKind = "node"
	sourceKindVM   sourceKind = "vm"
	sourceKindVMI  sourceKind = "vmi"
)

var sourceKindAliases = map[string]sourceKind{
	"pod":  sourceKindPod,
	"po":   sourceKindPod,
	"node": sourceKindNode,
	"vm":   sourceKindVM,
	"vmi":  sourceKindVMI,
}

// sourceRef is the common command-level source notation. Resolution into a
// Pod, node, or guest launcher remains in the existing resource adapters.
type sourceRef struct {
	kind      sourceKind
	namespace string
	name      string
}

func parseSourceRef(value string) (sourceRef, error) {
	if value == "" {
		return sourceRef{}, errors.New("source reference must not be empty")
	}
	if after, ok := strings.CutPrefix(value, "node//"); ok {
		value = after
		return sourceRef{kind: sourceKindNode, name: value}, validateSourceRef(sourceRef{kind: sourceKindNode, name: value})
	}
	parts := strings.Split(value, "/")
	ref := sourceRef{kind: sourceKindPod}
	switch len(parts) {
	case 1:
		ref.name = parts[0]
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return sourceRef{}, fmt.Errorf("source reference %q contains an empty namespace or name", value)
		}
		if kind, ok := sourceKindAliases[strings.ToLower(parts[0])]; ok {
			ref.kind, ref.name = kind, parts[1]
		} else {
			ref.namespace, ref.name = parts[0], parts[1]
		}
	case 3:
		if parts[1] == "" || parts[2] == "" {
			return sourceRef{}, fmt.Errorf("source reference %q contains an empty namespace or name", value)
		}
		kind, ok := sourceKindAliases[strings.ToLower(parts[0])]
		if !ok || kind == sourceKindNode {
			return sourceRef{}, fmt.Errorf("source reference %q must be KIND/NAME or KIND/NAMESPACE/NAME", value)
		}
		ref.kind, ref.namespace, ref.name = kind, parts[1], parts[2]
	default:
		return sourceRef{}, fmt.Errorf("source reference %q must be NAME, NAMESPACE/NAME, KIND/NAME or KIND/NAMESPACE/NAME", value)
	}
	if ref.kind == sourceKindNode && ref.namespace != "" {
		return sourceRef{}, fmt.Errorf("node source %q must not include a namespace", value)
	}
	return ref, validateSourceRef(ref)
}

func validateSourceRef(ref sourceRef) error {
	if ref.name == "" {
		return errors.New("source name must not be empty")
	}
	if ref.kind == sourceKindNode {
		return validateResourceName("node", ref.name)
	}
	value := ref.name
	if ref.namespace != "" {
		value = ref.namespace + "/" + ref.name
	}
	return validateNamespacedReference(string(ref.kind), value)
}

func (ref sourceRef) reference() string {
	if ref.namespace == "" {
		if ref.kind == sourceKindPod {
			return ref.name
		}
		return string(ref.kind) + "/" + ref.name
	}
	return string(ref.kind) + "/" + ref.namespace + "/" + ref.name
}

func (ref sourceRef) kubeReference() string {
	if ref.namespace == "" {
		return ref.name
	}
	return ref.namespace + "/" + ref.name
}

func parseTargetAddress(value string) (string, error) {
	address := value
	if after, ok := strings.CutPrefix(value, "addr/"); ok {
		address = after
	}
	ip, err := netip.ParseAddr(address)
	if err != nil || ip.Zone() != "" {
		return "", fmt.Errorf("target %q is not an address reference; this phase supports addr/IP only", value)
	}
	return ip.Unmap().String(), nil
}

func commandPositionals(cmd *cobra.Command, args []string) []string {
	dash := cmd.ArgsLenAtDash()
	if dash < 0 || dash > len(args) {
		return args
	}
	return args[:dash]
}
