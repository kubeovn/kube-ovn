package ovs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// benchNatSink keeps benchmark results live so the compiler can't elide the work.
var benchNatSink []*ovnnb.NAT

// listNatOldLoop reproduces the pre-optimization helper: one GetNATByUUID
// (context.WithTimeout + reflection + double deep clone) per UUID in lr.Nat.
func listNatOldLoop(c *OVNNbClient, lrName string) ([]*ovnnb.NAT, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}
	natList := make([]*ovnnb.NAT, 0, len(lr.Nat))
	for _, uuid := range lr.Nat {
		nat, err := c.GetNATByUUID(uuid)
		if err != nil {
			if errors.Is(err, client.ErrNotFound) {
				continue
			}
			return nil, err
		}
		natList = append(natList, nat)
	}
	return natList, nil
}

func newBenchNbClient(b testing.TB, name string) *OVNNbClient {
	b.Helper()
	dbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(b, err)
	_, sock := newOVSDBServer(b, name, dbModel, ovnnb.Schema())
	c, err := newOvnNbClient(b, "unix:"+sock, 10)
	require.NoError(b, err)
	return c
}

// seedNats creates `count` snat rules on lrName (single transaction).
func seedNats(b testing.TB, c *OVNNbClient, lrName string, count int) {
	b.Helper()
	require.NoError(b, c.CreateLogicalRouter(lrName))
	nats := make([]*ovnnb.NAT, 0, count)
	for i := range count {
		logicalIP := fmt.Sprintf("10.16.%d.%d", i/256, i%256)
		nat, err := c.newNat(lrName, "snat", "192.168.0.1", logicalIP, "", "")
		require.NoError(b, err)
		nats = append(nats, nat)
	}
	require.NoError(b, c.CreateNats(lrName, nats...))
}

// BenchmarkListNAT compares the per-UUID Get loop against the single
// WhereCache().List() scan. `target` is the NAT count on the router we list;
// `others` is unrelated NAT rows on a second router that bloat the table but
// must be filtered out (the N << T boundary case).
func BenchmarkListNAT(b *testing.B) {
	cases := []struct {
		label  string
		target int
		others int
	}{
		{"AllOnOneLR_N100", 100, 0},
		{"AllOnOneLR_N1000", 1000, 0},
		{"SmallLR_N10_inBigTable_T2010", 10, 2000},
	}

	for idx, tc := range cases {
		c := newBenchNbClient(b, fmt.Sprintf("bench-nb-%d", idx))
		const lrName = "bench-target-lr"
		seedNats(b, c, lrName, tc.target)
		if tc.others > 0 {
			seedNats(b, c, "bench-other-lr", tc.others)
		}

		b.Run(tc.label+"/old_GetLoop", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				res, err := listNatOldLoop(c, lrName)
				if err != nil {
					b.Fatal(err)
				}
				benchNatSink = res
			}
		})

		b.Run(tc.label+"/new_WhereCache", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				res, err := c.listLogicalRouterNatByFilter(lrName, nil)
				if err != nil {
					b.Fatal(err)
				}
				benchNatSink = res
			}
		})

		// sanity: both paths must return the same number of rows
		oldRes, err := listNatOldLoop(c, lrName)
		require.NoError(b, err)
		newRes, err := c.listLogicalRouterNatByFilter(lrName, nil)
		require.NoError(b, err)
		require.Len(b, newRes, len(oldRes))
		require.Len(b, newRes, tc.target)
	}
}
