package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testCreateLoadBalancer() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-create-lb"
	vips := map[string]string{
		"10.96.0.1:443":           "192.168.20.3:6443",
		"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e82f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
	}

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst", vips)
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, lbName, lb.Name)
	require.NotEmpty(t, lb.UUID)
	require.Equal(t, "tcp", *lb.Protocol)
	require.Equal(t, []string{"ip_dst"}, lb.SelectionFields)
	require.Equal(t, vips, lb.Vips)

	// should no err create lb repeatedly
	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst", vips)
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancers() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbNamePrefix := "test-del-lb"
	lbNames := make([]string, 0, 5)

	for i := 0; i < 5; i++ {
		lbName := fmt.Sprintf("%s-%d", lbNamePrefix, i)
		err := ovnClient.CreateLoadBalancer(lbName, "tcp", "", nil)
		require.NoError(t, err)

		lbNames = append(lbNames, lbName)
	}

	// add non-existent lb
	lbNames = append(lbNames, "73bbe5d4-2b9b-47d0-aba8-94e8694188a2")

	err := ovnClient.DeleteLoadBalancers(lbNames...)
	require.NoError(t, err)

	for _, lbName := range lbNames {
		_, err := ovnClient.GetLoadBalancer(lbName, false)
		require.ErrorContains(t, err, "not found load balancer")
	}
}

func (suite *OvnClientTestSuite) testGetLoadBalancer() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-get-lb"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "", nil)
	require.NoError(t, err)

	t.Run("should return no err when found load balancer", func(t *testing.T) {
		t.Parallel()
		lr, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, lbName, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found load balancer", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.GetLoadBalancer("test-get-lb-non-existent", false)
		require.ErrorContains(t, err, "not found load balancer")
	})

	t.Run("no err when not found load balancerand ignoreNotFound is true", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.GetLoadBalancer("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLoadBalancers() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbNamePrefix := "test-list-lb"
	lbNames := make([]string, 0, 5)

	for i := 0; i < 5; i++ {
		lbName := fmt.Sprintf("%s-%d", lbNamePrefix, i)
		err := ovnClient.CreateLoadBalancer(lbName, "tcp", "", nil)
		require.NoError(t, err)

		lbNames = append(lbNames, lbName)
	}

	lbs, err := ovnClient.ListLoadBalancers()
	require.NoError(t, err)
	require.NotEmpty(t, lbs)

	for _, lb := range lbs {
		if !strings.Contains(lb.Name, lbNamePrefix) {
			continue
		}
		require.Contains(t, lbNames, lb.Name)
	}
}

func (suite *OvnClientTestSuite) testLoadBalancerUpdateVips() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-update-lb-vips"

	vips := map[string]string{
		"10.96.0.1:443":           "192.168.20.3:6443",
		"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e82f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
	}

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "", vips)
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	fmt.Println(lb.UUID)
	t.Run("add new vips to load balancer", func(t *testing.T) {
		err := ovnClient.LoadBalancerUpdateVips(lbName, vips, true)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, vips, lb.Vips)
	})

	t.Run("add new vips to load balancer repeatedly", func(t *testing.T) {
		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, vips, lb.Vips)

		err = ovnClient.LoadBalancerUpdateVips(lbName, vips, true)
		require.NoError(t, err)

		lb, err = ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, vips, lb.Vips)
	})

	t.Run("del vips from load balancer", func(t *testing.T) {
		delVips := map[string]string{
			"10.96.0.1:443":           "192.168.20.3:6443",
			"[fd00:10:96::e82f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
		}

		err = ovnClient.LoadBalancerUpdateVips(lbName, delVips, false)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"10.107.43.237:8080": "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		}, lb.Vips)
	})

	t.Run("should no error when del non-existent vips from load balancer", func(t *testing.T) {
		delVips := map[string]string{
			"10.96.10.100:443": "192.168.100.31:6443",
		}

		err = ovnClient.LoadBalancerUpdateVips(lbName, delVips, false)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"10.107.43.237:8080": "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		}, lb.Vips)
	})
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-del-lb-op"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "", nil)
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	t.Run("normal delete", func(t *testing.T) {
		t.Parallel()

		ops, err := ovnClient.DeleteLoadBalancerOp(lbName)
		require.NoError(t, err)
		require.Len(t, ops, 1)

		require.Equal(t,
			ovsdb.Operation{
				Op:    "delete",
				Table: "Load_Balancer",
				Where: []ovsdb.Condition{
					{
						Column:   "_uuid",
						Function: "==",
						Value: ovsdb.UUID{
							GoUUID: lb.UUID,
						},
					},
				},
			}, ops[0])
	})

	t.Run("return ops is empty when delete non-existent load balancer", func(t *testing.T) {
		t.Parallel()

		ops, err := ovnClient.DeleteLoadBalancerOp(lbName + "-non-existent")
		require.NoError(t, err)
		require.Len(t, ops, 0)
	})
}
