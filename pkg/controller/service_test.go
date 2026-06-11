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
