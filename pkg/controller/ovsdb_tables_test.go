package controller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestMatchesExternalIDs(t *testing.T) {
	tests := []struct {
		name     string
		actual   map[string]string
		expected map[string]string
		want     bool
	}{
		{name: "empty selector", actual: map[string]string{"vendor": "kube-ovn"}, want: true},
		{name: "exact value", actual: map[string]string{"vendor": "kube-ovn"}, expected: map[string]string{"vendor": "kube-ovn"}, want: true},
		{name: "key only", actual: map[string]string{"node": "node-1"}, expected: map[string]string{"node": ""}, want: true},
		{name: "missing key", actual: map[string]string{}, expected: map[string]string{"node": ""}, want: false},
		{name: "different value", actual: map[string]string{"vendor": "other"}, expected: map[string]string{"vendor": "kube-ovn"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesExternalIDs(test.actual, test.expected); got != test.want {
				t.Fatalf("matchesExternalIDs(%v, %v) = %v, want %v", test.actual, test.expected, got, test.want)
			}
		})
	}
}

func TestGenericACLRejectsInvalidPriority(t *testing.T) {
	require.Panics(t, func() {
		genericACL("parent", ovnnb.ACLDirectionToLport, "invalid", "ip", ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil)
	})
	require.Panics(t, func() {
		genericACL("parent", ovnnb.ACLDirectionToLport, 1.5, "ip", ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil)
	})
}

func TestControllerTableProviderReadsAssociatedRows(t *testing.T) {
	logicalRouter := &ovnnb.LogicalRouter{
		UUID:         "lr-1",
		Name:         "router-1",
		Policies:     []string{"policy-1"},
		Nat:          []string{"nat-1"},
		StaticRoutes: []string{"route-1"},
	}
	policy := &ovnnb.LogicalRouterPolicy{
		UUID:        "policy-1",
		Priority:    100,
		Match:       "ip4.src == 10.0.0.0/24",
		ExternalIDs: map[string]string{"vendor": "kube-ovn"},
	}
	route := &ovnnb.LogicalRouterStaticRoute{
		UUID:        "route-1",
		RouteTable:  "main",
		IPPrefix:    "10.1.0.0/16",
		Nexthop:     "10.0.0.1",
		ExternalIDs: map[string]string{"vendor": "kube-ovn"},
	}
	lb := &ovnnb.LoadBalancer{UUID: "lb-1", Name: "lb-1"}
	lbhc := &ovnnb.LoadBalancerHealthCheck{UUID: "lbhc-1", Vip: "10.0.0.1:80"}
	nat := &ovnnb.NAT{UUID: "nat-1", LogicalIP: "10.2.0.5", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.5"}
	bfd := &ovnnb.BFD{UUID: "bfd-1", ExternalIDs: map[string]string{"owner": "egress"}}
	logicalSwitch := &ovnnb.LogicalSwitch{UUID: "ls-1", Name: "switch-1"}
	logicalSwitchPort := &ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "port-1"}
	portGroup := &ovnnb.PortGroup{UUID: "pg-1", Name: "group-1"}
	chassis := &ovnsb.Chassis{
		UUID:        "chassis-1",
		Name:        "chassis-1",
		Hostname:    "node-1",
		ExternalIDs: map[string]string{"vendor": "kube-ovn"},
	}
	database := compat.NewDatabase(newTableBackend(logicalRouter, policy, route, lb, lbhc, nat, bfd, logicalSwitch, logicalSwitchPort, portGroup, chassis), time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database, OVNSbTables: database}

	policies, err := controller.listLogicalRouterPolicies("router-1", 100, map[string]string{"vendor": "kube-ovn"}, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, "policy-1", policies[0].UUID)

	routes, err := controller.listLogicalRouterStaticRoutes("router-1", nil, nil, "", map[string]string{"vendor": "kube-ovn"})
	require.NoError(t, err)
	require.Len(t, routes, 1)
	require.Equal(t, "route-1", routes[0].UUID)

	loadBalancers, err := controller.listLoadBalancers(func(row *ovnnb.LoadBalancer) bool { return row.Name == "lb-1" })
	require.NoError(t, err)
	require.Len(t, loadBalancers, 1)
	foundLoadBalancer, err := controller.getLoadBalancer("lb-1", false)
	require.NoError(t, err)
	require.Equal(t, "lb-1", foundLoadBalancer.UUID)

	healthChecks, err := controller.listLoadBalancerHealthChecks(func(row *ovnnb.LoadBalancerHealthCheck) bool { return row.Vip == "10.0.0.1:80" })
	require.NoError(t, err)
	require.Len(t, healthChecks, 1)

	bfdRows, err := controller.findBFD(map[string]string{"owner": "egress"})
	require.NoError(t, err)
	require.Len(t, bfdRows, 1)

	exists, err := controller.natExists("router-1", ovnnb.NATTypeSNAT, "192.0.2.5", "10.2.0.5")
	require.NoError(t, err)
	require.True(t, exists)

	switchNames, err := controller.listLogicalSwitchNames(false, nil)
	require.NoError(t, err)
	require.Contains(t, switchNames, "switch-1")
	exists, err = controller.logicalSwitchPortExists("port-1")
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = controller.portGroupExists("group-1")
	require.NoError(t, err)
	require.True(t, exists)

	foundChassis, err := controller.getChassisByHost("node-1")
	require.NoError(t, err)
	require.Equal(t, "chassis-1", foundChassis.Name)
	foundChassis, err = controller.getChassis("chassis-1", false)
	require.NoError(t, err)
	require.Equal(t, "node-1", foundChassis.Hostname)
	kubeOvnChassises, err := controller.listKubeOvnChassises()
	require.NoError(t, err)
	require.Len(t, kubeOvnChassises, 1)
}

func TestControllerTableProviderWrites(t *testing.T) {
	backend := newTableBackend()
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.createAddressSet("as-1", map[string]string{"owner": "test"}))
	require.NoError(t, controller.createPortGroup("pg-1", map[string]string{"owner": "test"}))
	require.NoError(t, controller.createLogicalRouter("lr-1"))
	require.NoError(t, controller.createLoadBalancer("lb-1", ovnnb.LoadBalancerProtocolTCP))

	require.Equal(t, 4, backend.createCalls)
	require.Equal(t, 4, backend.transactCalls)
}

func TestControllerTableProviderFieldUpdates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LoadBalancer{
			UUID:    "lb-1",
			Name:    "lb-1",
			Options: map[string]string{"owner": "test"},
		},
		&ovnnb.LogicalSwitchPort{
			UUID:        "lsp-1",
			Name:        "port-1",
			ExternalIDs: map[string]string{"owner": "test"},
		},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.setLoadBalancerAffinityTimeout("lb-1", 30))
	require.NoError(t, controller.setLoadBalancerPreferLocalBackend("lb-1", true))
	require.NoError(t, controller.setLoadBalancerCtFlush("lb-1", true))
	require.NoError(t, controller.setLogicalSwitchPortExternalIDs("port-1", map[string]string{"managed": "true"}))
	require.NoError(t, controller.setLogicalSwitchPortVlanTag("port-1", 100))
	require.Equal(t, 5, backend.updateCalls)
	require.Equal(t, 5, backend.transactCalls)

	require.Error(t, controller.setLogicalSwitchPortVlanTag("port-1", 4096))
	require.Equal(t, 5, backend.updateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderCollectionUpdates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.AddressSet{UUID: "as-1", Name: "as-1", Addresses: []string{"10.0.0.0/24"}},
		&ovnnb.PortGroup{UUID: "pg-1", Name: "pg-1", Ports: []string{"lsp-old"}},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "port-1"},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-2", Name: "port-2"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	// Equivalent CIDRs are normalized and do not result in a transaction.
	require.NoError(t, controller.updateAddressSetAddresses("as-1", "10.0.0.1/24", "10.0.0.0/24"))
	require.Equal(t, 0, backend.updateCalls)
	require.NoError(t, controller.updateAddressSetAddresses("as-1", "10.0.0.0/24", "10.0.1.0/24"))
	require.Equal(t, 1, backend.updateCalls)

	require.NoError(t, controller.setPortGroupPorts("pg-1", []string{"port-1"}))
	require.NoError(t, controller.updatePortGroupPorts("pg-1", ovsdb.MutateOperationInsert, "port-2"))
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 3, backend.transactCalls)
}

func TestControllerTableProviderRelationUpdates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "ls-1"},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1"},
		&ovnnb.LoadBalancer{UUID: "lb-1", Name: "lb-1", ExternalIDs: map[string]string{"owner": "test"}},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "port-1"},
		&ovnnb.PortGroup{UUID: "pg-1", Name: "pg-1", Ports: []string{"lsp-1"}},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.updateLogicalSwitchLoadBalancers("ls-1", ovsdb.MutateOperationInsert, "lb-1"))
	require.NoError(t, controller.updateLogicalRouterLoadBalancers("lr-1", ovsdb.MutateOperationInsert, "lb-1"))
	require.NoError(t, controller.updateLogicalSwitchOtherConfig("ls-1", ovsdb.MutateOperationInsert, map[string]string{"mcast_snoop": "true"}))
	require.NoError(t, controller.setLoadBalancerExternalTrafficLocal("lb-1", "10.0.0.10:80", "node-worker-1"))
	require.NoError(t, controller.removePortFromPortGroups("port-1"))

	require.Equal(t, 5, backend.mutateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderNBGlobalUpdates(t *testing.T) {
	backend := newTableBackend(&ovnnb.NBGlobal{
		UUID:    "nb-global-1",
		Options: map[string]string{"stale": "value"},
	})
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.setNBGlobalOption("node_local_dns_ip", "10.96.0.10", true))
	require.NoError(t, controller.setNBGlobalOption("stale", "", false))
	require.NoError(t, controller.setNBGlobalIPSec(true))
	require.Equal(t, 3, backend.updateCalls)
	require.Equal(t, 3, backend.transactCalls)
}

func TestControllerTableProviderParentReferenceDeletes(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalRouter{
			UUID:     "lr-1",
			Name:     "lr-1",
			Policies: []string{"policy-1", "policy-2"},
			Nat:      []string{"nat-1", "nat-2"},
		},
		&ovnnb.LogicalRouterPolicy{
			UUID:        "policy-1",
			Priority:    100,
			Nexthop:     new("10.0.0.1"),
			ExternalIDs: map[string]string{"owner": "test"},
		},
		&ovnnb.LogicalRouterPolicy{
			UUID:        "policy-2",
			Priority:    200,
			Nexthops:    []string{"10.0.0.2"},
			ExternalIDs: map[string]string{"owner": "test"},
		},
		&ovnnb.NAT{UUID: "nat-1", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.1", LogicalIP: "10.0.0.1"},
		&ovnnb.NAT{UUID: "nat-2", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.2", LogicalIP: "10.0.0.2"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.deleteLogicalRouterPolicyByUUID("lr-1", "policy-1"))
	require.NoError(t, controller.deleteLogicalRouterPolicies("lr-1", 200, map[string]string{"owner": "test"}))
	require.NoError(t, controller.deleteLogicalRouterPolicyByNexthop("lr-1", 100, "10.0.0.1"))
	require.NoError(t, controller.deleteNats("lr-1", ovnnb.NATTypeSNAT, "10.0.0.1"))
	require.NoError(t, controller.deleteNat("lr-1", ovnnb.NATTypeSNAT, "192.0.2.2", "10.0.0.2"))

	require.Equal(t, 5, backend.mutateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderNatCreate(t *testing.T) {
	backend := newTableBackend(&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1"})
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

	require.NoError(t, controller.addNat("lr-1", ovnnb.NATTypeSNAT, "192.0.2.10", "10.0.0.10", "", "", nil))
	require.Equal(t, 1, backend.createCalls)
	require.Equal(t, 1, backend.mutateCalls)
	require.Equal(t, 1, backend.transactCalls)

	duplicateBackend := newTableBackend(
		&ovnnb.LogicalRouter{UUID: "lr-2", Name: "lr-2", Nat: []string{"nat-1"}},
		&ovnnb.NAT{UUID: "nat-1", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.10", LogicalIP: "10.0.0.10"},
	)
	duplicateController := &Controller{OVNNbTables: compat.NewDatabase(duplicateBackend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, duplicateController.addNat("lr-2", ovnnb.NATTypeSNAT, "192.0.2.10", "10.0.0.10", "", "", nil))
	require.Equal(t, 0, duplicateBackend.createCalls)
	require.Equal(t, 0, duplicateBackend.transactCalls)
}

func TestControllerTableProviderNatReconcile(t *testing.T) {
	backend := newTableBackend(&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1"})
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, controller.ensureSnat("lr-1", "192.0.2.11", "10.0.0.11"))
	require.NoError(t, controller.updateDnatAndSnat("lr-1", "192.0.2.12", "10.0.0.12", "pod.ns", "00:00:00:00:00:12", "distributed"))
	require.Equal(t, 2, backend.createCalls)
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 2, backend.transactCalls)

	existingBackend := newTableBackend(
		&ovnnb.LogicalRouter{UUID: "lr-2", Name: "lr-2", Nat: []string{"nat-1"}},
		&ovnnb.NAT{UUID: "nat-1", Type: ovnnb.NATTypeDNATAndSNAT, ExternalIP: "192.0.2.12", LogicalIP: "10.0.0.12"},
	)
	existingController := &Controller{OVNNbTables: compat.NewDatabase(existingBackend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, existingController.updateDnatAndSnat("lr-2", "192.0.2.12", "10.0.0.12", "pod.ns", "00:00:00:00:00:13", "distributed"))
	require.Equal(t, 1, existingBackend.updateCalls)
	require.Equal(t, 1, existingBackend.transactCalls)
}

func TestControllerTableProviderPeerAndPatchPorts(t *testing.T) {
	peerBackend := newTableBackend(&ovnnb.LogicalRouter{UUID: "lr-1", Name: "local"})
	peerController := &Controller{OVNNbTables: compat.NewDatabase(peerBackend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, peerController.createPeerRouterPort("local", "remote", "169.254.0.1/30"))
	require.Equal(t, 1, peerBackend.createCalls)
	require.Equal(t, 1, peerBackend.mutateCalls)
	require.Equal(t, 1, peerBackend.transactCalls)

	patchBackend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "switch-1"},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "router-1"},
	)
	patchController := &Controller{OVNNbTables: compat.NewDatabase(patchBackend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, patchController.createLogicalPatchPort(
		"switch-1", "router-1", "lsp-1", "lrp-1", "10.0.0.1/24", "00:00:00:00:00:01", "chassis-1",
	))
	require.Equal(t, 3, patchBackend.createCalls)
	require.Equal(t, 3, patchBackend.mutateCalls)
	require.Equal(t, 1, patchBackend.transactCalls)
}

func TestControllerTableProviderLogicalSwitchRepairsMissingPatchPorts(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "switch-1"},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "router-1"},
	)
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

	require.NoError(t, controller.createLogicalSwitch(
		"switch-1", "router-1", "10.0.0.0/24", "10.0.0.1", "00:00:00:00:00:01", true, false,
	))
	require.Equal(t, 2, backend.createCalls)
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 1, backend.transactCalls)
}

func TestControllerTableProviderLogicalPatchPortRepairsPartialTopology(t *testing.T) {
	tests := []struct {
		name        string
		existing    []model.Model
		wantCreate  int
		wantLSPUUID string
		wantLRPUUID string
	}{
		{
			name: "logical switch port exists",
			existing: []model.Model{&ovnnb.LogicalSwitchPort{
				UUID: "lsp-1", Name: "lsp-1", Type: "router", Options: map[string]string{"router-port": "lrp-1"},
			}},
			wantCreate:  1,
			wantLSPUUID: "lsp-1",
		},
		{
			name:        "logical router port exists",
			existing:    []model.Model{&ovnnb.LogicalRouterPort{UUID: "lrp-1", Name: "lrp-1", Networks: []string{"10.0.0.1/24"}}},
			wantCreate:  1,
			wantLRPUUID: "lrp-1",
		},
		{
			name: "both ports exist detached",
			existing: []model.Model{
				&ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "lsp-1", Type: "router", Options: map[string]string{"router-port": "lrp-1"}},
				&ovnnb.LogicalRouterPort{UUID: "lrp-1", Name: "lrp-1", Networks: []string{"10.0.0.1/24"}},
			},
			wantLSPUUID: "lsp-1",
			wantLRPUUID: "lrp-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := []any{
				&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "switch-1"},
				&ovnnb.LogicalRouter{UUID: "lr-1", Name: "router-1"},
			}
			for _, row := range test.existing {
				rows = append(rows, row)
			}
			backend := newTableBackend(rows...)
			controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

			require.NoError(t, controller.createLogicalPatchPort(
				"switch-1", "router-1", "lsp-1", "lrp-1", "10.0.0.1/24", "00:00:00:00:00:01",
			))
			require.Equal(t, test.wantCreate, backend.createCalls)
			require.Equal(t, 2, backend.mutateCalls)
			require.Equal(t, 1, backend.transactCalls)
			requireParentPortMutation(t, backend.mutations[0], "switch-1", test.wantLSPUUID, ovsdb.MutateOperationInsert)
			requireParentPortMutation(t, backend.mutations[1], "router-1", test.wantLRPUUID, ovsdb.MutateOperationInsert)
		})
	}
}

func TestControllerTableProviderLogicalSwitchPortMovesBetweenSwitches(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-old", Name: "subnet-old", Ports: []string{"lsp-1"}},
		&ovnnb.LogicalSwitch{UUID: "ls-new", Name: "subnet-new"},
		&ovnnb.LogicalSwitchPort{
			UUID: "lsp-1", Name: "vm.namespace", ExternalIDs: map[string]string{ovs.LogicalSwitchKey: "subnet-old"},
		},
	)
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

	require.NoError(t, controller.createLogicalSwitchPort(
		"subnet-new", "vm.namespace", "10.0.0.2", "00:00:00:00:00:02", "vm", "namespace",
		false, "", "", false, nil, util.DefaultVpc,
	))
	require.Equal(t, 1, backend.createCalls)
	require.Len(t, backend.created, 1)
	created, ok := backend.created[0].(*ovnnb.LogicalSwitchPort)
	require.True(t, ok)
	require.Equal(t, "subnet-new", created.ExternalIDs[ovs.LogicalSwitchKey])
	require.Equal(t, 2, backend.mutateCalls)
	requireParentPortMutation(t, backend.mutations[0], "subnet-old", "lsp-1", ovsdb.MutateOperationDelete)
	requireParentPortMutation(t, backend.mutations[1], "subnet-new", created.UUID, ovsdb.MutateOperationInsert)
	require.Equal(t, 1, backend.transactCalls)
}

func TestControllerTableProviderLogicalPatchPortsMoveBetweenParents(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-old", Name: "switch-old", Ports: []string{"lsp-1"}},
		&ovnnb.LogicalSwitch{UUID: "ls-new", Name: "switch-new"},
		&ovnnb.LogicalRouter{UUID: "lr-old", Name: "router-old", Ports: []string{"lrp-1"}},
		&ovnnb.LogicalRouter{UUID: "lr-new", Name: "router-new"},
		&ovnnb.LogicalSwitchPort{
			UUID: "lsp-1", Name: "lsp-1", ExternalIDs: map[string]string{ovs.LogicalSwitchKey: "switch-old"},
		},
		&ovnnb.LogicalRouterPort{
			UUID: "lrp-1", Name: "lrp-1", ExternalIDs: map[string]string{"lr": "router-old"},
		},
	)
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

	require.NoError(t, controller.createLogicalPatchPort(
		"switch-new", "router-new", "lsp-1", "lrp-1", "10.0.0.1/24", "00:00:00:00:00:01",
	))
	require.Equal(t, 0, backend.createCalls)
	require.Equal(t, 4, backend.mutateCalls)
	requireParentPortMutation(t, backend.mutations[0], "switch-old", "lsp-1", ovsdb.MutateOperationDelete)
	requireParentPortMutation(t, backend.mutations[1], "switch-new", "lsp-1", ovsdb.MutateOperationInsert)
	requireParentPortMutation(t, backend.mutations[2], "router-old", "lrp-1", ovsdb.MutateOperationDelete)
	requireParentPortMutation(t, backend.mutations[3], "router-new", "lrp-1", ovsdb.MutateOperationInsert)
	require.Equal(t, 1, backend.transactCalls)
}

func TestControllerTableProviderPolicyReconcile(t *testing.T) {
	backend := newTableBackend(&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1"})
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, controller.addLogicalRouterPolicy(
		"lr-1", 100, "ip4.src == 10.0.0.0/24", ovnnb.LogicalRouterPolicyActionReroute,
		[]string{"10.0.0.1"}, nil, map[string]string{"owner": "test"},
	))
	require.Equal(t, 1, backend.createCalls)
	require.Equal(t, 1, backend.mutateCalls)
	require.Equal(t, 1, backend.transactCalls)

	deleteBackend := newTableBackend(
		&ovnnb.LogicalRouter{UUID: "lr-2", Name: "lr-2", Policies: []string{"policy-1"}},
		&ovnnb.LogicalRouterPolicy{UUID: "policy-1", Priority: 100, Match: "ip4.src == 10.0.0.0/24"},
	)
	deleteController := &Controller{OVNNbTables: compat.NewDatabase(deleteBackend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, deleteController.batchDeleteLogicalRouterPolicies("lr-2", []*ovnnb.LogicalRouterPolicy{{
		Priority: 100, Match: "ip4.src == 10.0.0.0/24",
	}}))
	require.Equal(t, 1, deleteBackend.mutateCalls)
	require.Equal(t, 1, deleteBackend.transactCalls)
}

func TestControllerTableProviderRouterPortRA(t *testing.T) {
	backend := newTableBackend(&ovnnb.LogicalRouterPort{
		UUID: "lrp-1", Name: "lrp-1", Networks: []string{"10.0.0.1/24", "fd00::1/64"},
	})
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, controller.updateLogicalRouterPortRA("lrp-1", "", true))
	require.NoError(t, controller.updateLogicalRouterPortRA("lrp-1", "", false))
	require.Equal(t, 2, backend.updateCalls)
	require.Equal(t, 2, backend.transactCalls)
}

func TestControllerTableProviderLogicalSwitchAndDHCP(t *testing.T) {
	backend := newTableBackend(&ovnnb.LogicalRouter{UUID: "lr-1", Name: "vpc-1"})
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.createLogicalSwitch("subnet-1", "vpc-1", "10.0.0.0/24", "10.0.0.1", "", false, false))
	require.Equal(t, 1, backend.createCalls)
	_, err := controller.updateSubnetDHCPOptionsTable(&kubeovnv1.Subnet{
		Name: "subnet-1",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24", Gateway: "10.0.0.1", Protocol: kubeovnv1.ProtocolIPv4,
			EnableDHCP: true, DHCPv4Options: "dns_server=8.8.8.8",
		},
	}, 1450)
	require.NoError(t, err)
	require.Equal(t, 2, backend.createCalls)
}

func TestControllerTableProviderLogicalSwitchACL(t *testing.T) {
	backend := newTableBackend(&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "subnet-1"})
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, controller.updateLogicalSwitchACL("subnet-1", "10.0.0.0/24", []kubeovnv1.ACL{{
		Direction: ovnnb.ACLDirectionToLport, Priority: 100, Match: "ip4", Action: ovnnb.ACLActionAllow,
	}}, true))
	require.Equal(t, 1, backend.createCalls)
	require.Equal(t, 1, backend.transactCalls)
}

func TestControllerTableProviderPortDeletes(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "ls-1", Ports: []string{"lsp-1"}},
		&ovnnb.LogicalSwitchPort{
			UUID:        "lsp-1",
			Name:        "lsp-1",
			ExternalIDs: map[string]string{"ls": "ls-1"},
		},
		&ovnnb.DHCPOptions{
			UUID:        "dhcp-1",
			ExternalIDs: map[string]string{"port": "lsp-1"},
		},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1", Ports: []string{"lrp-1"}},
		&ovnnb.LogicalRouterPort{
			UUID:        "lrp-1",
			Name:        "lrp-1",
			ExternalIDs: map[string]string{"lr": "lr-1"},
		},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.deleteLogicalSwitchPort("lsp-1"))
	require.NoError(t, controller.deleteLogicalRouterPort("lrp-1"))
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 2, backend.transactCalls)
}

func TestControllerTableProviderHAChassisGroup(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.HAChassisGroup{
			UUID:        "group-1",
			Name:        "bfd-vpc-1",
			HaChassis:   []string{"ha-1"},
			ExternalIDs: map[string]string{"vendor": "kube-ovn"},
		},
		&ovnnb.HAChassis{UUID: "ha-1", ChassisName: "node-old", Priority: 100},
		&ovnnb.LogicalRouterPort{UUID: "lrp-1", Name: "bfd-vpc-1"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.createHAChassisGroup("bfd-vpc-1", []string{"node-new"}, map[string]string{"lrp": "bfd-vpc-1"}))
	require.NoError(t, controller.setLogicalRouterPortHAChassisGroup("bfd-vpc-1", "bfd-vpc-1"))
	require.NoError(t, controller.deleteHAChassisGroup("bfd-vpc-1"))

	require.Equal(t, 1, backend.createCalls)
	require.Equal(t, 2, backend.updateCalls)
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 2, backend.transactCalls)
}

func TestControllerTableProviderMeterDelete(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.Meter{UUID: "meter-1", Name: "meter-1", Bands: []string{"band-1", "band-2"}},
		&ovnnb.MeterBand{UUID: "band-1"},
		&ovnnb.MeterBand{UUID: "band-2"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.deleteMeter("meter-1"))
	require.NoError(t, controller.deleteMeter("missing-meter"))
}

func TestControllerTableProviderStaticRoutes(t *testing.T) {
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	backend := newTableBackend(
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1", StaticRoutes: []string{"route-1"}},
		&ovnnb.LogicalRouterStaticRoute{
			UUID:       "route-1",
			RouteTable: "main",
			Policy:     &policy,
			IPPrefix:   "10.0.0.0/24",
			Nexthop:    "192.0.2.1",
		},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.addLogicalRouterStaticRoute("lr-1", "main", "", "10.0.0.0/24", nil, nil, "192.0.2.2"))
	require.NoError(t, controller.deleteLogicalRouterStaticRoute("lr-1", new("main"), &policy, "10.0.0.0/24", "192.0.2.1"))
	require.NoError(t, controller.batchDeleteLogicalRouterStaticRoutes("lr-1", []*ovnnb.LogicalRouterStaticRoute{{
		RouteTable: "main", Policy: &policy, IPPrefix: "10.0.0.0/24", Nexthop: "192.0.2.1",
	}}))

	require.Equal(t, 1, backend.createCalls)
	require.Equal(t, 4, backend.mutateCalls)
	require.Equal(t, 4, backend.transactCalls)
}

func TestControllerTableProviderPortCreates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "ls-1"},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.createBareLogicalSwitchPort("ls-1", "bare-1", "10.0.0.2", "00:00:00:00:00:02"))
	require.NoError(t, controller.createVirtualLogicalSwitchPort("virtual-1", "ls-1", "10.0.0.3"))
	require.NoError(t, controller.createVirtualLogicalSwitchPorts("ls-1", "10.0.0.4", "10.0.0.5"))
	require.NoError(t, controller.createLocalnetLogicalSwitchPort("ls-1", "localnet-1", "provider", "10.0.0.0/24", 100))
	require.NoError(t, controller.createLogicalRouterPort("lr-1", "lrp-1", "00:00:00:00:00:03", []string{"10.0.0.1/24"}))

	require.Equal(t, 6, backend.createCalls)
	require.Equal(t, 6, backend.mutateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderLoadBalancerOperations(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LoadBalancer{
			UUID:           "lb-1",
			Name:           "lb-1",
			Vips:           map[string]string{"10.0.0.1:80": "10.0.0.2:8080"},
			IPPortMappings: map[string]string{"10.0.0.2": "lsp-old"},
			ExternalIDs:    map[string]string{"owner": "test"},
			HealthCheck:    []string{"lbhc-1"},
		},
		&ovnnb.LoadBalancerHealthCheck{UUID: "lbhc-1", Vip: "10.0.0.1:80"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.addLoadBalancerVIP("lb-1", "10.0.0.1:80", "10.0.0.3:8080"))
	require.NoError(t, controller.updateLoadBalancerIPPortMapping("lb-1", "10.0.0.1:80", map[string]string{"10.0.0.3": "lsp-new"}))
	require.NoError(t, controller.deleteLoadBalancerIPPortMapping("lb-1", "10.0.0.1:80"))
	transactionCount := len(backend.transactionOperationCounts)
	require.NoError(t, controller.deleteLoadBalancerHealthCheck("lb-1", "lbhc-1"))
	require.Equal(t, []int{2}, backend.transactionOperationCounts[transactionCount:])
	require.NoError(t, controller.deleteLoadBalancerVIP("lb-1", "10.0.0.1:80", true))
	require.GreaterOrEqual(t, backend.deleteCalls, 1)

	healthCheckBackend := newTableBackend(&ovnnb.LoadBalancer{UUID: "lb-2", Name: "lb-2"})
	healthCheckController := &Controller{OVNNbTables: compat.NewDatabase(healthCheckBackend, time.Second, compat.RetryPolicy{})}
	require.NoError(t, healthCheckController.addLoadBalancerHealthCheck(
		"lb-2", "10.0.0.2:80", false, nil, map[string]string{"owner": "test"},
	))
	require.Equal(t, 1, healthCheckBackend.createCalls)
	require.Equal(t, 1, healthCheckBackend.mutateCalls)
	require.Equal(t, 1, healthCheckBackend.transactCalls)

	// The fake backend records operation construction; each helper submits one
	// transaction while preserving the provider boundary.
	require.Equal(t, 7, backend.mutateCalls)
	require.Equal(t, 7, backend.transactCalls)
}

func TestControllerTableProviderSecurityGroupACLs(t *testing.T) {
	pgName := ovs.GetSgPortGroupName("sg-1")
	backend := newTableBackend(&ovnnb.PortGroup{UUID: "pg-1", Name: pgName})
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

	require.NoError(t, controller.createSgDenyAllACL("sg-1"))
	require.NoError(t, controller.createSgBaseACL("sg-1", ovnnb.ACLDirectionToLport))
	require.Equal(t, 2, backend.createCalls)
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 2, backend.transactCalls)

	rule := kubeovnv1.SecurityGroupRule{
		IPVersion:     "ipv4",
		Priority:      1,
		RemoteType:    kubeovnv1.SgRemoteTypeAddress,
		RemoteAddress: "10.0.0.0/8",
		Protocol:      kubeovnv1.SgProtocolTCP,
		Policy:        kubeovnv1.SgPolicyAllow,
		PortRangeMin:  80,
		PortRangeMax:  80,
	}
	sg := &kubeovnv1.SecurityGroup{Name: "sg-1", Spec: kubeovnv1.SecurityGroupSpec{Tier: util.SecurityGroupAPITierMinimum, IngressRules: []kubeovnv1.SecurityGroupRule{rule}}}
	require.NoError(t, controller.updateSgACL(sg, ovnnb.ACLDirectionToLport))
	require.Equal(t, 3, backend.createCalls)
	require.Equal(t, 3, backend.transactCalls)
}

func TestControllerTableProviderACLHelpers(t *testing.T) {
	pgName := "node-pg"
	logMatch := "inport == @" + pgName + " && ip"
	backend := newTableBackend(
		&ovnnb.PortGroup{UUID: "pg-1", Name: pgName, ACLs: []string{"acl-1"}},
		&ovnnb.ACL{UUID: "acl-1", ExternalIDs: map[string]string{"parent": pgName}, Direction: ovnnb.ACLDirectionFromLport, Priority: 2000, Match: logMatch, Tier: util.NetpolACLTier},
	)
	controller := &Controller{OVNNbTables: compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})}

	require.NoError(t, controller.createGatewayACL("", pgName))
	require.NoError(t, controller.createNodeACL(pgName, "10.0.0.2", "10.0.0.1"))
	require.NoError(t, controller.setNetPolACLLog(pgName, true, false))
	require.GreaterOrEqual(t, backend.createCalls, 2)
	require.GreaterOrEqual(t, backend.mutateCalls, 2)
	require.GreaterOrEqual(t, backend.updateCalls, 1)
	require.GreaterOrEqual(t, backend.transactCalls, 3)
}

type tableBackend struct {
	rows                       map[reflect.Type][]any
	createCalls                int
	mutateCalls                int
	updateCalls                int
	deleteCalls                int
	transactCalls              int
	transactionOperationCounts []int
	mutations                  []tableMutation
	created                    []model.Model
}

type tableMutation struct {
	row       model.Model
	mutations []model.Mutation
}

func requireParentPortMutation(t *testing.T, mutation tableMutation, parentName, childUUID string, mutator ovsdb.Mutator) {
	t.Helper()
	parent := reflect.ValueOf(mutation.row).Elem()
	require.Equal(t, parentName, parent.FieldByName("Name").String())
	require.Len(t, mutation.mutations, 1)
	require.Equal(t, mutator, mutation.mutations[0].Mutator)
	children, ok := mutation.mutations[0].Value.([]string)
	require.True(t, ok)
	require.Len(t, children, 1)
	if childUUID == "" {
		require.NotEmpty(t, children[0])
	} else {
		require.Equal(t, childUUID, children[0])
	}
	require.Equal(t, parent.FieldByName("Ports").Addr().Interface(), mutation.mutations[0].Field)
}

func newTableBackend(rows ...any) *tableBackend {
	backend := &tableBackend{rows: make(map[reflect.Type][]any)}
	for _, row := range rows {
		value := reflect.ValueOf(row)
		typ := value.Type()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		backend.rows[typ] = append(backend.rows[typ], row)
	}
	return backend
}

func (b *tableBackend) Get(_ context.Context, result model.Model) error {
	resultValue := reflect.ValueOf(result)
	rows := b.rows[resultValue.Elem().Type()]
	for _, candidate := range rows {
		candidateValue := reflect.ValueOf(candidate)
		if candidateValue.Kind() == reflect.Pointer {
			candidateValue = candidateValue.Elem()
		}
		for _, field := range []string{"UUID", "Name"} {
			wanted := resultValue.Elem().FieldByName(field)
			actual := candidateValue.FieldByName(field)
			if wanted.IsValid() && actual.IsValid() && wanted.Kind() == reflect.String && wanted.String() != "" && wanted.String() == actual.String() {
				resultValue.Elem().Set(candidateValue)
				return nil
			}
		}
	}
	return compat.ErrNotFound
}

func (b *tableBackend) List(_ context.Context, result any) error {
	resultValue := reflect.ValueOf(result).Elem()
	for _, candidate := range b.rows[resultValue.Type().Elem()] {
		appendTableRow(resultValue, reflect.ValueOf(candidate))
	}
	return nil
}

func (b *tableBackend) WhereCache(predicate any) compat.ConditionalAPI {
	return tableConditional{backend: b, predicate: predicate}
}

func (b *tableBackend) WhereCacheByUUIDs(predicate any, _ ...string) compat.ConditionalAPI {
	return b.WhereCache(predicate)
}

func (b *tableBackend) Where(selectors ...model.Model) compat.ConditionalAPI {
	deleteOperation := false
	for _, selector := range selectors {
		if reflect.TypeOf(selector) == reflect.TypeFor[*ovnnb.LoadBalancerHealthCheck]() {
			deleteOperation = true
			break
		}
	}
	return tableConditional{backend: b, deleteOperation: deleteOperation}
}

func (b *tableBackend) WhereAny(model.Model, ...model.Condition) compat.ConditionalAPI {
	return tableConditional{backend: b}
}

func (b *tableBackend) WhereAll(model.Model, ...model.Condition) compat.ConditionalAPI {
	return tableConditional{backend: b}
}

func (*tableBackend) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (b *tableBackend) Create(rows ...model.Model) ([]ovsdb.Operation, error) {
	b.createCalls++
	b.created = append(b.created, rows...)
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (b *tableBackend) Transact(_ context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	b.transactCalls++
	b.transactionOperationCounts = append(b.transactionOperationCounts, len(operations))
	return make([]ovsdb.OperationResult, len(operations)), nil
}

func (*tableBackend) Cache() compat.Cache { return tableCache{} }

func (*tableBackend) Schema() ovsdb.DatabaseSchema { return ovsdb.DatabaseSchema{} }

func (*tableBackend) Connected() bool { return true }

func (*tableBackend) NewMonitor(...compat.MonitorOption) *compat.Monitor { return nil }

func (*tableBackend) Monitor(context.Context, *compat.Monitor) (compat.MonitorCookie, error) {
	return compat.MonitorCookie{}, nil
}

func (*tableBackend) Echo(context.Context) error { return nil }

func (*tableBackend) Close() {}

type tableCache struct{}

func (tableCache) AddEventHandler(compat.EventHandler) {}

type tableConditional struct {
	backend         *tableBackend
	predicate       any
	deleteOperation bool
}

func (c tableConditional) List(_ context.Context, result any) error {
	resultValue := reflect.ValueOf(result).Elem()
	var rows []any
	if c.predicate != nil {
		predicateType := reflect.TypeOf(c.predicate)
		rows = c.backend.rows[predicateType.In(0).Elem()]
	} else {
		rows = c.backend.rows[resultValue.Type().Elem()]
	}
	for _, candidate := range rows {
		candidateValue := reflect.ValueOf(candidate)
		if c.predicate != nil && !reflect.ValueOf(c.predicate).Call([]reflect.Value{candidateValue})[0].Bool() {
			continue
		}
		appendTableRow(resultValue, candidateValue)
	}
	return nil
}

func appendTableRow(destination, candidate reflect.Value) {
	if candidate.Type().AssignableTo(destination.Type().Elem()) {
		destination.Set(reflect.Append(destination, candidate))
		return
	}
	if candidate.Kind() == reflect.Pointer && candidate.Elem().Type().AssignableTo(destination.Type().Elem()) {
		destination.Set(reflect.Append(destination, candidate.Elem()))
		return
	}
	if destination.Type().Elem().Kind() == reflect.Pointer && candidate.Type().AssignableTo(destination.Type().Elem()) {
		destination.Set(reflect.Append(destination, candidate))
	}
}

func (c tableConditional) Mutate(row model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	c.backend.mutateCalls++
	c.backend.mutations = append(c.backend.mutations, tableMutation{row: row, mutations: mutations})
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (c tableConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	c.backend.updateCalls++
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (c tableConditional) Delete() ([]ovsdb.Operation, error) {
	c.backend.deleteCalls++
	if !c.deleteOperation {
		return nil, nil
	}
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (tableConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (tableConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}
