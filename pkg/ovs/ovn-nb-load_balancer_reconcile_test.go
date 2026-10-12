package ovs

import (
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func newResourceLoadBalancerTestClient(t *testing.T) *OVNNbClient {
	t.Helper()
	dbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, fmt.Sprintf("resource-lb-%d", time.Now().UnixNano()), dbModel, ovnnb.Schema())
	nbClient, err := newOvnNbClient(t, "unix:"+socket, 3)
	require.NoError(t, err)
	t.Cleanup(nbClient.Close)
	return nbClient
}

func resourceLoadBalancerFixture(t *testing.T, nbClient *OVNNbClient) LoadBalancerReconciliation {
	t.Helper()
	owner := map[string]string{"owner": "service-uid"}
	require.NoError(t, nbClient.CreateBareLogicalSwitch("subnet"))
	require.NoError(t, nbClient.CreateLogicalRouter("router"))
	for _, protocol := range []string{"tcp", "sctp"} {
		require.NoError(t, nbClient.ReconcileLoadBalancer(LoadBalancerConfig{
			Name: protocol, Protocol: protocol, ExternalIDs: owner, Options: map[string]string{"unrelated": "keep", "affinity_timeout": "42"},
		}))
		require.NoError(t, nbClient.LoadBalancerAddVip(protocol, "10.96.0.10:90", "10.0.0.2:9090"))
	}
	require.NoError(t, nbClient.LogicalRouterUpdateLoadBalancers("router", "insert", "tcp", "sctp"))
	require.NoError(t, nbClient.CreateLoadBalancer("legacy", "tcp"))
	for _, vip := range []string{"10.96.0.10:80", "10.96.0.10:81"} {
		require.NoError(t, nbClient.LoadBalancerAddVip("legacy", vip, "10.0.0.2:8080"))
	}
	require.NoError(t, nbClient.LoadBalancerAddVip("legacy", "10.96.0.99:80", "10.0.0.9:8080"))
	require.NoError(t, nbClient.LoadBalancerAddIPPortMapping("legacy", "10.96.0.10:80", map[string]string{"10.0.0.2": "old", "10.0.0.9": "keep"}))
	return LoadBalancerReconciliation{
		OwnerExternalIDs: owner,
		LoadBalancers: []LoadBalancerDesiredState{
			{
				Config:          LoadBalancerConfig{Name: "tcp", Protocol: "tcp", ExternalIDs: owner, DeleteOptions: []string{"affinity_timeout"}},
				VIPs:            map[string]string{"10.96.0.10:80": "10.0.0.3:8080", "10.96.0.10:81": "10.0.0.3:8081", "[fd00::10]:80": "[fd00::3]:8080"},
				IPPortMappings:  map[string]string{"10.0.0.3": "backend", "[fd00::3]": "backend-v6"},
				HealthChecks:    map[string]map[string]string{"10.96.0.10:80": {"subnet": "subnet"}},
				LogicalSwitches: []string{"subnet"},
			},
			{
				Config:          LoadBalancerConfig{Name: "udp", Protocol: "udp", ExternalIDs: owner},
				VIPs:            map[string]string{"10.96.0.10:53": "10.0.0.3:5353"},
				LogicalSwitches: []string{"subnet"}, LogicalRouters: []string{"router"},
			},
		},
		VIPDeletions:             []TemplateVIPDeletion{{LoadBalancer: "legacy", VIP: "10.96.0.10:80"}, {LoadBalancer: "legacy", VIP: "10.96.0.10:81"}},
		TemplateVariablePrefixes: []string{"service_"},
		TemplateVariables:        []ChassisTemplateVariableReconciliation{{Chassis: "node", Prefix: "service_", Variables: map[string]string{"service_vip": "10.96.0.10"}}},
	}
}

func TestReconcileResourceLoadBalancers(t *testing.T) {
	nbClient := newResourceLoadBalancerTestClient(t)
	reconciliation := resourceLoadBalancerFixture(t, nbClient)
	recorder := &recordingOVSDBClient{Client: nbClient.Client}
	nbClient.Client = recorder
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	require.Len(t, recorder.transactions, 1, "create, update, delete, migrations and attachments must share one transaction")

	tcp, err := nbClient.GetLoadBalancer("tcp", false)
	require.NoError(t, err)
	require.Equal(t, reconciliation.LoadBalancers[0].VIPs, tcp.Vips)
	require.Equal(t, reconciliation.LoadBalancers[0].IPPortMappings, tcp.IPPortMappings)
	require.Equal(t, map[string]string{"unrelated": "keep"}, tcp.Options)
	require.Len(t, tcp.HealthCheck, 1)
	udp, err := nbClient.GetLoadBalancer("udp", false)
	require.NoError(t, err)
	require.Equal(t, reconciliation.LoadBalancers[1].VIPs, udp.Vips)
	sctp, err := nbClient.GetLoadBalancer("sctp", true)
	require.NoError(t, err)
	require.Nil(t, sctp)
	ls, err := nbClient.GetLogicalSwitch("subnet", false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{tcp.UUID, udp.UUID}, ls.LoadBalancer)
	lr, err := nbClient.GetLogicalRouter("router", false)
	require.NoError(t, err)
	require.Equal(t, []string{udp.UUID}, lr.LoadBalancer)
	legacy, err := nbClient.GetLoadBalancer("legacy", false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"10.96.0.99:80": "10.0.0.9:8080"}, legacy.Vips)
	require.Equal(t, map[string]string{"10.0.0.9": "keep"}, legacy.IPPortMappings)

	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	require.Len(t, recorder.transactions, 1, "an unchanged resource must not submit another transaction")
}

func TestReconcileResourceLoadBalancersRollsBack(t *testing.T) {
	nbClient := newResourceLoadBalancerTestClient(t)
	reconciliation := resourceLoadBalancerFixture(t, nbClient)
	before, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	lrBefore, err := nbClient.GetLogicalRouter("router", false)
	require.NoError(t, err)
	recorder := &templateVIPTransactionClient{Client: nbClient.Client, fail: true}
	nbClient.Client = recorder
	require.Error(t, nbClient.ReconcileLoadBalancers(reconciliation))
	require.Len(t, recorder.transactions, 1)
	after, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.ElementsMatch(t, before, after, "all protocol LB changes must roll back")
	lrAfter, err := nbClient.GetLogicalRouter("router", false)
	require.NoError(t, err)
	require.Equal(t, lrBefore, lrAfter)
	variables := templateVIPVariables(t, nbClient, "node")
	require.Empty(t, variables, "template variables must roll back with the LB changes")
	recorder.fail = false
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation), "the complete resource should reconcile on retry")
}

func TestReconcileResourceLoadBalancersConcurrentCreate(t *testing.T) {
	nbClient := newResourceLoadBalancerTestClient(t)
	owner := map[string]string{"owner": "concurrent-service"}
	reconciliation := LoadBalancerReconciliation{OwnerExternalIDs: owner}
	for _, protocol := range []string{"tcp", "udp"} {
		reconciliation.LoadBalancers = append(reconciliation.LoadBalancers, LoadBalancerDesiredState{
			Config: LoadBalancerConfig{Name: protocol, Protocol: protocol, ExternalIDs: maps.Clone(owner)},
			VIPs:   map[string]string{"10.96.0.10:80": "10.0.0.3:8080"},
		})
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range 4 {
		wg.Go(func() { errs[i] = nbClient.ReconcileLoadBalancers(reconciliation) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			require.ErrorContains(t, err, "reconcile resource load balancers")
		}
	}
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	lbs, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.Len(t, lbs, 2, "concurrent retries must not create duplicate protocol LBs")
}

func TestReconcileResourceLoadBalancersDeleteAll(t *testing.T) {
	nbClient := newResourceLoadBalancerTestClient(t)
	reconciliation := resourceLoadBalancerFixture(t, nbClient)
	reconciliation.LoadBalancers = nil
	reconciliation.TemplateVariables = nil
	recorder := &recordingOVSDBClient{Client: nbClient.Client}
	nbClient.Client = recorder
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	require.Len(t, recorder.transactions, 1)
	for _, name := range []string{"tcp", "sctp"} {
		lb, err := nbClient.GetLoadBalancer(name, true)
		require.NoError(t, err)
		require.Nil(t, lb)
	}
	lr, err := nbClient.GetLogicalRouter("router", false)
	require.NoError(t, err)
	require.Empty(t, lr.LoadBalancer)
}

func TestReconcileResourceLoadBalancersRejectsStaleSnapshot(t *testing.T) {
	nbClient := newResourceLoadBalancerTestClient(t)
	reconciliation := resourceLoadBalancerFixture(t, nbClient)
	ops, err := nbClient.resourceLoadBalancerOps(reconciliation)
	require.NoError(t, err)
	require.NoError(t, nbClient.SetLoadBalancerExternalIDs("tcp", map[string]string{"concurrent": "keep"}))
	require.Error(t, nbClient.Transact("stale-resource-lb", ops))
	udp, err := nbClient.GetLoadBalancer("udp", true)
	require.NoError(t, err)
	require.Nil(t, udp, "a stale TCP snapshot must also roll back the UDP insertion")
	sctp, err := nbClient.GetLoadBalancer("sctp", false)
	require.NoError(t, err)
	require.NotNil(t, sctp, "a stale TCP snapshot must also roll back the SCTP deletion")
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	tcp, err := nbClient.GetLoadBalancer("tcp", false)
	require.NoError(t, err)
	require.Equal(t, "keep", tcp.ExternalIDs["concurrent"], "retry must preserve the concurrent metadata update")
}

func TestReconcileResourceLoadBalancersGuardsUnchangedRows(t *testing.T) {
	nbClient := newResourceLoadBalancerTestClient(t)
	reconciliation := resourceLoadBalancerFixture(t, nbClient)
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	reconciliation.LoadBalancers = append(reconciliation.LoadBalancers, LoadBalancerDesiredState{
		Config: LoadBalancerConfig{Name: "new-sctp", Protocol: "sctp", ExternalIDs: maps.Clone(reconciliation.OwnerExternalIDs)},
		VIPs:   map[string]string{"10.96.0.10:90": "10.0.0.3:9090"}, LogicalSwitches: []string{"subnet"},
	})
	ops, err := nbClient.resourceLoadBalancerOps(reconciliation)
	require.NoError(t, err)
	require.NoError(t, nbClient.DeleteLoadBalancer("tcp"))
	require.Error(t, nbClient.Transact("vanished-resource-lb", ops))
	sctp, err := nbClient.GetLoadBalancer("new-sctp", true)
	require.NoError(t, err)
	require.Nil(t, sctp, "a vanished unchanged TCP LB must roll back the new SCTP LB")
	require.NoError(t, nbClient.ReconcileLoadBalancers(reconciliation))
	tcp, err := nbClient.GetLoadBalancer("tcp", false)
	require.NoError(t, err)
	require.Equal(t, reconciliation.LoadBalancers[0].VIPs, tcp.Vips)
}
