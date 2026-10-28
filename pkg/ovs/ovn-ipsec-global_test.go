package ovs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

type ipsecGlobalTransaction struct {
	client.Client
	results []ovsdb.OperationResult
	err     error
}

func (c *ipsecGlobalTransaction) Transact(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	return c.results, c.err
}

func TestIPsecGlobalReadRejectsMissingOrMalformedEvidence(t *testing.T) {
	uuid := ovsdb.UUID{GoUUID: "75980000-0000-0000-0000-000000000001"}
	for _, test := range []struct {
		name    string
		results []ovsdb.OperationResult
		err     error
	}{
		{name: "transport-failure", err: errors.New("unavailable")},
		{name: "missing-result"},
		{name: "missing-row", results: []ovsdb.OperationResult{{Rows: []ovsdb.Row{}}}},
		{name: "multiple-rows", results: []ovsdb.OperationResult{{Rows: []ovsdb.Row{{"_uuid": uuid, "ipsec": false}, {"_uuid": uuid, "ipsec": false}}}}},
		{name: "missing-boolean", results: []ovsdb.OperationResult{{Rows: []ovsdb.Row{{"_uuid": uuid}}}}},
		{name: "wrong-boolean-type", results: []ovsdb.OperationResult{{Rows: []ovsdb.Row{{"_uuid": uuid, "ipsec": "false"}}}}},
		{name: "invalid-uuid", results: []ovsdb.OperationResult{{Rows: []ovsdb.Row{{"_uuid": ovsdb.UUID{GoUUID: "invalid"}, "ipsec": false}}}}},
		{name: "operation-failure", results: []ovsdb.OperationResult{{Error: "not supported", Details: "missing column"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := &OVNSbClient{Client: &ipsecGlobalTransaction{results: test.results, err: test.err}, Timeout: time.Second}
			state, err := c.GetIPsecGlobal(t.Context())
			require.Error(t, err)
			require.Nil(t, state, "invalid evidence cannot authorize plaintext cleanup")
		})
	}
}

func TestIPsecGlobalReadsLiveUnmonitoredSouthbound(t *testing.T) {
	dbModel, err := ovnsb.FullDatabaseModel()
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, "ipsec-sb-global", dbModel, ovnsb.Schema())
	c, err := NewOvnSbClient("unix:"+socket, 5, 5, 10, 0)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	_, err = c.GetIPsecGlobal(t.Context())
	require.Error(t, err, "an absent global row cannot mean disabled")
	state := &ovnsb.SBGlobal{Ipsec: true}
	ops, err := c.Create(state)
	require.NoError(t, err)
	require.NoError(t, c.Transact("seed-ipsec-global", ops))
	actual, err := c.GetIPsecGlobal(t.Context())
	require.NoError(t, err)
	require.True(t, actual.Enabled)
	state.UUID, state.Ipsec = actual.UUID, false
	ops, err = c.Where(state).Update(state, &state.Ipsec)
	require.NoError(t, err)
	require.NoError(t, c.Transact("disable-ipsec-global", ops))
	actual, err = c.GetIPsecGlobal(t.Context())
	require.NoError(t, err)
	require.False(t, actual.Enabled, "the readback must observe committed state without a global-table monitor")
	require.Equal(t, state.UUID, actual.UUID)
	require.Empty(t, c.Cache().Table(ovnsb.SBGlobalTable).Rows(), "adding a second monitor must not be necessary")
}
