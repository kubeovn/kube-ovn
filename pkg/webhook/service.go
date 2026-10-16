package webhook

import (
	"context"
	"fmt"
	"net/http"

	v1 "k8s.io/api/core/v1"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (v *ValidatingHook) ServiceCreateHook(ctx context.Context, req admission.Request) admission.Response {
	newSvc := v1.Service{}
	if err := v.decoder.DecodeRaw(req.Object, &newSvc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.validateNftableLbServiceEipPort(ctx, &newSvc); err != nil {
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

	// A Service binds to a gateway and EIP once; the binding is immutable from then on.
	if oldGateway := oldSvc.Annotations[util.VpcNatGatewayAnnotation]; oldGateway != "" {
		if oldGateway != newSvc.Annotations[util.VpcNatGatewayAnnotation] {
			return ctrlwebhook.Errored(http.StatusBadRequest,
				fmt.Errorf("service %s/%s vpc nat gateway cannot change; delete and recreate the service", newSvc.Namespace, newSvc.Name))
		}
		if oldSvc.Annotations[util.EipAnnotation] != newSvc.Annotations[util.EipAnnotation] {
			return ctrlwebhook.Errored(http.StatusBadRequest,
				fmt.Errorf("service %s/%s eip cannot change; delete and recreate the service", newSvc.Namespace, newSvc.Name))
		}
	}
	if err := v.validateNftableLbServiceEipPort(ctx, &newSvc); err != nil {
		return ctrlwebhook.Errored(http.StatusForbidden, err)
	}
	return ctrlwebhook.Allowed("bypass")
}

// validateNftableLbServiceEipPort rejects a Service that would program the same share-DNAT
// identity (EIP, external port, protocol) as a Service that is already live. The gateway maps one
// identity to one backend set, so a second Service declaring it would silently fight over it.
//
// This is the early, user-visible rejection; the controller still arbitrates a duplicate that
// races past this admission check (two concurrent creates, or duplicates predating the check), so
// it is not the only line of defense.
func (v *ValidatingHook) validateNftableLbServiceEipPort(ctx context.Context, svc *v1.Service) error {
	// A terminating Service is releasing its identity: it must stay updatable (the controller
	// removing its finalizer) even when a successor already owns the same EIP:port.
	if !svc.DeletionTimestamp.IsZero() || !nftableLbServiceEipServing(svc) {
		return nil
	}
	services := &v1.ServiceList{}
	if err := v.cache.List(ctx, services); err != nil {
		return fmt.Errorf("failed to list services for eip %q: %w", svc.Annotations[util.EipAnnotation], err)
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
		if other.Annotations[util.EipAnnotation] != svc.Annotations[util.EipAnnotation] || !nftableLbServiceEipServing(other) {
			continue
		}
		if port, ok := sharedShareDnatPort(svc, other); ok {
			return fmt.Errorf("eip %q port %d/%s is already used by service %s/%s; a given eip and port can back only one LoadBalancer service",
				svc.Annotations[util.EipAnnotation], port.Port, port.Protocol, other.Namespace, other.Name)
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
