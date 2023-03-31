package controller

import (
	"context"
	"fmt"
	"reflect"
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
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type slrInfo struct {
	Name       string
	Namespace  string
	IsRecreate bool
}

func genSvcName(name string) string {
	return fmt.Sprintf("slr-%s", name)
}

func NewSlrInfo(slr *kubeovnv1.SwitchLBRule) *slrInfo {
	return &slrInfo{
		Name:       slr.Name,
		Namespace:  slr.Spec.Namespace,
		IsRecreate: false,
	}
}

func (c *Controller) enqueueAddSwitchLBRule(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add SwitchLBRule %s", key)
	c.addSwitchLBRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateSwitchLBRule(old, new interface{}) {
	oldSlr := old.(*kubeovnv1.SwitchLBRule)
	newSlr := new.(*kubeovnv1.SwitchLBRule)
	info := NewSlrInfo(oldSlr)

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
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue del SwitchLBRule %s", key)

	slr := obj.(*kubeovnv1.SwitchLBRule)
	info := NewSlrInfo(slr)
	c.delSwitchLBRuleQueue.Add(info)
}

func (c *Controller) processSwitchLBRuleWorkItem(processName string, queue workqueue.RateLimitingInterface, handler func(key *slrInfo) error) bool {
	obj, shutdown := queue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer queue.Done(obj)
		key, ok := obj.(*slrInfo)
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
	}(obj)

	if err != nil {
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
	slr, err := c.switchLBRuleLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	needToCreate := false
	name := genSvcName(slr.Name)
	oldSvc, err := c.config.KubeClient.CoreV1().Services(slr.Spec.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreate = true
		} else {
			klog.Errorf("failed to create service '%s', err: %v", name, err)
			return err
		}
	}

	svc := genHeadlessService(slr, oldSvc)
	if needToCreate {
		if _, err = c.config.KubeClient.CoreV1().Services(slr.Spec.Namespace).Create(context.Background(), svc, metav1.CreateOptions{}); err != nil {
			err := fmt.Errorf("failed to create service '%s', err: %v", svc, err)
			klog.Error(err)
			return err
		}
	} else {
		if _, err = c.config.KubeClient.CoreV1().Services(slr.Spec.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{}); err != nil {
			err := fmt.Errorf("failed to update service '%s', err: %v", svc, err)
			klog.Error(err)
			return err
		}
	}

	formatPorts := ""
	newSlr := slr.DeepCopy()
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
		err := fmt.Errorf("failed to update switch lb rule status, %v", err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) handleDelSwitchLBRule(info *slrInfo) error {
	klog.V(3).Infof("handleDelSwitchLBRule %s", info.Name)

	name := genSvcName(info.Name)
	err := c.config.KubeClient.CoreV1().Services(info.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to delete service %s,err: %v", name, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateSwitchLBRule(info *slrInfo) error {
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

func genHeadlessService(slr *kubeovnv1.SwitchLBRule, oldSvc *corev1.Service) *corev1.Service {
	name := genSvcName(slr.Name)

	var ports []corev1.ServicePort
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

	selectors := make(map[string]string)
	for _, s := range slr.Spec.Selector {
		keyValue := strings.Split(strings.TrimSpace(s), ":")
		if len(keyValue) != 2 {
			continue
		}
		selectors[strings.TrimSpace(keyValue[0])] = strings.TrimSpace(keyValue[1])
	}

	var resourceVersion string
	annotations := map[string]string{}
	if oldSvc != nil {
		for k, v := range oldSvc.Annotations {
			annotations[k] = v
		}
		resourceVersion = oldSvc.ResourceVersion
	}
	annotations[util.SwitchLBRuleVipsAnnotation] = slr.Spec.Vip

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       slr.Spec.Namespace,
			Annotations:     annotations,
			ResourceVersion: resourceVersion,
		},
		Spec: corev1.ServiceSpec{
			Ports:           ports,
			Selector:        selectors,
			ClusterIP:       corev1.ClusterIPNone,
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinity(slr.Spec.SessionAffinity),
		},
	}
	return svc
}
