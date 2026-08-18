package controller

import (
	"time"

	"golang.org/x/time/rate"
	"k8s.io/klog/v2"
)

const (
	aclSamplingOperationReconcile = "reconcile"
	aclSamplingOperationPrepare   = "prepare"
	aclSamplingOperationAttach    = "attach"
)

var aclSamplingPrepareWarningLimiter = rate.NewLimiter(rate.Every(time.Minute), 1)

func (c *Controller) reconcileACLSampling() bool {
	if err := c.OVNNbClient.ReconcileACLSampling(c.config.ACLSampling); err != nil {
		recordACLSamplingFailure(aclSamplingOperationReconcile)
		klog.Warningf("ACL sampling is unavailable: %v", err)
		return false
	}
	if c.config.ACLSampling.Enabled {
		metricACLSamplingControllerAvailable.Set(1)
	} else {
		metricACLSamplingControllerAvailable.Set(0)
	}
	return true
}

func recordACLSamplingFailure(operation string) {
	metricACLSamplingControllerAvailable.Set(0)
	metricACLSamplingControllerFailures.WithLabelValues(operation).Inc()
}

func recordACLSamplingSuccess() {
	metricACLSamplingControllerAvailable.Set(1)
}

func warnACLSamplingPrepareFailure(key string, err error) {
	if aclSamplingPrepareWarningLimiter.Allow() {
		klog.Warningf("failed to prepare ACL sampling for network policy %s: %v", key, err)
		return
	}
	klog.V(4).Infof("suppressed rate-limited ACL sampling prepare warning for network policy %s: %v", key, err)
}
