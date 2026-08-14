package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func objectFromEvent(obj any) metav1.Object {
	switch t := obj.(type) {
	case metav1.Object:
		return t
	case cache.DeletedFinalStateUnknown:
		if object, ok := t.Obj.(metav1.Object); ok {
			return object
		}
	}
	return nil
}

func validateVpcNatGatewayWorkloadController(object metav1.Object, gw *kubeovnv1.VpcNatGateway) error {
	ref := metav1.GetControllerOf(object)
	if ref == nil {
		return nil
	}
	if ref.APIVersion != kubeovnv1.SchemeGroupVersion.String() || ref.Kind != util.KindVpcNatGateway ||
		ref.Name != gw.Name || (ref.UID != "" && gw.UID != "" && ref.UID != gw.UID) {
		return fmt.Errorf("workload %s/%s is already controlled by %s %s/%s",
			object.GetNamespace(), object.GetName(), ref.Kind, ref.APIVersion, ref.Name)
	}
	return nil
}

func vpcNatGatewayControllerReferencePatch(current, desired metav1.Object, gw *kubeovnv1.VpcNatGateway) ([]byte, bool, error) {
	if reflect.DeepEqual(current.GetOwnerReferences(), desired.GetOwnerReferences()) {
		return nil, false, nil
	}
	if err := validateVpcNatGatewayWorkloadController(current, gw); err != nil {
		return nil, false, err
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"ownerReferences": desired.GetOwnerReferences()},
	})
	if err != nil {
		return nil, false, err
	}
	return patch, true, nil
}

// vpcNatGatewayOwnerRef returns the VpcNatGateway owner of a workload. New
// workloads use a controller reference. The all-owner fallback allows existing
// workloads created before controller references were introduced to migrate.
func vpcNatGatewayOwnerRef(object metav1.Object) *metav1.OwnerReference {
	if ref := metav1.GetControllerOf(object); ref != nil &&
		ref.APIVersion == kubeovnv1.SchemeGroupVersion.String() && ref.Kind == util.KindVpcNatGateway {
		return ref
	}
	ownerReferences := object.GetOwnerReferences()
	for i := range ownerReferences {
		ref := &ownerReferences[i]
		if ref.APIVersion == kubeovnv1.SchemeGroupVersion.String() && ref.Kind == util.KindVpcNatGateway {
			return ref
		}
	}
	return nil
}

func (c *Controller) enqueueVpcNatGatewayOwner(ref *metav1.OwnerReference, reason string) {
	if ref == nil || c.addOrUpdateVpcNatGatewayQueue == nil {
		return
	}
	if c.vpcNatGatewayLister != nil {
		gw, err := c.vpcNatGatewayLister.Get(ref.Name)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to resolve vpc nat gateway owner %s: %v", ref.Name, err)
			}
			return
		}
		if ref.UID != "" && gw.UID != "" && ref.UID != gw.UID {
			klog.V(4).Infof("ignore stale vpc nat gateway owner %s from %s", ref.Name, reason)
			return
		}
	}
	c.enqueueAddOrUpdateVpcNatGwByName(ref.Name, reason)
}

func (c *Controller) enqueueVpcNatGatewayForWorkload(obj any) {
	object := objectFromEvent(obj)
	if object == nil {
		klog.Warningf("unexpected vpc nat gateway workload event object %T", obj)
		return
	}
	if object.GetLabels()[util.VpcNatGatewayLabel] == "true" {
		c.enqueueVpcNatGatewayOwner(vpcNatGatewayOwnerRef(object), "owned-workload-update")
	}
}

func (c *Controller) enqueueUpdateVpcNatGatewayForWorkload(oldObj, newObj any) {
	c.enqueueVpcNatGatewayForWorkload(oldObj)
	c.enqueueVpcNatGatewayForWorkload(newObj)
}

func podEventAffectsVpcNatGateway(oldPod, newPod *corev1.Pod) bool {
	return oldPod.Status.Phase != newPod.Status.Phase ||
		!oldPod.DeletionTimestamp.Equal(newPod.DeletionTimestamp) ||
		!reflect.DeepEqual(oldPod.OwnerReferences, newPod.OwnerReferences) ||
		!maps.Equal(oldPod.Annotations, newPod.Annotations)
}

func (c *Controller) enqueueUpdateVpcNatGatewayForPod(oldObj, newObj any) {
	oldPod, oldOK := oldObj.(*corev1.Pod)
	newPod, newOK := newObj.(*corev1.Pod)
	if !oldOK || !newOK ||
		(oldPod.Labels[util.VpcNatGatewayLabel] != "true" && newPod.Labels[util.VpcNatGatewayLabel] != "true") ||
		!podEventAffectsVpcNatGateway(oldPod, newPod) {
		return
	}
	// Match controller-runtime's owner handler behavior by reconciling both
	// the old and new owners when ownership changes.
	c.enqueueVpcNatGatewayForPod(oldPod)
	c.enqueueVpcNatGatewayForPod(newPod)
}

func (c *Controller) enqueueVpcNatGatewayForPod(obj any) {
	object := objectFromEvent(obj)
	pod, ok := object.(*corev1.Pod)
	if !ok || pod.Labels[util.VpcNatGatewayLabel] != "true" {
		return
	}
	ref, err := c.vpcNatGatewayOwnerFromPod(pod)
	if err != nil {
		klog.V(4).Infof("failed to resolve vpc nat gateway owner for pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}
	c.enqueueVpcNatGatewayOwner(ref, "owned-pod-update")
}

func validateImmediateOwner(ref *metav1.OwnerReference, apiVersion, kind string) error {
	if ref == nil {
		return errors.New("controller owner is missing")
	}
	if ref.APIVersion != apiVersion || ref.Kind != kind {
		return fmt.Errorf("unexpected controller owner %s %s", ref.APIVersion, ref.Kind)
	}
	return nil
}

func validateOwnerUID(ref *metav1.OwnerReference, object metav1.Object) error {
	if ref.UID != "" && object.GetUID() != "" && ref.UID != object.GetUID() {
		return fmt.Errorf("owner UID %s does not match object UID %s", ref.UID, object.GetUID())
	}
	return nil
}

// vpcNatGatewayOwnerFromPod is the client-go equivalent of a controller-runtime
// Watches(Pod, EnqueueRequestsFromMapFunc(...)) owner-chain mapper.
func (c *Controller) vpcNatGatewayOwnerFromPod(pod *corev1.Pod) (*metav1.OwnerReference, error) {
	podOwner := metav1.GetControllerOf(pod)
	if podOwner == nil {
		return nil, errors.New("pod has no controller owner")
	}

	switch podOwner.Kind {
	case util.KindStatefulSet:
		if err := validateImmediateOwner(podOwner, appsv1.SchemeGroupVersion.String(), util.KindStatefulSet); err != nil {
			return nil, err
		}
		if c.statefulSetsLister == nil {
			return nil, errors.New("statefulset lister is not initialized")
		}
		sts, err := c.statefulSetsLister.StatefulSets(pod.Namespace).Get(podOwner.Name)
		if err != nil {
			return nil, err
		}
		if err := validateOwnerUID(podOwner, sts); err != nil {
			return nil, err
		}
		ref := vpcNatGatewayOwnerRef(sts)
		if ref == nil {
			return nil, errors.New("statefulset has no vpc nat gateway owner")
		}
		return ref, nil

	case "ReplicaSet":
		if err := validateImmediateOwner(podOwner, appsv1.SchemeGroupVersion.String(), "ReplicaSet"); err != nil {
			return nil, err
		}
		if c.replicaSetsLister == nil || c.deploymentsLister == nil {
			return nil, errors.New("replicaset or deployment lister is not initialized")
		}
		rs, err := c.replicaSetsLister.ReplicaSets(pod.Namespace).Get(podOwner.Name)
		if err != nil {
			return nil, err
		}
		if err := validateOwnerUID(podOwner, rs); err != nil {
			return nil, err
		}
		deployOwner := metav1.GetControllerOf(rs)
		if err := validateImmediateOwner(deployOwner, appsv1.SchemeGroupVersion.String(), util.KindDeployment); err != nil {
			return nil, err
		}
		deploy, err := c.deploymentsLister.Deployments(pod.Namespace).Get(deployOwner.Name)
		if err != nil {
			return nil, err
		}
		if err := validateOwnerUID(deployOwner, deploy); err != nil {
			return nil, err
		}
		ref := vpcNatGatewayOwnerRef(deploy)
		if ref == nil {
			return nil, errors.New("deployment has no vpc nat gateway owner")
		}
		return ref, nil
	default:
		return nil, fmt.Errorf("unsupported pod controller owner kind %s", podOwner.Kind)
	}
}
