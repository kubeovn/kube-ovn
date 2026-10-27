package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func nftableLbService(name string, port int32, protocol v1.Protocol, mutate func(*v1.Service)) *v1.Service {
	svc := &v1.Service{
		Namespace: "default",
		Name:      name,
		Annotations: map[string]string{
			util.VpcNatGatewayAnnotation: "gw-a",
			util.EipAnnotation:           "eip-a",
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Port: port, Protocol: protocol}},
		},
	}
	if mutate != nil {
		mutate(svc)
	}
	return svc
}

func newServiceHook(t *testing.T, existing ...*v1.Service) *ValidatingHook {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	cache := &mockCache{}
	for _, svc := range existing {
		cache.services = append(cache.services, *svc)
	}
	return &ValidatingHook{decoder: admission.NewDecoder(scheme), cache: cache}
}

func TestServiceCreateHookEipPortConflict(t *testing.T) {
	svc := func(name string, port int32, protocol v1.Protocol) *v1.Service {
		return nftableLbService(name, port, protocol, nil)
	}

	t.Run("allows a service that does not serve an eip", func(t *testing.T) {
		hook := newServiceHook(t, svc("web", 80, v1.ProtocolTCP))
		plain := &v1.Service{
			Namespace: "default", Name: "plain",
			Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
		}
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, plain))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("allows the first service for an eip", func(t *testing.T) {
		hook := newServiceHook(t)
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, svc("web", 80, v1.ProtocolTCP)))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("rejects the same eip and port", func(t *testing.T) {
		hook := newServiceHook(t, svc("web", 80, v1.ProtocolTCP))
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, svc("other", 80, v1.ProtocolTCP)))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "already used by service default/web")
	})

	t.Run("allows the same eip on a different port", func(t *testing.T) {
		hook := newServiceHook(t, svc("web", 80, v1.ProtocolTCP))
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, svc("other", 81, v1.ProtocolTCP)))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("allows the same eip and port on a different protocol", func(t *testing.T) {
		hook := newServiceHook(t, svc("web", 80, v1.ProtocolTCP))
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, svc("other", 80, v1.ProtocolUDP)))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("allows a takeover from a terminating service", func(t *testing.T) {
		now := metav1.Now()
		terminating := nftableLbService("old", 80, v1.ProtocolTCP, func(s *v1.Service) {
			s.DeletionTimestamp = &now
			s.Finalizers = []string{"keep"}
		})
		hook := newServiceHook(t, terminating)
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, svc("new", 80, v1.ProtocolTCP)))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	// Both features publish status.loadBalancer.ingress: the gateway publishes the EIP, the OVN
	// load balancer an address of the annotated subnet.
	t.Run("rejects an external ip from subnet on a gateway service", func(t *testing.T) {
		hook := newServiceHook(t)
		withSubnet := nftableLbService("web", 80, v1.ProtocolTCP, func(s *v1.Service) {
			s.Annotations[util.ServiceExternalIPFromSubnetAnnotation] = "subnet0"
		})
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, withSubnet))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "both publish the LoadBalancer ingress IP")
	})

	t.Run("allows an external ip from subnet without a gateway", func(t *testing.T) {
		hook := newServiceHook(t)
		plain := &v1.Service{
			Namespace: "default", Name: "plain",
			Annotations: map[string]string{util.ServiceExternalIPFromSubnetAnnotation: "subnet0"},
			Spec:        v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
		}
		resp := hook.ServiceCreateHook(context.Background(), createRequest(t, plain))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})
}

func TestServiceUpdateHook(t *testing.T) {
	service := func(gateway, eip string) *v1.Service {
		return &v1.Service{
			Namespace: "default",
			Name:      "web",
			Annotations: map[string]string{
				util.VpcNatGatewayAnnotation: gateway,
				util.EipAnnotation:           eip,
			},
		}
	}

	t.Run("allows initial binding", func(t *testing.T) {
		hook := newServiceHook(t)
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("", ""), service("gw-a", "eip-a")))
		require.True(t, resp.Allowed)
	})

	t.Run("rejects rebinding to another gateway", func(t *testing.T) {
		hook := newServiceHook(t)
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("gw-b", "eip-a")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "vpc nat gateway cannot change")
	})

	t.Run("rejects rebinding to another eip", func(t *testing.T) {
		hook := newServiceHook(t)
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("gw-a", "eip-b")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "eip cannot change")
	})

	// Clearing an annotation is the opt-out of the feature, not a rebinding: the controller
	// releases the identities, the lo addresses and the VIP routes the Service programmed.
	t.Run("allows opting out of the eip alone", func(t *testing.T) {
		hook := newServiceHook(t)
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("gw-a", "")))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("allows opting out of the gateway", func(t *testing.T) {
		hook := newServiceHook(t)
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("", "")))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("rejects a port change that collides with another service", func(t *testing.T) {
		other := nftableLbService("other", 81, v1.ProtocolTCP, nil)
		hook := newServiceHook(t, nftableLbService("web", 80, v1.ProtocolTCP, nil), other)
		updated := other.DeepCopy()
		updated.Spec.Ports = []v1.ServicePort{{Port: 80, Protocol: v1.ProtocolTCP}}
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, other, updated))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "already used by service default/web")
	})

	t.Run("allows the controller to drop the finalizer of a terminating service", func(t *testing.T) {
		now := metav1.Now()
		terminating := nftableLbService("web", 80, v1.ProtocolTCP, func(s *v1.Service) {
			s.DeletionTimestamp = &now
			s.Finalizers = []string{util.KubeOVNControllerFinalizer}
		})
		hook := newServiceHook(t, terminating, nftableLbService("successor", 80, v1.ProtocolTCP, nil))
		updated := terminating.DeepCopy()
		updated.Finalizers = nil
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, terminating, updated))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("allows finalizer bookkeeping on a live service despite a conflicting peer", func(t *testing.T) {
		// The disabled-mode drain also releases live leftovers; its finalizer update must not be
		// rejected by the duplicate-identity check, which only guards intent changes.
		target := nftableLbService("other", 80, v1.ProtocolTCP, func(s *v1.Service) {
			s.Finalizers = []string{util.KubeOVNControllerFinalizer}
		})
		hook := newServiceHook(t, nftableLbService("web", 80, v1.ProtocolTCP, nil), target)
		updated := target.DeepCopy()
		updated.Finalizers = nil
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, target, updated))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("rejects adding an external ip from subnet to a gateway service", func(t *testing.T) {
		target := nftableLbService("web", 80, v1.ProtocolTCP, nil)
		hook := newServiceHook(t, target)
		updated := target.DeepCopy()
		updated.Annotations[util.ServiceExternalIPFromSubnetAnnotation] = "subnet0"
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, target, updated))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "both publish the LoadBalancer ingress IP")
	})
}
