package ovs

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testAddLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient         = suite.ovnNBClient
		lbName           = "test-create-lb-hc"
		vip              = "1.1.1.1:80"
		lbhc, lbhcRepeat *ovnnb.LoadBalancerHealthCheck
		err              error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)
	require.Equal(t, vip, lbhc.Vip)
	require.NotEmpty(t, lbhc.UUID)

	// should no err create lbhc repeatedly
	err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	_, lbhcRepeat, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)
	require.Equal(t, vip, lbhcRepeat.Vip)
	require.NotEmpty(t, lbhcRepeat.UUID)

	require.Equal(t, lbhc.UUID, lbhcRepeat.UUID)

	// create lbhc with empty lbName
	err = nbClient.AddLoadBalancerHealthCheck("", vip, map[string]string{})
	require.ErrorContains(t, err, "the lb name is required")

	// create lbhc with empty vip
	err = nbClient.AddLoadBalancerHealthCheck(lbName, "", map[string]string{})
	require.ErrorContains(t, err, "the vip endpoint is required")
}

func (suite *OvnClientTestSuite) testUpdateLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient = suite.ovnNBClient
		lbName   = "test-update-lb-hc"
		vip      = "2.2.2.2:80"
		lbhc     *ovnnb.LoadBalancerHealthCheck
		err      error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)

	vip = "3.3.3.3:80"
	t.Run("update vip",
		func(t *testing.T) {
			lbhc.Vip = vip

			err = nbClient.UpdateLoadBalancerHealthCheck(lbhc, &lbhc.Vip)
			require.NoError(t, err)

			_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
			require.NoError(t, err)

			require.Equal(t, vip, lbhc.Vip)
		},
	)
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient = suite.ovnNBClient
		lbName   = "test-del-lb-hc"
		vip      = "1.1.1.11:80"
		err      error
	)

	// delete lbhc for non-exist lb
	err = nbClient.DeleteLoadBalancerHealthCheck(lbName, vip)
	require.ErrorContains(t, err, "not found load balancer")

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	err = nbClient.DeleteLoadBalancerHealthCheck(lbName, vip)
	require.NoError(t, err)

	_, _, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerHealthChecks() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient     = suite.ovnNBClient
		lbNamePrefix = "test-del-lb-hcs"
		vipFormat    = "5.5.5.%d:80"
		lbhc         *ovnnb.LoadBalancerHealthCheck
		vips         []string
		lbName, vip  string
		err          error
	)

	for i := range 5 {
		lbName = fmt.Sprintf("%s-%d", lbNamePrefix, i)
		vip = fmt.Sprintf(vipFormat, i+1)

		err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
		require.NoError(t, err)

		err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
		require.NoError(t, err)

		vips = append(vips, vip)

		_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
		require.NoError(t, err)
		require.NotNil(t, lbhc)
		require.Equal(t, vip, lbhc.Vip)

		err = nbClient.LoadBalancerDeleteHealthCheck(lbName, lbhc.UUID)
		require.NoError(t, err)
	}

	err = nbClient.DeleteLoadBalancerHealthChecks(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(vips, lbhc.Vip)
		},
	)
	require.NoError(t, err)

	for _, ip := range vips {
		_, _, err = nbClient.GetLoadBalancerHealthCheck(lbName, ip, true)
		require.NoError(t, err)

		_, _, err = nbClient.GetLoadBalancerHealthCheck(lbName, ip, false)
		require.Error(t, err)
	}
}

func (suite *OvnClientTestSuite) testGetLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient       = suite.ovnNBClient
		lbName         = "test-get-lb-hc"
		vip            = "1.1.1.22:80"
		vipNonExistent = "1.1.1.33:80"
		err            error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	t.Run("should return no err when found load balancer health check",
		func(t *testing.T) {
			t.Parallel()
			_, lbhc, err := nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
			require.NoError(t, err)
			require.Equal(t, vip, lbhc.Vip)
			require.NotEmpty(t, lbhc.UUID)
		},
	)

	t.Run("should return err when not found load balancer health check",
		func(t *testing.T) {
			t.Parallel()
			_, _, err := nbClient.GetLoadBalancerHealthCheck(lbName, vipNonExistent, false)
			require.Error(t, err)
		},
	)

	t.Run("no err when not found load balancer health check and ignoreNotFound is true",
		func(t *testing.T) {
			t.Parallel()
			_, _, err := nbClient.GetLoadBalancerHealthCheck(lbName, vipNonExistent, true)
			require.NoError(t, err)
		},
	)

	t.Run("should return err when health checks have the same vipEndpoint",
		func(t *testing.T) {
			t.Parallel()
			err = nbClient.CreateLoadBalancer(lbName+"1", "tcp", "")
			require.NoError(t, err)
			lbhc1 := &ovnnb.LoadBalancerHealthCheck{
				UUID:        ovsclient.NamedUUID(),
				ExternalIDs: nil,
				Options: map[string]string{
					"timeout":       "20",
					"interval":      "5",
					"success_count": "3",
					"failure_count": "3",
				},
				Vip: vip,
			}
			lbhc2 := &ovnnb.LoadBalancerHealthCheck{
				UUID:        ovsclient.NamedUUID(),
				ExternalIDs: nil,
				Options: map[string]string{
					"timeout":       "20",
					"interval":      "5",
					"success_count": "3",
					"failure_count": "3",
				},
				Vip: vip,
			}
			err = nbClient.CreateLoadBalancerHealthCheck(lbName+"1", vip, lbhc1)
			require.NoError(t, err)
			err = nbClient.CreateLoadBalancerHealthCheck(lbName+"1", vip, lbhc2)
			require.NoError(t, err)

			_, _, err := nbClient.GetLoadBalancerHealthCheck(lbName+"1", vip, true)
			require.ErrorContains(t, err, "lb test-get-lb-hc1 has more than one health check with the same vip 1.1.1.22:80")
		},
	)
}

func (suite *OvnClientTestSuite) testListLoadBalancerHealthChecks() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient     = suite.ovnNBClient
		lbNamePrefix = "test-list-lb-hcs"
		vipFormat    = "6.6.6.%d:80"
		vips         []string
		lbName, vip  string
		err          error
	)

	vips = make([]string, 0, 5)
	for i := 101; i <= 105; i++ {
		lbName = fmt.Sprintf("%s-%d", lbNamePrefix, i)
		vip = fmt.Sprintf(vipFormat, i+1)

		err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
		require.NoError(t, err)

		err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
		require.NoError(t, err)

		vips = append(vips, vip)
	}

	t.Run("has no custom filter",
		func(t *testing.T) {
			t.Parallel()

			lbhcs, err := nbClient.ListLoadBalancerHealthChecks(nil)
			require.NoError(t, err)
			require.NotEmpty(t, lbhcs)

			newVips := make([]string, 0, 5)
			for _, lbhc := range lbhcs {
				newVips = append(newVips, lbhc.Vip)
			}

			require.Subset(t, newVips, vips)
		},
	)

	t.Run("has custom filter",
		func(t *testing.T) {
			t.Parallel()

			t.Run("filter by vip",
				func(t *testing.T) {
					t.Parallel()

					lbhcs, err := nbClient.ListLoadBalancerHealthChecks(
						func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
							return strings.Contains(lbhc.Vip, "6.6.6.")
						},
					)
					require.NoError(t, err)
					require.NotEmpty(t, lbhcs)

					newVips := make([]string, 0, 5)
					for _, lbhc := range lbhcs {
						if !strings.Contains(lbhc.Vip, "6.6.6.10") {
							continue
						}
						newVips = append(newVips, lbhc.Vip)
					}
					require.ElementsMatch(t, vips, newVips)
				},
			)
		},
	)
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerHealthCheckOp() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient       = suite.ovnNBClient
		lbName         = "test-del-lb-hc-op"
		vip            = "1.1.1.44:80"
		vipNonExistent = "1.1.1.55:80"
		lbhc           *ovnnb.LoadBalancerHealthCheck
		err            error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.NotNil(t, lb)

	err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, nil)
	require.NoError(t, err)

	_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)

	t.Run("normal delete",
		func(t *testing.T) {
			t.Parallel()

			ops, err := nbClient.DeleteLoadBalancerHealthCheckOp(lbName, vip)
			require.NoError(t, err)
			require.Len(t, ops, 2)

			require.ElementsMatch(t, ops,
				[]ovsdb.Operation{
					{
						Op:    ovsdb.OperationMutate,
						Table: ovnnb.LoadBalancerTable,
						Where: []ovsdb.Condition{
							{
								Column:   "_uuid",
								Function: ovsdb.ConditionEqual,
								Value: ovsdb.UUID{
									GoUUID: lb.UUID,
								},
							},
						},
						Mutations: []ovsdb.Mutation{
							{
								Column:  "health_check",
								Mutator: ovsdb.MutateOperationDelete,
								Value: ovsdb.OvsSet{
									GoSet: []any{
										ovsdb.UUID{
											GoUUID: lbhc.UUID,
										},
									},
								},
							},
						},
					},
					{
						Op:    ovsdb.OperationDelete,
						Table: ovnnb.LoadBalancerHealthCheckTable,
						Where: []ovsdb.Condition{
							{
								Column:   "_uuid",
								Function: ovsdb.ConditionEqual,
								Value: ovsdb.UUID{
									GoUUID: lbhc.UUID,
								},
							},
						},
					},
				},
			)
		},
	)

	t.Run("return ops is empty when delete non-existent load balancer health check",
		func(t *testing.T) {
			t.Parallel()

			ops, err := nbClient.DeleteLoadBalancerHealthCheckOp(lbName, vipNonExistent)
			require.NoError(t, err)
			require.Len(t, ops, 0)
		},
	)
}

func (suite *OvnClientTestSuite) testNewLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient = suite.ovnNBClient
		lbName   = "test-new-lb-hc"
		vip      = "4.4.4.4:80"
	)

	t.Run("create new load balancer health check", func(t *testing.T) {
		externals := map[string]string{"key1": "value1", "key2": "value2"}
		lbhc, err := nbClient.newLoadBalancerHealthCheck(lbName, vip, externals)
		require.ErrorContains(t, err, "not found load balancer")
		require.Nil(t, lbhc)

		err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
		require.NoError(t, err)

		lbhc, err = nbClient.newLoadBalancerHealthCheck(lbName, vip, externals)
		require.NoError(t, err)
		require.NotNil(t, lbhc)
		require.Equal(t, vip, lbhc.Vip)
		require.Equal(t, externals, lbhc.ExternalIDs)
		require.Equal(t, "20", lbhc.Options["timeout"])
		require.Equal(t, "5", lbhc.Options["interval"])
		require.Equal(t, "3", lbhc.Options["success_count"])
		require.Equal(t, "3", lbhc.Options["failure_count"])
	})

	t.Run("create with empty lb name", func(t *testing.T) {
		lbhc, err := nbClient.newLoadBalancerHealthCheck("", vip, nil)
		require.Error(t, err)
		require.Nil(t, lbhc)
		require.Contains(t, err.Error(), "the lb name is required")
	})

	t.Run("create with empty vip", func(t *testing.T) {
		lbhc, err := nbClient.newLoadBalancerHealthCheck(lbName, "", nil)
		require.Error(t, err)
		require.Nil(t, lbhc)
		require.Contains(t, err.Error(), "the vip endpoint is required")
	})

	t.Run("create existing health check", func(t *testing.T) {
		err := nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
		require.NoError(t, err)

		err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, nil)
		require.NoError(t, err)

		lbhc, err := nbClient.newLoadBalancerHealthCheck(lbName, vip, nil)
		require.NoError(t, err)
		require.Nil(t, lbhc)
	})
}
