package controller

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

const (
	aclSamplingOperationReconcile = "reconcile"
	aclSamplingOperationPrepare   = "prepare"
	aclSamplingOperationAttach    = "attach"
)

var aclSamplingPrepareWarningLimiter = rate.NewLimiter(rate.Every(time.Minute), 1)

// aclSamplingProvider is an optional capability layered on top of the generic
// table provider. Sampling owns monitor setup, schema negotiation, and metadata
// allocation, so it remains a domain operation rather than a basic table CRUD
// method.
type aclSamplingProvider interface {
	ReconcileACLSampling(aclsampling.ControllerConfig) error
	PrepareNetworkPolicyACLSampling(string, string, string, string) (*ovs.NetworkPolicySamplingRequest, error)
	ApplyNetworkPolicyACLSampling(aclsampling.ControllerConfig, *ovs.NetworkPolicySamplingRequest) error
}

func (c *Controller) aclSamplingBackend() (aclSamplingProvider, error) {
	if provider, ok := c.OVNNbTables.(aclSamplingProvider); ok {
		return provider, nil
	}
	return nil, errors.New("OVN NB table provider does not support ACL sampling")
}

func (c *Controller) reconcileACLSamplingBackend(config aclsampling.ControllerConfig) error {
	provider, err := c.aclSamplingBackend()
	if err != nil {
		return err
	}
	return provider.ReconcileACLSampling(config)
}

func (c *Controller) prepareNetworkPolicyACLSamplingBackend(pgName, namespace, name, uid string) (*ovs.NetworkPolicySamplingRequest, error) {
	provider, err := c.aclSamplingBackend()
	if err != nil {
		return nil, fmt.Errorf("get ACL sampling provider: %w", err)
	}
	return provider.PrepareNetworkPolicyACLSampling(pgName, namespace, name, uid)
}

func (c *Controller) applyNetworkPolicyACLSamplingBackend(config aclsampling.ControllerConfig, request *ovs.NetworkPolicySamplingRequest) error {
	provider, err := c.aclSamplingBackend()
	if err != nil {
		return fmt.Errorf("get ACL sampling provider: %w", err)
	}
	return provider.ApplyNetworkPolicyACLSampling(config, request)
}

func (c *Controller) reconcileACLSampling() bool {
	if err := c.reconcileACLSamplingBackend(c.config.ACLSampling); err != nil {
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
