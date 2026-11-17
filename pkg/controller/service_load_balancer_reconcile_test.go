package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
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

func routerLBRuleHealthCheckReconcileFixture(t *testing.T, vips string) (*Controller, *ovs.OVNNbClient, *corev1.Service, []*discoveryv1.EndpointSlice) {
	t.Helper()

	svc := &corev1.Service{
		Namespace: "default",
		Name:      generateRlrSvcName("rlr1"),
		UID:       "service-uid",
		Annotations: map[string]string{
			util.LogicalRouterAnnotation:    "vpc",
			util.VpcAnnotation:              "vpc",
			util.LogicalSwitchAnnotation:    "subnet",
			util.RouterLBRuleVipsAnnotation: vips,
			util.ServiceHealthCheck:         "true",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{{
				Name:     "web",
				Protocol: corev1.ProtocolTCP,
				Port:     80,
			}},
			Selector: map[string]string{"app": "backend"},
		},
	}
	setServiceScopedLBOwner(svc, routerLBRuleLBOwnerKind, "rlr1", "rlr-uid")

	hasIPv4, hasIPv6 := false, false
	for vip := range strings.SplitSeq(vips, ",") {
		switch util.CheckProtocol(strings.TrimSpace(vip)) {
		case kubeovnv1.ProtocolIPv4:
			hasIPv4 = true
		case kubeovnv1.ProtocolIPv6:
			hasIPv6 = true
		}
	}
	addresses := make([]string, 0, 2)
	if hasIPv4 {
		addresses = append(addresses, "10.0.0.2")
	}
	if hasIPv6 {
		addresses = append(addresses, "fd00::2")
	}
	pod := &corev1.Pod{
		Namespace: "default",
		Name:      "backend",
		Labels:    map[string]string{"app": "backend"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: "subnet",
			util.LogicalRouterAnnotation: "vpc",
			util.IPAddressAnnotation:     strings.Join(addresses, ","),
		},
	}
	subnet := &kubeovnv1.Subnet{Name: "subnet", Spec: kubeovnv1.SubnetSpec{Vpc: "vpc", Provider: util.OvnProvider}}
	healthCheckVIP := &kubeovnv1.Vip{
		Name:   "subnet",
		Spec:   kubeovnv1.VipSpec{Subnet: "subnet"},
		Status: kubeovnv1.VipStatus{},
	}
	if hasIPv4 {
		healthCheckVIP.Status.V4ip = "10.0.0.254"
	}
	if hasIPv6 {
		healthCheckVIP.Status.V6ip = "fd00::254"
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services: []*corev1.Service{svc},
		Pods:     []*corev1.Pod{pod},
		Subnets:  []*kubeovnv1.Subnet{subnet},
		Vips:     []*kubeovnv1.Vip{healthCheckVIP},
		Vpcs:     []*kubeovnv1.Vpc{{Name: "vpc"}},
	})
	require.NoError(t, err)

	ctrl := fc.fakeController
	ctrl.config.EnableOvnLB = true
	ctrl.config.ClusterRouter = "vpc"
	nbClient := newInMemoryOVNNbClient(t)
	ctrl.OVNNbClient = nbClient
	require.NoError(t, nbClient.CreateBareLogicalSwitch("subnet"))
	require.NoError(t, nbClient.CreateLogicalRouter("vpc"))

	targetRef := &corev1.ObjectReference{Kind: util.KindPod, Namespace: "default", Name: pod.Name}
	portName := "web"
	protocol := corev1.ProtocolTCP
	port := int32(8080)
	endpoints := make([]*discoveryv1.EndpointSlice, 0, 2)
	if hasIPv4 {
		endpoints = append(endpoints, &discoveryv1.EndpointSlice{
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: &portName, Protocol: &protocol, Port: &port}},
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.2"}, TargetRef: targetRef}},
		})
	}
	if hasIPv6 {
		endpoints = append(endpoints, &discoveryv1.EndpointSlice{
			AddressType: discoveryv1.AddressTypeIPv6,
			Ports:       []discoveryv1.EndpointPort{{Name: &portName, Protocol: &protocol, Port: &port}},
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"fd00::2"}, TargetRef: targetRef}},
		})
	}
	portNameForOVN := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	require.NoError(t, nbClient.CreateBareLogicalSwitchPort("subnet", portNameForOVN, strings.Join(addresses, ","), "00:00:00:00:00:02"))
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	return ctrl, nbClient, svc, endpoints
}

func TestRouterLBRuleHealthCheckReconcileUsesIPv6SourceBrackets(t *testing.T) {
	for _, tc := range []struct {
		name string
		vips string
	}{
		{name: "IPv6-only", vips: "fd00::100"},
		{name: "dual-stack", vips: "192.0.2.100,fd00::100"},
		{name: "dual-stack reversed", vips: "fd00::100,192.0.2.100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, nbClient, svc, _ := routerLBRuleHealthCheckReconcileFixture(t, tc.vips)
			lbName := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic)
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			lspName := ovs.PodNameToPortName("backend", "default", util.OvnProvider)
			wantVIPs := map[string]string{"[fd00::100]:80": "[fd00::2]:8080"}
			wantMappings := map[string]string{"[fd00::2]": lspName + ":[fd00::254]"}
			wantHealthCheckVIPs := []string{"[fd00::100]:80"}
			if strings.Contains(tc.name, "dual-stack") {
				wantVIPs["192.0.2.100:80"] = "10.0.0.2:8080"
				wantMappings["10.0.0.2"] = lspName + ":10.0.0.254"
				wantHealthCheckVIPs = append(wantHealthCheckVIPs, "192.0.2.100:80")
			}
			require.Equal(t, wantVIPs, lb.Vips)
			require.Equal(t, wantMappings, lb.IPPortMappings)

			healthChecks, err := nbClient.ListLoadBalancerHealthChecks(nil)
			require.NoError(t, err)
			require.Len(t, healthChecks, len(wantHealthCheckVIPs))
			healthCheckUUIDs := make([]string, 0, len(healthChecks))
			healthCheckVIPs := make([]string, 0, len(healthChecks))
			healthChecksByVIP := make(map[string]string, len(healthChecks))
			for _, healthCheck := range healthChecks {
				healthCheckUUIDs = append(healthCheckUUIDs, healthCheck.UUID)
				healthCheckVIPs = append(healthCheckVIPs, healthCheck.Vip)
				healthChecksByVIP[healthCheck.Vip] = healthCheck.UUID
			}
			require.ElementsMatch(t, lb.HealthCheck, healthCheckUUIDs)
			require.ElementsMatch(t, wantHealthCheckVIPs, healthCheckVIPs)
			for _, vip := range wantHealthCheckVIPs {
				require.Contains(t, lb.HealthCheck, healthChecksByVIP[vip])
			}
		})
	}
}

func TestRouterLBRuleHealthCheckReconcileDisablesAndPrunesFamilyMappings(t *testing.T) {
	ctrl, nbClient, svc, endpoints := routerLBRuleHealthCheckReconcileFixture(t, "192.0.2.100,fd00::100")
	lbName := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic)

	svc.Annotations[util.ServiceHealthCheck] = "false"
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	lb, err := nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"192.0.2.100:80": "10.0.0.2:8080",
		"[fd00::100]:80": "[fd00::2]:8080",
	}, lb.Vips)
	require.Empty(t, lb.HealthCheck)
	require.Empty(t, lb.IPPortMappings)
	healthChecks, err := nbClient.ListLoadBalancerHealthChecks(nil)
	require.NoError(t, err)
	require.Empty(t, healthChecks)

	svc.Annotations[util.ServiceHealthCheck] = "true"
	endpoints[0].Endpoints[0].TargetRef = nil
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	lb, err = nbClient.GetLoadBalancer(lbName, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"192.0.2.100:80": "10.0.0.2:8080",
		"[fd00::100]:80": "[fd00::2]:8080",
	}, lb.Vips)
	require.Equal(t, map[string]string{"[fd00::2]": "backend.default:[fd00::254]"}, lb.IPPortMappings)
	require.Len(t, lb.HealthCheck, 1)
	healthChecks, err = nbClient.ListLoadBalancerHealthChecks(nil)
	require.NoError(t, err)
	require.Len(t, healthChecks, 1)
	require.Equal(t, "[fd00::100]:80", healthChecks[0].Vip)
	require.Equal(t, lb.HealthCheck[0], healthChecks[0].UUID)
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
	require.Equal(t, map[string]string{"10.96.0.10:80": "10.0.0.3:8080", "10.96.0.10:81": "10.0.0.3:8081"}, tcp.Vips)
	require.Equal(t, "10800", tcp.Options["affinity_timeout"])
	sctp, err := nbClient.GetLoadBalancer(serviceScopedLBName(svc, corev1.ProtocolSCTP), false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"10.96.0.10:9000": "10.0.0.3:9000"}, sctp.Vips)
	recorder.transactions = nil
	require.NoError(t, ctrl.handleDeleteService(&vpcService{Svc: svc, Vpc: "vpc"}))
	require.Len(t, recorder.transactions, 1, "Service deletion must delete all protocol LBs together")
	remaining, err := nbClient.ListLoadBalancers(nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestNodePortTemplateUsesNodeAddressAndLocalBackends(t *testing.T) {
	ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeNodePort, false)
	for index := range svc.Spec.Ports {
		svc.Spec.Ports[index].NodePort = int32(30080 + index)
	}
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
		{Type: corev1.NodeInternalIP, Address: "2001:db8::10"},
	}
	ctrl.OVNSbClient.(*mockovs.MockSbClient).EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis", Hostname: "node"}}, nil)

	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	for _, family := range []string{"ipv4", "ipv6"} {
		lbName := serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBExternalTraffic, family)
		lb, err := nbClient.GetLoadBalancer(lbName, false)
		require.NoError(t, err)
		require.NotNil(t, lb)
		require.Equal(t, "true", lb.Options["template"])
		require.Equal(t, family, lb.Options["address-family"])
		foundTemplateVIP := false
		for vip := range lb.Vips {
			if strings.HasPrefix(vip, "^"+serviceNodePortTemplatePrefix(svc)+"tcp_") {
				foundTemplateVIP = true
				break
			}
		}
		require.True(t, foundTemplateVIP)
	}
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
		require.Empty(t, lb.Vips)
		require.Empty(t, lb.IPPortMappings)
		require.Empty(t, lb.HealthCheck)
	}
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
		})
	}
}

func TestRuleServiceSkipsHealthCheckWithoutOVNBackendMapping(t *testing.T) {
	ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
	svc.Spec.ClusterIP = corev1.ClusterIPNone
	svc.Spec.ClusterIPs = nil
	svc.Annotations[util.SwitchLBRuleVipsAnnotation] = "10.96.0.20"
	setServiceScopedLBOwner(svc, switchLBRuleLBOwnerKind, "rule", "rule-uid")

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(&corev1.Pod{
		Namespace: svc.Namespace, Name: "unrelated",
		Annotations: map[string]string{nadv1.NetworkAttachmentAnnot: "["},
	}))
	ctrl.podsLister = corelisters.NewPodLister(indexer)
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))

	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), "subnet", metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))

	lbs, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
	require.NoError(t, err)
	require.NotEmpty(t, lbs)
	backendFound := false
	for _, lb := range lbs {
		require.Empty(t, lb.HealthCheck, lb.Name)
		if backend, ok := lb.Vips["10.96.0.20:80"]; ok {
			require.Equal(t, "10.0.0.2:8080", backend)
			backendFound = true
		}
	}
	require.True(t, backendFound)
}

func TestRuleServiceSkipsHealthCheckForUnmappedEligibleBackend(t *testing.T) {
	for _, backendReady := range []bool{false, true} {
		t.Run(fmt.Sprintf("OVN backend ready=%t", backendReady), func(t *testing.T) {
			ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
			svc.Spec.ClusterIP = corev1.ClusterIPNone
			svc.Spec.ClusterIPs = nil
			svc.Spec.Ports = svc.Spec.Ports[:1]
			svc.Spec.Selector = map[string]string{"app": "backend"}
			svc.Annotations[util.SwitchLBRuleVipsAnnotation] = "10.96.0.20"
			setServiceScopedLBOwner(svc, switchLBRuleLBOwnerKind, "rule", "rule-uid")
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			ctrl.podsLister = corelisters.NewPodLister(indexer)
			for _, pod := range []*corev1.Pod{
				{Name: "agent", Namespace: svc.Namespace, Spec: corev1.PodSpec{HostNetwork: true}},
				{Name: "backend", Namespace: svc.Namespace, Annotations: map[string]string{
					util.LogicalSwitchAnnotation: "subnet", util.IPAddressAnnotation: "10.0.0.3",
				}},
			} {
				require.NoError(t, indexer.Add(pod))
			}
			require.NoError(t, nbClient.CreateBareLogicalSwitchPort("subnet", "backend.default", "10.0.0.3", "00:00:00:00:00:03"))
			endpoints[0].Endpoints[0].TargetRef = &corev1.ObjectReference{Kind: "Pod", Namespace: svc.Namespace, Name: "agent"}
			endpoints[0].Endpoints = append(endpoints[0].Endpoints, discoveryv1.Endpoint{
				Addresses:  []string{"10.0.0.3"},
				TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: svc.Namespace, Name: "backend"},
				Conditions: discoveryv1.EndpointConditions{Ready: new(backendReady)},
			})

			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints[:1]))

			_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), "subnet", metav1.GetOptions{})
			require.True(t, k8serrors.IsNotFound(err))
			lbName := serviceScopedExternalLBName(svc, corev1.ProtocolTCP, "10.96.0.20")
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			wantBackend := "10.0.0.2:8080"
			if backendReady {
				wantBackend += ",10.0.0.3:8080"
			}
			require.Equal(t, map[string]string{"10.96.0.20:80": wantBackend}, lb.Vips)
			require.Empty(t, lb.HealthCheck)
			require.Empty(t, lb.IPPortMappings)
		})
	}
}

type serviceLBCompatibilityClient struct {
	ovs.NbClient
}

func TestServiceLoadBalancerCompatibilityPathDisablesHealthCheck(t *testing.T) {
	ctrl, nbClient, svc, _ := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
	ctrl.OVNNbClient = serviceLBCompatibilityClient{NbClient: nbClient}
	const lbName, vip, backend = "legacy", "10.96.0.10:80", "10.0.0.2:8080"
	require.NoError(t, nbClient.CreateLoadBalancer(lbName, "tcp"))
	require.NoError(t, nbClient.LoadBalancerAddVip(lbName, vip, backend))
	require.NoError(t, nbClient.LoadBalancerAddHealthCheck(lbName, vip, false, map[string]string{"10.0.0.2": "backend.default:10.0.0.254"}, nil))

	mapping := IPPortMapping{"10.0.0.2": "backend.default"}
	ctx := &endpointSliceReconcileContext{
		service: svc, vpc: &kubeovnv1.Vpc{Name: "vpc"},
		desiredScopedVIPs: make(map[string]map[string]struct{}),
	}
	state := &serviceEndpointVIPState{
		lb: lbName, vip: vip, port: svc.Spec.Ports[0],
		backends: []string{backend}, mapping: mapping, distributed: true,
	}
	require.NoError(t, ctrl.addServiceEndpointVIP(ctx, state))

	lb, check, err := nbClient.GetLoadBalancerHealthCheck(lbName, vip, true)
	require.NoError(t, err)
	require.Nil(t, check)
	require.Equal(t, map[string]string{vip: backend}, lb.Vips)
	require.Equal(t, map[string]string(mapping), lb.IPPortMappings)
}

func TestRuleServiceHealthChecksRemainPerVIP(t *testing.T) {
	service := &corev1.Service{
		Namespace: "default",
		Name:      "rlr-service",
		UID:       "service-uid",
		Annotations: map[string]string{
			util.LogicalRouterAnnotation:    "vpc",
			util.VpcAnnotation:              "vpc",
			util.LogicalSwitchAnnotation:    "subnet",
			util.RouterLBRuleVipsAnnotation: "fd00::20,10.96.0.20",
			util.ServiceHealthCheck:         "true",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Ports:     []corev1.ServicePort{{Name: "web", Protocol: corev1.ProtocolTCP, Port: 80}},
			Selector:  map[string]string{"app": "backend"},
		},
	}
	setServiceScopedLBOwner(service, routerLBRuleLBOwnerKind, "rule", "rule-uid")
	pod := &corev1.Pod{
		Namespace: "default",
		Name:      "backend",
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: "subnet",
			util.IPAddressAnnotation:     "10.0.0.2,fd00::2",
		},
	}
	endpointSlice := &discoveryv1.EndpointSlice{
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: new("web"), Protocol: new(corev1.ProtocolTCP), Port: new(int32(8080))}},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"10.0.0.2"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "backend"},
		}},
	}
	ipv6Endpoints := endpointSlice.DeepCopy()
	ipv6Endpoints.AddressType = discoveryv1.AddressTypeIPv6
	ipv6Endpoints.Endpoints[0].Addresses = []string{"fd00::2"}
	healthCheckVIP := &kubeovnv1.Vip{
		Name:   "subnet",
		Spec:   kubeovnv1.VipSpec{Subnet: "subnet"},
		Status: kubeovnv1.VipStatus{V4ip: "10.0.0.254", V6ip: "fd00::254"},
	}
	subnet := &kubeovnv1.Subnet{Name: "subnet", Spec: kubeovnv1.SubnetSpec{Vpc: "vpc", Provider: util.OvnProvider}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services: []*corev1.Service{service},
		Pods:     []*corev1.Pod{pod},
		Subnets:  []*kubeovnv1.Subnet{subnet},
		Vpcs:     []*kubeovnv1.Vpc{{Name: "vpc"}},
		Vips:     []*kubeovnv1.Vip{healthCheckVIP},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.svcKeyMutex = keymutex.NewHashed(0)
	ctrl.config.EnableOvnLB = true
	ctrl.config.ClusterRouter = "vpc"
	nbClient := newInMemoryOVNNbClient(t)
	ctrl.OVNNbClient = nbClient
	require.NoError(t, nbClient.CreateBareLogicalSwitch("subnet"))
	require.NoError(t, nbClient.CreateLogicalRouter("vpc"))

	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	lbName := serviceScopedExternalLBName(service, corev1.ProtocolTCP, "10.96.0.20")
	endpoints := []*discoveryv1.EndpointSlice{endpointSlice, ipv6Endpoints}

	assertForwardingWithoutProbe := func() {
		t.Helper()
		lb, check, err := nbClient.GetLoadBalancerHealthCheck(lbName, "10.96.0.20:80", true)
		require.NoError(t, err)
		require.Nil(t, check)
		require.Equal(t, map[string]string{"10.96.0.20:80": "10.0.0.2:8080", "[fd00::20]:80": "[fd00::2]:8080"}, lb.Vips)
	}
	require.ErrorContains(t, ctrl.reconcileServiceEndpointSlices(service, endpoints), "waiting for health-check backend logical port")
	assertForwardingWithoutProbe()

	require.NoError(t, nbClient.CreateBareLogicalSwitchPort("subnet", portName, "10.0.0.2,fd00::2", "00:00:00:00:00:02"))
	stalePod := pod.DeepCopy()
	delete(stalePod.Annotations, util.IPAddressAnnotation)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(stalePod))
	ctrl.podsLister = corelisters.NewPodLister(indexer)
	require.ErrorContains(t, ctrl.reconcileServiceEndpointSlices(service, endpoints), "waiting for OVN IP annotation")
	assertForwardingWithoutProbe()

	require.NoError(t, indexer.Update(pod))
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(service, endpoints))

	lb, check, err := nbClient.GetLoadBalancerHealthCheck(lbName, "10.96.0.20:80", false)
	require.NoError(t, err)
	require.NotNil(t, check)
	require.Equal(t, map[string]string{"10.96.0.20:80": "10.0.0.2:8080", "[fd00::20]:80": "[fd00::2]:8080"}, lb.Vips)
	require.Equal(t, map[string]string{
		"10.0.0.2":  portName + ":10.0.0.254",
		"[fd00::2]": portName + ":[fd00::254]",
	}, lb.IPPortMappings)
	_, check, err = nbClient.GetLoadBalancerHealthCheck(lbName, "[fd00::20]:80", false)
	require.NoError(t, err)
	require.NotNil(t, check)
}

func TestRuleServiceKeepsForwardingWhenHealthCheckVIPIsUnavailable(t *testing.T) {
	ctrl, nbClient, svc, _ := routerLBRuleHealthCheckReconcileFixture(t, "10.96.0.20")
	lbName := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic)

	require.NoError(t, ctrl.config.KubeOvnClient.KubeovnV1().Vips().Delete(t.Context(), "subnet", metav1.DeleteOptions{}))
	ctrl.virtualIpsLister = kubeovnlister.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	err := ctrl.reconcileServiceEndpointSlices(svc, []*discoveryv1.EndpointSlice{
		{
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: new("web"), Protocol: new(corev1.ProtocolTCP), Port: new(int32(8080))}},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.2"},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "backend"},
			}},
		},
	})
	require.ErrorContains(t, err, "get health-check VIP for service")

	lb, check, err := nbClient.GetLoadBalancerHealthCheck(lbName, "10.96.0.20:80", true)
	require.NoError(t, err)
	require.Nil(t, check)
	require.Equal(t, map[string]string{"10.96.0.20:80": "10.0.0.2:8080"}, lb.Vips)
}

func TestDistributedServiceMappingIsKeptWithoutHealthCheck(t *testing.T) {
	ctrl, nbClient, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeClusterIP, false)
	svc.Spec.ClusterIPs = []string{"10.96.0.10"}
	svc.Spec.Ports = svc.Spec.Ports[:1]
	svc.Spec.InternalTrafficPolicy = new(corev1.ServiceInternalTrafficPolicyLocal)
	svc.Spec.Selector = map[string]string{"app": "backend"}
	pod := &corev1.Pod{
		Namespace: svc.Namespace, Name: "backend",
		Annotations: map[string]string{util.LogicalSwitchAnnotation: "subnet", util.IPAddressAnnotation: "10.0.0.2"},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(pod))
	ctrl.podsLister = corelisters.NewPodLister(indexer)
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	require.NoError(t, nbClient.CreateBareLogicalSwitchPort("subnet", portName, "10.0.0.2", "00:00:00:00:00:02"))
	endpoints[0].Endpoints[0].TargetRef = &corev1.ObjectReference{Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name}

	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints[:1]))

	lb, err := nbClient.GetLoadBalancer(serviceScopedLBName(svc, corev1.ProtocolTCP), false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"10.96.0.10:80": "10.0.0.2:8080"}, lb.Vips)
	require.Equal(t, map[string]string{"10.0.0.2": portName}, lb.IPPortMappings)
	require.Empty(t, lb.HealthCheck)
}
