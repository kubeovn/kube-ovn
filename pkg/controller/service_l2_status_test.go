package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// overrideDiscovery overrides ServerResourcesForGroupVersion of an embedded
// discovery client to simulate API discovery failures or custom API surfaces.
type overrideDiscovery struct {
	discovery.DiscoveryInterface
	resources map[string]*metav1.APIResourceList
	err       error
}

func (d *overrideDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if d.err != nil {
		return nil, d.err
	}
	if list, ok := d.resources[groupVersion]; ok {
		return list, nil
	}
	return d.DiscoveryInterface.ServerResourcesForGroupVersion(groupVersion)
}

// overrideKubeClient returns a discovery client different from the embedded one.
type overrideKubeClient struct {
	kubernetes.Interface
	discovery discovery.DiscoveryInterface
}

func (c *overrideKubeClient) Discovery() discovery.DiscoveryInterface {
	return c.discovery
}

func TestTryStartServiceL2StatusInformer(t *testing.T) {
	t.Run("already started short-circuits", func(t *testing.T) {
		controller := &Controller{serviceL2StatusStarted: true}
		require.True(t, controller.tryStartServiceL2StatusInformer(context.Background()))
	})

	t.Run("discovery error degrades to false", func(t *testing.T) {
		controller := &Controller{config: &Configuration{
			KubeClient: &overrideKubeClient{
				Interface: k8sfake.NewSimpleClientset(),
				discovery: &overrideDiscovery{err: errors.New("discovery unavailable")},
			},
		}}
		require.False(t, controller.tryStartServiceL2StatusInformer(context.Background()))
		require.False(t, controller.serviceL2StatusStarted)
	})

	t.Run("ServiceL2Status API not found degrades to false", func(t *testing.T) {
		// the fake discovery client returns NotFound for the metallb group version
		controller := &Controller{config: &Configuration{
			KubeClient: k8sfake.NewSimpleClientset(),
		}}
		require.False(t, controller.tryStartServiceL2StatusInformer(context.Background()))
		require.False(t, controller.serviceL2StatusStarted)
	})

	t.Run("nil REST config degrades to false", func(t *testing.T) {
		controller := &Controller{config: &Configuration{
			KubeClient: &overrideKubeClient{
				Interface: k8sfake.NewSimpleClientset(),
				discovery: &overrideDiscovery{resources: map[string]*metav1.APIResourceList{
					metallbv1beta1.GroupVersion.String(): {
						APIResources: []metav1.APIResource{{Kind: "ServiceL2Status"}},
					},
				}},
			},
		}}
		require.False(t, controller.tryStartServiceL2StatusInformer(context.Background()))
		require.False(t, controller.serviceL2StatusStarted)
	})

	t.Run("informer starts when the API is available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			list := metallbv1beta1.ServiceL2StatusList{
				APIVersion: metallbv1beta1.GroupVersion.String(), Kind: "ServiceL2StatusList",
			}
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(list))
		}))
		defer server.Close()

		controller := &Controller{config: &Configuration{
			KubeClient: &overrideKubeClient{
				Interface: k8sfake.NewSimpleClientset(),
				discovery: &overrideDiscovery{resources: map[string]*metav1.APIResourceList{
					metallbv1beta1.GroupVersion.String(): {
						APIResources: []metav1.APIResource{{Kind: "ServiceL2Status"}},
					},
				}},
			},
			KubeRestConfig: &rest.Config{Host: server.URL},
		}}

		ctx := t.Context()

		require.True(t, controller.tryStartServiceL2StatusInformer(ctx))
		// a second call must short-circuit instead of starting a second informer
		require.True(t, controller.tryStartServiceL2StatusInformer(ctx))
		require.True(t, controller.serviceL2StatusStarted)
		require.NotNil(t, controller.serviceL2StatusIndexer)
		require.NotNil(t, controller.serviceL2StatusSynced)
	})
}

func TestGetServiceL2StatusNodeIndexerError(t *testing.T) {
	// an indexer that lacks the service index makes ByIndex fail; the error
	// must propagate so the endpointSlice handler retries the key
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	controller := &Controller{
		serviceL2StatusIndexer: indexer,
		serviceL2StatusStarted: true,
		serviceL2StatusSynced:  func() bool { return true },
	}

	_, ready, err := controller.getServiceL2StatusNode("test-ns", "test-svc")
	require.ErrorContains(t, err, "failed to list ServiceL2Statuses")
	require.True(t, ready)
}

func TestStartServiceL2StatusInformerDisabled(t *testing.T) {
	tests := []struct {
		name   string
		config *Configuration
	}{
		{
			name: "load balancer disabled",
			config: &Configuration{
				EnableOVNLBPreferLocal: true,
			},
		},
		{
			name: "prefer local disabled",
			config: &Configuration{
				EnableLb: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{config: tt.config}
			require.NotPanics(t, func() {
				controller.StartServiceL2StatusInformer(context.Background())
			})
		})
	}
}

func TestServiceL2StatusServiceKey(t *testing.T) {
	status := &metallbv1beta1.ServiceL2Status{
		Status: metallbv1beta1.MetalLBServiceL2Status{
			ServiceNamespace: "test-ns",
			ServiceName:      "test-svc",
		},
	}

	key, ok := serviceL2StatusServiceKey(status)
	require.True(t, ok)
	require.Equal(t, "test-ns/test-svc", key)

	_, ok = serviceL2StatusServiceKey(&metallbv1beta1.ServiceL2Status{})
	require.False(t, ok)
}

func TestGetServiceL2StatusNode(t *testing.T) {
	controller := &Controller{}
	node, ready, err := controller.getServiceL2StatusNode("test-ns", "test-svc")
	require.NoError(t, err)
	require.True(t, ready)
	require.Empty(t, node)

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		serviceL2StatusServiceIndex: indexServiceL2StatusByService,
	})
	controller = &Controller{
		serviceL2StatusIndexer: indexer,
		serviceL2StatusStarted: true,
		serviceL2StatusSynced:  func() bool { return false },
	}

	node, ready, err = controller.getServiceL2StatusNode("test-ns", "test-svc")
	require.NoError(t, err)
	require.False(t, ready)
	require.Empty(t, node)

	controller.serviceL2StatusSynced = func() bool { return true }

	node, ready, err = controller.getServiceL2StatusNode("test-ns", "test-svc")
	require.NoError(t, err)
	require.True(t, ready)
	require.Empty(t, node)

	status := &metallbv1beta1.ServiceL2Status{
		Name: "l2-status", Namespace: "metallb-system",
		Status: metallbv1beta1.MetalLBServiceL2Status{
			Node:             "worker-1",
			ServiceNamespace: "test-ns",
			ServiceName:      "test-svc",
		},
	}
	require.NoError(t, indexer.Add(status))

	node, ready, err = controller.getServiceL2StatusNode("test-ns", "test-svc")
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, "worker-1", node)

	conflictingStatus := status.DeepCopy()
	conflictingStatus.Name = "l2-status-conflict"
	conflictingStatus.Status.Node = "worker-2"
	require.NoError(t, indexer.Add(conflictingStatus))

	_, ready, err = controller.getServiceL2StatusNode("test-ns", "test-svc")
	require.True(t, ready)
	require.ErrorContains(t, err, "multiple announcing nodes")
}

func TestEnqueueServiceL2Status(t *testing.T) {
	controller := &Controller{
		addOrUpdateEndpointSliceQueue: newTypedRateLimitingQueue[string]("test-service-l2-status", nil),
	}
	t.Cleanup(controller.addOrUpdateEndpointSliceQueue.ShutDown)

	status := &metallbv1beta1.ServiceL2Status{
		Status: metallbv1beta1.MetalLBServiceL2Status{
			ServiceNamespace: "test-ns",
			ServiceName:      "test-svc",
		},
	}
	controller.enqueueServiceL2Status(status)

	key, shutdown := controller.addOrUpdateEndpointSliceQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "test-ns/test-svc", key)
	controller.addOrUpdateEndpointSliceQueue.Done(key)

	controller.enqueueServiceL2Status(cache.DeletedFinalStateUnknown{
		Key: "metallb-system/l2-status",
		Obj: status,
	})
	key, shutdown = controller.addOrUpdateEndpointSliceQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "test-ns/test-svc", key)
	controller.addOrUpdateEndpointSliceQueue.Done(key)
}

func TestClearLoadBalancerVIPExternalTrafficLocal(t *testing.T) {
	fakeController := newFakeController(t)
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
			Ports: []corev1.ServicePort{{
				Port:     80,
				Protocol: corev1.ProtocolTCP,
			}},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.10"}},
			},
		},
	}

	fakeController.mockOvnClient.EXPECT().
		SetLoadBalancerVIPExternalTrafficLocal("tcp-lb", "10.0.0.10:80", "").
		Return(nil)
	fakeController.mockOvnClient.EXPECT().
		LoadBalancerDeleteIPPortMapping("tcp-lb", "10.0.0.10:80").
		Return(nil)

	err := fakeController.fakeController.clearLoadBalancerVIPExternalTrafficLocal(
		service, "tcp-lb", "udp-lb", "sctp-lb",
	)
	require.NoError(t, err)

	service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	require.NoError(t, fakeController.fakeController.clearLoadBalancerVIPExternalTrafficLocal(
		service, "tcp-lb", "udp-lb", "sctp-lb",
	))
}
