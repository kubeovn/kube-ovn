package ovn_ic_controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestClassifyICConfig(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"enable-ic":  "true",
		"az-name":    "region1",
		"ic-db-host": "192.168.0.1",
		"ic-nb-port": "6645",
		"ic-sb-port": "6646",
		"auto-route": "true",
		"gw-nodes":   "192.168.137.158,192.168.142.47,192.168.143.70",
	}

	t.Run("first establish", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, icFirstEstablish, classifyICConfig("unknown", nil, base))
	})

	t.Run("no action when unchanged", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, icNoAction, classifyICConfig("true", cloneICConfig(base), cloneICConfig(base)))
	})

	t.Run("gateway-only change", func(t *testing.T) {
		t.Parallel()
		cur := cloneICConfig(base)
		cur["gw-nodes"] = "192.168.142.47"
		require.Equal(t, icGatewayChange, classifyICConfig("true", cloneICConfig(base), cur))
	})

	t.Run("az change is full reestablish", func(t *testing.T) {
		t.Parallel()
		cur := cloneICConfig(base)
		cur["az-name"] = "region2"
		require.Equal(t, icConfigChange, classifyICConfig("true", cloneICConfig(base), cur))
	})
}

func TestCloneICConfigIsIndependent(t *testing.T) {
	t.Parallel()

	orig := map[string]string{"gw-nodes": "a,b"}
	cloned := cloneICConfig(orig)
	orig["gw-nodes"] = "b"
	require.Equal(t, "a,b", cloned["gw-nodes"])
}

func TestMergeConflictCIDRs(t *testing.T) {
	t.Parallel()

	local := []string{"10.16.0.0/16", "10.254.201.0/24"}
	learned := []string{"10.3.0.0/16", "10.254.201.0/24"}
	got := mergeConflictCIDRs(local, learned, nil)
	require.Equal(t, []string{"10.254.201.0/24"}, got)

	t.Run("blacklists both sides of a broad overlap", func(t *testing.T) {
		t.Parallel()
		got := mergeConflictCIDRs([]string{"10.0.1.0/24"}, []string{"10.0.0.0/16"}, nil)
		require.Equal(t, []string{"10.0.0.0/16", "10.0.1.0/24"}, got)
	})

	t.Run("keeps persisted overlap after learned route is deleted", func(t *testing.T) {
		t.Parallel()
		got := mergeConflictCIDRs([]string{"10.0.1.0/24"}, nil, []string{"10.0.0.0/16"})
		require.Equal(t, []string{"10.0.0.0/16"}, got)
	})

	t.Run("sticky keeps conflict after learned route is gone", func(t *testing.T) {
		t.Parallel()
		got := mergeConflictCIDRs(local, []string{"10.3.0.0/16"}, []string{"10.254.201.0/24"})
		require.Equal(t, []string{"10.254.201.0/24"}, got)
	})

	t.Run("drop sticky after local subnet is deleted", func(t *testing.T) {
		t.Parallel()
		got := mergeConflictCIDRs([]string{"10.16.0.0/16"}, nil, []string{"10.254.201.0/24"})
		require.Empty(t, got)
	})

	t.Run("unique remote cidr is not a conflict", func(t *testing.T) {
		t.Parallel()
		got := mergeConflictCIDRs([]string{"10.16.0.0/16"}, []string{"10.3.0.0/16"}, nil)
		require.Empty(t, got)
	})
}

func TestSubnetCIDRsSplitsFamilies(t *testing.T) {
	t.Parallel()

	subnets := []*kubeovnv1.Subnet{{Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24,fd00::/64"}}}
	require.Equal(t, []string{"10.0.0.0/24", "fd00::/64"}, subnetCIDRs(subnets))
}

func TestFilterPersistedConflictCIDRs(t *testing.T) {
	t.Parallel()

	local := []string{"10.0.1.0/24", "fd00::/64"}
	require.Equal(t, []string{"10.0.0.0/16", "fd00::/64"}, filterPersistedConflictCIDRs(local, "10.0.0.0/16,10.10.0.0/16,fd00::/64"))
}

func TestGenerateNewOrderGwNodesEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, generateNewOrderGwNodes(nil, 0))
}
