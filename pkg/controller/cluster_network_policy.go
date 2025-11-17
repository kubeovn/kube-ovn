package controller

import (
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
)

func (c *Controller) enqueueAddCnp(obj any) {
	key := cache.MetaObjectToName(obj.(*v1alpha2.ClusterNetworkPolicy)).String()
	klog.V(3).Infof("enqueue add cnp %s", key)
	c.addAnpQueue.Add(key)
}

func (c *Controller) enqueueDeleteCnp(obj any) {

}

func (c *Controller) enqueueUpdateCnp(oldObj, newObj any) {

}

func (c *Controller) handleAddCnp(key string) (err error) {

}

func (c *Controller) handleUpdateCnp(changed *AdminNetworkPolicyChangedDelta) error {

}

func (c *Controller) handleDeleteCnp(anp *v1alpha1.AdminNetworkPolicy) error {
}
