package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

func TestControllerProviderSharedHealthCheckDetach(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	c := &Controller{OVNNbTables: nbClient}
	const vip = "192.0.2.1:80"
	for _, name := range []string{"owner-a", "owner-b"} {
		require.NoError(t, nbClient.CreateLoadBalancer(name, "tcp"))
	}
	require.NoError(t, nbClient.LoadBalancerAddHealthCheck("owner-a", vip, false, nil, nil))
	_, check, err := nbClient.GetLoadBalancerHealthCheck("owner-a", vip, false)
	require.NoError(t, err)
	ops, err := nbClient.LoadBalancerUpdateHealthCheckOp("owner-b", []string{check.UUID}, ovsdb.MutateOperationInsert)
	require.NoError(t, err)
	require.NoError(t, nbClient.Transact("share-health-check", ops))
	for range 2 {
		require.NoError(t, c.deleteLoadBalancerHealthCheck("owner-a", check.UUID))
		first, err := nbClient.GetLoadBalancer("owner-a", false)
		require.NoError(t, err)
		require.Empty(t, first.HealthCheck)
		_, remaining, err := nbClient.GetLoadBalancerHealthCheck("owner-b", vip, false)
		require.NoError(t, err)
		require.Equal(t, check, remaining, "detaching one owner must preserve the shared health check")
	}
}

type healthCheckTransactionBackend struct {
	*table.Client
	operations [][]ovsdb.Operation
	fail       bool
}

func (b *healthCheckTransactionBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	b.operations = append(b.operations, operations)
	if b.fail {
		for _, operation := range operations {
			for _, mutation := range operation.Mutations {
				if mutation.Column == "vips" {
					// Fail VIP deletion after any earlier cleanup mutations.
					operations = append(operations, ovsdb.Operation{Op: ovsdb.OperationAbort})
					return b.TransactResults(ctx, operations...)
				}
			}
		}
	}
	return b.TransactResults(ctx, operations...)
}

func seedProviderHealthCheckVIP(t *testing.T, nbClient *ovs.OVNNbClient, healthCheck bool) {
	t.Helper()
	const vip = "192.0.2.1:80"
	require.NoError(t, nbClient.CreateLoadBalancer("vip-lb", "tcp"))
	require.NoError(t, nbClient.LoadBalancerAddVip("vip-lb", vip, "10.0.0.1:8080", "10.0.0.2:8080"))
	require.NoError(t, nbClient.LoadBalancerAddVip("vip-lb", "192.0.2.2:80", "10.0.0.2:9090"))
	require.NoError(t, nbClient.LoadBalancerAddHealthCheck("vip-lb", vip, !healthCheck, map[string]string{
		"10.0.0.1": "backend-a", "10.0.0.2": "backend-shared",
	}, nil))
	lb, err := nbClient.GetLoadBalancer("vip-lb", false)
	require.NoError(t, err)
	require.NoError(t, nbClient.Table(lb).Mutate(t.Context(), "seed-vip-metadata", lb, model.Mutation{
		Field: &lb.ExternalIDs, Value: map[string]string{localExternalVIPKeyPrefix + vip: "vip-node"}, Mutator: ovsdb.MutateOperationInsert,
	}))
}

func TestControllerProviderDeleteVIPHealthCheckAtomic(t *testing.T) {
	for _, healthCheck := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("health-check-%t/failure-%t", healthCheck, fail), func(t *testing.T) {
				nbClient := newInMemoryOVNNbClient(t)
				seedProviderHealthCheckVIP(t, nbClient, healthCheck)
				before, err := nbClient.GetLoadBalancer("vip-lb", false)
				require.NoError(t, err)
				checksBefore, err := nbClient.ListLoadBalancerHealthChecks(nil)
				require.NoError(t, err)
				backend := &healthCheckTransactionBackend{Client: nbClient.Client, fail: fail}
				c := &Controller{OVNNbTables: table.NewDatabase(backend, 3*time.Second, table.RetryPolicy{})}
				err = c.deleteLoadBalancerVIP("vip-lb", "192.0.2.1:80", true)
				if fail {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Len(t, backend.operations, 1, "VIP, mapping, metadata and health check cleanup must share one transaction")
				after, err := nbClient.GetLoadBalancer("vip-lb", false)
				require.NoError(t, err)
				checksAfter, err := nbClient.ListLoadBalancerHealthChecks(nil)
				require.NoError(t, err)
				if fail {
					require.Equal(t, before, after)
					require.Equal(t, checksBefore, checksAfter)
					return
				}
				require.Equal(t, map[string]string{"192.0.2.2:80": "10.0.0.2:9090"}, after.Vips)
				require.Equal(t, map[string]string{"10.0.0.2": "backend-shared"}, after.IPPortMappings)
				require.NotContains(t, after.ExternalIDs, localExternalVIPKeyPrefix+"192.0.2.1:80")
				require.Empty(t, after.HealthCheck)
				require.Empty(t, checksAfter)
			})
		}
	}
}
