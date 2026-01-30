package webhook

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	createHooks = make(map[schema.GroupVersionKind]admission.HandlerFunc)
	updateHooks = make(map[schema.GroupVersionKind]admission.HandlerFunc)
	deleteHooks = make(map[schema.GroupVersionKind]admission.HandlerFunc)
)

type ValidatingHook struct {
	client  client.Client
	decoder admission.Decoder
	cache   cache.Cache
}

func NewValidatingHook(client client.Client, scheme *runtime.Scheme, cache cache.Cache) (*ValidatingHook, error) {
	v := &ValidatingHook{
		client:  client,
		decoder: admission.NewDecoder(scheme),
		cache:   cache,
	}

	// initialize hook handlers mapping
	createHooks[deploymentGVK] = v.DeploymentCreateHook
	createHooks[statefulSetGVK] = v.StatefulSetCreateHook
	createHooks[daemonSetGVK] = v.DaemonSetCreateHook
	createHooks[cronJobGVK] = v.CronJobCreateHook
	createHooks[jobGVK] = v.JobCreateHook
	createHooks[podGVK] = v.PodCreateHook

	createHooks[subnetGVK] = v.SubnetCreateHook
	updateHooks[subnetGVK] = v.SubnetUpdateHook
	deleteHooks[subnetGVK] = v.SubnetDeleteHook

	createHooks[vpcGVK] = v.VpcCreateHook
	updateHooks[vpcGVK] = v.VpcUpdateHook
	deleteHooks[vpcGVK] = v.VpcDeleteHook

	createHooks[ipGVK] = v.IPCreateHook
	updateHooks[ipGVK] = v.IPUpdateHook

	createHooks[vipGVK] = v.VipCreateHook
	updateHooks[vipGVK] = v.VipUpdateHook

	createHooks[vpcNatGatewayGVK] = v.VpcNatGwCreateOrUpdateHook
	updateHooks[vpcNatGatewayGVK] = v.VpcNatGwCreateOrUpdateHook
	deleteHooks[vpcNatGatewayGVK] = v.VpcNatGwDeleteHook
	createHooks[iptablesEIPGVK] = v.iptablesEIPCreateHook
	updateHooks[iptablesEIPGVK] = v.iptablesEIPUpdateHook
	deleteHooks[iptablesEIPGVK] = v.iptablesEIPDeleteHook
	createHooks[iptablesSnatRule] = v.iptablesSnatCreateHook
	updateHooks[iptablesSnatRule] = v.iptablesSnatUpdateHook
	createHooks[iptablesDnatRule] = v.iptablesDnatCreateHook
	updateHooks[iptablesDnatRule] = v.iptablesDnatUpdateHook
	createHooks[iptablesFIPRule] = v.iptablesFipCreateHook
	updateHooks[iptablesFIPRule] = v.iptablesFipUpdateHook

	createHooks[ovnEip] = v.ovnEipCreateHook
	updateHooks[ovnEip] = v.ovnEipUpdateHook
	deleteHooks[ovnEip] = v.ovnEipDeleteHook
	createHooks[ovnFip] = v.ovnFipCreateHook
	updateHooks[ovnFip] = v.ovnFipUpdateHook
	createHooks[ovnSnat] = v.ovnSnatCreateHook
	updateHooks[ovnSnat] = v.ovnSnatUpdateHook
	createHooks[ovnDnat] = v.ovnDnatCreateHook
	updateHooks[ovnDnat] = v.ovnDnatUpdateHook
	return v, nil
}

func (v *ValidatingHook) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	defer func() {
		if resp.Allowed {
			klog.V(3).Info("result: allowed")
		} else {
			klog.V(3).Infof("result: reject, reason: %s", resp.Result.Reason)
		}
	}()

	key := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}.String()
	gvk := schema.GroupVersionKind{
		Group:   req.Kind.Group,
		Version: req.Kind.Version,
		Kind:    req.Kind.Kind,
	}
	klog.V(3).Infof("admission request for %s %s: operation=%s, uid=%s", gvk.String(), key, req.Operation, req.UID)
	switch req.Operation {
	case admissionv1.Create:
		if createHooks[gvk] != nil {
			klog.Infof("handle create %s %s", gvk, key)
			resp = createHooks[gvk](ctx, req)
			return resp
		}
	case admissionv1.Update:
		if updateHooks[gvk] != nil {
			klog.Infof("handle update %s %s", gvk, key)
			resp = updateHooks[gvk](ctx, req)
			return resp
		}
	case admissionv1.Delete:
		if deleteHooks[gvk] != nil {
			klog.Infof("handle delete %s %s", gvk, key)
			resp = deleteHooks[gvk](ctx, req)
			return resp
		}
	}
	resp = ctrlwebhook.Allowed("bypass")
	return resp
}
