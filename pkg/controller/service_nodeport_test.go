package controller

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

func TestServiceNodePortsUseStaticClusterBackends(t *testing.T) {
	for _, serviceType := range []corev1.ServiceType{corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer} {
		for _, policy := range []corev1.ServiceExternalTrafficPolicy{corev1.ServiceExternalTrafficPolicyTypeCluster, corev1.ServiceExternalTrafficPolicyTypeLocal} {
			t.Run(string(serviceType)+"/"+string(policy), func(t *testing.T) {
				ctrl, nb, svc, endpoints := serviceLBReconcileFixture(t, serviceType, serviceType == corev1.ServiceTypeLoadBalancer && policy == corev1.ServiceExternalTrafficPolicyTypeLocal)
				svc.Spec.ExternalTrafficPolicy = policy
				svc.Spec.TrafficDistribution = new(corev1.ServiceTrafficDistributionPreferSameNode)
				svc.Spec.Ports[0].NodePort = 30080
				node, err := ctrl.nodesLister.Get("node")
				require.NoError(t, err)
				node.Status.Addresses = []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
					{Type: corev1.NodeExternalIP, Address: "192.0.2.10"},
					{Type: corev1.NodeExternalIP, Address: "2001:db8::10"},
				}
				endpoints[0].Endpoints = append(endpoints[0].Endpoints,
					discoveryv1.Endpoint{Addresses: []string{"10.0.0.3"}, NodeName: new("remote"), Hints: &discoveryv1.EndpointHints{ForNodes: []discoveryv1.ForNode{{Name: "remote"}}}},
					discoveryv1.Endpoint{Addresses: []string{"10.0.0.4"}, Conditions: discoveryv1.EndpointConditions{Ready: new(false)}},
				)
				endpoints[0].Endpoints[0].Hints = &discoveryv1.EndpointHints{ForNodes: []discoveryv1.ForNode{{Name: "node"}}}
				require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
				name := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBTrafficClass("nodeport"))
				lb, err := nb.GetLoadBalancer(name, true)
				require.NoError(t, err)
				require.NotNil(t, lb, "allocated NodePorts must have a separate static LB")
				require.Equal(t, "false", lb.Options["template"])
				require.Equal(t, "none", lb.Options["neighbor_responder"])
				require.NotContains(t, lb.Options, "distributed")
				require.NotContains(t, lb.Options, "address-family")
				require.Equal(t, map[string]string{
					"192.0.2.10:30080":     "10.0.0.2:8080,10.0.0.3:8080",
					"[2001:db8::10]:30080": "[fd00::2]:8080",
				}, lb.Vips)
				lr, err := nb.GetLogicalRouter("vpc", false)
				require.NoError(t, err)
				require.NotContains(t, lr.LoadBalancer, lb.UUID, "non-logical-gateway NodePort traffic must remain switch-only")
				ls, err := nb.GetLogicalSwitch("subnet", false)
				require.NoError(t, err)
				require.Contains(t, ls.LoadBalancer, lb.UUID)
				var variables []ovnnb.ChassisTemplateVar
				require.NoError(t, nb.WhereCache(func(*ovnnb.ChassisTemplateVar) bool { return true }).List(t.Context(), &variables))
				if serviceType == corev1.ServiceTypeNodePort {
					require.Empty(t, variables, "static NodePort LBs must not create chassis variables")
				}
			})
		}
	}
}

func TestServiceNodePortsAttachToRouterForLogicalGateway(t *testing.T) {
	ctrl, nb, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeNodePort, false)
	subnet, err := ctrl.subnetsLister.Get("subnet")
	require.NoError(t, err)
	subnet.Spec.LogicalGateway = true
	svc.Spec.Ports[0].NodePort = 30080
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.0.2.10"}}
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	lb, err := nb.GetLoadBalancer(serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBNodePortTraffic), false)
	require.NoError(t, err)
	lr, err := nb.GetLogicalRouter("vpc", false)
	require.NoError(t, err)
	require.Contains(t, lr.LoadBalancer, lb.UUID)
}

func TestServiceNodePortsSeparateProtocolsWithTheSamePort(t *testing.T) {
	ctrl, nb, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeNodePort, false)
	svc.Spec.Ports[0].NodePort, svc.Spec.Ports[2].NodePort = 30080, 30080
	svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{Name: "sctp", Protocol: corev1.ProtocolSCTP, Port: 90, NodePort: 30080})
	for _, slice := range endpoints {
		slice.Ports = append(slice.Ports, discoveryv1.EndpointPort{Name: new("sctp"), Protocol: new(corev1.ProtocolSCTP), Port: new(int32(9000))})
	}
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
		{Type: corev1.NodeExternalIP, Address: "2001:db8::10"},
	}
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	for protocol, target := range map[corev1.Protocol]string{corev1.ProtocolTCP: "8080", corev1.ProtocolUDP: "8053", corev1.ProtocolSCTP: "9000"} {
		lb, err := nb.GetLoadBalancer(serviceScopedLBNameForTrafficClass(svc, protocol, serviceLBNodePortTraffic), false)
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"192.0.2.10:30080":     "10.0.0.2:" + target,
			"[2001:db8::10]:30080": "[fd00::2]:" + target,
		}, lb.Vips)
	}
}

func nodePortFixture(t *testing.T) (*Controller, *ovs.OVNNbClient, *corev1.Service, []*discoveryv1.EndpointSlice) {
	t.Helper()
	ctrl, nb, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeNodePort, false)
	svc.Spec.Ports[0].NodePort = 30080
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.0.2.10"}}
	return ctrl, nb, svc, endpoints
}

func TestServiceNodePortReconciliationIsAtomic(t *testing.T) {
	ctrl, nb, svc, endpoints := nodePortFixture(t)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	before, err := nb.ListLoadBalancers(nil)
	require.NoError(t, err)
	guardsBefore, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	svc.Spec.Ports[0].NodePort = 30081
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses[0].Address = "192.0.2.11"
	recorder := &serviceLBTransactionClient{Client: nb.Client, fail: true}
	nb.Database = table.NewDatabase(recorder, nb.Timeout, table.RetryPolicy{})
	require.Error(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	after, err := nb.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.ElementsMatch(t, before, after, "a failed reconcile must preserve the old data plane")
	guards, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	require.ElementsMatch(t, guardsBefore, guards, "a failed reconcile must preserve the ingress boundary")
	recorder.fail = false
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	require.Len(t, recorder.transactions, 2, "each complete attempt must be one transaction")
	lb, err := nb.GetLoadBalancer(serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBNodePortTraffic), false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"192.0.2.11:30081": "10.0.0.2:8080"}, lb.Vips)
	guards, err = nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	require.Len(t, guards, 1)
	require.Equal(t, "drop", guards[0].Action)
	require.Equal(t, "false", guards[0].Options["apply-after-lb"])
	require.Equal(t, "inport == \"localnet.subnet\" && ((ip4.dst == 192.0.2.11 && tcp.dst == 30081))", guards[0].Match)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	require.Len(t, recorder.transactions, 2, "unchanged reconciliation must not write")
}

func TestServiceNodePortLifecycle(t *testing.T) {
	ctrl, nb, svc, endpoints := nodePortFixture(t)
	svc.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	svc.Spec.SessionAffinityConfig = &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: new(int32(42))}}
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	name := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBNodePortTraffic)
	lb, err := nb.GetLoadBalancer(name, false)
	require.NoError(t, err)
	require.Equal(t, "42", lb.Options["affinity_timeout"])
	require.Empty(t, lb.SelectionFields)
	endpoints[0].Endpoints[0].Addresses = []string{"10.0.0.9"}
	svc.Spec.Ports[0].NodePort = 30081
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses[0].Address = "192.0.2.11"
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	lb, err = nb.GetLoadBalancer(name, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"192.0.2.11:30081": "10.0.0.9:8080"}, lb.Vips)
	guards, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	require.Len(t, guards, 1)
	require.Contains(t, guards[0].Match, "192.0.2.11")
	require.NotContains(t, guards[0].Match, "192.0.2.10")
	// Losing every eligible node address must remove old VIPs and guards.
	node.Status.Addresses = nil
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	lb, err = nb.GetLoadBalancer(name, false)
	require.NoError(t, err)
	require.Empty(t, lb.Vips)
	guards, err = nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	require.Empty(t, guards)
	// Changing Service type removes the separate NodePort class.
	svc.Spec.Type = corev1.ServiceTypeClusterIP
	svc.Spec.Ports[0].NodePort = 0
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	lbs, err := nb.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.False(t, slices.ContainsFunc(lbs, func(lb ovnnb.LoadBalancer) bool { return lb.Name == name }))
}

func TestServiceNodePortGuardDeletionAndGC(t *testing.T) {
	for _, useGC := range []bool{false, true} {
		t.Run(map[bool]string{false: "deletion", true: "missed deletion GC"}[useGC], func(t *testing.T) {
			ctrl, nb, svc, endpoints := nodePortFixture(t)
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			if useGC {
				require.NoError(t, ctrl.gcServiceNodePortIngressGuards([]*corev1.Service{svc}))
				guards, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
				require.NoError(t, err)
				require.NotEmpty(t, guards)
				ctrl.servicesLister = corelister.NewServiceLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
				require.NoError(t, ctrl.gcServiceNodePortIngressGuards(nil))
				lbs, err := nb.ListLoadBalancers(nil)
				require.NoError(t, err)
				require.Empty(t, lbs, "missed deletion GC must remove the LB and guard together")
			} else {
				require.NoError(t, ctrl.deleteServiceLoadBalancerState(&vpcService{Svc: svc}, nil))
			}
			guards, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
			require.NoError(t, err)
			require.Empty(t, guards)
		})
	}
}

func TestServiceNodePortGuardSurvivesSubnetACLReset(t *testing.T) {
	ctrl, nb, svc, endpoints := nodePortFixture(t)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	before, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.NoError(t, nb.DeleteAcls("subnet", ovs.LogicalSwitchKey, "", nil))
	require.NoError(t, nb.MigrateACLTier())
	after, err := nb.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	require.NoError(t, err)
	require.Equal(t, before, after, "subnet ACL lifecycle and tier migration must preserve the Service guard")
}

func TestServiceNodePortNewSubnetAttachmentIsGuarded(t *testing.T) {
	ctrl, nb, svc, endpoints := nodePortFixture(t)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	name := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBNodePortTraffic)
	lb, err := nb.GetLoadBalancer(name, false)
	require.NoError(t, err)
	oldSubnet, err := ctrl.subnetsLister.Get("subnet")
	require.NoError(t, err)
	newSubnet := &kubeovnv1.Subnet{Name: "new-subnet", Spec: oldSubnet.Spec}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(oldSubnet))
	require.NoError(t, indexer.Add(newSubnet))
	ctrl.subnetsLister = kubeovnlister.NewSubnetLister(indexer)
	require.NoError(t, nb.CreateBareLogicalSwitch(newSubnet.Name))
	vpc, err := ctrl.vpcsLister.Get("vpc")
	require.NoError(t, err)
	require.NoError(t, ctrl.updateSubnetLoadBalancers(newSubnet, vpc))
	ls, err := nb.GetLogicalSwitch(newSubnet.Name, false)
	require.NoError(t, err)
	require.NotContains(t, ls.LoadBalancer, lb.UUID, "generic Subnet attachment must not install an unguarded NodePort LB")
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	ls, err = nb.GetLogicalSwitch(newSubnet.Name, false)
	require.NoError(t, err)
	require.Contains(t, ls.LoadBalancer, lb.UUID)
	require.NotEmpty(t, ls.ACLs)
	newSubnet.Spec.EnableLb = new(false)
	require.NoError(t, indexer.Update(newSubnet))
	require.NoError(t, ctrl.updateSubnetLoadBalancers(newSubnet, vpc))
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	ls, err = nb.GetLogicalSwitch(newSubnet.Name, false)
	require.NoError(t, err)
	require.NotContains(t, ls.LoadBalancer, lb.UUID)
	require.Empty(t, ls.ACLs)
}
