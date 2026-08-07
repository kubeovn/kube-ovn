package controller

import "k8s.io/klog/v2"

const (
	aclSamplingOperationReconcile = "reconcile"
	aclSamplingOperationPrepare   = "prepare"
	aclSamplingOperationAttach    = "attach"
)

func (c *Controller) reconcileACLSampling() {
	if err := c.OVNNbClient.ReconcileACLSampling(c.config.ACLSampling); err != nil {
		recordACLSamplingFailure(aclSamplingOperationReconcile)
		klog.Warningf("ACL sampling is unavailable: %v", err)
		return
	}
	if c.config.ACLSampling.Enabled {
		metricACLSamplingControllerAvailable.Set(1)
	} else {
		metricACLSamplingControllerAvailable.Set(0)
	}
}

func recordACLSamplingFailure(operation string) {
	metricACLSamplingControllerAvailable.Set(0)
	metricACLSamplingControllerFailures.WithLabelValues(operation).Inc()
}

func recordACLSamplingSuccess() {
	metricACLSamplingControllerAvailable.Set(1)
}
