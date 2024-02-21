package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type SlrInfo struct {
	Name       string
	Namespace  string
	IsRecreate bool
	Vips       []string
}

func generateSvcName(name string) string {
	return fmt.Sprintf("slr-%s", name)
}

func NewSlrInfo(slr *kubeovnv1.SwitchLBRule) *SlrInfo {
	var (
		vips      []string
		namespace string
	)

	if namespace = slr.Spec.Namespace; namespace == "" {
		namespace = "default"
	}

	for _, port := range slr.Spec.Ports {
		vips = append(
			vips,
			strings.Join(
				[]string{slr.Spec.Vip, strconv.Itoa(int(port.Port))},
				":",
			),
		)
	}

	return &SlrInfo{
		Name:       slr.Name,
		Namespace:  namespace,
		IsRecreate: false,
		Vips:       vips,
	}
}

func (c *Controller) enqueueAddSwitchLBRule(obj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.Infof("enqueue add SwitchLBRule %s", key)
	c.addSwitchLBRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateSwitchLBRule(oldObj, newObj interface{}) {
	var (
		oldSlr = oldObj.(*kubeovnv1.SwitchLBRule)
		newSlr = newObj.(*kubeovnv1.SwitchLBRule)
		info   = NewSlrInfo(oldSlr)
	)

	if oldSlr.ResourceVersion == newSlr.ResourceVersion ||
		reflect.DeepEqual(oldSlr.Spec, newSlr.Spec) {
		return
	}

	if oldSlr.Spec.Namespace != newSlr.Spec.Namespace ||
		oldSlr.Spec.Vip != newSlr.Spec.Vip {
		info.IsRecreate = true
	}

	c.UpdateSwitchLBRuleQueue.Add(info)
}

func (c *Controller) enqueueDeleteSwitchLBRule(obj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.Infof("enqueue del SwitchLBRule %s", key)
	c.delSwitchLBRuleQueue.Add(NewSlrInfo(obj.(*kubeovnv1.SwitchLBRule)))
}

func (c *Controller) processSwitchLBRuleWorkItem(processName string, queue workqueue.RateLimitingInterface, handler func(key *SlrInfo) error) bool {
	obj, shutdown := queue.Get()
	if shutdown {
		return false
	}

	if err := func(obj interface{}) error {
		defer queue.Done(obj)
		key, ok := obj.(*SlrInfo)
		if !ok {
			queue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected switchLBRule in workqueue but got %#v", obj))
			return nil
		}
		if err := handler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s, requeuing", key.Name, err.Error())
		}
		queue.Forget(obj)
		return nil
	}(obj); err != nil {
		utilruntime.HandleError(fmt.Errorf("process: %s. err: %v", processName, err))
		queue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) runDelSwitchLBRuleWorker() {
	for c.processSwitchLBRuleWorkItem("delSwitchLBRule", c.delSwitchLBRuleQueue, c.handleDelSwitchLBRule) {
	}
}

func (c *Controller) runUpdateSwitchLBRuleWorker() {
	for c.processSwitchLBRuleWorkItem("updateSwitchLBRule", c.UpdateSwitchLBRuleQueue, c.handleUpdateSwitchLBRule) {
	}
}

func (c *Controller) runAddSwitchLBRuleWorker() {
	for c.processNextWorkItem("addSwitchLBRule", c.addSwitchLBRuleQueue, c.handleAddOrUpdateSwitchLBRule) {
	}
}

func (c *Controller) handleAddOrUpdateSwitchLBRule(key string) error {
	klog.V(3).Infof("handleAddOrUpdateSwitchLBRule %s", key)

	var (
		slr                              *kubeovnv1.SwitchLBRule
		oldSvc                           *corev1.Service
		oldEps                           *corev1.Endpoints
		svcName                          string
		needToCreateSvc, needToCreateEps bool
		err                              error
	)

	if slr, err = c.switchLBRuleLister.Get(key); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	svcName = generateSvcName(slr.Name)
	if oldSvc, err = c.servicesLister.Services(slr.Spec.Namespace).Get(svcName); err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateSvc = true
			needToCreateEps = true
		} else {
			klog.Errorf("failed to create service '%s', err: %v", svcName, err)
			return err
		}
	}

	if oldEps, err = c.config.KubeClient.CoreV1().Endpoints(slr.Spec.Namespace).Get(context.Background(), svcName, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateEps = true
		} else {
			klog.Errorf("failed to get service endpoints '%s', err: %v", svcName, err)
			return err
		}
	}

	var (
		eps *corev1.Endpoints
		svc *corev1.Service
	)

	// user-defined endpoints used to work with the case of static ips which could not get by selector
	if len(slr.Spec.Endpoints) > 0 {
		eps = generateEndpoints(slr, oldEps)
		if needToCreateEps {
			if _, err = c.config.KubeClient.CoreV1().Endpoints(slr.Spec.Namespace).Create(context.Background(), eps, metav1.CreateOptions{}); err != nil {
				err = fmt.Errorf("failed to create endpoints '%s', err: %v", eps, err)
				klog.Error(err)
				return err
			}
		} else {
			if _, err = c.config.KubeClient.CoreV1().Endpoints(slr.Spec.Namespace).Update(context.Background(), eps, metav1.UpdateOptions{}); err != nil {
				err = fmt.Errorf("failed to update endpoints '%s', err: %v", eps, err)
				klog.Error(err)
				return err
			}
		}
		// avoid conflicts between selectors and user-defined endpoints
		slr.Spec.Selector = nil
	}

	svc = generateHeadlessService(slr, oldSvc)
	if needToCreateSvc {
		if _, err = c.config.KubeClient.CoreV1().Services(slr.Spec.Namespace).Create(context.Background(), svc, metav1.CreateOptions{}); err != nil {
			err = fmt.Errorf("failed to create service '%s', err: %v", svc, err)
			klog.Error(err)
			return err
		}
	} else {
		if _, err = c.config.KubeClient.CoreV1().Services(slr.Spec.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to update service '%s', err: %v", svc, err)
			klog.Error(err)
			return err
		}
	}

	var (
		formatPorts string
		newSlr      = slr.DeepCopy()
	)
	newSlr.Status.Service = fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

	for _, port := range newSlr.Spec.Ports {
		protocol := port.Protocol
		if len(protocol) == 0 {
			protocol = "TCP"
		}
		formatPorts = fmt.Sprintf("%s,%d/%s", formatPorts, port.Port, protocol)
	}
	newSlr.Status.Ports = strings.TrimPrefix(formatPorts, ",")

	if _, err = c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().UpdateStatus(context.Background(), newSlr, metav1.UpdateOptions{}); err != nil {
		err = fmt.Errorf("failed to update switch lb rule status, %v", err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDelSwitchLBRule(info *SlrInfo) error {
	klog.V(3).Infof("handleDelSwitchLBRule %s", info.Name)

	var (
		name  string
		lbhcs []ovnnb.LoadBalancerHealthCheck
		vips  map[string]struct{}
		err   error
	)

	name = generateSvcName(info.Name)
	if err = c.config.KubeClient.CoreV1().Services(info.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to delete service %s, err: %v", name, err)
		return err
	}

	if lbhcs, err = c.OVNNbClient.ListLoadBalancerHealthChecks(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(info.Vips, lbhc.Vip)
		},
	); err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list load balancer health checks matched vips %s, err: %v", info.Vips, err)
		return err
	}

	vips = make(map[string]struct{})
	for _, lbhc := range lbhcs {
		var (
			lbs []ovnnb.LoadBalancer
			vip string
			ex  bool
		)

		if lbs, err = c.OVNNbClient.ListLoadBalancers(
			func(lb *ovnnb.LoadBalancer) bool {
				return slices.Contains(lb.HealthCheck, lbhc.UUID)
			},
		); err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to list load balancer matched vips %s, err: %v", lbhc.Vip, err)
			return err
		}

		for _, lb := range lbs {
			err = c.OVNNbClient.LoadBalancerDeleteHealthCheck(lb.Name, lbhc.UUID)
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete load balancer health checks health checks %s from load balancer matched vip %s, err: %v", lbhc.Vip, lb.Name, err)
				return err
			}

			err = c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lb.Name, lbhc.Vip)
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip port mappings %s from load balancer matched vip %s, err: %v", lbhc.Vip, lb.Name, err)
				return err
			}
		}

		if vip, ex = lbhc.ExternalIDs[util.SwitchLBRuleSubnet]; ex && vip != "" {
			vips[vip] = struct{}{}
		}
	}

	if err = c.OVNNbClient.DeleteLoadBalancerHealthChecks(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(info.Vips, lbhc.Vip)
		},
	); err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("delete load balancer health checks matched vip %s, err: %v", info.Vips, err)
		return err
	}

	for vip := range vips {
		if lbhcs, err = c.OVNNbClient.ListLoadBalancerHealthChecks(
			func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
				return lbhc.ExternalIDs[util.SwitchLBRuleSubnet] == vip
			},
		); err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to list load balancer, err: %v", err)
			return err
		}

		if len(lbhcs) == 0 {
			err = c.config.KubeOvnClient.KubeovnV1().Vips().Delete(context.Background(), vip, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete vip %s for load balancer health check, err: %v", vip, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) handleUpdateSwitchLBRule(info *SlrInfo) error {
	klog.V(3).Infof("handleUpdateSwitchLBRule %s", info.Name)
	if info.IsRecreate {
		if err := c.handleDelSwitchLBRule(info); err != nil {
			klog.Errorf("failed to update switchLBRule, %s", err)
			return err
		}
	}

	if err := c.handleAddOrUpdateSwitchLBRule(info.Name); err != nil {
		klog.Errorf("failed to update switchLBRule, %s", err)
		return err
	}
	return nil
}

func generateHeadlessService(slr *kubeovnv1.SwitchLBRule, oldSvc *corev1.Service) *corev1.Service {
	var (
		name      string
		newSvc    *corev1.Service
		ports     []corev1.ServicePort
		selectors map[string]string
	)

	selectors = make(map[string]string, 0)
	for _, s := range slr.Spec.Selector {
		keyValue := strings.Split(strings.TrimSpace(s), ":")
		if len(keyValue) != 2 {
			continue
		}
		selectors[strings.TrimSpace(keyValue[0])] = strings.TrimSpace(keyValue[1])
	}

	for _, port := range slr.Spec.Ports {
		ports = append(ports, corev1.ServicePort{
			Name:     port.Name,
			Protocol: corev1.Protocol(port.Protocol),
			Port:     port.Port,
			TargetPort: intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: port.TargetPort,
			},
		})
	}

	name = generateSvcName(slr.Name)
	if oldSvc != nil {
		newSvc = oldSvc.DeepCopy()
		newSvc.Name = name
		newSvc.Namespace = slr.Spec.Namespace
		newSvc.Annotations[util.SwitchLBRuleVipsAnnotation] = slr.Spec.Vip
		newSvc.Spec.Ports = ports
		newSvc.Spec.Selector = selectors
		newSvc.Spec.SessionAffinity = corev1.ServiceAffinity(slr.Spec.SessionAffinity)
	} else {
		newSvc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   slr.Spec.Namespace,
				Annotations: map[string]string{util.SwitchLBRuleVipsAnnotation: slr.Spec.Vip},
			},
			Spec: corev1.ServiceSpec{
				Ports:           ports,
				Selector:        selectors,
				ClusterIP:       corev1.ClusterIPNone,
				Type:            corev1.ServiceTypeClusterIP,
				SessionAffinity: corev1.ServiceAffinity(slr.Spec.SessionAffinity),
			},
		}
	}
	return newSvc
}

func generateEndpoints(slr *kubeovnv1.SwitchLBRule, oldEps *corev1.Endpoints) *corev1.Endpoints {
	var (
		name    string
		newEps  *corev1.Endpoints
		ports   []corev1.EndpointPort
		addrs   []corev1.EndpointAddress
		subsets []corev1.EndpointSubset
	)

	for _, port := range slr.Spec.Ports {
		ports = append(
			ports,
			corev1.EndpointPort{
				Name:     port.Name,
				Protocol: corev1.Protocol(port.Protocol),
				Port:     port.TargetPort,
			},
		)
	}

	for _, endpoint := range slr.Spec.Endpoints {
		addrs = append(
			addrs,
			corev1.EndpointAddress{
				IP: endpoint,
				TargetRef: &corev1.ObjectReference{
					Namespace: slr.Namespace,
				},
			},
		)
	}

	subsets = []corev1.EndpointSubset{
		{
			Addresses: addrs,
			Ports:     ports,
		},
	}

	name = generateSvcName(slr.Name)
	if oldEps != nil {
		newEps = oldEps.DeepCopy()
		newEps.Name = name
		newEps.Namespace = slr.Spec.Namespace
		newEps.Subsets = subsets
	} else {
		newEps = &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: slr.Spec.Namespace,
			},
			Subsets: subsets,
		}
	}
	return newEps
}
