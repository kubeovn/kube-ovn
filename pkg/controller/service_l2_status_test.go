package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

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
		ObjectMeta: metav1.ObjectMeta{Name: "l2-status", Namespace: "metallb-system"},
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

	err := fakeController.fakeController.clearLoadBalancerVIPExternalTrafficLocal(
		service, "tcp-lb", "udp-lb", "sctp-lb",
	)
	require.NoError(t, err)

	service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	require.NoError(t, fakeController.fakeController.clearLoadBalancerVIPExternalTrafficLocal(
		service, "tcp-lb", "udp-lb", "sctp-lb",
	))
}
