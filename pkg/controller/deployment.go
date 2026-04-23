package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddDeployment(obj any) {
	deploy := obj.(*appsv1.Deployment)
	klog.Infof("enqueue update blabla")
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
		}
	}
}

func (c *Controller) enqueueUpdateDeployment(_, newObj any) {
	c.enqueueAddDeployment(newObj)
}
