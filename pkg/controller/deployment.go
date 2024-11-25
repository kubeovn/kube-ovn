package controller

import (
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
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
	name := reflect.TypeOf(&appsv1.Deployment{}).Elem().Name()
	gvk := appsv1.SchemeGroupVersion.WithKind(name)
	deploymentGroupVersion = gvk.GroupVersion().String()
	deploymentKind = gvk.Kind

	name = reflect.TypeOf(&kubeovnv1.VpcEgressGateway{}).Elem().Name()
	gvk = kubeovnv1.SchemeGroupVersion.WithKind(name)
	vpcEgressGatewayGroupVersion = gvk.GroupVersion().String()
	vpcEgressGatewayKind = gvk.Kind
}

func (c *Controller) enqueueAddDeployment(obj interface{}) {
	deploy := obj.(*appsv1.Deployment)
	var vegName string
	for _, ref := range deploy.OwnerReferences {
		if ref.APIVersion == vpcEgressGatewayGroupVersion && ref.Kind == vpcEgressGatewayKind {
			vegName = ref.Name
			break
		}
	}

	if vegName != "" {
		key := fmt.Sprintf("%s/%s", deploy.Namespace, vegName)
		klog.V(3).Infof("enqueue update vpc-egress-gateway %s", key)
		c.addOrUpdateVpcEgressGatewayQueue.Add(key)
	}
}

func (c *Controller) enqueueUpdateDeployment(_, newObj interface{}) {
	c.enqueueAddDeployment(newObj)
}
