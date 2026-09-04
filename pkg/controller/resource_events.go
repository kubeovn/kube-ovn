package controller

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func (c *Controller) recordResourceEvent(object runtime.Object, eventType, reason, message string) {
	if c.recorder == nil || object == nil {
		return
	}
	value := reflect.ValueOf(object)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return
	}
	c.recorder.Eventf(object, eventType, reason, "%s", message)
}

func (c *Controller) recordResourceError(object runtime.Object, reason string, err error) error {
	if err != nil {
		c.recordResourceEvent(object, corev1.EventTypeWarning, reason, err.Error())
	}
	return err
}

func (c *Controller) recordSubnetKeyError(name, reason string, err error) error {
	subnet := &kubeovnv1.Subnet{Name: name}
	return c.recordResourceError(subnet, reason, err)
}

func (c *Controller) recordIPPoolKeyError(name, reason string, err error) error {
	ippool := &kubeovnv1.IPPool{Name: name}
	return c.recordResourceError(ippool, reason, err)
}
