package ovs

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateLoadBalancer() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-create-lb"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, lbName, lb.Name)
	require.NotEmpty(t, lb.UUID)
	require.Equal(t, "tcp", *lb.Protocol)
	require.ElementsMatch(t, []string{"ip_dst"}, lb.SelectionFields)

	// should no err create lb repeatedly
	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testUpdateLoadBalancer() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-update-lb"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	t.Run("update vips", func(t *testing.T) {
		lb.Vips = map[string]string{
			"10.96.0.1:443":           "192.168.20.3:6443",
			"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
			"[fd00:10:96::e82f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
		}

		err := ovnClient.UpdateLoadBalancer(lb, &lb.Vips)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Equal(t, map[string]string{
			"10.96.0.1:443":           "192.168.20.3:6443",
			"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
			"[fd00:10:96::e82f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
		}, lb.Vips)
	})

	t.Run("clear vips", func(t *testing.T) {
		lb.Vips = nil

		err := ovnClient.UpdateLoadBalancer(lb, &lb.Vips)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Nil(t, lb.Vips)
	})
}

func (suite *OvnClientTestSuite) testLoadBalancerAddVip() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-lb-add-vip"

	vips := map[string]string{
		"10.96.0.1:443":           "192.168.20.3:6443",
		"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e82f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	_, err = ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	expectedVips := make(map[string]string, len(vips))
	t.Run("add new vips to load balancer", func(t *testing.T) {
		for vip, backends := range vips {
			err := ovnClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
			require.NoError(t, err)

			lb, err := ovnClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)

			expectedVips[vip] = backends
			require.Equal(t, lb.Vips, expectedVips)
		}
	})

	vips = map[string]string{
		"10.96.0.1:443":   "192.168.20.3:6443,192.168.20.4:6443",
		"10.96.0.112:143": "192.168.120.3:6443,192.168.120.4:6443",
	}
	t.Run("add new vips to load balancer repeatedly", func(t *testing.T) {
		for vip, backends := range vips {
			err := ovnClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
			require.NoError(t, err)

			lb, err := ovnClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)

			expectedVips[vip] = backends
			require.Equal(t, expectedVips, lb.Vips)
		}
	})
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancers() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbNamePrefix := "test-del-lbs"
	lbNames := make([]string, 0, 5)

	for i := 0; i < 5; i++ {
		lbName := fmt.Sprintf("%s-%d", lbNamePrefix, i)
		err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
		require.NoError(t, err)

		lbNames = append(lbNames, lbName)
	}

	err := ovnClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return util.ContainsString(lbNames, lb.Name)
	})
	require.NoError(t, err)

	for _, lbName := range lbNames {
		_, err := ovnClient.GetLoadBalancer(lbName, false)
		require.ErrorContains(t, err, "not found load balancer")
	}
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancer() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-del-lb"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	err = ovnClient.DeleteLoadBalancer(lbName)
	require.NoError(t, err)

	_, err = ovnClient.GetLoadBalancer(lbName, false)
	require.ErrorContains(t, err, "not found load balancer")
}

func (suite *OvnClientTestSuite) testLoadBalancerDeleteVip() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-lb-del-vip"

	vips := map[string]string{
		"10.96.0.1:443":           "192.168.20.3:6443",
		"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e82f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	for vip, backends := range vips {
		err = ovnClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
		require.NoError(t, err)
	}

	deletedVips := []string{
		"10.96.0.1:443",
		"[fd00:10:96::e82f]:8080",
		"10.96.0.100:1443", // non-existent vip
	}
	for _, vip := range deletedVips {
		err = ovnClient.LoadBalancerDeleteVip(lbName, vip)
		require.NoError(t, err)
		delete(vips, vip)
	}

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, vips, lb.Vips)
}

func (suite *OvnClientTestSuite) testGetLoadBalancer() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-get-lb"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
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
	lbNames := make([]string, 0, 3)
	protocol := []string{"tcp", "udp"}

	for i := 0; i < 3; i++ {
		for _, p := range protocol {
			lbName := fmt.Sprintf("%s-%s-%d", lbNamePrefix, p, i)
			err := ovnClient.CreateLoadBalancer(lbName, p, "")
			require.NoError(t, err)

			lbNames = append(lbNames, lbName)
		}
	}

	t.Run("has no custom filter", func(t *testing.T) {
		t.Parallel()

		lbs, err := ovnClient.ListLoadBalancers(nil)
		require.NoError(t, err)
		require.NotEmpty(t, lbs)

		newLbNames := make([]string, 0, 3)
		for _, lb := range lbs {
			if !strings.Contains(lb.Name, lbNamePrefix) {
				continue
			}
			newLbNames = append(newLbNames, lb.Name)
		}

		require.ElementsMatch(t, lbNames, newLbNames)
	})

	t.Run("has custom filter", func(t *testing.T) {
		t.Parallel()
		t.Run("fliter by name", func(t *testing.T) {
			t.Parallel()

			except := lbNames[1:]

			lbs, err := ovnClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
				return !util.ContainsString(except, lb.Name)
			})
			require.NoError(t, err)
			require.NotEmpty(t, lbs)

			newLbNames := make([]string, 0, 3)
			for _, lb := range lbs {
				if !strings.Contains(lb.Name, lbNamePrefix) {
					continue
				}
				newLbNames = append(newLbNames, lb.Name)
			}

			require.ElementsMatch(t, lbNames[:1], newLbNames)
		})

		t.Run("fliter by tcp protocol", func(t *testing.T) {
			t.Parallel()

			for _, p := range protocol {
				lbs, err := ovnClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
					return *lb.Protocol == p
				})
				require.NoError(t, err)
				require.NotEmpty(t, lbs)

				for _, lb := range lbs {
					if !strings.Contains(lb.Name, lbNamePrefix) {
						continue
					}

					require.Equal(t, p, *lb.Protocol)
				}
			}
		})
	})
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancerOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-del-lb-op"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
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

func (suite *OvnClientTestSuite) testSetLoadBalancerAffinityTimeout() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lbName := "test-set-lb-affinity-timeout"

	err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	lb, err := ovnClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	oldOptions := make(map[string]string, 1)
	oldOptions["stateless"] = "true"
	lb.Options = oldOptions
	err = ovnClient.UpdateLoadBalancer(lb, &lb.Options)
	require.NoError(t, err)

	expectedTimeout := 30
	t.Run("add new affinity timeout to load balancer options", func(t *testing.T) {
		err := ovnClient.SetLoadBalancerAffinityTimeout(lbName, expectedTimeout)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Equal(t, lb.Options["affinity_timeout"], strconv.Itoa(expectedTimeout))
	})

	t.Run("add new affinityTimeout to load balancer options repeatedly", func(t *testing.T) {
		err := ovnClient.SetLoadBalancerAffinityTimeout(lbName, expectedTimeout)
		require.NoError(t, err)

		lb, err := ovnClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Equal(t, lb.Options["affinity_timeout"], strconv.Itoa(expectedTimeout))
	})
}
