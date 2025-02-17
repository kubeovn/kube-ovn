package controller

import (
	"fmt"
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func newLogicalRouterPort(lrName, lrpName, mac string, networks []string) *ovnnb.LogicalRouterPort {
	return &ovnnb.LogicalRouterPort{
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
		ExternalIDs: map[string]string{
			"lr": lrName,
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
		peer := fmt.Sprintf("%s-peer", lrpName)
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

func (c *Controller) Test_matchOmitName(t *testing.T) {
	t.Parallel()

	lrp := newLogicalRouterPort("", "name-0", "", nil)

	require.False(t, c.matchOmitName(lrp.Name))

	c.config.OmitKnownName = "findMe"
	require.False(t, c.matchOmitName(lrp.Name))

	lrp.Name = "findMe"
	require.True(t, c.matchOmitName(lrp.Name))
}

func (c *Controller) Test_matchOmitExternalIDs(t *testing.T) {
	t.Parallel()

	lrp := newLogicalRouterPort("", "name-0", "", nil)

	c.config.OmitExternalID = "notFound"
	require.False(t, c.matchOmitExternalIDs(lrp.ExternalIDs))
	require.False(t, c.matchOmitExternalIDs(lrp.ExternalIDs))

	c.config.OmitExternalID = "findMe"
	require.True(t, c.matchOmitExternalIDs(lrp.ExternalIDs))
}
