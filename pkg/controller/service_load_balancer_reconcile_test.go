package controller

import (
	"context"
	"slices"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type serviceLBTransactionClient struct {
	client.Client
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
	return c.Client.Transact(ctx, ops...)
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
			nbClient.Client = recorder
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, 1, "all protocols, address families, VIPs and attachments must be committed together")
			lbs, err := nbClient.ListLoadBalancers(serviceScopedLBOwner(svc).ownsLoadBalancer)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(lbs), 2)
			for _, lb := range lbs {
				if tc.serviceType == corev1.ServiceTypeLoadBalancer && lb.Name == serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBExternalTraffic, "ipv6") ||
					tc.serviceType == corev1.ServiceTypeLoadBalancer && lb.Name == serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolUDP, serviceLBExternalTraffic, "ipv6") {
					// No IPv6 external ingress was allocated, so this family has no VIP.
					require.Empty(t, lb.Vips)
					continue
				}
				require.NotEmpty(t, lb.Vips)
			}
			require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
			require.Len(t, recorder.transactions, 1, "an unchanged Service must not produce another NB transaction")
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
	nbClient.Client = recorder
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
			nbClient.Client = recorder
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
	nbClient.Client = recorder
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
