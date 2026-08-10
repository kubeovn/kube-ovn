package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func (c *Controller) recordSubnetEvent(subnet *kubeovnv1.Subnet, eventType, reason, message string) {
	if c.recorder == nil || subnet == nil {
		return
	}
	c.recorder.Eventf(subnet, eventType, reason, "%s", message)
}

func (c *Controller) recordSubnetError(subnet *kubeovnv1.Subnet, reason string, err error) error {
	if err != nil {
		c.recordSubnetEvent(subnet, corev1.EventTypeWarning, reason, err.Error())
	}
	return err
}

func (c *Controller) recordSubnetKeyError(name, reason string, err error) error {
	subnet := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: name}}
	return c.recordSubnetError(subnet, reason, err)
}

func (c *Controller) recordIPPoolEvent(ippool *kubeovnv1.IPPool, eventType, reason, message string) {
	if c.recorder == nil || ippool == nil {
		return
	}
	c.recorder.Eventf(ippool, eventType, reason, "%s", message)
}

func (c *Controller) recordIPPoolError(ippool *kubeovnv1.IPPool, reason string, err error) error {
	if err != nil {
		c.recordIPPoolEvent(ippool, corev1.EventTypeWarning, reason, err.Error())
	}
	return err
}

func (c *Controller) recordIPPoolKeyError(name, reason string, err error) error {
	ippool := &kubeovnv1.IPPool{ObjectMeta: metav1.ObjectMeta{Name: name}}
	return c.recordIPPoolError(ippool, reason, err)
}
