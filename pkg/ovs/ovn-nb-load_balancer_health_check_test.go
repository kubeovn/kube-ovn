package ovs

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testAddLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		ovnClient        = suite.ovnClient
		lbName           = "test-create-lb-hc"
		vip              = "1.1.1.1:80"
		lbhc, lbhcRepeat *ovnnb.LoadBalancerHealthCheck
		err              error
	)

	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	_, lbhc, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)
	require.Equal(t, vip, lbhc.Vip)
	require.NotEmpty(t, lbhc.UUID)

	// should no err create lbhc repeatedly
	err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	_, lbhcRepeat, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)
	require.Equal(t, vip, lbhcRepeat.Vip)
	require.NotEmpty(t, lbhcRepeat.UUID)

	require.Equal(t, lbhc.UUID, lbhcRepeat.UUID)
}

func (suite *OvnClientTestSuite) testUpdateLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		ovnClient = suite.ovnClient
		lbName    = "test-update-lb-hc"
		vip       = "2.2.2.2:80"
		lbhc      *ovnnb.LoadBalancerHealthCheck
		err       error
	)

	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	_, lbhc, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)

	vip = "3.3.3.3:80"
	t.Run("update vip",
		func(t *testing.T) {
			lbhc.Vip = vip

			err = ovnClient.UpdateLoadBalancerHealthCheck(lbhc, &lbhc.Vip)
			require.NoError(t, err)

			_, lbhc, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
			require.NoError(t, err)

			require.Equal(t, vip, lbhc.Vip)
		},
	)
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		ovnClient = suite.ovnClient
		lbName    = "test-del-lb-hc"
		vip       = "1.1.1.11:80"
		err       error
	)

	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	err = ovnClient.DeleteLoadBalancerHealthCheck(lbName, vip)
	require.NoError(t, err)

	_, _, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerHealthChecks() {
	t := suite.T()
	t.Parallel()

	var (
		ovnClient    = suite.ovnClient
		lbNamePrefix = "test-del-lb-hcs"
		vipFormat    = "5.5.5.%d:80"
		lbhc         *ovnnb.LoadBalancerHealthCheck
		vips         []string
		lbName, vip  string
		err          error
	)

	for i := 0; i < 5; i++ {
		lbName = fmt.Sprintf("%s-%d", lbNamePrefix, i)
		vip = fmt.Sprintf(vipFormat, i+1)

		err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
		require.NoError(t, err)

		err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
		require.NoError(t, err)

		vips = append(vips, vip)

		_, lbhc, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
		require.NoError(t, err)
		require.NotNil(t, lbhc)
		require.Equal(t, vip, lbhc.Vip)

		err = ovnClient.LoadBalancerDeleteHealthCheck(lbName, lbhc.UUID)
		require.NoError(t, err)
	}

	err = ovnClient.DeleteLoadBalancerHealthChecks(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(vips, lbhc.Vip)
		},
	)
	require.NoError(t, err)

	for _, ip := range vips {
		_, _, err = ovnClient.GetLoadBalancerHealthCheck(lbName, ip, true)
		require.NoError(t, err)

		_, _, err = ovnClient.GetLoadBalancerHealthCheck(lbName, ip, false)
		require.Error(t, err)
	}
}

func (suite *OvnClientTestSuite) testGetLoadBalancerHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		ovnClient      = suite.ovnClient
		lbName         = "test-get-lb-hc"
		vip            = "1.1.1.22:80"
		vipNonExistent = "1.1.1.33:80"
		err            error
	)

	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
	require.NoError(t, err)

	t.Run("should return no err when found load balancer health check",
		func(t *testing.T) {
			t.Parallel()
			_, lbhc, err := ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
			require.NoError(t, err)
			require.Equal(t, vip, lbhc.Vip)
			require.NotEmpty(t, lbhc.UUID)
		},
	)

	t.Run("should return err when not found load balancer health check",
		func(t *testing.T) {
			t.Parallel()
			_, _, err := ovnClient.GetLoadBalancerHealthCheck(lbName, vipNonExistent, false)
			require.Error(t, err)
		},
	)

	t.Run("no err when not found load balancer health check and ignoreNotFound is true",
		func(t *testing.T) {
			t.Parallel()
			_, _, err := ovnClient.GetLoadBalancerHealthCheck(lbName, vipNonExistent, true)
			require.NoError(t, err)
		},
	)
}

func (suite *OvnClientTestSuite) testListLoadBalancerHealthChecks() {
	t := suite.T()
	t.Parallel()

	var (
		ovnClient    = suite.ovnClient
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

		err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
		require.NoError(t, err)

		err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
		require.NoError(t, err)

		vips = append(vips, vip)
	}

	t.Run("has no custom filter",
		func(t *testing.T) {
			t.Parallel()

			lbhcs, err := ovnClient.ListLoadBalancerHealthChecks(nil)
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

			t.Run("fliter by vip",
				func(t *testing.T) {
					t.Parallel()

					lbhcs, err := ovnClient.ListLoadBalancerHealthChecks(
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
		ovnClient      = suite.ovnClient
		lbName         = "test-del-lb-hc-op"
		vip            = "1.1.1.44:80"
		vipNonExistent = "1.1.1.55:80"
		lbhc           *ovnnb.LoadBalancerHealthCheck
		err            error
	)

	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.NotNil(t, lb)

	err = ovnClient.AddLoadBalancerHealthCheck(lbName, vip, nil)
	require.NoError(t, err)

	_, lbhc, err = ovnClient.GetLoadBalancerHealthCheck(lbName, vip, false)
	require.NoError(t, err)

	t.Run("normal delete",
		func(t *testing.T) {
			t.Parallel()

			ops, err := ovnClient.DeleteLoadBalancerHealthCheckOp(lbName, vip)
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
									GoSet: []interface{}{
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

			ops, err := ovnClient.DeleteLoadBalancerHealthCheckOp(lbName, vipNonExistent)
			require.NoError(t, err)
			require.Len(t, ops, 0)
		},
	)
}
