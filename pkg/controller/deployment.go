package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueDeploymentEvent(obj any) {
	object := objectFromEvent(obj)
	deploy, ok := object.(*appsv1.Deployment)
	if !ok {
		klog.Warningf("unexpected deployment event object %T", obj)
		return
	}
	_, hasNatGwLabel := deploy.Labels[util.VpcNatGatewayLabel]
	_, hasEgressGwLabel := deploy.Labels[util.VpcEgressGatewayLabel]
	if !hasNatGwLabel && !hasEgressGwLabel {
		return
	}
	c.enqueueVpcNatGatewayForWorkload(deploy)
	for _, ref := range deploy.OwnerReferences {
		if ref.APIVersion == kubeovnv1.SchemeGroupVersion.String() && ref.Kind == util.KindVpcEgressGateway {
			key := types.NamespacedName{Namespace: deploy.Namespace, Name: ref.Name}.String()
			klog.V(3).Infof("enqueue update vpc-egress-gateway %s", key)
			c.addOrUpdateVpcEgressGatewayQueue.Add(key)
			return
		}
	}
}

func (c *Controller) enqueueUpdateDeployment(oldObj, newObj any) {
	c.enqueueDeploymentEvent(oldObj)
	c.enqueueDeploymentEvent(newObj)
}
