package ovs

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testCreateLoadBalancer() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-create-lb"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, lbName, lb.Name)
	require.NotEmpty(t, lb.UUID)
	require.Equal(t, "tcp", *lb.Protocol)
	require.ElementsMatch(t, []string{"ip_dst"}, lb.SelectionFields)

	// should no err create lb repeatedly
	err = nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testUpdateLoadBalancer() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-update-lb"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "ip_dst")
	require.NoError(t, err)

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	t.Run("update vips", func(t *testing.T) {
		lb.Vips = map[string]string{
			"10.96.0.1:443":           "192.168.20.3:6443",
			"10.107.43.238:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
			"[fd00:10:96::e83f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
		}

		err := nbClient.UpdateLoadBalancer(lb, &lb.Vips)
		require.NoError(t, err)

		lb, err := nbClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Equal(t, map[string]string{
			"10.96.0.1:443":           "192.168.20.3:6443",
			"10.107.43.238:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
			"[fd00:10:96::e83f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
		}, lb.Vips)
	})

	t.Run("clear vips", func(t *testing.T) {
		lb.Vips = nil

		err := nbClient.UpdateLoadBalancer(lb, &lb.Vips)
		require.NoError(t, err)

		lb, err := nbClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Nil(t, lb.Vips)
	})
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancers() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbNamePrefix := "test-del-lbs"
	lbNames := make([]string, 0, 5)

	for i := range 5 {
		lbName := fmt.Sprintf("%s-%d", lbNamePrefix, i)
		err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
		require.NoError(t, err)

		lbNames = append(lbNames, lbName)
	}

	err := nbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return slices.Contains(lbNames, lb.Name)
	})
	require.NoError(t, err)

	for _, lbName := range lbNames {
		_, err := nbClient.GetLoadBalancer(lbName, false)
		require.ErrorContains(t, err, "not found load balancer")
	}
}

func (suite *OvnClientTestSuite) testDeleteLoadBalancer() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-del-lb"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	err = nbClient.DeleteLoadBalancer(lbName)
	require.NoError(t, err)

	_, err = nbClient.GetLoadBalancer(lbName, false)
	require.ErrorContains(t, err, "not found load balancer")
}

func (suite *OvnClientTestSuite) testGetLoadBalancer() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-get-lb"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	t.Run("should return no err when found load balancer", func(t *testing.T) {
		t.Parallel()
		lr, err := nbClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.Equal(t, lbName, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found load balancer", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.GetLoadBalancer("test-get-lb-non-existent", false)
		require.ErrorContains(t, err, "not found load balancer")
	})

	t.Run("no err when not found load balancer and ignoreNotFound is true", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.GetLoadBalancer("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLoadBalancers() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbNamePrefix := "test-list-lbs"
	lbNames := make([]string, 0, 3)
	protocol := []string{"tcp", "udp"}

	for i := range 3 {
		for _, p := range protocol {
			lbName := fmt.Sprintf("%s-%s-%d", lbNamePrefix, p, i)
			err := nbClient.CreateLoadBalancer(lbName, p, "")
			require.NoError(t, err)

			lbNames = append(lbNames, lbName)
		}
	}

	t.Run("has no custom filter", func(t *testing.T) {
		t.Parallel()

		lbs, err := nbClient.ListLoadBalancers(nil)
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
		t.Run("filter by name", func(t *testing.T) {
			t.Parallel()

			except := lbNames[1:]

			lbs, err := nbClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
				return !slices.Contains(except, lb.Name)
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

		t.Run("filter by tcp protocol", func(t *testing.T) {
			t.Parallel()

			for _, p := range protocol {
				lbs, err := nbClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
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

	nbClient := suite.ovnNBClient
	lbName := "test-del-lb-op"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	t.Run("normal delete", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.DeleteLoadBalancerOp(lbName)
		require.NoError(t, err)
		require.Len(t, ops, 1)

		require.Equal(t,
			ovsdb.Operation{
				Op:    ovsdb.OperationDelete,
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
			}, ops[0])
	})

	t.Run("return ops is empty when delete non-existent load balancer", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.DeleteLoadBalancerOp(lbName + "-non-existent")
		require.NoError(t, err)
		require.Len(t, ops, 0)
	})

	t.Run("Create load balancer when multiple load balancer exist", func(t *testing.T) {
		t.Parallel()

		lbName := "test-delete-lb-op-duplicate"
		// create load balancer
		lb1 := &ovnnb.LoadBalancer{
			UUID:     ovsclient.NamedUUID(),
			Name:     lbName,
			Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
		}
		ops, err := nbClient.Create(lb1)
		require.NoError(t, err)
		require.NotNil(t, ops)
		err = nbClient.Transact("lb-add", ops)
		require.NoError(t, err)

		lb2 := &ovnnb.LoadBalancer{
			UUID:     ovsclient.NamedUUID(),
			Name:     lbName,
			Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
		}
		ops, err = nbClient.Create(lb2)
		require.NoError(t, err)
		require.NotNil(t, ops)
		err = nbClient.Transact("lb-add", ops)
		require.NoError(t, err)

		ops, err = nbClient.DeleteLoadBalancerOp(lbName)
		require.ErrorContains(t, err, "more than one load balancer with same name")
		require.Nil(t, ops)
	})
}

func (suite *OvnClientTestSuite) testSetLoadBalancerAffinityTimeout() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-set-lb-affinity-timeout"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	oldOptions := make(map[string]string, 1)
	oldOptions["stateless"] = "true"
	lb.Options = oldOptions
	err = nbClient.UpdateLoadBalancer(lb, &lb.Options)
	require.NoError(t, err)

	expectedTimeout := 30
	t.Run("add new affinity timeout to load balancer options", func(t *testing.T) {
		err := nbClient.SetLoadBalancerAffinityTimeout(lbName, expectedTimeout)
		require.NoError(t, err)

		lb, err := nbClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Equal(t, lb.Options["affinity_timeout"], strconv.Itoa(expectedTimeout))
	})

	t.Run("add new affinityTimeout to load balancer options repeatedly", func(t *testing.T) {
		err := nbClient.SetLoadBalancerAffinityTimeout(lbName, expectedTimeout)
		require.NoError(t, err)

		lb, err := nbClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)

		require.Equal(t, lb.Options["affinity_timeout"], strconv.Itoa(expectedTimeout))
	})

	t.Run("set loadbalancer affinity timeout when multiple load balancer exist",
		func(t *testing.T) {
			lbName := "test-set-lb-affinity"
			// create load balancer
			lb1 := &ovnnb.LoadBalancer{
				UUID:     ovsclient.NamedUUID(),
				Name:     lbName,
				Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
			}
			ops, err := nbClient.Create(lb1)
			require.NoError(t, err)
			require.NotNil(t, ops)
			err = nbClient.Transact("lb-add", ops)
			require.NoError(t, err)

			lb2 := &ovnnb.LoadBalancer{
				UUID:     ovsclient.NamedUUID(),
				Name:     lbName,
				Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
			}
			ops, err = nbClient.Create(lb2)
			require.NoError(t, err)
			require.NotNil(t, ops)
			err = nbClient.Transact("lb-add", ops)
			require.NoError(t, err)

			err = nbClient.SetLoadBalancerAffinityTimeout(lbName, expectedTimeout)
			require.ErrorContains(t, err, "more than one load balancer with same name")
		},
	)
}

func (suite *OvnClientTestSuite) testLoadBalancerAddVip() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient           = suite.ovnNBClient
		lbName             = "test-lb-add-vip"
		vips, expectedVips map[string]string
		lb                 *ovnnb.LoadBalancer
		err                error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	_, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	vips = map[string]string{
		"10.96.0.2:443":           "192.168.20.3:6443",
		"10.107.43.237:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e82f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}
	expectedVips = make(map[string]string, len(vips))

	t.Run("add new vips to load balancer",
		func(t *testing.T) {
			for vip, backends := range vips {
				err = nbClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
				require.NoError(t, err)

				expectedVips[vip] = backends
			}

			lb, err = nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)

			require.Equal(t, lb.Vips, expectedVips)
		},
	)

	vips = map[string]string{
		"10.96.0.2:443":   "192.168.20.3:6443,192.168.20.4:6443",
		"10.96.0.112:143": "192.168.120.3:6443,192.168.120.4:6443",
	}

	t.Run("add new vips to load balancer repeatedly",
		func(t *testing.T) {
			for vip, backends := range vips {
				err := nbClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
				require.NoError(t, err)

				expectedVips[vip] = backends
			}

			lb, err = nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)

			require.Equal(t, lb.Vips, expectedVips)
		},
	)

	t.Run("add new vips to non-exist load balancer",
		func(t *testing.T) {
			err := nbClient.LoadBalancerAddVip("non-exist-lb", "10.96.0.2:443", "192.168.20.3:6443")
			require.ErrorContains(t, err, "not found load balancer")
		},
	)
}

func (suite *OvnClientTestSuite) testLoadBalancerAddHealthCheck() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("add health check to load balancer",
		func(t *testing.T) {
			lbName := "test-add-hc-lb"
			vips := map[string]string{
				"10.96.0.5:443":           "192.168.20.3:6443",
				"10.107.43.241:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
				"[fd00:10:96::e86f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
			}
			// create load balancer
			err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
			require.NoError(t, err)
			for vip, backends := range vips {
				backends := strings.Split(backends, ",")
				mappings := make(map[string]string)
				for _, backend := range backends {
					host, _, err := net.SplitHostPort(backend)
					require.NoError(t, err)
					mappings[host] = host
				}

				err := nbClient.LoadBalancerAddVip(lbName, vip, backends...)
				require.NoError(t, err)

				ignoreHealthCheck := false
				err = nbClient.LoadBalancerAddHealthCheck(lbName, vip, ignoreHealthCheck, mappings, nil)
				require.NoError(t, err)

				lb, err := nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)
				require.NotNil(t, lb.HealthCheck)
			}
		},
	)

	t.Run("create load balancer when multiple load balancer exist",
		func(t *testing.T) {
			lbName := "test-create-lb-duplicate"
			// create load balancer
			lb1 := &ovnnb.LoadBalancer{
				UUID:     ovsclient.NamedUUID(),
				Name:     lbName,
				Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
			}
			ops, err := nbClient.Create(lb1)
			require.NoError(t, err)
			require.NotNil(t, ops)
			err = nbClient.Transact("lb-add", ops)
			require.NoError(t, err)

			lb2 := &ovnnb.LoadBalancer{
				UUID:     ovsclient.NamedUUID(),
				Name:     lbName,
				Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
			}
			ops, err = nbClient.Create(lb2)
			require.NoError(t, err)
			require.NotNil(t, ops)
			err = nbClient.Transact("lb-add", ops)
			require.NoError(t, err)

			err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
			require.ErrorContains(t, err, "more than one load balancer with same name")
		},
	)
}

func (suite *OvnClientTestSuite) testLoadBalancerDeleteVip() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient    = suite.ovnNBClient
		lbName      = "test-lb-del-vip"
		vips        map[string]string
		deletedVips []string
		lb          *ovnnb.LoadBalancer
		err         error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	_, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	vips = map[string]string{
		"10.96.0.3:443":           "192.168.20.3:6443",
		"10.107.43.239:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e84f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}
	ignoreHealthCheck := true
	for vip, backends := range vips {
		err = nbClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
		require.NoError(t, err)
	}

	deletedVips = []string{
		"10.96.0.3:443",
		"[fd00:10:96::e84f]:8080",
		"10.96.0.100:1443", // non-existent vip
	}

	for _, vip := range deletedVips {
		err = nbClient.LoadBalancerDeleteVip(lbName, vip, ignoreHealthCheck)
		require.NoError(t, err)
		delete(vips, vip)
	}

	lb, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, vips, lb.Vips)

	err = nbClient.LoadBalancerAddHealthCheck(lbName, "10.107.43.239:8080", false, nil, nil)
	require.NoError(t, err)

	err = nbClient.LoadBalancerDeleteVip(lbName, "10.107.43.239:8080", false)
	require.NoError(t, err)

	// delete vip when lb.Vips is empty
	err = nbClient.LoadBalancerDeleteVip(lbName, "10.107.43.239:8080", false)
	require.NoError(t, err)

	// delete vip when multiple load balancer exist
	lbName = "test-delete-lb-vip"
	lb1 := &ovnnb.LoadBalancer{
		UUID:     ovsclient.NamedUUID(),
		Name:     lbName,
		Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
	}
	ops, err := nbClient.Create(lb1)
	require.NoError(t, err)
	require.NotNil(t, ops)
	err = nbClient.Transact("lb-add", ops)
	require.NoError(t, err)

	lb2 := &ovnnb.LoadBalancer{
		UUID:     ovsclient.NamedUUID(),
		Name:     lbName,
		Protocol: ptr.To(ovnnb.LoadBalancerProtocolTCP),
	}
	ops, err = nbClient.Create(lb2)
	require.NoError(t, err)
	require.NotNil(t, ops)
	err = nbClient.Transact("lb-add", ops)
	require.NoError(t, err)

	err = nbClient.LoadBalancerDeleteVip(lbName, "10.107.43.239:8080", ignoreHealthCheck)
	require.ErrorContains(t, err, "more than one load balancer with same name")
}

func (suite *OvnClientTestSuite) testLoadBalancerAddIPPortMapping() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient       = suite.ovnNBClient
		lbName         = "test-lb-add-ip-port-mapping"
		vips, mappings map[string]string
		lb             *ovnnb.LoadBalancer
		err            error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	_, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	vips = map[string]string{
		"10.96.0.4:443":           "192.168.20.3:6443",
		"10.107.43.240:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e85f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}
	t.Run("add new ip port mappings to load balancer",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list []string
					host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}
				err = nbClient.LoadBalancerAddVip(lbName, vip, list...)
				require.NoError(t, err)

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vip, mappings)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.Contains(t, lb.IPPortMappings, backend)
				}
			}
		},
	)

	vips = map[string]string{
		"10.96.0.4:443":   "192.168.20.3:6443,192.168.20.4:6443",
		"10.96.0.112:143": "192.168.120.3:6443,192.168.120.4:6443",
	}
	t.Run("add new ip port mappings to load balancer repeatedly",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list []string
					host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}
				err = nbClient.LoadBalancerAddVip(lbName, vip, list...)
				require.NoError(t, err)

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vip, mappings)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.Contains(t, lb.IPPortMappings, backend)
				}
			}
		},
	)

	t.Run("add nil port mappings to load balancer",
		func(t *testing.T) {
			err = nbClient.LoadBalancerAddIPPortMapping(lbName, "", nil)
			require.NoError(t, err)
		},
	)
}

func (suite *OvnClientTestSuite) testLoadBalancerDeleteIPPortMapping() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient       = suite.ovnNBClient
		lbName         = "test-lb-del-ip-port-mapping"
		vips, mappings map[string]string
		lb             *ovnnb.LoadBalancer
		err            error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	_, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	vips = map[string]string{
		"10.96.0.5:443":           "192.168.20.3:6443",
		"10.107.43.241:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e86f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}
	t.Run("delete ip port mappings from load balancer",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list        []string
					vhost, host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}

				vhost, _, err = net.SplitHostPort(vip)
				require.NoError(t, err)
				err = nbClient.LoadBalancerAddVip(lbName, vhost, strings.Split(backends, ",")...)
				require.NoError(t, err)

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vhost, mappings)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.Contains(t, lb.IPPortMappings, backend)
				}

				err = nbClient.LoadBalancerDeleteIPPortMapping(lbName, vhost)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.NotContains(t, lb.IPPortMappings, backend)
				}

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vhost, mappings)
				require.NoError(t, err)
			}
		},
	)

	vips = map[string]string{
		"10.96.0.5:443":   "192.168.20.3:6443,192.168.20.4:6443",
		"10.96.0.112:143": "192.168.120.3:6443,192.168.120.4:6443",
	}
	t.Run("delete ip port mappings from load balancer repeatedly",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list        []string
					vhost, host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}

				vhost, _, err = net.SplitHostPort(vip)
				require.NoError(t, err)
				err = nbClient.LoadBalancerAddVip(lbName, vhost, list...)
				require.NoError(t, err)

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vhost, mappings)
				require.NoError(t, err)

				err = nbClient.LoadBalancerDeleteIPPortMapping(lbName, vhost)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.NotContains(t, lb.IPPortMappings, backend)
				}
			}
		},
	)

	vips = map[string]string{
		"[fd00:10:96::e86f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}
	t.Run("delete ip port mappings from load balancer repeatedly",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list        []string
					vhost, host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}

				vhost, _, err = net.SplitHostPort(vip)
				require.NoError(t, err)
				err = nbClient.LoadBalancerAddVip(lbName, vhost, list...)
				require.NoError(t, err)

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vhost, mappings)
				require.NoError(t, err)

				err = nbClient.LoadBalancerDeleteIPPortMapping(lbName, vhost)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.NotContains(t, lb.IPPortMappings, backend)
				}
			}
		},
	)
}

func (suite *OvnClientTestSuite) testLoadBalancerWithHealthCheck() {
	t := suite.T()
	t.Parallel()

	var (
		nbClient       = suite.ovnNBClient
		lbName         = "test-lb-with-health-check"
		vips, mappings map[string]string
		lb             *ovnnb.LoadBalancer
		lbhc           *ovnnb.LoadBalancerHealthCheck
		lbhcID, vip    string
		err            error
	)

	err = nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	_, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)

	vips = map[string]string{
		"10.96.0.6:443":           "192.168.20.3:6443",
		"10.107.43.242:8080":      "10.244.0.15:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e87f]:8080": "[fc00::af4:a]:8080,[fc00::af4:b]:8080,[fc00::af4:c]:8080",
	}
	t.Run("add ip port mappings from load balancer",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list []string
					host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}
				err = nbClient.LoadBalancerAddVip(lbName, vip, list...)
				require.NoError(t, err)

				err = nbClient.LoadBalancerAddIPPortMapping(lbName, vip, mappings)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.Contains(t, lb.IPPortMappings, backend)
				}
			}
		},
	)

	vips = map[string]string{
		"10.96.0.6:443": "192.168.20.4:6443",
	}
	t.Run("update ip port mappings from load balancer repeatedly",
		func(t *testing.T) {
			for vip, backends := range vips {
				var (
					list []string
					host string
				)
				list = strings.Split(backends, ",")
				mappings = make(map[string]string)

				for _, backend := range list {
					host, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					mappings[host] = host
				}

				err = nbClient.LoadBalancerUpdateIPPortMapping(lbName, vip, mappings)
				require.NoError(t, err)

				lb, err = nbClient.GetLoadBalancer(lbName, false)
				require.NoError(t, err)

				for _, backend := range list {
					backend, _, err = net.SplitHostPort(backend)
					require.NoError(t, err)

					require.Contains(t, lb.IPPortMappings, backend)
				}
			}
		},
	)

	vip = "10.96.0.6:443"
	t.Run("add new health check to load balancer",
		func(t *testing.T) {
			err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
			require.NoError(t, err)

			lb, err = nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
			require.NoError(t, err)
			lbhcID = lbhc.UUID
			require.Contains(t, lb.HealthCheck, lbhcID)
		},
	)

	t.Run("add new health check to load balancer repeatedly",
		func(t *testing.T) {
			err = nbClient.AddLoadBalancerHealthCheck(lbName, vip, map[string]string{})
			require.NoError(t, err)
			lb, err = nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			_, lbhc, err = nbClient.GetLoadBalancerHealthCheck(lbName, vip, false)
			require.NoError(t, err)
			require.Contains(t, lb.HealthCheck, lbhcID)
		},
	)

	t.Run("delete health check from load balancer",
		func(t *testing.T) {
			err = nbClient.LoadBalancerDeleteHealthCheck(lbName, lbhcID)
			require.NoError(t, err)

			lb, err = nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.NotContains(t, lb.HealthCheck, lbhcID)
		},
	)

	t.Run("delete health check from load balancer repeatedly",
		func(t *testing.T) {
			err = nbClient.LoadBalancerDeleteHealthCheck(lbName, lbhcID)
			require.NoError(t, err)

			lb, err = nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.NotContains(t, lb.HealthCheck, lbhcID)
		},
	)

	t.Run("delete health check from non-exist load balancer",
		func(t *testing.T) {
			err = nbClient.LoadBalancerDeleteHealthCheck("non-exist-lbName", lbhcID)
			require.ErrorContains(t, err, "not found load balancer")
		},
	)
}

func (suite *OvnClientTestSuite) testLoadBalancerOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-lb-op"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	t.Run("no mutations", func(t *testing.T) {
		ops, err := nbClient.LoadBalancerOp(lbName)
		require.NoError(t, err)
		require.Empty(t, ops)
	})

	t.Run("single mutation", func(t *testing.T) {
		mutationFunc := func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.HealthCheck,
					Value:   []string{},
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		}

		ops, err := nbClient.LoadBalancerOp(lbName, mutationFunc)
		require.NoError(t, err)
		require.Len(t, ops, 1)
		require.Equal(t, ovsdb.OperationMutate, ops[0].Op)
	})

	t.Run("multiple mutations", func(t *testing.T) {
		mutationFunc1 := func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.HealthCheck,
					Value:   []string{},
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		}
		mutationFunc2 := func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.Options,
					Value:   map[string]string{"skip_snat": "true"},
					Mutator: ovsdb.MutateOperationInsert,
				},
			}
		}

		ops, err := nbClient.LoadBalancerOp(lbName, mutationFunc1, mutationFunc2)
		require.NoError(t, err)
		require.Len(t, ops, 1)
		require.Equal(t, ovsdb.OperationMutate, ops[0].Op)
		require.Len(t, ops[0].Mutations, 2)
	})

	t.Run("empty mutation", func(t *testing.T) {
		mutationFunc := func(_ *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{}
		}

		ops, err := nbClient.LoadBalancerOp(lbName, mutationFunc)
		require.NoError(t, err)
		require.Empty(t, ops)
	})

	t.Run("non-existent load balancer", func(t *testing.T) {
		mutationFunc := func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.HealthCheck,
					Value:   []string{},
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		}

		ops, err := nbClient.LoadBalancerOp("non-existent-lb", mutationFunc)
		require.Error(t, err)
		require.Nil(t, ops)
	})
}

func (suite *OvnClientTestSuite) testLoadBalancerUpdateHealthCheckOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lbName := "test-lb-update-hc-op"

	err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
	require.NoError(t, err)

	t.Run("empty lbhcUUIDs", func(t *testing.T) {
		ops, err := nbClient.LoadBalancerUpdateHealthCheckOp(lbName, []string{}, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Nil(t, ops)
	})
}
