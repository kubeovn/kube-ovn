package speaker

import (
	"context"
	"fmt"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/kubesphere/porter/pkg/bgp/apiutil"
	api "github.com/osrg/gobgp/api"
	"github.com/osrg/gobgp/pkg/packet/bgp"
	v1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"net"
	"strings"
	"time"
)

func isPodAlive(p *v1.Pod) bool {
	if p.Status.Phase == v1.PodSucceeded && p.Spec.RestartPolicy != v1.RestartPolicyAlways {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Spec.RestartPolicy == v1.RestartPolicyNever {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Status.Reason == "Evicted" {
		return false
	}
	return true
}

func generateKey(p *v1.Pod) string {
	return fmt.Sprintf("%s/%s", p.Status.PodIP, p.Status.HostIP)
}

func (c *Controller) enqueueAddPod(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	p := obj.(*v1.Pod)
	if p.Annotations[util.BgpAnnotation] != "true" ||
		p.Annotations[util.AllocatedAnnotation] != "true" ||
		p.Status.PodIP == "" ||
		p.Status.HostIP == "" {
		return
	}

	if !isPodAlive(p) {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(generateKey(p))
	}

	klog.V(3).Infof("enqueue add pod %s", key)
	c.addPodQueue.Add(generateKey(p))
}

func (c *Controller) enqueueDeletePod(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	p := obj.(*v1.Pod)
	if p.Annotations[util.BgpAnnotation] != "true" ||
		p.Annotations[util.AllocatedAnnotation] != "true" ||
		p.Status.PodIP == "" ||
		p.Status.HostIP == "" {
		return
	}

	klog.V(3).Infof("enqueue delete pod %s", key)
	c.deletePodQueue.Add(generateKey(p))
}

func (c *Controller) enqueueUpdatePod(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)
	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	if newPod.Annotations[util.AllocatedAnnotation] != "true" ||
		newPod.Status.PodIP == "" ||
		newPod.Status.HostIP == "" {
		return
	}

	if !isPodAlive(newPod) {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(generateKey(newPod))
	}

	if oldPod.Annotations[util.BgpAnnotation] == "true" && newPod.Annotations[util.BgpAnnotation] != "true" {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(generateKey(newPod))
		return
	}

	if newPod.Annotations[util.BgpAnnotation] == "true" {
		klog.V(3).Infof("enqueue add pod %s", key)
		c.addPodQueue.Add(generateKey(newPod))
		return
	}
}

func (c *Controller) runAddPodWorker() {
	for c.processNextAddPodWorkItem() {
	}
}

func (c *Controller) runDeletePodWorker() {
	for c.processNextDeletePodWorkItem() {
	}
}

func (c *Controller) processNextAddPodWorkItem() bool {
	obj, shutdown := c.addPodQueue.Get()

	if shutdown {
		return false
	}
	now := time.Now()

	err := func(obj interface{}) error {
		defer c.addPodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addPodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddPod(key); err != nil {
			c.addPodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addPodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	last := time.Since(now)
	klog.Infof("take %d ms to deal with add pod", last.Milliseconds())
	return true
}

func (c *Controller) processNextDeletePodWorkItem() bool {
	obj, shutdown := c.deletePodQueue.Get()

	if shutdown {
		return false
	}

	now := time.Now()
	err := func(obj interface{}) error {
		defer c.deletePodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deletePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeletePod(key); err != nil {
			c.deletePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deletePodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	last := time.Since(now)
	klog.Infof("take %d ms to deal with delete pod", last.Milliseconds())
	return true
}

func (c *Controller) handleAddPod(key string) error {
	ip, host := parseKey(key)

	nlri, _ := ptypes.MarshalAny(&api.IPAddressPrefix{
		Prefix:    ip,
		PrefixLen: 32,
	})
	a1, _ := ptypes.MarshalAny(&api.OriginAttribute{
		Origin: 0,
	})
	a2, _ := ptypes.MarshalAny(&api.NextHopAttribute{
		NextHop: host,
	})
	attrs := []*any.Any{a1, a2}
	_, err := c.config.BgpServer.AddPath(context.Background(), &api.AddPathRequest{
		Path: &api.Path{
			Family: &api.Family{Afi: api.Family_AFI_IP, Safi: api.Family_SAFI_UNICAST},
			Nlri:   nlri,
			Pattrs: attrs,
		},
	})
	if err != nil {
		klog.Errorf("add path failed, %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDeletePod(key string) error {
	ip, host := parseKey(key)
	lookup := &api.TableLookupPrefix{
		Prefix: ip,
	}
	listPathRequest := &api.ListPathRequest{
		TableType: api.TableType_GLOBAL,
		Family:    &api.Family{Afi: api.Family_AFI_IP, Safi: api.Family_SAFI_UNICAST},
		Prefixes:  []*api.TableLookupPrefix{lookup},
	}
	fn := func(d *api.Destination) {
		for _, path := range d.Paths {
			attrInterfaces, _ := apiutil.UnmarshalPathAttributes(path.Pattrs)
			nextHop := getNextHopFromPathAttributes(attrInterfaces)
			if nextHop.String() == host {
				if err := c.config.BgpServer.DeletePath(context.Background(), &api.DeletePathRequest{
					Path: path,
				}); err != nil {
					klog.Errorf("failed to delete path %s, %v", path, err)
				}
			}
		}
	}
	err := c.config.BgpServer.ListPath(context.Background(), listPathRequest, fn)
	return err
}

func parseKey(key string) (string, string) {
	tmp := strings.Split(key, "/")
	return tmp[0], tmp[1]
}

func getNextHopFromPathAttributes(attrs []bgp.PathAttributeInterface) net.IP {
	for _, attr := range attrs {
		switch a := attr.(type) {
		case *bgp.PathAttributeNextHop:
			return a.Value
		case *bgp.PathAttributeMpReachNLRI:
			return a.Nexthop
		}
	}
	return nil
}
