package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestPodNATAnnotationUpdateReconcilesRoutedPod(t *testing.T) {
	for _, annotation := range []string{util.EipAnnotation, util.SnatAnnotation} {
		t.Run(annotation, func(t *testing.T) {
			fc, client, pod, subnet := newPodEIPRouteFixture(t, kubeovnv1.GWDistributedType, false)
			if annotation == util.SnatAnnotation {
				delete(pod.Annotations, util.EipAnnotation)
				pod.Annotations[util.SnatAnnotation] = "203.0.113.2"
			}
			controller := fc.fakeController
			nets := []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}
			require.NoError(t, controller.reconcileRouteSubnets(pod, nets))
			oldPod := pod.DeepCopy()
			oldPod.ResourceVersion = "1"
			newPod := oldPod.DeepCopy()
			newPod.ResourceVersion = "2"
			delete(newPod.Annotations, annotation)
			_, err := fc.kubeClient.CoreV1().Pods(pod.Namespace).Update(t.Context(), newPod, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				cached, err := controller.podsLister.Pods(pod.Namespace).Get(pod.Name)
				return err == nil && cached.ResourceVersion == newPod.ResourceVersion
			}, time.Second, time.Millisecond)
			require.Empty(t, needRouteSubnets(newPod, nets, false), "the Pod is already routed")

			controller.enqueueUpdatePod(oldPod, newPod)
			require.Equal(t, 1, controller.updatePodRouteQueue.Len())
			key, shutdown := controller.updatePodRouteQueue.Get()
			require.False(t, shutdown)
			defer controller.updatePodRouteQueue.Done(key)
			require.NoError(t, controller.handleUpdatePodRoute(key))
			policies, err := client.ListLogicalRouterPolicies(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority, nil, true)
			require.NoError(t, err)
			require.Empty(t, policies)
			nats, err := client.ListNats(util.DefaultVpc, "", "10.0.0.2", nil)
			require.NoError(t, err)
			require.Empty(t, nats)

			// A routed annotation update must not force another route reconciliation.
			updatedPod := newPod.DeepCopy()
			updatedPod.ResourceVersion = "3"
			controller.enqueueUpdatePod(newPod, updatedPod)
			require.Zero(t, controller.updatePodRouteQueue.Len())
			require.Equal(t, 1, controller.addOrUpdatePodQueue.Len())
		})
	}
}

func TestReconcileRouteSubnetsPreservesRequiredEIPPolicy(t *testing.T) {
	for _, annotation := range []string{util.SnatAnnotation, util.NorthGatewayAnnotation} {
		t.Run(annotation, func(t *testing.T) {
			fc, client, pod, subnet := newPodEIPRouteFixture(t, kubeovnv1.GWDistributedType, false)
			nets := []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}
			require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
			pod = pod.DeepCopy()
			delete(pod.Annotations, util.EipAnnotation)
			nextHop := "203.0.113.1"
			pod.Annotations[annotation] = "203.0.113.3"
			if annotation == util.NorthGatewayAnnotation {
				nextHop = pod.Annotations[annotation]
			}
			require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
			policies, err := client.ListLogicalRouterPolicies(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority, nil, true)
			require.NoError(t, err)
			require.Len(t, policies, 1)
			require.Equal(t, "ip4.src == 10.0.0.2", policies[0].Match)
			require.Equal(t, []string{nextHop}, policies[0].Nexthops)
			nats, err := client.ListNats(util.DefaultVpc, ovnnb.NATTypeDNATAndSNAT, "10.0.0.2", nil)
			require.NoError(t, err)
			require.Empty(t, nats)
			if annotation == util.SnatAnnotation {
				nats, err = client.ListNats(util.DefaultVpc, ovnnb.NATTypeSNAT, "10.0.0.2", nil)
				require.NoError(t, err)
				require.Len(t, nats, 1)
				require.Equal(t, "203.0.113.3", nats[0].ExternalIP)
			}
		})
	}
}

func TestReconcileRouteSubnetsRetriesEIPPolicyCleanup(t *testing.T) {
	fc, client, pod, subnet := newPodEIPRouteFixture(t, kubeovnv1.GWDistributedType, false)
	nets := []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}
	require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
	pod = pod.DeepCopy()
	delete(pod.Annotations, util.EipAnnotation)
	fc.fakeController.OVNNbClient = fc.mockOvnClient
	wantErr := errors.New("policy deletion failed")
	fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicy(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority,
		"ip4.src == 10.0.0.2").Return(wantErr)
	require.ErrorIs(t, fc.fakeController.reconcileRouteSubnets(pod, nets), wantErr)
	fc.fakeController.OVNNbClient = client
	require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
	policies, err := client.ListLogicalRouterPolicies(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority, nil, true)
	require.NoError(t, err)
	require.Empty(t, policies)
}

func TestReconcileRouteSubnetsRemovesEIPPolicy(t *testing.T) {
	for _, gateway := range []string{kubeovnv1.GWDistributedType, kubeovnv1.GWCentralizedType} {
		t.Run(gateway, func(t *testing.T) {
			for _, dualStack := range []bool{false, true} {
				name := "IPv4"
				if dualStack {
					name = "dual-stack"
				}
				t.Run(name, func(t *testing.T) {
					fc, client, pod, subnet := newPodEIPRouteFixture(t, gateway, dualStack)
					nets := []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}
					require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
					policies, err := client.ListLogicalRouterPolicies(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority, nil, true)
					require.NoError(t, err)
					require.NotEmpty(t, policies)
					nats, err := client.ListNats(util.DefaultVpc, ovnnb.NATTypeDNATAndSNAT, "10.0.0.2", nil)
					require.NoError(t, err)
					require.Len(t, nats, 1)

					// Another Pod's route must survive cleanup of this Pod.
					const otherMatch = "ip4.src == 10.0.0.3"
					require.NoError(t, client.AddLogicalRouterPolicy(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority,
						otherMatch, ovnnb.LogicalRouterPolicyActionReroute, []string{"203.0.113.1"}, nil, nil))
					pod = pod.DeepCopy()
					delete(pod.Annotations, util.EipAnnotation)
					require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))

					policies, err = client.ListLogicalRouterPolicies(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority, nil, true)
					require.NoError(t, err)
					require.Len(t, policies, 1, "removing EIP must remove every source policy for the Pod")
					require.Equal(t, otherMatch, policies[0].Match)
					nats, err = client.ListNats(util.DefaultVpc, "", "10.0.0.2", nil)
					require.NoError(t, err)
					require.Empty(t, nats)
					if gateway == kubeovnv1.GWDistributedType {
						port, err := client.GetLogicalSwitchPort("test-pod.default", false)
						require.NoError(t, err)
						pg, err := client.GetPortGroup(getOverlaySubnetsPortGroupName(subnet.Name, "node1"), false)
						require.NoError(t, err)
						require.Contains(t, pg.Ports, port.UUID)
					}
				})
			}
		})
	}
}

func TestReconcileRouteSubnetsRestoresUnaffectedAddressFamily(t *testing.T) {
	for _, gateway := range []string{kubeovnv1.GWDistributedType, kubeovnv1.GWCentralizedType} {
		for _, annotation := range []string{util.SnatAnnotation, util.NorthGatewayAnnotation} {
			for _, remainingIP := range []string{"203.0.113.3", "fd01::3"} {
				t.Run(gateway+"/"+annotation+"/"+remainingIP, func(t *testing.T) {
					fc, client, pod, subnet := newPodEIPRouteFixture(t, gateway, true)
					pod.Annotations[util.SnatAnnotation] = "fd01::3"
					nets := []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}
					require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
					pod = pod.DeepCopy()
					delete(pod.Annotations, util.EipAnnotation)
					delete(pod.Annotations, util.SnatAnnotation)
					pod.Annotations[annotation] = remainingIP
					require.NoError(t, fc.fakeController.reconcileRouteSubnets(pod, nets))
					policies, err := client.ListLogicalRouterPolicies(util.DefaultVpc, util.NorthGatewayRoutePolicyPriority, nil, true)
					require.NoError(t, err)
					require.Len(t, policies, 1)
					match, nextHop, removedIP := "ip4.src == 10.0.0.2", "203.0.113.1", "fd00::2"
					if remainingIP == "fd01::3" {
						match, nextHop, removedIP = "ip6.src == fd00::2", "fd01::1", "10.0.0.2"
					}
					if annotation == util.NorthGatewayAnnotation {
						nextHop = remainingIP
					}
					require.Equal(t, match, policies[0].Match)
					require.Equal(t, []string{nextHop}, policies[0].Nexthops)
					nats, err := client.ListNats(util.DefaultVpc, "", removedIP, nil)
					require.NoError(t, err)
					require.Empty(t, nats)
					if gateway == kubeovnv1.GWDistributedType {
						port, err := client.GetLogicalSwitchPort("test-pod.default", false)
						require.NoError(t, err)
						pg, err := client.GetPortGroup(getOverlaySubnetsPortGroupName(subnet.Name, "node1"), false)
						require.NoError(t, err)
						require.Contains(t, pg.Ports, port.UUID, "the other address family needs the Subnet route")
					}
				})
			}
		}
	}
}

type podNATFailureProvider struct {
	table.Provider
	err error
}

func (p *podNATFailureProvider) Table(prototype model.Model) table.Handle {
	return &podNATFailureHandle{Handle: p.Provider.Table(prototype), provider: p}
}

type podNATFailureHandle struct {
	table.Handle
	provider *podNATFailureProvider
}

func (h *podNATFailureHandle) Transact(ctx context.Context, name string, ops ...ovsdb.Operation) error {
	if name == "nats-del" && h.provider.err != nil {
		return h.provider.err
	}
	return h.Handle.Transact(ctx, name, ops...)
}

func TestPodNATCleanupFailureRetriesRouteWorker(t *testing.T) {
	for _, annotation := range []string{util.EipAnnotation, util.SnatAnnotation} {
		t.Run(annotation, func(t *testing.T) {
			fc, client, pod, subnet := newPodEIPRouteFixture(t, kubeovnv1.GWDistributedType, false)
			if annotation == util.SnatAnnotation {
				delete(pod.Annotations, util.EipAnnotation)
				pod.Annotations[annotation] = "203.0.113.3"
			}
			controller := fc.fakeController
			nets := []*kubeovnNet{{ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}}
			require.NoError(t, controller.reconcileRouteSubnets(pod, nets))
			oldPod := pod.DeepCopy()
			oldPod.ResourceVersion = "1"
			pod = oldPod.DeepCopy()
			pod.ResourceVersion = "2"
			delete(pod.Annotations, annotation)
			_, err := fc.kubeClient.CoreV1().Pods(pod.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				cached, err := controller.podsLister.Pods(pod.Namespace).Get(pod.Name)
				return err == nil && cached.ResourceVersion == pod.ResourceVersion
			}, time.Second, time.Millisecond)
			provider := &podNATFailureProvider{Provider: client, err: errors.New("NAT deletion failed")}
			controller.OVNNbTables = provider
			controller.enqueueUpdatePod(oldPod, pod)
			queue := controller.updatePodRouteQueue
			require.True(t, processNextWorkItem("update pod route", queue, controller.handleUpdatePodRoute, getWorkItemKey))
			key := pod.Namespace + "/" + pod.Name
			require.Equal(t, 1, queue.NumRequeues(key))
			provider.err = nil
			require.Eventually(t, func() bool { return queue.Len() == 1 }, time.Second, time.Millisecond)
			require.True(t, processNextWorkItem("update pod route", queue, controller.handleUpdatePodRoute, getWorkItemKey))
			require.Zero(t, queue.NumRequeues(key))
			nats, err := client.ListNats(util.DefaultVpc, "", "10.0.0.2", nil)
			require.NoError(t, err)
			require.Empty(t, nats)
		})
	}
}

func newPodEIPRouteFixture(t *testing.T, gateway string, dualStack bool) (*fakeController, *ovs.OVNNbClient, *corev1.Pod, *kubeovnv1.Subnet) {
	t.Helper()
	pod, subnet := podEventFixture()
	pod.Spec.NodeName = "node1"
	pod.Status.Phase = corev1.PodRunning
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[util.MacAddressAnnotation] = "00:00:00:00:00:02"
	pod.Annotations[util.EipAnnotation] = "203.0.113.2"
	subnet.Spec.GatewayType = gateway
	subnet.Spec.EnableLb = new(true)
	nextHop := "203.0.113.1"
	if dualStack {
		pod.Annotations[util.IPAddressAnnotation] += ",fd00::2"
		subnet.Spec.CIDRBlock += ",fd00::/64"
		subnet.Spec.Gateway += ",fd00::1"
		subnet.Spec.Protocol = kubeovnv1.ProtocolDual
		nextHop += ",fd01::1"
	}
	node := &corev1.Node{Name: "node1", Annotations: map[string]string{
		util.PortNameAnnotation: "node-node1", util.IPAddressAnnotation: "100.64.0.2,fd02::2",
	}}
	cm := &corev1.ConfigMap{
		Name: util.ExternalGatewayConfig, Namespace: metav1.NamespaceSystem,
		Data: map[string]string{"external-gw-addr": nextHop},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod}, Nodes: []*corev1.Node{node}, Subnets: []*kubeovnv1.Subnet{subnet}, ConfigMaps: []*corev1.ConfigMap{cm},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableEipSnat = true
	fc.fakeController.config.EnableOvnLB = true
	fc.fakeController.config.ExternalGatewayConfigNS = metav1.NamespaceSystem
	fc.fakeController.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil)
	fc.fakeController.updatePodRouteQueue = newTypedRateLimitingQueue[string]("UpdatePodRoute", nil)
	t.Cleanup(fc.fakeController.addOrUpdatePodQueue.ShutDown)
	t.Cleanup(fc.fakeController.updatePodRouteQueue.ShutDown)
	client := newInMemoryOVNNbClient(t)
	fc.fakeController.OVNNbClient = client
	fc.fakeController.OVNNbTables = client
	require.NoError(t, client.CreateLogicalRouter(util.DefaultVpc))
	require.NoError(t, client.CreateBareLogicalSwitch(subnet.Name))
	require.NoError(t, client.CreateBareLogicalSwitchPort(subnet.Name, "test-pod.default",
		pod.Annotations[util.IPAddressAnnotation], pod.Annotations[util.MacAddressAnnotation]))
	require.NoError(t, client.CreatePortGroup("node.node1", map[string]string{"node": node.Name}))
	require.NoError(t, client.CreatePortGroup(getOverlaySubnetsPortGroupName(subnet.Name, node.Name),
		map[string]string{"subnet": subnet.Name, "node": node.Name}))
	return fc, client, pod, subnet
}
