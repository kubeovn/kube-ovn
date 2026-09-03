package daemon

import "k8s.io/klog/v2"

func (c *Controller) reconcileACLSamplingCollectorSet() {
	if err := c.vswitchClient.ReconcileACLSamplingCollectorSet(c.config.ACLSampling); err != nil {
		metricACLSamplingNodeAvailable.WithLabelValues(c.config.NodeName).Set(0)
		metricACLSamplingNodeFailures.WithLabelValues(c.config.NodeName).Inc()
		klog.Warningf("ACL sampling is unavailable on node %s: %v", c.config.NodeName, err)
		return
	}
	if c.config.ACLSampling.Enabled {
		metricACLSamplingNodeAvailable.WithLabelValues(c.config.NodeName).Set(1)
	} else {
		metricACLSamplingNodeAvailable.WithLabelValues(c.config.NodeName).Set(0)
	}
}
