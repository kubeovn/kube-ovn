package webhook

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (v *ValidatingHook) ServiceCreateHook(ctx context.Context, req admission.Request) admission.Response {
	newSvc := v1.Service{}
	if err := v.decoder.DecodeRaw(req.Object, &newSvc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.validateNftableLbService(ctx, &newSvc); err != nil {
		return ctrlwebhook.Errored(http.StatusForbidden, err)
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ServiceUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	oldSvc := v1.Service{}
	if err := v.decoder.DecodeRaw(req.OldObject, &oldSvc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	newSvc := v1.Service{}
	if err := v.decoder.DecodeRaw(req.Object, &newSvc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	// A Service binds to one gateway and one EIP: rebinding it to another would move a live data
	// plane, which has no migration path. Clearing an annotation is not a rebinding but the opt-out
	// this feature supports: the controller releases everything the Service programmed (see
	// nftableLbSvcQualifies), so it stays allowed.
	if oldGateway := oldSvc.Annotations[util.VpcNatGatewayAnnotation]; oldGateway != "" {
		if newGateway := newSvc.Annotations[util.VpcNatGatewayAnnotation]; newGateway != "" && newGateway != oldGateway {
			return ctrlwebhook.Errored(http.StatusBadRequest,
				fmt.Errorf("service %s/%s vpc nat gateway cannot change; delete and recreate the service", newSvc.Namespace, newSvc.Name))
		}
		oldEip, newEip := oldSvc.Annotations[util.EipAnnotation], newSvc.Annotations[util.EipAnnotation]
		if oldEip != "" && newEip != "" && oldEip != newEip {
			return ctrlwebhook.Errored(http.StatusBadRequest,
				fmt.Errorf("service %s/%s eip cannot change; delete and recreate the service", newSvc.Namespace, newSvc.Name))
		}
	}
	if nftableLbServiceIntentChanged(&oldSvc, &newSvc) {
		if err := v.validateNftableLbService(ctx, &newSvc); err != nil {
			return ctrlwebhook.Errored(http.StatusForbidden, err)
		}
	}
	return ctrlwebhook.Allowed("bypass")
}

// nftableLbServiceIntentChanged reports whether the update changes the Service's share-DNAT
// identity intent (binding, type, ports or ingress-IP ownership). Only those updates need the
// validation below: the controller's own finalizer bookkeeping must not be rejected as a
// duplicate, otherwise a terminating or disabled-mode Service could never release its finalizer.
func nftableLbServiceIntentChanged(oldSvc, newSvc *v1.Service) bool {
	return oldSvc.Spec.Type != newSvc.Spec.Type ||
		oldSvc.Annotations[util.VpcNatGatewayAnnotation] != newSvc.Annotations[util.VpcNatGatewayAnnotation] ||
		oldSvc.Annotations[util.EipAnnotation] != newSvc.Annotations[util.EipAnnotation] ||
		oldSvc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != newSvc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] ||
		!reflect.DeepEqual(oldSvc.Spec.Ports, newSvc.Spec.Ports)
}

// validateNftableLbService checks what a Service that is served by a gateway may declare: it must
// own its ingress IP alone, and the share-DNAT identity it programs must be free.
func (v *ValidatingHook) validateNftableLbService(ctx context.Context, svc *v1.Service) error {
	// A terminating Service is releasing its identity: it must stay updatable (the controller
	// removing its finalizer) even when a successor already owns the same EIP:port.
	if !svc.DeletionTimestamp.IsZero() || !nftableLbServiceEipServing(svc) {
		return nil
	}
	// The gateway publishes the EIP as the Service's ingress IP; the OVN load balancer publishes
	// an address from the annotated subnet as the same field. Two owners of one field means the
	// published address flaps between them.
	if svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		return fmt.Errorf("service %s/%s cannot combine %s with %s: both publish the LoadBalancer ingress IP",
			svc.Namespace, svc.Name, util.VpcNatGatewayAnnotation, util.ServiceExternalIPFromSubnetAnnotation)
	}
	return v.validateNftableLbServiceEipPort(ctx, svc)
}

// nftableLbSvcEipIndex indexes Services by the EIP they publish as their ingress IP, so the
// duplicate check below reads only the Services of one EIP instead of every Service in the
// cluster. The conflict is cluster-wide (an EIP is a cluster-scoped object two namespaces can
// reference), which is why the lookup cannot be narrowed to the Service's own namespace.
const nftableLbSvcEipIndex = "nftableLbSvcEip"

func nftableLbSvcEipIndexValue(obj client.Object) []string {
	svc, ok := obj.(*v1.Service)
	if !ok || !nftableLbServiceEipServing(svc) {
		return nil
	}
	return []string{svc.Annotations[util.EipAnnotation]}
}

// validateNftableLbServiceEipPort rejects a Service that would program the same share-DNAT
// identity (EIP, external port, protocol) as a Service that is already live. The gateway maps one
// identity to one backend set, so a second Service declaring it would silently fight over it.
//
// This is the early, user-visible rejection; the controller still arbitrates a duplicate that
// races past this admission check (two concurrent creates, or duplicates predating the check), so
// it is not the only line of defense.
func (v *ValidatingHook) validateNftableLbServiceEipPort(ctx context.Context, svc *v1.Service) error {
	eipName := svc.Annotations[util.EipAnnotation]
	services := &v1.ServiceList{}
	if err := v.cache.List(ctx, services, client.MatchingFields{nftableLbSvcEipIndex: eipName}); err != nil {
		return fmt.Errorf("failed to list services for eip %q: %w", eipName, err)
	}
	for i := range services.Items {
		other := &services.Items[i]
		if other.Namespace == svc.Namespace && other.Name == svc.Name {
			continue
		}
		// A terminating Service releases its identity to its successor (the controller skips it
		// when it resolves ownership), so it must not block the replacement.
		if !other.DeletionTimestamp.IsZero() {
			continue
		}
		if other.Annotations[util.EipAnnotation] != eipName || !nftableLbServiceEipServing(other) {
			continue
		}
		if port, ok := sharedShareDnatPort(svc, other); ok {
			return fmt.Errorf("eip %q port %d/%s is already used by service %s/%s; a given eip and port can back only one LoadBalancer service",
				eipName, port.Port, port.Protocol, other.Namespace, other.Name)
		}
	}
	return nil
}

// nftableLbServiceEipServing reports whether the Service declares an external share-DNAT identity
// (the gateway nftable LB LoadBalancer feature).
func nftableLbServiceEipServing(svc *v1.Service) bool {
	return svc.Spec.Type == v1.ServiceTypeLoadBalancer &&
		svc.Annotations[util.VpcNatGatewayAnnotation] != "" && svc.Annotations[util.EipAnnotation] != ""
}

// sharedShareDnatPort returns a service port that both Services would program as the same
// share-DNAT identity. Only tcp/udp are programmed, and the identity is (external port, protocol),
// so the same port on different protocols does not collide.
func sharedShareDnatPort(a, b *v1.Service) (v1.ServicePort, bool) {
	for _, pa := range a.Spec.Ports {
		if pa.Protocol != v1.ProtocolTCP && pa.Protocol != v1.ProtocolUDP {
			continue
		}
		for _, pb := range b.Spec.Ports {
			if pa.Port == pb.Port && pa.Protocol == pb.Protocol {
				return pa, true
			}
		}
	}
	return v1.ServicePort{}, false
}
