package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// objectFromEvent returns the metadata object carried by an informer event,
// including delete tombstones.
//
// VPC NAT gateway workload events are mapped back to the owning gateway through
// controller owner references. Kubernetes uses owner references for ownership
// and garbage collection, but does not propagate child events automatically, so
// this controller watches the workloads and enqueues the domain root:
//
//	VpcNatGateway event          -> VpcNatGateway
//	StatefulSet/Deployment event -> VpcNatGateway
//
// Workloads are watched so that a change made to them, including the removal of the
// controller reference, is reconciled back to the desired state.
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
	for _, ref := range object.GetOwnerReferences() {
		if ref.APIVersion != kubeovnv1.SchemeGroupVersion.String() || ref.Kind != util.KindVpcNatGateway {
			continue
		}
		if ref.Name != gw.Name || (gw.UID != "" && ref.UID != gw.UID) {
			return fmt.Errorf("workload %s/%s already references vpc nat gateway %s",
				object.GetNamespace(), object.GetName(), ref.Name)
		}
	}
	if ref := metav1.GetControllerOf(object); ref != nil &&
		(ref.APIVersion != kubeovnv1.SchemeGroupVersion.String() || ref.Kind != util.KindVpcNatGateway ||
			ref.Name != gw.Name || (ref.UID != "" && gw.UID != "" && ref.UID != gw.UID)) {
		return fmt.Errorf("workload %s/%s is already controlled by %s %s/%s",
			object.GetNamespace(), object.GetName(), ref.Kind, ref.APIVersion, ref.Name)
	}
	return nil
}

// vpcNatGatewayControllerReferencePatch migrates a workload created before v1.18
// to a controller reference. It returns needed=false once the migration is done,
// so the patch is issued at most once per workload.
// TODO: remove together with the fallback in vpcNatGatewayOwnerRef.
func vpcNatGatewayControllerReferencePatch(current, desired metav1.Object, gw *kubeovnv1.VpcNatGateway) ([]byte, bool, error) {
	if reflect.DeepEqual(current.GetOwnerReferences(), desired.GetOwnerReferences()) {
		return nil, false, nil
	}
	if err := validateVpcNatGatewayWorkloadController(current, gw); err != nil {
		return nil, false, err
	}

	desiredOwnerReferences := desired.GetOwnerReferences()
	if len(desiredOwnerReferences) != 1 {
		return nil, false, errors.New("desired workload must have exactly one owner reference")
	}
	// Replace only the VpcNatGateway entry so unrelated owner references keep their GC semantics.
	ownerReferences := append([]metav1.OwnerReference(nil), current.GetOwnerReferences()...)
	index := slices.IndexFunc(ownerReferences, func(ref metav1.OwnerReference) bool {
		return ref.APIVersion == kubeovnv1.SchemeGroupVersion.String() && ref.Kind == util.KindVpcNatGateway
	})
	if index < 0 {
		ownerReferences = append(ownerReferences, desiredOwnerReferences[0])
	} else {
		ownerReferences[index] = desiredOwnerReferences[0]
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"ownerReferences": ownerReferences}})
	if err != nil {
		return nil, false, err
	}
	return patch, true, nil
}

// vpcNatGatewayOwnerRef returns the VpcNatGateway owner of a workload. New
// workloads use a controller reference. The all-owner fallback allows existing
// workloads created before controller references were introduced to migrate.
//
// TODO: drop the fallback once every supported upgrade path has passed through
// v1.18, i.e. once no workload can still carry a plain (non-controller) owner
// reference. vpcNatGatewayControllerReferencePatch below can then go as well,
// together with the statefulsets/deployments `patch` RBAC verbs it requires.
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

// enqueueUpdateVpcNatGatewayForWorkload only looks at the new object: a workload keeps
// the same owning gateway for its whole life, so the old object resolves to the same key.
func (c *Controller) enqueueUpdateVpcNatGatewayForWorkload(_, newObj any) {
	c.enqueueVpcNatGatewayForWorkload(newObj)
}
