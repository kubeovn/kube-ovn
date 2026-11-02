package ko

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type sourceKind string

const (
	sourceKindPod                   sourceKind = "pod"
	sourceKindNode                  sourceKind = "node"
	sourceKindVM                    sourceKind = "vm"
	sourceKindVMI                   sourceKind = "vmi"
	sourceKindService               sourceKind = "service"
	sourceKindDeployment            sourceKind = "deployment"
	sourceKindDaemonSet             sourceKind = "daemonset"
	sourceKindStatefulSet           sourceKind = "statefulset"
	sourceKindReplicaSet            sourceKind = "replicaset"
	sourceKindReplicationController sourceKind = "replicationcontroller"
	sourceKindJob                   sourceKind = "job"
)

var sourceKindAliases = map[string]sourceKind{
	"pod":                     sourceKindPod,
	"po":                      sourceKindPod,
	"pods":                    sourceKindPod,
	"node":                    sourceKindNode,
	"no":                      sourceKindNode,
	"nodes":                   sourceKindNode,
	"vm":                      sourceKindVM,
	"virtualmachine":          sourceKindVM,
	"virtualmachines":         sourceKindVM,
	"vmi":                     sourceKindVMI,
	"virtualmachineinstance":  sourceKindVMI,
	"virtualmachineinstances": sourceKindVMI,
	"svc":                     sourceKindService,
	"service":                 sourceKindService,
	"services":                sourceKindService,
	"deploy":                  sourceKindDeployment,
	"deployment":              sourceKindDeployment,
	"deployments":             sourceKindDeployment,
	"ds":                      sourceKindDaemonSet,
	"daemonset":               sourceKindDaemonSet,
	"daemonsets":              sourceKindDaemonSet,
	"sts":                     sourceKindStatefulSet,
	"statefulset":             sourceKindStatefulSet,
	"statefulsets":            sourceKindStatefulSet,
	"rs":                      sourceKindReplicaSet,
	"replicaset":              sourceKindReplicaSet,
	"replicasets":             sourceKindReplicaSet,
	"rc":                      sourceKindReplicationController,
	"replicationcontroller":   sourceKindReplicationController,
	"replicationcontrollers":  sourceKindReplicationController,
	"job":                     sourceKindJob,
	"jobs":                    sourceKindJob,
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
	parts := strings.Split(value, "/")
	ref := sourceRef{kind: sourceKindPod}
	switch len(parts) {
	case 1:
		ref.name = parts[0]
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return sourceRef{}, fmt.Errorf("source reference %q contains an empty namespace or name", value)
		}
		kind, ok := sourceKindAliases[strings.ToLower(parts[0])]
		if !ok {
			return sourceRef{}, fmt.Errorf("unsupported source kind %q", parts[0])
		}
		ref.kind, ref.name = kind, parts[1]
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
		return sourceRef{}, fmt.Errorf("source reference %q must be NAME, KIND/NAME or KIND/NAMESPACE/NAME", value)
	}
	return ref, validateSourceRef(ref)
}

func validateSourceRef(ref sourceRef) error {
	if ref.name == "" {
		return errors.New("source name must not be empty")
	}
	if ref.kind == sourceKindNode {
		if ref.namespace != "" {
			return errors.New("node source must not include a namespace")
		}
		return validateResourceName("node", ref.name)
	}
	value := ref.name
	if ref.namespace != "" {
		value = ref.namespace + "/" + ref.name
	}
	return validateNamespacedReference(string(ref.kind), value)
}

func (ref sourceRef) kubeReference() string {
	if ref.namespace == "" {
		return ref.name
	}
	return ref.namespace + "/" + ref.name
}

func (ref sourceRef) reference() string {
	if ref.namespace == "" {
		return string(ref.kind) + "/" + ref.name
	}
	return string(ref.kind) + "/" + ref.namespace + "/" + ref.name
}

func parseTargetAddress(value string) (string, error) {
	address, ok := strings.CutPrefix(value, "addr/")
	if !ok {
		return "", fmt.Errorf("target %q must be addr/IP", value)
	}
	ip, err := netip.ParseAddr(address)
	if err != nil || ip.Zone() != "" {
		return "", fmt.Errorf("target %q is not an address reference; this phase supports addr/IP only", value)
	}
	return ip.Unmap().String(), nil
}
