package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func TestPlanDistributedPortGroupMembershipUnchanged(t *testing.T) {
	t.Parallel()

	groups := []ovnnb.PortGroup{
		{Name: "target", UUID: "pg-uuid", Ports: []string{"held", "valid"}},
	}
	batches, err := planDistributedPortGroupMembership(groups, map[string]string{
		"valid":   "target",
		"retired": "",
	})
	require.NoError(t, err)
	require.Empty(t, batches)
}

func TestPlanDistributedPortGroupMembershipMoveAndRetire(t *testing.T) {
	t.Parallel()

	oldGroup := ovnnb.PortGroup{
		Name: "old", UUID: "old-uuid", Ports: []string{"unknown", "retired", "move", "duplicate"},
	}
	targetGroup := ovnnb.PortGroup{
		Name: "new", UUID: "new-uuid", Ports: []string{"duplicate"},
	}
	groups := []ovnnb.PortGroup{oldGroup, targetGroup}
	batches, err := planDistributedPortGroupMembership(groups, map[string]string{
		"retired":   "",
		"move":      "new",
		"duplicate": "new",
		"fresh":     "new",
	})
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.Equal(t, []string{"duplicate", "fresh", "move", "retired"}, batches[0].lspUUIDs)
	// The old group snapshot includes the unknown member, but the removal does not.
	oldGroup.Ports = []string{"duplicate", "move", "retired", "unknown"}
	require.Equal(t, []ovs.PortGroupMembershipChange{
		{Group: targetGroup, Add: []string{"fresh", "move"}},
		{Group: oldGroup, Remove: []string{"duplicate", "move", "retired"}},
	}, batches[0].changes)
	require.Equal(t, []string{"unknown", "retired", "move", "duplicate"}, groups[0].Ports)
}

func TestPlanDistributedPortGroupMembershipGuardsExistingTarget(t *testing.T) {
	t.Parallel()

	oldGroup := ovnnb.PortGroup{Name: "a", UUID: "pg-a", Ports: []string{"port"}}
	targetGroup := ovnnb.PortGroup{Name: "b", UUID: "pg-b", Ports: []string{"port"}}
	batches, err := planDistributedPortGroupMembership(
		[]ovnnb.PortGroup{targetGroup, oldGroup}, map[string]string{"port": "b"},
	)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.Equal(t, []ovs.PortGroupMembershipChange{
		{Group: oldGroup, Remove: []string{"port"}},
		{Group: targetGroup},
	}, batches[0].changes)
}

func TestPlanDistributedPortGroupMembershipMissingTarget(t *testing.T) {
	t.Parallel()

	oldGroup := ovnnb.PortGroup{Name: "old", UUID: "pg-a", Ports: []string{"move", "retire"}}
	batches, err := planDistributedPortGroupMembership([]ovnnb.PortGroup{oldGroup}, map[string]string{
		"move":   "missing",
		"retire": "",
	})
	require.ErrorContains(t, err, "missing")
	require.Len(t, batches, 1)
	require.Equal(t, []string{"retire"}, batches[0].lspUUIDs)
	require.Equal(t, []ovs.PortGroupMembershipChange{
		{Group: oldGroup, Remove: []string{"retire"}},
	}, batches[0].changes)
}

func TestPlanDistributedPortGroupMembershipProjectedSnapshot(t *testing.T) {
	t.Parallel()

	ports := make([]string, distributedPortGroupBatchSize+1)
	desired := make(map[string]string, len(ports))
	for i := range ports {
		ports[i] = fmt.Sprintf("port-%03d", i)
		desired[ports[i]] = "target"
	}
	old := ovnnb.PortGroup{Name: "old", UUID: "pg-old", Ports: ports}
	target := ovnnb.PortGroup{Name: "target", UUID: "pg-new"}
	batches, err := planDistributedPortGroupMembership([]ovnnb.PortGroup{target, old}, desired)

	require.NoError(t, err)
	require.Len(t, batches, 2)
	firstPorts, lastPorts := ports[:distributedPortGroupBatchSize], ports[distributedPortGroupBatchSize:]
	require.Equal(t, firstPorts, batches[0].lspUUIDs)
	require.Equal(t, lastPorts, batches[1].lspUUIDs)
	require.Equal(t, []ovs.PortGroupMembershipChange{
		{Group: old, Remove: firstPorts},
		{Group: target, Add: firstPorts},
	}, batches[0].changes)
	old.Ports, target.Ports = lastPorts, firstPorts
	require.Equal(t, []ovs.PortGroupMembershipChange{
		{Group: old, Remove: lastPorts},
		{Group: target, Add: lastPorts},
	}, batches[1].changes)
}

func BenchmarkPlanDistributedPortGroupMembership(b *testing.B) {
	const nodeCount, portCount = 100, 10000
	for _, moved := range []int{0, 1, portCount} {
		b.Run(fmt.Sprintf("%dnodes_%dports_%dmoves", nodeCount, portCount, moved), func(b *testing.B) {
			groups := make([]ovnnb.PortGroup, nodeCount)
			for i := range groups {
				groups[i] = ovnnb.PortGroup{
					Name: fmt.Sprintf("node-%03d", i), UUID: fmt.Sprintf("pg-%03d", i),
				}
			}
			desired := make(map[string]string, portCount)
			for i := range portCount {
				port := fmt.Sprintf("port-%05d", i)
				source, target := i%nodeCount, i%nodeCount
				if i < moved {
					target = (source + 1) % nodeCount
				}
				groups[source].Ports = append(groups[source].Ports, port)
				desired[port] = groups[target].Name
			}
			b.ReportAllocs()
			b.ResetTimer()
			var batchCount int
			for b.Loop() {
				batches, err := planDistributedPortGroupMembership(groups, desired)
				if err != nil {
					b.Fatal(err)
				}
				batchCount = len(batches)
			}
			b.ReportMetric(float64(batchCount), "write-batches/op")
		})
	}
}
