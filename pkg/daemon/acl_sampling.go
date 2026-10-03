package daemon

import (
	"errors"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
)

type aclSamplingReconciler interface {
	ReconcileACLSamplingCollectorSet(config aclsampling.NodeConfig) error
}

func (c *Controller) reconcileACLSamplingCollectorSet() {
	err := c.reconcileACLSamplingCollectorSetBackend()
	if err != nil {
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

func (c *Controller) reconcileACLSamplingCollectorSetBackend() error {
	if c.vswitchTables == nil {
		return errors.New("vswitch table provider is nil")
	}
	reconciler, ok := c.vswitchTables.(aclSamplingReconciler)
	if !ok {
		return errors.New("vswitch table provider does not support ACL sampling")
	}
	return reconciler.ReconcileACLSamplingCollectorSet(c.config.ACLSampling)
}
