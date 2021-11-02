package controller

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddEndpoint(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add endpoint %s", key)
	c.updateEndpointQueue.Add(key)
}

func (c *Controller) enqueueUpdateEndpoint(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldEp := old.(*v1.Endpoints)
	newEp := new.(*v1.Endpoints)
	if oldEp.ResourceVersion == newEp.ResourceVersion {
		return
	}

	if len(oldEp.Subsets) == 0 && len(newEp.Subsets) == 0 {
		return
	}

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue update endpoint %s", key)
	c.updateEndpointQueue.Add(key)
}

func (c *Controller) runUpdateEndpointWorker() {
	for c.processNextUpdateEndpointWorkItem() {
	}
}

func (c *Controller) processNextUpdateEndpointWorkItem() bool {
	obj, shutdown := c.updateEndpointQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateEndpointQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateEndpointQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateEndpoint(key); err != nil {
			c.updateEndpointQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateEndpointQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleUpdateEndpoint(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	klog.Infof("update endpoint %s/%s", namespace, name)

	ep, err := c.endpointsLister.Endpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	svc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	clusterIPs := svc.Spec.ClusterIPs
	if len(clusterIPs) == 0 && svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		clusterIPs = []string{svc.Spec.ClusterIP}
	}
	if len(clusterIPs) == 0 {
		return nil
	}

	pods, err := c.podsLister.Pods(namespace).List(labels.Set(svc.Spec.Selector).AsSelector())
	if err != nil {
		klog.Errorf("failed to get pods for service %s in namespace %s: %v", name, namespace, err)
		return err
	}

	tcpLb, udpLb := c.config.ClusterTcpLoadBalancer, c.config.ClusterUdpLoadBalancer
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		tcpLb, udpLb = c.config.ClusterTcpSessionLoadBalancer, c.config.ClusterUdpSessionLoadBalancer
	}

	for _, clusterIP := range clusterIPs {
		for _, port := range svc.Spec.Ports {
			vip := util.JoinHostPort(clusterIP, port.Port)
			backends := getServicePortBackends(ep, pods, port, clusterIP)
			if port.Protocol == v1.ProtocolTCP {
				// for performance reason delete lb with no backends
				if len(backends) != 0 {
					err = c.ovnClient.CreateLoadBalancerRule(tcpLb, vip, backends, string(port.Protocol))
					if err != nil {
						klog.Errorf("failed to update vip %s to tcp lb, %v", vip, err)
						return err
					}
				} else {
					err = c.ovnClient.DeleteLoadBalancerVip(vip, tcpLb)
					if err != nil {
						klog.Errorf("failed to delete vip %s at tcp lb, %v", vip, err)
						return err
					}
				}
			} else {
				if len(backends) != 0 {
					err = c.ovnClient.CreateLoadBalancerRule(udpLb, vip, backends, string(port.Protocol))
					if err != nil {
						klog.Errorf("failed to update vip %s to udp lb, %v", vip, err)
						return err
					}
				} else {
					err = c.ovnClient.DeleteLoadBalancerVip(vip, udpLb)
					if err != nil {
						klog.Errorf("failed to delete vip %s at udp lb, %v", vip, err)
						return err
					}
				}
			}
		}
	}

	return nil
}

func getServicePortBackends(endpoints *v1.Endpoints, pods []*v1.Pod, servicePort v1.ServicePort, serviceIP string) string {
	backends := []string{}
	protocol := util.CheckProtocol(serviceIP)
	for _, subset := range endpoints.Subsets {
		var targetPort int32
		for _, port := range subset.Ports {
			if port.Name == servicePort.Name {
				targetPort = port.Port
				break
			}
		}
		if targetPort == 0 {
			continue
		}

		for _, address := range subset.Addresses {
			if address.TargetRef == nil || address.TargetRef.Kind != "Pod" {
				backends = append(backends, util.JoinHostPort(address.IP, targetPort))
				continue
			}

			var ip string
			for _, pod := range pods {
				if pod.Name == address.TargetRef.Name {
					podIPs := pod.Status.PodIPs
					if len(podIPs) == 0 && pod.Status.PodIP != "" {
						podIPs = []v1.PodIP{{IP: pod.Status.PodIP}}
					}
					for _, podIP := range podIPs {
						if util.CheckProtocol(podIP.IP) == protocol {
							ip = podIP.IP
							break
						}
					}
					break
				}
			}
			if ip == "" {
				ip = address.IP
			}
			if ip != "" {
				backends = append(backends, util.JoinHostPort(ip, targetPort))
			}
		}
	}

	return strings.Join(backends, ",")
}
