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

// sourceRef is the common command-level resource notation.
type sourceRef struct {
	kind sourceKind
	name string
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
			return sourceRef{}, fmt.Errorf("source reference %q contains an empty kind or name", value)
		}
		kind, ok := sourceKindAliases[strings.ToLower(parts[0])]
		if !ok {
			return sourceRef{}, fmt.Errorf("unsupported source kind %q", parts[0])
		}
		ref.kind, ref.name = kind, parts[1]
	default:
		return sourceRef{}, fmt.Errorf("source reference %q must be NAME or KIND/NAME; use -n for the namespace", value)
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
	return validateNamespacedReference(string(ref.kind), ref.name)
}

func (ref sourceRef) reference() string {
	return string(ref.kind) + "/" + ref.name
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
