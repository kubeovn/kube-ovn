package controller

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type serviceLBTransactionClient struct {
	*table.Client
	transactions [][]ovsdb.Operation
	fail         bool
}

func (c *serviceLBTransactionClient) Transact(ctx context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	c.transactions = append(c.transactions, slices.Clone(ops))
	if c.fail {
		ops = append(slices.Clone(ops), ovsdb.Operation{
			Op: ovsdb.OperationWait, Table: ovnnb.LoadBalancerTable, Timeout: new(0), Until: "==", Columns: []string{"name"},
			Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "missing-lb-to-reject-transaction")},
			Rows:  []ovsdb.Row{{"name": "missing-lb-to-reject-transaction"}},
		})
	}
	return c.TransactResults(ctx, ops...)
}

func serviceLBReconcileFixture(t *testing.T, serviceType corev1.ServiceType, template bool) (*Controller, *ovs.OVNNbClient, *corev1.Service, []*discoveryv1.EndpointSlice) {
	t.Helper()
	svc := &corev1.Service{
		Namespace: "default", Name: "multi-protocol", UID: "service-uid",
		Annotations: map[string]string{util.LogicalRouterAnnotation: "vpc", util.VpcAnnotation: "vpc", util.LogicalSwitchAnnotation: "subnet"},
		Spec: corev1.ServiceSpec{
			Type: serviceType, ClusterIP: "10.96.0.10", ClusterIPs: []string{"10.96.0.10", "fd00::10"},
			Ports: []corev1.ServicePort{
				{Name: "web", Protocol: corev1.ProtocolTCP, Port: 80},
				{Name: "web2", Protocol: corev1.ProtocolTCP, Port: 81},
				{Name: "dns", Protocol: corev1.ProtocolUDP, Port: 53},
			},
		},
	}
	if serviceType == corev1.ServiceTypeLoadBalancer {
		svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "172.19.0.100"}}
	}
	if template && serviceType == corev1.ServiceTypeClusterIP {
		svc.Spec.TrafficDistribution = new(corev1.ServiceTrafficDistributionPreferSameNode)
	}
	if template && serviceType == corev1.ServiceTypeLoadBalancer {
		svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	}
	subnet := &kubeovnv1.Subnet{Name: "subnet", Spec: kubeovnv1.SubnetSpec{Vpc: "vpc", Provider: util.OvnProvider}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services: []*corev1.Service{svc}, Subnets: []*kubeovnv1.Subnet{subnet}, Vpcs: []*kubeovnv1.Vpc{{Name: "vpc"}},
		Nodes: []*corev1.Node{{Name: "node"}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.svcKeyMutex = keymutex.NewHashed(0)
	ctrl.config.EnableOvnLB = true
	ctrl.config.ClusterRouter = "vpc"
	if template {
		chassises := []ovnsb.Chassis{{Name: "chassis", Hostname: "node"}}
		fc.mockOvnSbClient.EXPECT().ListChassis().Return(&chassises, nil).AnyTimes()
		ctrl.serviceL2StatusIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{serviceL2StatusServiceIndex: indexServiceL2StatusByService})
		ctrl.serviceL2StatusSynced = func() bool { return true }
		ctrl.serviceL2StatusStarted = true
		require.NoError(t, ctrl.serviceL2StatusIndexer.Add(&metallbv1beta1.ServiceL2Status{
			Name: "announcer", Namespace: "metallb-system",
			Status: metallbv1beta1.MetalLBServiceL2Status{Node: "node", ServiceNamespace: svc.Namespace, ServiceName: svc.Name},
		}))
	}
	nbClient := newInMemoryOVNNbClient(t)
	ctrl.OVNNbClient = nbClient
	require.NoError(t, nbClient.CreateBareLogicalSwitch("subnet"))
	require.NoError(t, nbClient.CreateLogicalRouter("vpc"))
	var ports []discoveryv1.EndpointPort
	for _, port := range svc.Spec.Ports {
		ports = append(ports, discoveryv1.EndpointPort{Name: new(port.Name), Protocol: new(port.Protocol), Port: new(port.Port + 8000)})
	}
	endpoints := []*discoveryv1.EndpointSlice{
		{AddressType: discoveryv1.AddressTypeIPv4, Ports: ports, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.2"}, NodeName: new("node")}}},
		{AddressType: discoveryv1.AddressTypeIPv6, Ports: ports, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"fd00::2"}, NodeName: new("node")}}},
	}
	return ctrl, nbClient, svc, endpoints
}

func TestServiceLoadBalancersReconcileAtomically(t *testing.T) {
	for _, tc := range []struct {
		name        string
		serviceType corev1.ServiceType
		template    bool
	}{
		{"ClusterIP", corev1.ServiceTypeClusterIP, false},
		{"NodePort", corev1.ServiceTypeNodePort, false},
		{"LoadBalancer", corev1.ServiceTypeLoadBalancer, false},
		{"traffic distribution", corev1.ServiceTypeClusterIP, true},
		{"external local template", corev1.ServiceTypeLoadBalancer, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, tc.serviceType, tc.template)
			recorder := &serviceLBTransactionClient{Client: nbClient.Client}
			nbClient.Database = table.NewDatabase(recorder, nbClient.Timeout, table.RetryPolicy{})
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, 1, "all protocols, address families, VIPs and attachments must be committed together")
			lbs, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(lbs), 2)
			templateRevisions := make(map[string]string)
			for _, lb := range lbs {
				if tc.serviceType == corev1.ServiceTypeLoadBalancer && lb.Name == serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBExternalTraffic, "ipv6") ||
					tc.serviceType == corev1.ServiceTypeLoadBalancer && lb.Name == serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolUDP, serviceLBExternalTraffic, "ipv6") {
					// No IPv6 external ingress was allocated, so this family has no VIP.
					require.Empty(t, lb.Vips)
					continue
				}
				require.NotEmpty(t, lb.Vips)
				isTemplate := tc.template && (tc.serviceType == corev1.ServiceTypeClusterIP ||
					lb.ExternalIDs[serviceLBScopeExternalID] == "logical-switch/external")
				if isTemplate {
					require.Equal(t, "true", lb.Options["ct_flush"], lb.Name)
					templateRevisions[lb.Name] = lb.Options[serviceLBTemplateRevisionOption]
					require.NotEmpty(t, templateRevisions[lb.Name], lb.Name)
				} else {
					require.NotContains(t, lb.Options, "ct_flush", lb.Name)
					require.NotContains(t, lb.Options, serviceLBTemplateRevisionOption, lb.Name)
				}
			}
			if tc.template {
				updatedEndpoints := make([]*discoveryv1.EndpointSlice, 0, len(endpoints))
				for _, endpointSlice := range endpoints {
					updatedEndpoints = append(updatedEndpoints, endpointSlice.DeepCopy())
				}
				updatedEndpoints[0].Endpoints[0].Addresses[0] = "10.0.0.3"
				require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, updatedEndpoints))
				require.Len(t, recorder.transactions, 2, "a template backend change must update the load balancer revision")
				loadBalancerUpdate := slices.IndexFunc(recorder.transactions[1], func(op ovsdb.Operation) bool {
					return op.Op == ovsdb.OperationUpdate && op.Table == ovnnb.LoadBalancerTable
				})
				templateVariableUpdate := slices.IndexFunc(recorder.transactions[1], func(op ovsdb.Operation) bool {
					return op.Op == ovsdb.OperationUpdate && op.Table == ovnnb.ChassisTemplateVarTable
				})
				require.GreaterOrEqual(t, loadBalancerUpdate, 0, "template backend changes must update the load balancer row")
				require.GreaterOrEqual(t, templateVariableUpdate, 0, "template backend changes must update chassis variables")
				require.Less(t, loadBalancerUpdate, templateVariableUpdate, "load balancer update must precede template variable update")
				updated, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
				require.NoError(t, err)
				for _, lb := range updated {
					if revision := templateRevisions[lb.Name]; revision != "" {
						require.NotEqual(t, revision, lb.Options[serviceLBTemplateRevisionOption], lb.Name)
					}
				}
				endpoints = updatedEndpoints
			}
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, func() int {
				if tc.template {
					return 2
				}
				return 1
			}(), "an unchanged Service must not produce another NB transaction")
		})
	}
}

func TestServiceLoadBalancerReconciliationPreservesAffinityFields(t *testing.T) {
	for _, tc := range []struct {
		name        string
		serviceType corev1.ServiceType
		template    bool
		timeout     int32
	}{
		{"ClusterIP", corev1.ServiceTypeClusterIP, false, 0},
		{"NodePort", corev1.ServiceTypeNodePort, false, 0},
		{"LoadBalancer", corev1.ServiceTypeLoadBalancer, false, 0},
		{"traffic distribution", corev1.ServiceTypeClusterIP, true, 0},
		{"external local template", corev1.ServiceTypeLoadBalancer, true, 0},
		{"explicit TCP timeout", corev1.ServiceTypeClusterIP, false, 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, tc.serviceType, tc.template)
			svc.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
			if tc.timeout != 0 {
				svc.Spec.SessionAffinityConfig = &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: new(tc.timeout)}}
			}
			recorder := &serviceLBTransactionClient{Client: nbClient.Client}
			nbClient.Database = table.NewDatabase(recorder, nbClient.Timeout, table.RetryPolicy{})
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, 1)
			lbs, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
			require.NoError(t, err)
			require.NotEmpty(t, lbs)
			for _, lb := range lbs {
				fields := []string{ovnnb.LoadBalancerSelectionFieldsIPSrc, ovnnb.LoadBalancerSelectionFieldsIpv6Src}
				switch {
				case strings.HasSuffix(lb.Name, ":ipv4"):
					fields = []string{ovnnb.LoadBalancerSelectionFieldsIPSrc}
				case strings.HasSuffix(lb.Name, ":ipv6"):
					fields = []string{ovnnb.LoadBalancerSelectionFieldsIpv6Src}
				}
				if tc.timeout != 0 && lb.Protocol != nil && *lb.Protocol == "tcp" {
					require.Empty(t, lb.SelectionFields, lb.Name)
				} else {
					require.ElementsMatch(t, fields, lb.SelectionFields, lb.Name)
				}
			}
			svc.Spec.SessionAffinity = corev1.ServiceAffinityNone
			svc.Spec.SessionAffinityConfig = nil
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, 2)
			lbs, err = nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
			require.NoError(t, err)
			require.NotEmpty(t, lbs)
			for _, lb := range lbs {
				require.Empty(t, lb.SelectionFields, lb.Name)
				require.NotContains(t, lb.Options, "affinity_timeout", lb.Name)
			}
		})
	}
}

func TestDeleteServiceLoadBalancerStateCleansEmptyLegacyLoadBalancers(t *testing.T) {
	for _, kind := range []string{serviceLBOwnerKind, switchLBRuleLBOwnerKind, routerLBRuleLBOwnerKind} {
		t.Run(kind, func(t *testing.T) {
			ctrl, nbClient, svc, _ := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
			ctrl.config.ClusterTCPLoadBalancer = "vpc-tcp-load"
			ctrl.config.ClusterTCPSessionLoadBalancer = "vpc-tcp-sess-load"
			vpc, err := ctrl.vpcsLister.Get("vpc")
			require.NoError(t, err)
			vpc = vpc.DeepCopy()
			legacy := ctrl.GenVpcLoadBalancer("vpc")
			vpc.Status.TCPLoadBalancer = legacy.TCPLoadBalancer
			vpc.Status.TCPSessionLoadBalancer = legacy.TCPSessLoadBalancer
			_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().UpdateStatus(t.Context(), vpc, metav1.UpdateOptions{})
			require.NoError(t, err)
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(vpc))
			ctrl.vpcsLister = kubeovnlister.NewVpcLister(indexer)
			require.NoError(t, nbClient.CreateLoadBalancer(legacy.TCPLoadBalancer, "tcp"))
			require.NoError(t, nbClient.CreateLoadBalancer(legacy.TCPSessLoadBalancer, "tcp"))
			require.NoError(t, nbClient.LoadBalancerAddVip(legacy.TCPLoadBalancer, "10.96.0.10:80", "10.0.0.2:8080"))
			require.NoError(t, nbClient.LoadBalancerAddVip(legacy.TCPSessLoadBalancer, "10.96.0.10:80", "10.0.0.2:8080"))
			require.NoError(t, nbClient.LoadBalancerAddVip(legacy.TCPSessLoadBalancer, "10.96.0.99:80", "10.0.0.9:8080"))

			if kind == serviceLBOwnerKind {
				require.NoError(t, ctrl.deleteServiceLoadBalancerState(&vpcService{Svc: svc, Vpc: "vpc"}, nil))
			} else {
				setServiceScopedLBOwner(svc, kind, "rule", "rule-uid")
				require.NoError(t, ctrl.deleteServiceScopedLoadBalancersWithLegacyVIPs(svc, "vpc", []string{"10.96.0.10:80"}))
			}
			lb, err := nbClient.GetLoadBalancer(legacy.TCPLoadBalancer, true)
			require.NoError(t, err)
			require.Nil(t, lb)
			session, err := nbClient.GetLoadBalancer(legacy.TCPSessLoadBalancer, false)
			require.NoError(t, err)
			require.Equal(t, map[string]string{"10.96.0.99:80": "10.0.0.9:8080"}, session.Vips)
			updated, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Get(t.Context(), "vpc", metav1.GetOptions{})
			require.NoError(t, err)
			require.Empty(t, updated.Status.TCPLoadBalancer)
			require.Equal(t, legacy.TCPSessLoadBalancer, updated.Status.TCPSessionLoadBalancer)
		})
	}
}

func TestServiceLoadBalancerProtocolChangeRollsBack(t *testing.T) {
	ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeNodePort, false)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	before, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	updated := svc.DeepCopy()
	updated.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	updated.Spec.Ports[2] = corev1.ServicePort{Name: "sctp", Protocol: corev1.ProtocolSCTP, Port: 9000}
	newEndpoints := []*discoveryv1.EndpointSlice{endpoints[0].DeepCopy()}
	newEndpoints[0].Endpoints[0].Addresses = []string{"10.0.0.3"}
	newEndpoints[0].Ports[2] = discoveryv1.EndpointPort{Name: new("sctp"), Protocol: new(corev1.ProtocolSCTP), Port: new(int32(9000))}
	recorder := &serviceLBTransactionClient{Client: nbClient.Client, fail: true}
	nbClient.Database = table.NewDatabase(recorder, nbClient.Timeout, table.RetryPolicy{})
	require.Error(t, ctrl.reconcileServiceEndpointSlices(updated, newEndpoints))
	require.Len(t, recorder.transactions, 1)
	after, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.ElementsMatch(t, before, after, "TCP update, SCTP creation and UDP deletion must all roll back")
	recorder.fail = false
	recorder.transactions = nil
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(updated, newEndpoints))
	require.Len(t, recorder.transactions, 1)
	udp, err := nbClient.GetLoadBalancer(serviceScopedLBName(svc, corev1.ProtocolUDP), true)
	require.NoError(t, err)
	require.Nil(t, udp)
	tcp, err := nbClient.GetLoadBalancer(serviceScopedLBName(svc, corev1.ProtocolTCP), false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"10.96.0.10:80": "10.0.0.3:8080", "10.96.0.10:81": "10.0.0.3:8081", "[fd00::10]:80": "", "[fd00::10]:81": "",
	}, tcp.Vips)
	require.Equal(t, "10800", tcp.Options["affinity_timeout"])
	sctp, err := nbClient.GetLoadBalancer(serviceScopedLBName(svc, corev1.ProtocolSCTP), false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"10.96.0.10:9000": "10.0.0.3:9000", "[fd00::10]:9000": ""}, sctp.Vips)
	recorder.transactions = nil
	require.NoError(t, ctrl.handleDeleteService(&vpcService{Svc: svc, Vpc: "vpc"}))
	require.Len(t, recorder.transactions, 1, "Service deletion must delete all protocol LBs together")
	remaining, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestServiceLoadBalancerTemplateTransitionIsAtomic(t *testing.T) {
	for _, serviceType := range []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeLoadBalancer} {
		t.Run(string(serviceType), func(t *testing.T) {
			ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, serviceType, true)
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			before, err := nbClient.ListLoadBalancers(nil)
			require.NoError(t, err)
			updated := svc.DeepCopy()
			updated.Spec.TrafficDistribution = nil
			updated.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
			recorder := &serviceLBTransactionClient{Client: nbClient.Client, fail: true}
			nbClient.Database = table.NewDatabase(recorder, nbClient.Timeout, table.RetryPolicy{})
			require.Error(t, ctrl.reconcileServiceEndpointSlices(updated, endpoints))
			require.Len(t, recorder.transactions, 1)
			after, err := nbClient.ListLoadBalancers(nil)
			require.NoError(t, err)
			require.ElementsMatch(t, before, after)
			recorder.fail = false
			recorder.transactions = nil
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(updated, endpoints))
			require.Len(t, recorder.transactions, 1, "template cleanup and literal VIP installation must share a transaction")
			lbs, err := nbClient.ListLoadBalancers(nil)
			require.NoError(t, err)
			for _, lb := range lbs {
				require.Equal(t, "false", lb.Options["template"])
				require.NotContains(t, lb.Options, "ct_flush", lb.Name)
			}
			var variables []ovnnb.ChassisTemplateVar
			require.NoError(t, nbClient.WhereCache(func(*ovnnb.ChassisTemplateVar) bool { return true }).List(t.Context(), &variables))
			for _, row := range variables {
				require.Empty(t, row.Variables)
			}
		})
	}
}

func TestServiceLoadBalancerEmptyEndpointsIsAtomic(t *testing.T) {
	ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	recorder := &serviceLBTransactionClient{Client: nbClient.Client}
	nbClient.Database = table.NewDatabase(recorder, nbClient.Timeout, table.RetryPolicy{})
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, nil))
	require.Len(t, recorder.transactions, 1)
	lbs, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.Len(t, lbs, 2)
	for _, lb := range lbs {
		require.Equal(t, "true", lb.Options["reject"])
		require.NotEmpty(t, lb.Vips)
		for _, backends := range lb.Vips {
			require.Empty(t, backends)
		}
		require.Empty(t, lb.IPPortMappings)
		require.Empty(t, lb.HealthCheck)
	}
}

func TestServiceLoadBalancerEmptyBackendLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		serviceType corev1.ServiceType
		template    bool
		affinity    corev1.ServiceAffinity
	}{
		{"ClusterIP", corev1.ServiceTypeClusterIP, false, corev1.ServiceAffinityNone},
		{"session affinity", corev1.ServiceTypeClusterIP, false, corev1.ServiceAffinityClientIP},
		{"traffic distribution", corev1.ServiceTypeClusterIP, true, corev1.ServiceAffinityNone},
		{"NodePort ClusterIP", corev1.ServiceTypeNodePort, false, corev1.ServiceAffinityNone},
		{"LoadBalancer ClusterIP", corev1.ServiceTypeLoadBalancer, false, corev1.ServiceAffinityNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, tc.serviceType, tc.template)
			svc.Spec.SessionAffinity = tc.affinity
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{Name: "sctp", Protocol: corev1.ProtocolSCTP, Port: 9000})
			for _, slice := range endpoints {
				slice.Ports = append(slice.Ports, discoveryv1.EndpointPort{Name: new("sctp"), Protocol: new(corev1.ProtocolSCTP), Port: new(int32(9000))})
			}
			unready := []*discoveryv1.EndpointSlice{endpoints[0].DeepCopy(), endpoints[1].DeepCopy()}
			for _, slice := range unready {
				slice.Endpoints[0].Conditions.Ready = new(false)
			}
			for _, stage := range []struct {
				name      string
				endpoints []*discoveryv1.EndpointSlice
				empty     bool
			}{
				{"initially empty", nil, true},
				{"ready", endpoints, false},
				{"all unready", unready, true},
				{"removed", nil, true},
				{"restored", endpoints, false},
			} {
				t.Run(stage.name, func(t *testing.T) {
					require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, stage.endpoints))
					assertServiceClusterVIPBackends(t, nbClient, svc, stage.empty)
					if tc.serviceType == corev1.ServiceTypeLoadBalancer {
						if tc.template {
							assertServiceExternalLocalTemplateState(t, nbClient, svc)
						} else {
							assertServiceExternalVIPBackends(t, nbClient, svc, stage.empty)
						}
					}
				})
			}
			require.NoError(t, ctrl.handleDeleteService(&vpcService{Svc: svc, Vpc: "vpc"}))
			remaining, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
			require.NoError(t, err)
			require.Empty(t, remaining)
		})
	}
}

func assertServiceClusterVIPBackends(t *testing.T, nbClient *ovs.OVNNbClient, svc *corev1.Service, empty bool) {
	t.Helper()
	var variables []ovnnb.ChassisTemplateVar
	require.NoError(t, nbClient.WhereCache(func(*ovnnb.ChassisTemplateVar) bool { return true }).List(t.Context(), &variables))
	for _, port := range svc.Spec.Ports {
		for _, ip := range svc.Spec.ClusterIPs {
			family := ""
			if serviceUsesTemplateLB(svc) {
				family = strings.ToLower(util.CheckProtocol(ip))
			}
			lb, err := nbClient.GetLoadBalancer(serviceScopedLBNameForTrafficClassAndFamily(svc, port.Protocol, serviceLBInternalTraffic, family), false)
			require.NoError(t, err)
			require.Equal(t, "true", lb.Options["reject"])
			vip := util.JoinHostPort(ip, port.Port)
			if serviceUsesTemplateLB(svc) {
				require.Len(t, variables, 1)
				base := fmt.Sprintf("%s%s_%s", serviceTrafficDistributionVariablePrefix(svc), strings.ToLower(string(port.Protocol)), util.Sha256Hash([]byte(vip))[:8])
				vip = "^" + base + "_vip:" + strconv.Itoa(int(port.Port))
				require.Equal(t, ip, variables[0].Variables[base+"_vip"])
				value, exists := lb.Vips[vip]
				require.True(t, exists)
				if empty {
					require.Empty(t, value, "northd must see empty backends before chassis variables are resolved")
				} else {
					require.Equal(t, "^"+base+"_backends", value)
				}
				_, exists = variables[0].Variables[base+"_backends"]
				require.True(t, exists)
				require.Equal(t, empty, variables[0].Variables[base+"_backends"] == "")
			} else {
				backends, exists := lb.Vips[vip]
				require.True(t, exists, "VIP %s must remain while its Service exists", vip)
				require.Equal(t, empty, backends == "", vip)
			}
		}
	}
}

func assertServiceExternalVIPBackends(t *testing.T, nbClient *ovs.OVNNbClient, svc *corev1.Service, empty bool) {
	t.Helper()
	var vips []string
	switch {
	case svc.Annotations[util.SwitchLBRuleVipsAnnotation] != "":
		vips = []string{svc.Annotations[util.SwitchLBRuleVipsAnnotation]}
	case svc.Annotations[util.RouterLBRuleVipsAnnotation] != "":
		vips = strings.Split(svc.Annotations[util.RouterLBRuleVipsAnnotation], ",")
	default:
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				vips = append(vips, ingress.IP)
			}
		}
	}
	for _, lbVip := range vips {
		lbVip = strings.TrimSpace(lbVip)
		if lbVip == "" {
			continue
		}
		for _, port := range svc.Spec.Ports {
			lbName := serviceScopedExternalLBName(svc, port.Protocol, lbVip)
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.Equal(t, "true", lb.Options["reject"], lbName)
			vip := util.JoinHostPort(lbVip, port.Port)
			backends, exists := lb.Vips[vip]
			require.True(t, exists, "VIP %s must remain while its Service exists", vip)
			require.Equal(t, empty, backends == "", vip)
		}
	}
}

func assertServiceExternalLocalTemplateState(t *testing.T, nbClient *ovs.OVNNbClient, svc *corev1.Service) {
	t.Helper()
	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP == "" {
			continue
		}
		for _, port := range svc.Spec.Ports {
			lbName := serviceScopedExternalLBName(svc, port.Protocol, ingress.IP)
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.NotContains(t, lb.Options, "reject", lbName)
			require.NotEmpty(t, lb.Vips, lbName)
		}
	}
}

func TestServiceLoadBalancerRejectPolicyTransitions(t *testing.T) {
	ctrl, nbClient, svc, _ := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
	svc.Spec.Selector = map[string]string{"app": "no-matching-backend"}
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, nil))
	assertServiceClusterVIPBackends(t, nbClient, svc, true)
	svc.Spec.InternalTrafficPolicy = new(corev1.ServiceInternalTrafficPolicyLocal)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, nil))
	lbs, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
	require.NoError(t, err)
	require.NotEmpty(t, lbs)
	for _, lb := range lbs {
		require.NotContains(t, lb.Options, "reject")
		require.Empty(t, lb.Vips)
	}
	svc.Spec.InternalTrafficPolicy = new(corev1.ServiceInternalTrafficPolicyCluster)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, nil))
	assertServiceClusterVIPBackends(t, nbClient, svc, true)
}

func TestRuleServiceLoadBalancersAreAtomic(t *testing.T) {
	for _, kind := range []string{switchLBRuleLBOwnerKind, routerLBRuleLBOwnerKind} {
		t.Run(kind, func(t *testing.T) {
			ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
			svc.Name = generateSvcName("rule")
			if kind == routerLBRuleLBOwnerKind {
				svc.Name = generateRlrSvcName("rule")
			}
			setServiceScopedLBOwner(svc, kind, "rule", "rule-uid")
			svc.Spec.ClusterIP = corev1.ClusterIPNone
			svc.Spec.ClusterIPs = nil
			svc.Annotations[util.ServiceHealthCheck] = "false"
			annotation := util.SwitchLBRuleVipsAnnotation
			if kind == routerLBRuleLBOwnerKind {
				annotation = util.RouterLBRuleVipsAnnotation
			}
			svc.Annotations[annotation] = "10.96.0.10"
			recorder := &serviceLBTransactionClient{Client: nbClient.Client}
			nbClient.Database = table.NewDatabase(recorder, nbClient.Timeout, table.RetryPolicy{})
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, 1)
			lbs, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
			require.NoError(t, err)
			require.Len(t, lbs, 2)
			var uuids []string
			for _, lb := range lbs {
				uuids = append(uuids, lb.UUID)
				require.NotEmpty(t, lb.Vips)
			}
			ls, err := nbClient.GetLogicalSwitch("subnet", false)
			require.NoError(t, err)
			require.ElementsMatch(t, uuids, ls.LoadBalancer)
			lr, err := nbClient.GetLogicalRouter("vpc", false)
			require.NoError(t, err)
			if kind == routerLBRuleLBOwnerKind {
				require.ElementsMatch(t, uuids, lr.LoadBalancer)
			} else {
				require.Empty(t, lr.LoadBalancer)
			}

			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, nil))
			assertServiceExternalVIPBackends(t, nbClient, svc, true)
		})
	}
}
