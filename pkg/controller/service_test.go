package controller

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
		if c.addServiceQueue.Len() != 0 ||
			c.deleteServiceQueue.Len() != 0 ||
			c.updateServiceQueue.Len() != 0 ||
			c.addOrUpdateEndpointSliceQueue.Len() != 0 {
			t.Fatal("all queues should be empty when EnableLb is false")
		}
	})

	t.Run("EnableLb=true enqueues", func(t *testing.T) {
		t.Parallel()
		c := newController(true, false)
		enqueueAll(c)
		if c.addServiceQueue.Len() != 0 ||
			c.deleteServiceQueue.Len() != 1 ||
			c.updateServiceQueue.Len() != 1 ||
			c.addOrUpdateEndpointSliceQueue.Len() != 1 {
			t.Fatal("expected non-empty queues when EnableLb is true")
		}
	})

	t.Run("EnableLbSvc requires EnableLb for addServiceQueue", func(t *testing.T) {
		t.Parallel()
		c := newController(true, true)
		c.enqueueAddService(svc)
		if c.addServiceQueue.Len() != 1 || c.addOrUpdateEndpointSliceQueue.Len() != 1 {
			t.Fatal("addServiceQueue should receive when both flags are set")
		}

		c = newController(false, true)
		c.enqueueAddService(svc)
		if c.addServiceQueue.Len() != 0 || c.addOrUpdateEndpointSliceQueue.Len() != 0 {
			t.Fatal("queues should be empty when EnableLb is false even with EnableLbSvc true")
		}
	})
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

	if c.updateServiceQueue.Len() != 1 {
		t.Fatalf("expected update service queue length 1, got %d", c.updateServiceQueue.Len())
	}
	if c.addOrUpdateEndpointSliceQueue.Len() != 1 {
		t.Fatalf("expected endpoint slice queue length 1, got %d", c.addOrUpdateEndpointSliceQueue.Len())
	}
	key, shutdown := c.addOrUpdateEndpointSliceQueue.Get()
	if shutdown {
		t.Fatal("endpoint slice queue was shut down")
	}
	if key != "default/svc" {
		t.Fatalf("expected endpoint slice queue key default/svc, got %s", key)
	}
	c.addOrUpdateEndpointSliceQueue.Done(key)

	c.config.EnableOVNLBPreferLocal = false
	c.addOrUpdateEndpointSliceQueue = newTypedRateLimitingQueue[string]("UpdateEndpointSliceDisabled", nil)
	t.Cleanup(c.addOrUpdateEndpointSliceQueue.ShutDown)
	c.enqueueUpdateService(oldSvc, newSvc)
	if c.addOrUpdateEndpointSliceQueue.Len() != 0 {
		t.Fatalf("expected endpoint slice queue length 0 when prefer local is disabled, got %d", c.addOrUpdateEndpointSliceQueue.Len())
	}
}
