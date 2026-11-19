package controller

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

type nodePortGCInterleavingClient struct {
	*table.Client
	beforeTransact func()
	transactions   int
	failed         []ovsdb.Operation
}

func (c *nodePortGCInterleavingClient) Transact(ctx context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	c.transactions++
	if c.beforeTransact != nil {
		before := c.beforeTransact
		c.beforeTransact = nil
		before()
	}
	results, err := c.TransactResults(ctx, ops...)
	for i, result := range results {
		if result.Error != "" && i < len(ops) {
			c.failed = append(c.failed, ops[i])
		}
	}
	return results, err
}

func TestServiceNodePortGCCommutesWithConcurrentCleanup(t *testing.T) {
	for _, mutation := range []string{
		"other load balancer", "other ACL", "same Service deletion", "same Service VIP update",
		"same Service deletion with health check", "other chassis variables",
		"other chassis variables with owned keys", "same Service deletion with chassis variables",
	} {
		t.Run(mutation, func(t *testing.T) {
			ctrl, nb, svc, endpoints := nodePortFixture(t)
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			ctrl.servicesLister = corelister.NewServiceLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
			writer := &ovs.OVNNbClient{Database: nb.Database}
			require.NoError(t, writer.CreateLoadBalancer("other-service", "tcp"))
			if mutation == "same Service deletion with health check" {
				name := serviceScopedLBNameForTrafficClass(svc, "TCP", serviceLBNodePortTraffic)
				require.NoError(t, writer.AddLoadBalancerHealthCheck(name, "192.0.2.10:30080", map[string]string{"subnet": "subnet"}))
			}
			if strings.Contains(mutation, "chassis variables") {
				variables := map[string]string{"unrelated": "before"}
				if mutation != "other chassis variables" {
					variables[serviceTrafficDistributionVariablePrefix(svc)+"vip"] = "10.0.0.2"
				}
				require.NoError(t, writer.ReconcileChassisTemplateVariables("node", "", variables))
			}
			client := &nodePortGCInterleavingClient{Client: nb.Client}
			client.beforeTransact = func() {
				switch mutation {
				case "other load balancer":
					require.NoError(t, writer.LogicalSwitchUpdateLoadBalancers("subnet", ovsdb.MutateOperationInsert, "other-service"))
				case "other ACL":
					require.NoError(t, writer.CreateAcls("subnet", ovs.LogicalSwitchKey, &ovnnb.ACL{
						UUID:      ovsclient.NamedUUID(),
						Direction: ovnnb.ACLDirectionToLport, Priority: 1000, Match: "ip", Action: ovnnb.ACLActionAllow,
					}))
				case "same Service deletion", "same Service deletion with health check", "same Service deletion with chassis variables":
					require.NoError(t, writer.ReconcileLoadBalancers(newServiceLoadBalancerReconciliation(svc).batch))
				case "same Service VIP update":
					name := serviceScopedLBNameForTrafficClass(svc, "TCP", serviceLBNodePortTraffic)
					require.NoError(t, writer.LoadBalancerAddVip(name, "192.0.2.10:30080", "10.0.0.9:8080"))
				case "other chassis variables", "other chassis variables with owned keys":
					require.NoError(t, writer.ReconcileChassisTemplateVariables("node", "unrelated", map[string]string{"unrelated": "after"}))
				}
			}
			nb.Database = table.NewDatabase(client, nb.Timeout, table.RetryPolicy{})
			err := ctrl.gcServiceNodePortIngressGuards(nil)
			for _, op := range client.failed {
				t.Logf("failed operation: %s %s columns=%v", op.Op, op.Table, op.Columns)
			}
			require.NoError(t, err, "GC must tolerate this interleaving without retrying the transaction")
			require.Equal(t, 1, client.transactions)
			guards, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
			require.NoError(t, err)
			require.Empty(t, guards)
			owned, err := nb.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
				return lb.ExternalIDs[serviceLBOwnerExternalID] == string(svc.UID)
			})
			require.NoError(t, err)
			require.Empty(t, owned)
			ls, err := nb.GetLogicalSwitch("subnet", false)
			require.NoError(t, err)
			switch mutation {
			case "other load balancer":
				other, err := nb.GetLoadBalancer("other-service", false)
				require.NoError(t, err)
				require.Equal(t, []string{other.UUID}, ls.LoadBalancer)
			case "other ACL":
				require.Len(t, ls.ACLs, 1, "GC must preserve the concurrently attached unrelated ACL")
			}
			if strings.Contains(mutation, "chassis variables") {
				var variables []ovnnb.ChassisTemplateVar
				require.NoError(t, nb.WhereCache(func(*ovnnb.ChassisTemplateVar) bool { return true }).List(t.Context(), &variables))
				require.Len(t, variables, 1)
				value := "after"
				if strings.HasPrefix(mutation, "same Service deletion") {
					value = "before"
				}
				require.Equal(t, map[string]string{"unrelated": value}, variables[0].Variables)
			}
		})
	}
}

func TestServiceNodePortGCRejectsOwnershipChanges(t *testing.T) {
	for _, resource := range []string{"load balancer", "ACL"} {
		t.Run(resource, func(t *testing.T) {
			ctrl, nb, svc, endpoints := nodePortFixture(t)
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			ctrl.servicesLister = corelister.NewServiceLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
			before, err := nb.GetLogicalSwitch("subnet", false)
			require.NoError(t, err)
			writer := &ovs.OVNNbClient{Database: nb.Database}
			client := &nodePortGCInterleavingClient{Client: nb.Client}
			client.beforeTransact = func() {
				switch resource {
				case "load balancer":
					name := serviceScopedLBNameForTrafficClass(svc, "TCP", serviceLBNodePortTraffic)
					require.NoError(t, writer.SetLoadBalancerExternalIDs(name, map[string]string{serviceLBOwnerExternalID: "replacement-uid"}))
				case "ACL":
					guards, err := writer.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
					require.NoError(t, err)
					require.Len(t, guards, 1)
					guard := guards[0]
					guard.ExternalIDs = maps.Clone(guard.ExternalIDs)
					guard.ExternalIDs[serviceLBOwnerExternalID] = "replacement-uid"
					require.NoError(t, writer.UpdateACL(&guard, &guard.ExternalIDs))
				}
			}
			nb.Database = table.NewDatabase(client, nb.Timeout, table.RetryPolicy{})
			require.Error(t, ctrl.gcServiceNodePortIngressGuards(nil), "GC must not detach or delete a reassigned resource")
			require.Equal(t, 1, client.transactions)
			after, err := nb.GetLogicalSwitch("subnet", false)
			require.NoError(t, err)
			require.ElementsMatch(t, before.LoadBalancer, after.LoadBalancer)
			require.ElementsMatch(t, before.ACLs, after.ACLs)
		})
	}
}
