package ovs

import (
	"net"

	"github.com/stretchr/testify/require"
)

// Test regression for switch lb rule ip_port_mappings cleanup timing.
// It models the observed E2E order:
// 1. VIP has 3 backends and 3 mappings.
// 2. Endpoint update shrinks VIP to 2 backends and updates mappings.
// 3. Remaining pod/VIP cleanup operations happen later.
// The stale backend mapping must already be gone after step 2.
func (suite *OvnClientTestSuite) Test_LoadBalancerUpdateIPPortMapping_RemovesOrphanedBackendImmediately() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-lb-orphaned-cleanup-immediate"
	vip := "10.96.0.9:3000"
	backend1 := "192.168.50.10:3000"
	backend2 := "192.168.50.11:3000"
	backend3 := "192.168.50.12:3000"

	err := nbClient.CreateLoadBalancer(lbName, "tcp")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, nbClient.DeleteLoadBalancer(lbName))
	})

	require.NoError(t, nbClient.LoadBalancerAddVip(lbName, vip, backend1, backend2, backend3))

	host1, _, err := net.SplitHostPort(backend1)
	require.NoError(t, err)
	host2, _, err := net.SplitHostPort(backend2)
	require.NoError(t, err)
	host3, _, err := net.SplitHostPort(backend3)
	require.NoError(t, err)

	initialMappings := map[string]string{
		host1: "pod1.ns.ovn:169.254.169.5",
		host2: "pod2.ns.ovn:169.254.169.5",
		host3: "pod3.ns.ovn:169.254.169.5",
	}
	require.NoError(t, nbClient.LoadBalancerUpdateIPPortMapping(lbName, vip, initialMappings))

	// Simulate endpoint shrink first: the VIP now only has backend1 and backend2.
	require.NoError(t, nbClient.LoadBalancerAddVip(lbName, vip, backend1, backend2))

	updatedMappings := map[string]string{
		host1: "pod1.ns.ovn:169.254.169.5",
		host2: "pod2.ns.ovn:169.254.169.5",
	}
	require.NoError(t, nbClient.LoadBalancerUpdateIPPortMapping(lbName, vip, updatedMappings))

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Contains(t, lb.IPPortMappings, host1)
	require.Contains(t, lb.IPPortMappings, host2)
	require.NotContains(t, lb.IPPortMappings, host3, "host3 should be removed as soon as VIP backends shrink")
}

// Test regression for replacing an existing IP -> LSP mapping.
// libovsdb map insert does not overwrite an existing key, so updating the LSP
// for the same backend IP must effectively behave like delete(key)+insert(key,value).
func (suite *OvnClientTestSuite) Test_LoadBalancerUpdateIPPortMapping_ReplacesExistingMappingValue() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-lb-replace-existing-mapping"
	vip := "10.96.0.10:8080"
	backend := "192.168.51.10:8080"

	err := nbClient.CreateLoadBalancer(lbName, "tcp")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, nbClient.DeleteLoadBalancer(lbName))
	})

	require.NoError(t, nbClient.LoadBalancerAddVip(lbName, vip, backend))

	host, _, err := net.SplitHostPort(backend)
	require.NoError(t, err)

	initialMappings := map[string]string{
		host: "pod-old.ns.ovn:169.254.169.5",
	}
	require.NoError(t, nbClient.LoadBalancerUpdateIPPortMapping(lbName, vip, initialMappings))

	updatedMappings := map[string]string{
		host: "pod-new.ns.ovn:169.254.169.5",
	}
	require.NoError(t, nbClient.LoadBalancerUpdateIPPortMapping(lbName, vip, updatedMappings))

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, updatedMappings[host], lb.IPPortMappings[host], "mapping value should be replaced for the same backend IP")
	for _, value := range lb.IPPortMappings {
		require.NotEqual(t, initialMappings[host], value, "old mapping value should not remain after replacement")
	}
}
