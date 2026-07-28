package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_getVipIps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		svc      *v1.Service
		expected []string
	}{
		{
			name: "annotation with single IPv4",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.SwitchLBRuleVipsAnnotation: "10.0.0.1",
					},
				},
			},
			expected: []string{"10.0.0.1"},
		},
		{
			name: "annotation with dual-stack IPs",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.SwitchLBRuleVipsAnnotation: "10.0.0.1,fd00::1",
					},
				},
			},
			expected: []string{"10.0.0.1", "fd00::1"},
		},
		{
			name: "annotation with empty value should return no IPs",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.SwitchLBRuleVipsAnnotation: "",
					},
				},
			},
			expected: nil,
		},
		{
			name: "annotation with trailing comma should filter empty elements",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.SwitchLBRuleVipsAnnotation: "10.0.0.1,",
					},
				},
			},
			expected: []string{"10.0.0.1"},
		},
		{
			name: "annotation with leading comma should filter empty elements",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.SwitchLBRuleVipsAnnotation: ",10.0.0.1",
					},
				},
			},
			expected: []string{"10.0.0.1"},
		},
		{
			name: "no annotation falls back to ClusterIPs",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1"},
				},
			},
			expected: []string{"10.96.0.1"},
		},
		{
			name: "no annotation with external IP from subnet",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.ServiceExternalIPFromSubnetAnnotation: "external-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1"},
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "192.168.1.1"},
						},
					},
				},
			},
			expected: []string{"10.96.0.1", "192.168.1.1"},
		},
		{
			name: "no annotation with empty ingress IP should be filtered",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.ServiceExternalIPFromSubnetAnnotation: "external-subnet",
					},
				},
				Spec: v1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1"},
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "192.168.1.1"},
							{IP: ""},
							{Hostname: "lb.example.com"},
						},
					},
				},
			},
			expected: []string{"10.96.0.1", "192.168.1.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := getVipIps(tt.svc)
			require.Equal(t, tt.expected, got)
		})
	}
}

func Test_enqueueServiceGatedByEnableLb(t *testing.T) {
	t.Parallel()

	newController := func(enableLb, enableLbSvc bool) *Controller {
		return &Controller{
			config: &Configuration{
				EnableLb:    enableLb,
				EnableLbSvc: enableLbSvc,
			},
			addServiceQueue:               newTypedRateLimitingQueue[string]("AddService", nil),
			deleteServiceQueue:            newTypedRateLimitingQueue[*vpcService]("DeleteService", nil),
			updateServiceQueue:            newTypedRateLimitingQueue[*updateSvcObject]("UpdateService", nil),
			addOrUpdateEndpointSliceQueue: newTypedRateLimitingQueue[string]("UpdateEndpointSlice", nil),
		}
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1.ServiceSpec{
			ClusterIP:  "10.96.0.10",
			ClusterIPs: []string{"10.96.0.10"},
			Ports:      []v1.ServicePort{{Name: "tcp", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	updatedSvc := svc.DeepCopy()
	updatedSvc.ResourceVersion = "2"
	updatedSvc.Spec.Ports[0].Port = 8080

	eps := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-abc12",
			Namespace: metav1.NamespaceDefault,
			Labels:    map[string]string{discoveryv1.LabelServiceName: "svc"},
		},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.16.0.2"}}},
	}
	updatedEps := eps.DeepCopy()
	updatedEps.ResourceVersion = "2"

	enqueueAll := func(c *Controller) {
		c.enqueueAddService(svc)
		c.enqueueUpdateService(svc, updatedSvc)
		c.enqueueDeleteService(svc)
		c.enqueueAddEndpointSlice(eps)
		c.enqueueUpdateEndpointSlice(eps, updatedEps)
	}

	t.Run("EnableLb=false skips enqueueing", func(t *testing.T) {
		t.Parallel()
		c := newController(false, false)
		enqueueAll(c)
		require.Zero(t, c.addServiceQueue.Len())
		require.Zero(t, c.deleteServiceQueue.Len())
		require.Zero(t, c.updateServiceQueue.Len())
		require.Zero(t, c.addOrUpdateEndpointSliceQueue.Len())
	})

	t.Run("EnableLb=true enqueues", func(t *testing.T) {
		t.Parallel()
		c := newController(true, false)
		enqueueAll(c)
		require.Zero(t, c.addServiceQueue.Len())
		require.Equal(t, 1, c.deleteServiceQueue.Len())
		require.Equal(t, 1, c.updateServiceQueue.Len())
		// the same service key from the service/endpointSlice events is deduplicated
		require.Equal(t, 1, c.addOrUpdateEndpointSliceQueue.Len())
	})

	t.Run("EnableLbSvc=true feeds addServiceQueue only when EnableLb is set", func(t *testing.T) {
		t.Parallel()
		c := newController(true, true)
		c.enqueueAddService(svc)
		require.Equal(t, 1, c.addServiceQueue.Len())
		require.Equal(t, 1, c.addOrUpdateEndpointSliceQueue.Len())

		// the add service worker is gated by EnableLb, so the producer must be too
		c = newController(false, true)
		c.enqueueAddService(svc)
		require.Zero(t, c.addServiceQueue.Len())
		require.Zero(t, c.addOrUpdateEndpointSliceQueue.Len())
	})
}

func Test_enqueueUpdateServiceSkipsIrrelevantUpdates(t *testing.T) {
	t.Parallel()

	newController := func() *Controller {
		return &Controller{
			config:             &Configuration{EnableLb: true},
			updateServiceQueue: newTypedRateLimitingQueue[*updateSvcObject]("UpdateService", nil),
		}
	}

	baseSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "svc",
			Namespace:       metav1.NamespaceDefault,
			ResourceVersion: "1",
		},
		Spec: v1.ServiceSpec{
			ClusterIP:  "10.96.0.10",
			ClusterIPs: []string{"10.96.0.10"},
			Ports:      []v1.ServicePort{{Name: "tcp", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}

	tests := []struct {
		name      string
		mutateOld func(svc *v1.Service)
		mutate    func(svc *v1.Service)
		enqueued  bool
	}{
		{
			name: "third-party annotation churn is skipped",
			mutate: func(svc *v1.Service) {
				svc.Annotations = map[string]string{"third-party/annotation": "changed"}
			},
			enqueued: false,
		},
		{
			name: "status-only update is skipped",
			mutate: func(svc *v1.Service) {
				svc.Status.Conditions = []metav1.Condition{{Type: "Foo", Status: metav1.ConditionTrue}}
			},
			enqueued: false,
		},
		{
			name: "port change is enqueued",
			mutate: func(svc *v1.Service) {
				svc.Spec.Ports[0].Port = 8080
			},
			enqueued: true,
		},
		{
			name: "vpc annotation change is enqueued",
			mutate: func(svc *v1.Service) {
				svc.Annotations = map[string]string{util.VpcAnnotation: "custom-vpc"}
			},
			enqueued: true,
		},
		{
			name: "switch lb rule vip annotation change is enqueued",
			mutate: func(svc *v1.Service) {
				svc.Annotations = map[string]string{util.SwitchLBRuleVipsAnnotation: "10.0.0.1"}
			},
			enqueued: true,
		},
		{
			name: "cluster ip change is enqueued",
			mutate: func(svc *v1.Service) {
				svc.Spec.ClusterIP = "10.96.0.11"
				svc.Spec.ClusterIPs = []string{"10.96.0.11"}
			},
			enqueued: true,
		},
		{
			name: "deletion timestamp change is enqueued",
			mutate: func(svc *v1.Service) {
				now := metav1.Now()
				svc.DeletionTimestamp = &now
			},
			enqueued: true,
		},
		{
			name: "loadbalancer service is enqueued even on annotation churn",
			mutateOld: func(svc *v1.Service) {
				svc.Spec.Type = v1.ServiceTypeLoadBalancer
			},
			mutate: func(svc *v1.Service) {
				svc.Spec.Type = v1.ServiceTypeLoadBalancer
				svc.Annotations = map[string]string{"third-party/annotation": "changed"}
			},
			enqueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			oldSvc := baseSvc.DeepCopy()
			newSvc := baseSvc.DeepCopy()
			newSvc.ResourceVersion = "2"
			if tt.mutateOld != nil {
				tt.mutateOld(oldSvc)
			}
			tt.mutate(newSvc)

			c := newController()
			c.enqueueUpdateService(oldSvc, newSvc)
			if tt.enqueued {
				require.Equal(t, 1, c.updateServiceQueue.Len())
			} else {
				require.Zero(t, c.updateServiceQueue.Len())
			}
		})
	}
}

func TestEnqueueUpdateServiceReconcilesEndpointSliceOnExternalTrafficPolicyChange(t *testing.T) {
	oldSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "svc",
			Namespace:       metav1.NamespaceDefault,
			ResourceVersion: "1",
		},
		Spec: v1.ServiceSpec{
			Type:                  v1.ServiceTypeLoadBalancer,
			ClusterIP:             "10.96.0.10",
			ClusterIPs:            []string{"10.96.0.10"},
			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
			Ports:                 []v1.ServicePort{{Name: "tcp", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	newSvc := oldSvc.DeepCopy()
	newSvc.ResourceVersion = "2"
	newSvc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeCluster

	c := &Controller{
		config: &Configuration{
			EnableLb:               true,
			EnableOVNLBPreferLocal: true,
		},
		updateServiceQueue:            newTypedRateLimitingQueue[*updateSvcObject]("UpdateService", nil),
		addOrUpdateEndpointSliceQueue: newTypedRateLimitingQueue[string]("UpdateEndpointSlice", nil),
	}
	t.Cleanup(c.updateServiceQueue.ShutDown)
	t.Cleanup(c.addOrUpdateEndpointSliceQueue.ShutDown)

	c.enqueueUpdateService(oldSvc, newSvc)

	require.Equal(t, 1, c.updateServiceQueue.Len())
	require.Equal(t, 1, c.addOrUpdateEndpointSliceQueue.Len())
	key, shutdown := c.addOrUpdateEndpointSliceQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "default/svc", key)
	c.addOrUpdateEndpointSliceQueue.Done(key)

	c.config.EnableOVNLBPreferLocal = false
	c.addOrUpdateEndpointSliceQueue = newTypedRateLimitingQueue[string]("UpdateEndpointSliceDisabled", nil)
	t.Cleanup(c.addOrUpdateEndpointSliceQueue.ShutDown)
	c.enqueueUpdateService(oldSvc, newSvc)
	require.Zero(t, c.addOrUpdateEndpointSliceQueue.Len())
}

func Test_enqueueUpdateEndpointSliceSkipsContentlessUpdates(t *testing.T) {
	t.Parallel()

	newController := func() *Controller {
		return &Controller{
			config:                        &Configuration{EnableLb: true},
			addOrUpdateEndpointSliceQueue: newTypedRateLimitingQueue[string]("UpdateEndpointSlice", nil),
		}
	}

	ready := true
	baseEps := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "svc-abc12",
			Namespace:       metav1.NamespaceDefault,
			ResourceVersion: "1",
			Labels:          map[string]string{discoveryv1.LabelServiceName: "svc"},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.16.0.2"},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
		}},
		Ports: []discoveryv1.EndpointPort{{Port: &[]int32{80}[0]}},
	}

	tests := []struct {
		name     string
		mutate   func(eps *discoveryv1.EndpointSlice)
		enqueued bool
	}{
		{
			name: "trigger-time annotation churn is skipped",
			mutate: func(eps *discoveryv1.EndpointSlice) {
				eps.Annotations = map[string]string{"endpoints.kubernetes.io/last-change-trigger-time": "changed"}
			},
			enqueued: false,
		},
		{
			name: "endpoint ready condition change is enqueued",
			mutate: func(eps *discoveryv1.EndpointSlice) {
				notReady := false
				eps.Endpoints[0].Conditions.Ready = &notReady
			},
			enqueued: true,
		},
		{
			name: "endpoint address change is enqueued",
			mutate: func(eps *discoveryv1.EndpointSlice) {
				eps.Endpoints[0].Addresses = []string{"10.16.0.3"}
			},
			enqueued: true,
		},
		{
			name: "port change is enqueued",
			mutate: func(eps *discoveryv1.EndpointSlice) {
				eps.Ports[0].Port = &[]int32{8080}[0]
			},
			enqueued: true,
		},
		{
			name: "service-name label change is enqueued",
			mutate: func(eps *discoveryv1.EndpointSlice) {
				eps.Labels[discoveryv1.LabelServiceName] = "svc2"
			},
			enqueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			oldEps := baseEps.DeepCopy()
			newEps := baseEps.DeepCopy()
			newEps.ResourceVersion = "2"
			tt.mutate(newEps)

			c := newController()
			c.enqueueUpdateEndpointSlice(oldEps, newEps)
			if tt.enqueued {
				require.Equal(t, 1, c.addOrUpdateEndpointSliceQueue.Len())
			} else {
				require.Zero(t, c.addOrUpdateEndpointSliceQueue.Len())
			}
		})
	}
}

func Test_checkServiceLBIPBelongToSubnet(t *testing.T) {
	const (
		ns         = metav1.NamespaceDefault
		svcName    = "svc1"
		subnetName = "ext-subnet"
	)

	newSvc := func(annotations map[string]string, ingressIPs ...string) *v1.Service {
		var ingress []v1.LoadBalancerIngress
		for _, ip := range ingressIPs {
			ingress = append(ingress, v1.LoadBalancerIngress{IP: ip})
		}
		return &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        svcName,
				Namespace:   ns,
				Annotations: annotations,
			},
			Status: v1.ServiceStatus{
				LoadBalancer: v1.LoadBalancerStatus{Ingress: ingress},
			},
		}
	}

	countSvcUpdates := func(c *fake.Clientset) int {
		n := 0
		for _, a := range c.Actions() {
			if a.Matches("update", "services") {
				n++
			}
		}
		return n
	}

	tests := []struct {
		name        string
		svc         *v1.Service
		wantUpdate  bool
		wantSubnet  string // expected annotation value after reconcile when wantPresent is true
		wantPresent bool   // whether the annotation key should exist after reconcile
	}{
		{
			name:        "external IP belongs to subnet sets annotation",
			svc:         newSvc(nil, "192.168.1.10"),
			wantUpdate:  true,
			wantSubnet:  subnetName,
			wantPresent: true,
		},
		{
			name:        "annotation already correct is a no-op",
			svc:         newSvc(map[string]string{util.ServiceExternalIPFromSubnetAnnotation: subnetName}, "192.168.1.10"),
			wantUpdate:  false,
			wantSubnet:  subnetName,
			wantPresent: true,
		},
		{
			name:        "external IP outside any subnet without annotation is a no-op",
			svc:         newSvc(nil, "10.0.0.10"),
			wantUpdate:  false,
			wantPresent: false,
		},
		{
			name:        "no ingress without annotation is a no-op",
			svc:         newSvc(nil),
			wantUpdate:  false,
			wantPresent: false,
		},
		{
			name:        "hostname-only ingress without annotation is a no-op",
			svc:         newSvc(nil, ""),
			wantUpdate:  false,
			wantPresent: false,
		},
		{
			name:        "stale annotation removed when external IP no longer matches",
			svc:         newSvc(map[string]string{util.ServiceExternalIPFromSubnetAnnotation: subnetName}, "10.0.0.10"),
			wantUpdate:  true,
			wantPresent: false,
		},
		{
			name:        "explicit empty annotation removed when no subnet matches",
			svc:         newSvc(map[string]string{util.ServiceExternalIPFromSubnetAnnotation: ""}, "10.0.0.10"),
			wantUpdate:  true,
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{{
					ObjectMeta: metav1.ObjectMeta{Name: subnetName},
					Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "192.168.1.0/24"},
				}},
			})
			require.NoError(t, err)
			ctrl := fakeCtrl.fakeController
			kubeClient := ctrl.config.KubeClient.(*fake.Clientset)

			created, err := kubeClient.CoreV1().Services(ns).Create(context.Background(), tt.svc, metav1.CreateOptions{})
			require.NoError(t, err)
			kubeClient.ClearActions()

			require.NoError(t, ctrl.checkServiceLBIPBelongToSubnet(created))

			if tt.wantUpdate {
				require.Positive(t, countSvcUpdates(kubeClient), "expected the service to be updated")
			} else {
				require.Zero(t, countSvcUpdates(kubeClient), "expected no service update")
			}

			got, err := kubeClient.CoreV1().Services(ns).Get(context.Background(), svcName, metav1.GetOptions{})
			require.NoError(t, err)
			val, ok := got.Annotations[util.ServiceExternalIPFromSubnetAnnotation]
			require.Equal(t, tt.wantPresent, ok, "unexpected annotation presence, value=%q", val)
			if tt.wantPresent {
				require.Equal(t, tt.wantSubnet, val)
			}
		})
	}
}
