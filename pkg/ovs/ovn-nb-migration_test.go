package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

// ensureNbGlobalExists creates NBGlobal if it doesn't exist (needed for migration tests)
func ensureNbGlobalExists(t *testing.T, nbClient *OVNNbClient) {
	_, err := nbClient.GetNbGlobal()
	if err != nil {
		// NBGlobal doesn't exist, create it
		nbGlobal := &ovnnb.NBGlobal{
			Options: map[string]string{},
		}
		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)
	}
}

func (suite *OvnClientTestSuite) testMigrateVendorExternalIDs() {
	t := suite.T()
	// Note: Cannot run in parallel as these tests modify shared NBGlobal state

	nbClient := suite.ovnNBClient
	lrName := "test-migrate-lr"
	lsName := "test-migrate-ls"

	// Clean up NBGlobal after test to avoid affecting other tests
	t.Cleanup(func() {
		_ = nbClient.DeleteNbGlobal()
	})

	// Ensure NBGlobal exists for test
	ensureNbGlobalExists(t, nbClient)

	// Clear any existing version to simulate upgrade from old version
	nbGlobal, err := nbClient.GetNbGlobal()
	require.NoError(t, err)
	if nbGlobal.ExternalIDs != nil {
		delete(nbGlobal.ExternalIDs, kubeOvnVersionKey)
		err = nbClient.UpdateNbGlobal(nbGlobal, &nbGlobal.ExternalIDs)
		require.NoError(t, err)
	}

	// Create a logical router with vendor tag (this simulates existing kube-ovn router)
	err = nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	// Create a logical switch with vendor tag
	err = nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	// Create test resources without vendor tags to simulate pre-v1.15.0 resources

	// 1. Create LRP without vendor tag but with 'lr' externalID
	lrpName := lrName + "-" + lsName
	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		MAC:      util.GenerateMac(),
		Networks: []string{"10.0.0.1/24"},
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName, // This identifies it as kube-ovn resource
			// vendor tag intentionally missing
		},
	}
	ops, err := nbClient.CreateLogicalRouterPortOp(lrp, lrName)
	require.NoError(t, err)
	err = nbClient.Transact("test-lrp-add", ops)
	require.NoError(t, err)

	// 2. Create port group without vendor tag using low-level OVSDB operation
	// to simulate pre-v1.15.0 resources (high-level CreatePortGroup auto-adds vendor tag)
	sgPgName := "ovn.sg.test.security.group"
	pg := &ovnnb.PortGroup{
		UUID: ovsclient.NamedUUID(),
		Name: sgPgName,
		ExternalIDs: map[string]string{
			sgKey: "test-sg",
			// vendor tag intentionally missing
		},
	}
	ops, err = nbClient.Create(pg)
	require.NoError(t, err)
	err = nbClient.Transact("test-pg-add", ops)
	require.NoError(t, err)

	// 3. Create address set without vendor tag using low-level OVSDB operation
	asName := "ovn.sg.test.sg.associated.v4"
	as := &ovnnb.AddressSet{
		UUID: ovsclient.NamedUUID(),
		Name: asName,
		ExternalIDs: map[string]string{
			sgKey: "test-sg",
			// vendor tag intentionally missing
		},
	}
	ops, err = nbClient.Create(as)
	require.NoError(t, err)
	err = nbClient.Transact("test-as-add", ops)
	require.NoError(t, err)

	// 4. Create load balancer without vendor tag using low-level OVSDB operation
	lbName := "cluster-tcp-loadbalancer"
	lb := &ovnnb.LoadBalancer{
		UUID:     ovsclient.NamedUUID(),
		Name:     lbName,
		Protocol: &[]string{"tcp"}[0],
		// vendor tag intentionally missing (ExternalIDs is nil)
	}
	ops, err = nbClient.Create(lb)
	require.NoError(t, err)
	err = nbClient.Transact("test-lb-add", ops)
	require.NoError(t, err)

	// Run migration (should run because no version is stored)
	err = nbClient.MigrateVendorExternalIDs()
	require.NoError(t, err)

	// Verify LRP has vendor tag
	migratedLrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
	require.NoError(t, err)
	require.Equal(t, util.CniTypeName, migratedLrp.ExternalIDs["vendor"])

	// Verify port group has vendor tag
	migratedPg, err := nbClient.GetPortGroup(sgPgName, false)
	require.NoError(t, err)
	require.Equal(t, util.CniTypeName, migratedPg.ExternalIDs["vendor"])

	// Verify load balancer has vendor tag
	migratedLb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, util.CniTypeName, migratedLb.ExternalIDs["vendor"])

	// Verify version was stored
	storedVersion, err := nbClient.GetKubeOvnVersion()
	require.NoError(t, err)
	require.Equal(t, versions.VERSION, storedVersion)
}

func (suite *OvnClientTestSuite) testMigrateVendorExternalIDsIdempotent() {
	t := suite.T()
	// Note: Cannot run in parallel as these tests modify shared NBGlobal state

	nbClient := suite.ovnNBClient
	lrName := "test-migrate-idempotent-lr"

	// Clean up NBGlobal after test to avoid affecting other tests
	t.Cleanup(func() {
		_ = nbClient.DeleteNbGlobal()
	})

	// Ensure NBGlobal exists for test
	ensureNbGlobalExists(t, nbClient)

	// Create a logical router with vendor tag
	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	// Clear version to trigger first migration
	nbGlobal, err := nbClient.GetNbGlobal()
	require.NoError(t, err)
	if nbGlobal.ExternalIDs != nil {
		delete(nbGlobal.ExternalIDs, kubeOvnVersionKey)
		err = nbClient.UpdateNbGlobal(nbGlobal, &nbGlobal.ExternalIDs)
		require.NoError(t, err)
	}

	// First migration should run
	err = nbClient.MigrateVendorExternalIDs()
	require.NoError(t, err)

	// Verify version was stored
	storedVersion, err := nbClient.GetKubeOvnVersion()
	require.NoError(t, err)
	require.Equal(t, versions.VERSION, storedVersion)

	// Subsequent calls should skip migration but not fail
	for range 3 {
		err = nbClient.MigrateVendorExternalIDs()
		require.NoError(t, err)
	}

	// Version should still be set
	storedVersion, err = nbClient.GetKubeOvnVersion()
	require.NoError(t, err)
	require.Equal(t, versions.VERSION, storedVersion)
}

func (suite *OvnClientTestSuite) testMigrateSkipsWhenVersionSet() {
	t := suite.T()
	// Note: Cannot run in parallel as these tests modify shared NBGlobal state

	nbClient := suite.ovnNBClient

	// Clean up NBGlobal after test to avoid affecting other tests
	t.Cleanup(func() {
		_ = nbClient.DeleteNbGlobal()
	})

	// Ensure NBGlobal exists for test
	ensureNbGlobalExists(t, nbClient)

	// Set version to current (simulating already-migrated system)
	err := nbClient.SetKubeOvnVersion(versions.VERSION)
	require.NoError(t, err)

	// Check that migration is not needed
	needsMigration, err := nbClient.needsVendorMigration()
	require.NoError(t, err)
	require.False(t, needsMigration, "migration should not be needed when current version is set")
}

func (suite *OvnClientTestSuite) testMigrateRunsWhenOldVersion() {
	t := suite.T()
	// Note: Cannot run in parallel as these tests modify shared NBGlobal state

	nbClient := suite.ovnNBClient

	// Clean up NBGlobal after test to avoid affecting other tests
	t.Cleanup(func() {
		_ = nbClient.DeleteNbGlobal()
	})

	// Ensure NBGlobal exists for test
	ensureNbGlobalExists(t, nbClient)

	// Set version to old version (before vendor tagging was introduced)
	err := nbClient.SetKubeOvnVersion("v1.14.0")
	require.NoError(t, err)

	// Check that migration IS needed
	needsMigration, err := nbClient.needsVendorMigration()
	require.NoError(t, err)
	require.True(t, needsMigration, "migration should be needed when old version is stored")
}

func (suite *OvnClientTestSuite) testMigrateVendorExternalIDsSkipsNonKubeOvn() {
	t := suite.T()
	// Note: Cannot run in parallel as these tests modify shared NBGlobal state

	nbClient := suite.ovnNBClient

	// Clean up NBGlobal after test to avoid affecting other tests
	t.Cleanup(func() {
		_ = nbClient.DeleteNbGlobal()
	})

	// Ensure NBGlobal exists for test
	ensureNbGlobalExists(t, nbClient)

	// Create resources that should NOT be tagged (simulating external resources)

	// 1. Port group with neutron-like naming (should be skipped)
	neutronPgName := "neutron.security.group.123"
	pg := &ovnnb.PortGroup{
		UUID: ovsclient.NamedUUID(),
		Name: neutronPgName,
		ExternalIDs: map[string]string{
			"neutron:security_group_id": "123",
		},
	}
	ops, err := nbClient.Create(pg)
	require.NoError(t, err)
	err = nbClient.Transact("test-neutron-pg", ops)
	require.NoError(t, err)

	// Run migration
	err = nbClient.MigrateVendorExternalIDs()
	require.NoError(t, err)

	// Verify neutron resource was NOT tagged
	migratedPg, err := nbClient.GetPortGroup(neutronPgName, false)
	require.NoError(t, err)
	// Should not have vendor tag
	require.NotEqual(t, util.CniTypeName, migratedPg.ExternalIDs["vendor"])
}

func TestSecurityGroupPatterns(t *testing.T) {
	t.Parallel()

	// Test security group port group pattern
	testCases := []struct {
		name     string
		expected bool
	}{
		{"ovn.sg.default", true},
		{"ovn.sg.my.security.group", true},
		{"ovn.sg.test.with.many.dots", true},
		{"ovn.sg.", false}, // empty sg name
		{"ovn.other.thing", false},
		{"neutron.sg.something", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sgPortGroupPattern.MatchString(tc.name)
			if result != tc.expected {
				t.Errorf("sgPortGroupPattern.MatchString(%q) = %v, want %v", tc.name, result, tc.expected)
			}
		})
	}
}

func TestLoadBalancerPatterns(t *testing.T) {
	t.Parallel()

	clusterCases := []struct {
		name     string
		expected bool
	}{
		{"cluster-tcp-loadbalancer", true},
		{"cluster-udp-loadbalancer", true},
		{"cluster-sctp-loadbalancer", true},
		{"cluster-tcp-session-loadbalancer", true},
		{"cluster-udp-session-loadbalancer", true},
		{"cluster-sctp-session-loadbalancer", true},
		{"other-tcp-loadbalancer", false},
		{"cluster-http-loadbalancer", false},
	}

	for _, tc := range clusterCases {
		t.Run("cluster/"+tc.name, func(t *testing.T) {
			result := clusterLBPattern.MatchString(tc.name)
			if result != tc.expected {
				t.Errorf("clusterLBPattern.MatchString(%q) = %v, want %v", tc.name, result, tc.expected)
			}
		})
	}

	vpcCases := []struct {
		name     string
		expected bool
	}{
		{"vpc-default-tcp-load", true},
		{"vpc-default-udp-load", true},
		{"vpc-default-sctp-load", true},
		{"vpc-default-tcp-sess-load", true},
		{"vpc-my-custom-vpc-udp-sess-load", true},
		{"vpc-custom-sctp-load", true},
		{"vpc-default-http-load", false},
		{"cluster-tcp-loadbalancer", false},
	}

	for _, tc := range vpcCases {
		t.Run("vpc/"+tc.name, func(t *testing.T) {
			result := vpcLBPattern.MatchString(tc.name)
			if result != tc.expected {
				t.Errorf("vpcLBPattern.MatchString(%q) = %v, want %v", tc.name, result, tc.expected)
			}
		})
	}
}
