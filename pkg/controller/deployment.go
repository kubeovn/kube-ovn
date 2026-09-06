package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddDeployment(obj any) {
	deploy := obj.(*appsv1.Deployment)
	if role := deploy.Labels[util.VpcEndpointStitcherLabel]; role != "" {
		owner := deploy.Labels[util.VpcEndpointOwnerLabel]
		if owner == "" {
			return
		}
		switch role {
		case "provider":
			klog.V(3).Infof("enqueue update VpcEndpointService %s from stitcher deployment %s/%s", owner, deploy.Namespace, deploy.Name)
			c.addOrUpdateVpcEndpointServiceQueue.Add(owner)
		case "consumer":
			klog.V(3).Infof("enqueue update VpcEndpoint %s from stitcher deployment %s/%s", owner, deploy.Namespace, deploy.Name)
			c.addOrUpdateVpcEndpointQueue.Add(owner)
		}
		return
	}
	for _, ref := range deploy.OwnerReferences {
		if ref.APIVersion == kubeovnv1.SchemeGroupVersion.String() {
			if ref.Kind == util.KindVpcEgressGateway {
				key := types.NamespacedName{Namespace: deploy.Namespace, Name: ref.Name}.String()
				klog.V(3).Infof("enqueue update vpc-egress-gateway %s", key)
				c.addOrUpdateVpcEgressGatewayQueue.Add(key)
				return
			}
			if ref.Kind == util.KindVpcNatGateway {
				klog.V(3).Infof("enqueue update vpc-nat-gw %s", ref.Name)
				c.addOrUpdateVpcNatGatewayQueue.Add(ref.Name)
				return
			}
			if ref.Kind == util.KindVpcEndpointService {
				klog.V(3).Infof("enqueue update VpcEndpointService %s", ref.Name)
				c.addOrUpdateVpcEndpointServiceQueue.Add(ref.Name)
				return
			}
			if ref.Kind == util.KindVpcEndpoint {
				klog.V(3).Infof("enqueue update VpcEndpoint %s", ref.Name)
				c.addOrUpdateVpcEndpointQueue.Add(ref.Name)
				return
			}
		}
	}
}

func (c *Controller) enqueueUpdateDeployment(_, newObj any) {
	c.enqueueAddDeployment(newObj)
}

func (c *Controller) enqueueDeleteDeployment(obj any) {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Warningf("unexpected object type: %T", obj)
			return
		}
		deploy, ok = tombstone.Obj.(*appsv1.Deployment)
		if !ok {
			klog.Warningf("unexpected object type: %T", tombstone.Obj)
			return
		}
	}
	c.enqueueAddDeployment(deploy)
}
