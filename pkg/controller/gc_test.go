package controller

import (
	"context"
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newLogicalRouterPort(lrName, lrpName, mac string, networks []string) *ovnnb.LogicalRouterPort {
	return &ovnnb.LogicalRouterPort{
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
		ExternalIDs: map[string]string{
			"lr":     lrName,
			"vendor": util.CniTypeName,
		},
	}
}

func Test_logicalRouterPortFilter(t *testing.T) {
	t.Parallel()

	exceptPeerPorts := strset.New(
		"except-lrp-0",
		"except-lrp-1",
	)

	lrpNames := []string{"other-0", "other-1", "other-2", "except-lrp-0", "except-lrp-1"}
	lrps := make([]*ovnnb.LogicalRouterPort, 0)
	for _, lrpName := range lrpNames {
		lrp := newLogicalRouterPort("", lrpName, "", nil)
		peer := lrpName + "-peer"
		lrp.Peer = &peer
		lrps = append(lrps, lrp)
	}

	filterFunc := logicalRouterPortFilter(exceptPeerPorts)

	for _, lrp := range lrps {
		if exceptPeerPorts.Has(lrp.Name) {
			require.False(t, filterFunc(lrp))
		} else {
			require.True(t, filterFunc(lrp))
		}
	}
}

func Test_gcSecurityGroup(t *testing.T) {
	t.Parallel()

	denyAllPg := ovs.GetSgPortGroupName(util.DenyAllSecurityGroup)
	defaultPg := ovs.GetSgPortGroupName(util.DefaultSecurityGroupName)

	t.Run("EnableSecurityGroup=true preserves deny_all port group", func(t *testing.T) {
		t.Parallel()
		fc := newFakeController(t)
		ctrl := fc.fakeController
		ctrl.config.EnableSecurityGroup = true

		// Create a SecurityGroup CR so its port group is not orphaned
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-sg"},
		}
		_, err := ctrl.config.KubeOvnClient.KubeovnV1().SecurityGroups().Create(
			context.Background(), sg, metav1.CreateOptions{})
		require.NoError(t, err)

		existingSgPg := ovs.GetSgPortGroupName("existing-sg")
		orphanedPg := ovs.GetSgPortGroupName("orphaned-sg")

		portGroups := []ovnnb.PortGroup{
			{Name: denyAllPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DenyAllSecurityGroup}},
			{Name: defaultPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DefaultSecurityGroupName}},
			{Name: existingSgPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": "existing-sg"}},
			{Name: orphanedPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": "orphaned-sg"}},
			{Name: "np-pg", ExternalIDs: map[string]string{"vendor": util.CniTypeName, networkPolicyKey: "some-np"}},
		}

		fc.mockOvnClient.EXPECT().
			ListPortGroups(map[string]string{"vendor": util.CniTypeName}).
			Return(portGroups, nil)
		// Only the orphaned port group should be deleted; deny_all, default, existing-sg, and np-pg are preserved
		fc.mockOvnClient.EXPECT().
			DeletePortGroup(orphanedPg).
			Return(nil)

		err = ctrl.gcSecurityGroup()
		require.NoError(t, err)
	})

	t.Run("EnableSecurityGroup=false garbage-collects deny_all port group", func(t *testing.T) {
		t.Parallel()
		fc := newFakeController(t)
		ctrl := fc.fakeController
		ctrl.config.EnableSecurityGroup = false

		// Create a SecurityGroup CR
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-sg"},
		}
		_, err := ctrl.config.KubeOvnClient.KubeovnV1().SecurityGroups().Create(
			context.Background(), sg, metav1.CreateOptions{})
		require.NoError(t, err)

		existingSgPg := ovs.GetSgPortGroupName("existing-sg")
		orphanedPg := ovs.GetSgPortGroupName("orphaned-sg")

		portGroups := []ovnnb.PortGroup{
			// deny_all should now be GC'd because EnableSecurityGroup=false
			{Name: denyAllPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DenyAllSecurityGroup}},
			{Name: defaultPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DefaultSecurityGroupName}},
			{Name: existingSgPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": "existing-sg"}},
			{Name: orphanedPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": "orphaned-sg"}},
			{Name: "np-pg", ExternalIDs: map[string]string{"vendor": util.CniTypeName, networkPolicyKey: "some-np"}},
		}

		fc.mockOvnClient.EXPECT().
			ListPortGroups(map[string]string{"vendor": util.CniTypeName}).
			Return(portGroups, nil)
		// Both deny_all and orphaned port groups should be deleted
		// deny_all is no longer preserved because EnableSecurityGroup=false
		fc.mockOvnClient.EXPECT().
			DeletePortGroup(denyAllPg, orphanedPg).
			Return(nil)

		err = ctrl.gcSecurityGroup()
		require.NoError(t, err)
	})

	t.Run("no orphaned port groups results in no deletion", func(t *testing.T) {
		t.Parallel()
		fc := newFakeController(t)
		ctrl := fc.fakeController
		ctrl.config.EnableSecurityGroup = true

		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-sg"},
		}
		_, err := ctrl.config.KubeOvnClient.KubeovnV1().SecurityGroups().Create(
			context.Background(), sg, metav1.CreateOptions{})
		require.NoError(t, err)

		existingSgPg := ovs.GetSgPortGroupName("existing-sg")

		portGroups := []ovnnb.PortGroup{
			{Name: denyAllPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DenyAllSecurityGroup}},
			{Name: defaultPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DefaultSecurityGroupName}},
			{Name: existingSgPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": "existing-sg"}},
		}

		fc.mockOvnClient.EXPECT().
			ListPortGroups(map[string]string{"vendor": util.CniTypeName}).
			Return(portGroups, nil)
		// DeletePortGroup should NOT be called

		err = ctrl.gcSecurityGroup()
		require.NoError(t, err)
	})

	t.Run("network policy port groups are always preserved", func(t *testing.T) {
		t.Parallel()
		fc := newFakeController(t)
		ctrl := fc.fakeController
		ctrl.config.EnableSecurityGroup = false

		portGroups := []ovnnb.PortGroup{
			{Name: "np-pg-1", ExternalIDs: map[string]string{"vendor": util.CniTypeName, networkPolicyKey: "policy-a"}},
			{Name: "np-pg-2", ExternalIDs: map[string]string{"vendor": util.CniTypeName, networkPolicyKey: "policy-b"}},
			{Name: defaultPg, ExternalIDs: map[string]string{"vendor": util.CniTypeName, "sg": util.DefaultSecurityGroupName}},
		}

		fc.mockOvnClient.EXPECT().
			ListPortGroups(map[string]string{"vendor": util.CniTypeName}).
			Return(portGroups, nil)
		// No port groups should be deleted

		err := ctrl.gcSecurityGroup()
		require.NoError(t, err)
	})
}
