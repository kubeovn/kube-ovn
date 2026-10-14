package ovs

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newServiceProxyFallbackTestClient(t *testing.T) *OVNNbClient {
	t.Helper()
	dbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, fmt.Sprintf("service-proxy-%d", time.Now().UnixNano()), dbModel, ovnnb.Schema())
	client, err := newOvnNbClient(t, "unix:"+socket, 3)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	require.NoError(t, client.CreateBareLogicalSwitch("fallback-switch"))
	return client
}

func serviceProxyFallbackACLs(t *testing.T, client *OVNNbClient) []ovnnb.ACL {
	t.Helper()
	acls, err := client.ListAcls("", map[string]string{
		aclParentKey: "fallback-switch", "vendor": util.CniTypeName, serviceProxyFallbackACLKey: "true",
	})
	require.NoError(t, err)
	ls, err := client.GetLogicalSwitch("fallback-switch", false)
	require.NoError(t, err)
	for _, acl := range acls {
		require.Contains(t, ls.ACLs, acl.UUID)
		require.Equal(t, ovnnb.ACLDirectionFromLport, acl.Direction)
		require.Zero(t, acl.Tier)
		require.Equal(t, util.ServiceProxyFallbackACLPriority, acl.Priority)
		require.Equal(t, ovnnb.ACLActionDrop, acl.Action)
		require.Equal(t, map[string]string{"apply-after-lb": "true"}, acl.Options)
		require.NotContains(t, acl.ExternalIDs, "subnet")
	}
	return acls
}

func TestReconcileServiceProxyFallbackACL(t *testing.T) {
	client := newServiceProxyFallbackTestClient(t)
	matches := []string{"ip4.dst == 10.96.0.0/12", "ip6.dst == fd00:10:96::/108"}
	foreign := newACL("fallback-switch", ovnnb.ACLDirectionFromLport,
		strconv.Itoa(util.ServiceProxyFallbackACLPriority), matches[0], ovnnb.ACLActionAllow, 0)
	require.NoError(t, client.CreateAcls("fallback-switch", LogicalSwitchKey, foreign))
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", append(slices.Clone(matches), matches[0])))
	acls := serviceProxyFallbackACLs(t, client)
	require.Len(t, acls, 2)
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", matches))
	require.ElementsMatch(t, acls, serviceProxyFallbackACLs(t, client), "unchanged ACL rows must be reused")
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", []string{matches[1], "ip4.dst == 10.112.0.0/12"}))
	updated := serviceProxyFallbackACLs(t, client)
	require.Len(t, updated, 2)
	for _, acl := range updated {
		require.NotEqual(t, matches[0], acl.Match)
		if acl.Match == matches[1] {
			require.Contains(t, acls, acl, "unchanged family must retain its ACL row")
		}
	}
	require.Error(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", []string{matches[0], ""}))
	require.ElementsMatch(t, updated, serviceProxyFallbackACLs(t, client), "invalid desired state must not partially apply")
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", nil))
	require.Empty(t, serviceProxyFallbackACLs(t, client))
	remaining, err := client.ListAcls("", map[string]string{aclParentKey: "fallback-switch"})
	require.NoError(t, err)
	require.Len(t, remaining, 1, "unrelated ACL with the same identity must survive")
	require.Equal(t, ovnnb.ACLActionAllow, remaining[0].Action)
	require.Error(t, client.ReconcileServiceProxyFallbackACL("", nil))
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("absent-switch", nil))
	require.Error(t, client.ReconcileServiceProxyFallbackACL("absent-switch", matches))
}

func TestReconcileServiceProxyFallbackACLRepairsOwnedRows(t *testing.T) {
	for name, corrupt := range map[string]func(*ovnnb.ACL){
		"duplicate": func(*ovnnb.ACL) {},
		"action":    func(acl *ovnnb.ACL) { acl.Action = ovnnb.ACLActionAllow },
		"options":   func(acl *ovnnb.ACL) { acl.Options = map[string]string{"apply-after-lb": "false"} },
		"direction": func(acl *ovnnb.ACL) { acl.Direction = ovnnb.ACLDirectionToLport },
		"priority":  func(acl *ovnnb.ACL) { acl.Priority-- },
		"tier":      func(acl *ovnnb.ACL) { acl.Tier = 1 },
		"ownership": func(acl *ovnnb.ACL) { acl.ExternalIDs["subnet"] = "fallback-switch" },
	} {
		t.Run(name, func(t *testing.T) {
			client := newServiceProxyFallbackTestClient(t)
			match := "ip4.dst == 10.96.0.0/12"
			makeACL := func() *ovnnb.ACL {
				return newACL("fallback-switch", ovnnb.ACLDirectionFromLport,
					strconv.Itoa(util.ServiceProxyFallbackACLPriority), match, ovnnb.ACLActionDrop, 0,
					func(acl *ovnnb.ACL) {
						acl.Options = map[string]string{"apply-after-lb": "true"}
						acl.ExternalIDs[serviceProxyFallbackACLKey] = "true"
						corrupt(acl)
					})
			}
			require.NoError(t, client.CreateAcls("fallback-switch", LogicalSwitchKey, makeACL(), makeACL()))
			require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", []string{match}))
			require.Len(t, serviceProxyFallbackACLs(t, client), 1)
		})
	}
}

func TestLogicalSwitchPolicyACLsPreserveServiceProxyFallback(t *testing.T) {
	client := newServiceProxyFallbackTestClient(t)
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", []string{"ip4.dst == 10.96.0.0/12"}))
	expected := serviceProxyFallbackACLs(t, client)
	for name, reset := range map[string]func() error{
		"private": func() error {
			return client.SetLogicalSwitchPrivate("fallback-switch", "10.16.0.0/16", "100.64.0.0/16", nil)
		},
		"routed": func() error {
			return client.SetLogicalSwitchRouted("fallback-switch", "ovn-cluster", "10.16.0.0/16", "10.16.0.1",
				"00:00:00:11:22:33", "100.64.0.0/16", nil, false)
		},
		"subnet ACL": func() error {
			return client.UpdateLogicalSwitchACL("fallback-switch", "10.16.0.0/16", []kubeovnv1.ACL{{
				Direction: ovnnb.ACLDirectionFromLport, Priority: 1000, Match: "ip", Action: ovnnb.ACLActionDrop,
			}}, false)
		},
		"clear policies": func() error { return client.DeleteLogicalSwitchPolicyACLs("fallback-switch") },
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, reset())
			require.ElementsMatch(t, expected, serviceProxyFallbackACLs(t, client))
		})
	}
	require.NoError(t, client.DeleteAcls("fallback-switch", LogicalSwitchKey, "", nil))
	ls, err := client.GetLogicalSwitch("fallback-switch", false)
	require.NoError(t, err)
	require.Empty(t, ls.ACLs, "full switch deletion must remove fallback ACLs too")
}

func TestMigrateACLTierPreservesServiceProxyFallback(t *testing.T) {
	client := newServiceProxyFallbackTestClient(t)
	require.NoError(t, client.ReconcileServiceProxyFallbackACL("fallback-switch", []string{"ip4.dst == 10.96.0.0/12"}))
	before := serviceProxyFallbackACLs(t, client)
	legacy := &ovnnb.ACL{
		UUID: ovsclient.NamedUUID(), Direction: ovnnb.ACLDirectionFromLport,
		Priority: 1000, Match: "ip4", Action: ovnnb.ACLActionAllowRelated,
		ExternalIDs: map[string]string{"parent": "fallback-switch", "vendor": util.CniTypeName},
	}
	require.NoError(t, client.CreateAcls("fallback-switch", LogicalSwitchKey, legacy))
	require.NoError(t, client.MigrateACLTier())
	require.ElementsMatch(t, before, serviceProxyFallbackACLs(t, client))
	acls, err := client.ListAcls("", map[string]string{"parent": "fallback-switch"})
	require.NoError(t, err)
	require.Len(t, acls, 2)
	for _, acl := range acls {
		if acl.Match == legacy.Match {
			require.Equal(t, util.NetpolACLTier, acl.Tier)
		}
	}
}
