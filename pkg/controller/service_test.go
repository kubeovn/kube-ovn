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

	t.Run("EnableLbSvc=true still feeds addServiceQueue when EnableLb=false", func(t *testing.T) {
		t.Parallel()
		c := newController(false, true)
		c.enqueueAddService(svc)
		require.Equal(t, 1, c.addServiceQueue.Len())
		require.Zero(t, c.addOrUpdateEndpointSliceQueue.Len())
	})
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
