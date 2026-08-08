package daemon

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

type fakeACLSamplingVswitch struct {
	ovs.Vswitch
	err     error
	configs []aclsampling.NodeConfig
}

func (f *fakeACLSamplingVswitch) ReconcileACLSamplingCollectorSet(config aclsampling.NodeConfig) error {
	f.configs = append(f.configs, config)
	return f.err
}

func TestReconcileACLSamplingCollectorSetRecordsAvailability(t *testing.T) {
	nodeName := "acl-sampling-node-success"
	config := aclsampling.NodeConfig{Enabled: true, SetID: 142, LocalGroupID: 142}
	client := &fakeACLSamplingVswitch{}
	controller := &Controller{
		config:        &Configuration{NodeName: nodeName, ACLSampling: config},
		vswitchClient: client,
	}

	controller.reconcileACLSamplingCollectorSet()
	require.Equal(t, []aclsampling.NodeConfig{config}, client.configs)
	require.Equal(t, float64(1), testutil.ToFloat64(metricACLSamplingNodeAvailable.WithLabelValues(nodeName)))

	controller.config.ACLSampling.Enabled = false
	controller.reconcileACLSamplingCollectorSet()
	require.Equal(t, float64(0), testutil.ToFloat64(metricACLSamplingNodeAvailable.WithLabelValues(nodeName)))
}

func TestReconcileACLSamplingCollectorSetRecordsFailure(t *testing.T) {
	nodeName := "acl-sampling-node-failure"
	config := aclsampling.NodeConfig{Enabled: true, SetID: 142, LocalGroupID: 142}
	client := &fakeACLSamplingVswitch{err: errors.New("injected psample capability failure")}
	controller := &Controller{
		config:        &Configuration{NodeName: nodeName, ACLSampling: config},
		vswitchClient: client,
	}
	failuresBefore := testutil.ToFloat64(metricACLSamplingNodeFailures.WithLabelValues(nodeName))

	controller.reconcileACLSamplingCollectorSet()
	require.Equal(t, failuresBefore+1, testutil.ToFloat64(metricACLSamplingNodeFailures.WithLabelValues(nodeName)))
	require.Equal(t, float64(0), testutil.ToFloat64(metricACLSamplingNodeAvailable.WithLabelValues(nodeName)))
}
