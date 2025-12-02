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
	bgpEdgeRouterGroupVersion    string
	bgpEdgeRouterKind            string
)

func init() {
	name := reflect.TypeOf(&appsv1.Deployment{}).Elem().Name()
	gvk := appsv1.SchemeGroupVersion.WithKind(name)
	deploymentGroupVersion = gvk.GroupVersion().String()
	deploymentKind = gvk.Kind

	name = reflect.TypeOf(&kubeovnv1.VpcEgressGateway{}).Elem().Name()
	gvk = kubeovnv1.SchemeGroupVersion.WithKind(name)
	vpcEgressGatewayGroupVersion = gvk.GroupVersion().String()
	vpcEgressGatewayKind = gvk.Kind

	name = reflect.TypeOf(&kubeovnv1.BgpEdgeRouter{}).Elem().Name()
	gvk = kubeovnv1.SchemeGroupVersion.WithKind(name)
	bgpEdgeRouterGroupVersion = gvk.GroupVersion().String()
	bgpEdgeRouterKind = gvk.Kind
}

func (c *Controller) enqueueAddDeployment(obj any) {
	deploy := obj.(*appsv1.Deployment)
	for _, ref := range deploy.OwnerReferences {
		if ref.APIVersion == vpcEgressGatewayGroupVersion && ref.Kind == vpcEgressGatewayKind {
			key := types.NamespacedName{Namespace: deploy.Namespace, Name: ref.Name}.String()
			klog.V(3).Infof("enqueue update vpc-egress-gateway %s", key)
			c.addOrUpdateVpcEgressGatewayQueue.Add(key)
			return
		} else if ref.APIVersion == bgpEdgeRouterGroupVersion && ref.Kind == bgpEdgeRouterKind {
			key := types.NamespacedName{Namespace: deploy.Namespace, Name: ref.Name}.String()
			klog.V(3).Infof("enqueue update bgp-edge-router %s", key)
			c.addOrUpdateBgpEdgeRouterQueue.Add(key)
			return
		}
	}
}

func (c *Controller) enqueueUpdateDeployment(_, newObj any) {
	c.enqueueAddDeployment(newObj)
}
