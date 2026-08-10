package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func (c *Controller) recordVpcResourceEvent(object runtime.Object, eventType, reason, message string) {
	if c.recorder == nil || object == nil {
		return
	}
	c.recorder.Eventf(object, eventType, reason, "%s", message)
}

func (c *Controller) recordVpcResourceError(object runtime.Object, reason string, err error) {
	if err != nil {
		c.recordVpcResourceEvent(object, corev1.EventTypeWarning, reason, err.Error())
	}
}
