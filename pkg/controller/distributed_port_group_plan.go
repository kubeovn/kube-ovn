package controller

import (
	"fmt"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

const distributedPortGroupBatchSize = 128

type distributedPortGroupBatch struct {
	changes  []ovs.PortGroupMembershipChange
	lspUUIDs []string
}

// planDistributedPortGroupMembership builds guarded batches from one snapshot of the subnet's groups.
// A port omitted from desired has unknown ownership, and an empty target means it has no route group.
func planDistributedPortGroupMembership(groups []ovnnb.PortGroup, desired map[string]string) ([]distributedPortGroupBatch, error) {
	groupsByName := make(map[string]ovnnb.PortGroup, len(groups))
	membership := make(map[string][]string)
	for _, group := range groups {
		groupsByName[group.Name] = group
		for _, port := range group.Ports {
			membership[port] = append(membership[port], group.Name)
		}
	}

	var ports []string
	missing := sets.New[string]()
	for port, target := range desired {
		if _, exists := groupsByName[target]; target != "" && !exists {
			missing.Insert(target)
			continue
		}
		current := membership[port]
		switch len(current) {
		case 0:
			if target == "" {
				continue
			}
		case 1:
			if current[0] == target {
				continue
			}
		}
		ports = append(ports, port)
	}

	slices.Sort(ports)
	batches := make([]distributedPortGroupBatch, 0, (len(ports)+distributedPortGroupBatchSize-1)/distributedPortGroupBatchSize)
	for chunk := range slices.Chunk(ports, distributedPortGroupBatchSize) {
		batches = append(batches, buildDistributedPortGroupBatch(groupsByName, membership, desired, chunk))
	}
	if missing.Len() > 0 {
		return batches, fmt.Errorf("missing distributed subnet port groups: %v", sets.List(missing))
	}
	return batches, nil
}

// buildDistributedPortGroupBatch retains each group's preconditions and projects the applied
// membership for later batches. A failed batch invalidates the remaining plan.
func buildDistributedPortGroupBatch(groups map[string]ovnnb.PortGroup, membership map[string][]string, desired map[string]string, ports []string) distributedPortGroupBatch {
	changes := make(map[string]*ovs.PortGroupMembershipChange)
	changeFor := func(name string) *ovs.PortGroupMembershipChange {
		if changes[name] == nil {
			changes[name] = &ovs.PortGroupMembershipChange{}
		}
		return changes[name]
	}
	for _, port := range ports {
		target := desired[port]
		var inTarget bool
		for _, group := range membership[port] {
			if group == target {
				inTarget = true
			} else {
				change := changeFor(group)
				change.Remove = append(change.Remove, port)
			}
		}
		if target != "" {
			// Even if already present, guard the target against concurrent removal.
			change := changeFor(target)
			if !inTarget {
				change.Add = append(change.Add, port)
			}
		}
	}
	batch := distributedPortGroupBatch{
		changes:  make([]ovs.PortGroupMembershipChange, 0, len(changes)),
		lspUUIDs: ports,
	}
	for _, name := range slices.Sorted(maps.Keys(changes)) {
		group := groups[name]
		group.Ports = slices.Clone(group.Ports)
		slices.Sort(group.Ports)
		change := *changes[name]
		change.Group = group
		batch.changes = append(batch.changes, change)

		if len(change.Add) == 0 && len(change.Remove) == 0 {
			continue
		}
		group.Ports = slices.Clone(group.Ports)
		removed := sets.New(change.Remove...)
		group.Ports = slices.DeleteFunc(group.Ports, removed.Has)
		group.Ports = append(group.Ports, change.Add...)
		groups[name] = group
	}
	return batch
}
