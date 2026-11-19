package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func readySubnet(name, vpc string) *kubeovnv1.Subnet {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kubeovnv1.SubnetSpec{Vpc: vpc, CIDRBlock: "10.1.0.0/24", Provider: util.OvnProvider},
	}
	subnet.Status.Validated("ValidateLogicalSwitchSuccess", "")
	subnet.Status.Ready("ResetLogicalSwitchAclSuccess", "")
	return subnet
}

func switchDatapath(uuid, name string, newSchema bool) *ovnsb.DatapathBinding {
	dp := &ovnsb.DatapathBinding{UUID: uuid, ExternalIDs: map[string]string{"name": name}}
	if newSchema {
		dpType := ovnsb.DatapathBindingTypeLogicalSwitch
		dp.Type = &dpType
	} else {
		dp.ExternalIDs["logical-switch"] = uuid + "-nb"
	}
	return dp
}

func subnetReadyCondition(t *testing.T, ctrl *Controller, name string) *kubeovnv1.Condition {
	t.Helper()
	subnet, err := ctrl.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return subnet.Status.GetCondition(kubeovnv1.Ready)
}

func TestIsLogicalSwitchDatapath(t *testing.T) {
	routerType := ovnsb.DatapathBindingTypeLogicalRouter
	require.True(t, isLogicalSwitchDatapath(switchDatapath("dp1", "ls1", true)))
	require.True(t, isLogicalSwitchDatapath(switchDatapath("dp2", "ls2", false)))
	require.False(t, isLogicalSwitchDatapath(&ovnsb.DatapathBinding{Type: &routerType, ExternalIDs: map[string]string{"name": "lr"}}))
	require.False(t, isLogicalSwitchDatapath(&ovnsb.DatapathBinding{ExternalIDs: map[string]string{"logical-router": "x", "name": "lr"}}))
}

func TestSubnetDatapathIssuesThreshold(t *testing.T) {
	var issues subnetDatapathIssues
	issue := &subnetDatapathIssue{reason: reasonDatapathNotBound, message: "m"}

	require.Nil(t, issues.observe("s1", issue), "first miss is within the grace period")
	_, ok := issues.get("s1")
	require.False(t, ok)
	require.Equal(t, issue, issues.observe("s1", issue))
	got, ok := issues.get("s1")
	require.True(t, ok)
	require.Equal(t, reasonDatapathNotBound, got.reason)

	require.Nil(t, issues.observe("s1", nil))
	_, ok = issues.get("s1")
	require.False(t, ok)
	require.Nil(t, issues.observe("s1", issue), "a recovered subnet starts a new grace period")

	issues.observe("s2", issue)
	issues.observe("s2", issue)
	issues.forget(map[string]struct{}{"s1": {}})
	_, ok = issues.get("s2")
	require.False(t, ok)
}

func TestCheckSubnetDatapathBindings(t *testing.T) {
	vpc := &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: "vpc1"},
		Status:     kubeovnv1.VpcStatus{Router: "vpc1"},
	}
	bound := readySubnet("bound", "vpc1")
	unboundDP := readySubnet("unbound-dp", "vpc1")
	unboundPort := readySubnet("unbound-port", "vpc1")
	notCreated := readySubnet("not-created", "vpc1")

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{bound, unboundDP, unboundPort, notCreated},
		Vpcs:    []*kubeovnv1.Vpc{vpc},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	nbGlobal := &ovnnb.NBGlobal{UUID: "nb-global", Options: map[string]string{"max_tunid": "4095"}}
	rows := []any{
		nbGlobal,
		&ovnnb.LogicalSwitch{UUID: "ls-bound", Name: "bound"},
		&ovnnb.LogicalSwitch{UUID: "ls-unbound-dp", Name: "unbound-dp"},
		&ovnnb.LogicalSwitch{UUID: "ls-unbound-port", Name: "unbound-port"},
		&ovnnb.LogicalRouterPort{UUID: "lrp-bound", Name: "vpc1-bound"},
		&ovnnb.LogicalRouterPort{UUID: "lrp-unbound-dp", Name: "vpc1-unbound-dp"},
		&ovnnb.LogicalRouterPort{UUID: "lrp-unbound-port", Name: "vpc1-unbound-port"},
		switchDatapath("dp-bound", "bound", true),
		switchDatapath("dp-unbound-port", "unbound-port", false),
		&ovnsb.PortBinding{UUID: "pb-bound", LogicalPort: "vpc1-bound"},
	}
	db := table.NewDatabase(newTableBackend(rows...), time.Second, table.RetryPolicy{})
	ctrl.OVNNbTables = db
	ctrl.OVNSbTables = db

	// first pass: within the grace period, nothing changes
	require.NoError(t, ctrl.checkSubnetDatapathBindingsWithContext(context.Background()))
	for _, name := range []string{"bound", "unbound-dp", "unbound-port", "not-created"} {
		require.Equal(t, corev1.ConditionTrue, subnetReadyCondition(t, ctrl, name).Status, name)
	}

	// second pass: the unbound subnets are reported
	require.NoError(t, ctrl.checkSubnetDatapathBindingsWithContext(context.Background()))
	require.Equal(t, corev1.ConditionTrue, subnetReadyCondition(t, ctrl, "bound").Status)
	require.Equal(t, corev1.ConditionTrue, subnetReadyCondition(t, ctrl, "not-created").Status)

	cond := subnetReadyCondition(t, ctrl, "unbound-dp")
	require.Equal(t, corev1.ConditionFalse, cond.Status)
	require.Equal(t, reasonDatapathNotBound, cond.Reason)
	require.Contains(t, cond.Message, "max_tunid=4095")

	cond = subnetReadyCondition(t, ctrl, "unbound-port")
	require.Equal(t, corev1.ConditionFalse, cond.Status)
	require.Equal(t, reasonRouterPortNotBound, cond.Reason)
	require.Contains(t, cond.Message, "vpc1-unbound-port")

	// a regular reconcile must not flip a reported subnet back to Ready
	subnet, err := ctrl.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), "unbound-dp", metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, ctrl.patchSubnetStatusWithContext(context.Background(), subnet, "ResetLogicalSwitchAclSuccess", ""))
	require.Equal(t, reasonDatapathNotBound, subnetReadyCondition(t, ctrl, "unbound-dp").Reason)

	// northd binds the datapath and the port (e.g. after other subnets were deleted)
	rows = append(rows,
		switchDatapath("dp-unbound-dp", "unbound-dp", true),
		&ovnsb.PortBinding{UUID: "pb-unbound-dp", LogicalPort: "vpc1-unbound-dp"},
		&ovnsb.PortBinding{UUID: "pb-unbound-port", LogicalPort: "vpc1-unbound-port"},
	)
	db = table.NewDatabase(newTableBackend(rows...), time.Second, table.RetryPolicy{})
	ctrl.OVNNbTables = db
	ctrl.OVNSbTables = db

	// wait for the informer to see the NotReady status the check relies on
	require.Eventually(t, func() bool {
		cached, err := ctrl.subnetsLister.Get("unbound-port")
		return err == nil && cached.Status.IsNotReady()
	}, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		cached, err := ctrl.subnetsLister.Get("unbound-dp")
		return err == nil && cached.Status.IsNotReady()
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, ctrl.checkSubnetDatapathBindingsWithContext(context.Background()))
	for _, name := range []string{"unbound-dp", "unbound-port"} {
		cond = subnetReadyCondition(t, ctrl, name)
		require.Equal(t, corev1.ConditionTrue, cond.Status, name)
		require.Equal(t, reasonDatapathBound, cond.Reason, name)
	}
}
