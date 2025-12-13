package controller

import (
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var (
	deploymentGroupVersion       string
	deploymentKind               string
	vpcEgressGatewayGroupVersion string
	vpcEgressGatewayKind         string
)

func init() {
	name := reflect.TypeFor[appsv1.Deployment]().Name()
	gvk := appsv1.SchemeGroupVersion.WithKind(name)
	deploymentGroupVersion = gvk.GroupVersion().String()
	deploymentKind = gvk.Kind

	name = reflect.TypeFor[kubeovnv1.VpcEgressGateway]().Name()
	gvk = kubeovnv1.SchemeGroupVersion.WithKind(name)
	vpcEgressGatewayGroupVersion = gvk.GroupVersion().String()
	vpcEgressGatewayKind = gvk.Kind
}

func (c *Controller) enqueueAddDeployment(obj any) {
	deploy := obj.(*appsv1.Deployment)
	for _, ref := range deploy.OwnerReferences {
		if ref.APIVersion == vpcEgressGatewayGroupVersion && ref.Kind == vpcEgressGatewayKind {
			key := types.NamespacedName{Namespace: deploy.Namespace, Name: ref.Name}.String()
			klog.V(3).Infof("enqueue update vpc-egress-gateway %s", key)
			c.addOrUpdateVpcEgressGatewayQueue.Add(key)
			return
		}
	}
}

func (c *Controller) enqueueUpdateDeployment(_, newObj any) {
	c.enqueueAddDeployment(newObj)
}
