package ipsec

import (
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

func drainFixture(t *testing.T) (protectionReservation, []netlink.XfrmPolicy, saLedger) {
	t.Helper()
	r, intent, name := saIntentFixture(t, "192.0.2.1", "192.0.2.2")
	reservation, err := r.store.loadProtection("node-uid")
	require.NoError(t, err)
	owner := protectionOwner{reservation: *reservation}
	policies := []netlink.XfrmPolicy{*owner.guard(0, reservation.Indexes[0]), *owner.guard(1, reservation.Indexes[1])}
	bindings, err := intent.bindChildren(map[string]childAssociation{"child": {Connection: name, UniqueID: 11, InboundSPI: 0x11223344, OutboundSPI: 0xaabbccdd}}, saStatesFixture("192.0.2.1", "192.0.2.2"))
	require.NoError(t, err)
	return *reservation, policies, saLedger{connectionSession: intent.connectionSession, Bindings: bindings}
}

func TestDrainInventoryRequiresAbsenceNotJustPreviouslyObservedOwnership(t *testing.T) {
	reservation, policies, ledger := drainFixture(t)
	require.NoError(t, drainedInventory(reservation, ledger.BootID, []saLedger{ledger}, nil, policies))
	for _, scenario := range []struct {
		name   string
		mutate func(*netlink.XfrmState)
	}{
		{"observed-sa", func(*netlink.XfrmState) {}},
		{"unobserved-install", func(state *netlink.XfrmState) { state.Spi++ }},
		{"tuple-reused-within-same-second", func(state *netlink.XfrmState) {
			state.Reqid, state.Mark = 99, nil
			state.Aead.Key = []byte("different-never-export-this-key")
		}},
		{"tuple-reused-later", func(state *netlink.XfrmState) {
			state.Reqid, state.Mark = 99, nil
			state.Statistics.AddTime++
		}},
		{"overlapping-input-mark", func(state *netlink.XfrmState) {
			state.Spi, state.Reqid = 99, 99
			state.Mark.Mask = 0xff
		}},
		{"overlapping-output-mark", func(state *netlink.XfrmState) {
			state.Spi, state.Reqid, state.Mark = 99, 99, nil
			state.OutputMark = &netlink.XfrmMark{Value: reservation.Mark, Mask: 0}
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state := saStatesFixture("192.0.2.1", "192.0.2.2")[1]
			scenario.mutate(&state)
			err := drainedInventory(reservation, ledger.BootID, []saLedger{ledger}, []netlink.XfrmState{state}, policies)
			require.ErrorContains(t, err, "remaining or ambiguous kernel SA")
			require.NotContains(t, err.Error(), "never-export")
		})
	}
}

func TestDrainInventoryKeepsForeignResourcesAndSeparatesKernelBoots(t *testing.T) {
	reservation, policies, ledger := drainFixture(t)
	foreign := saStatesFixture("198.51.100.1", "198.51.100.2")[1]
	foreign.Reqid, foreign.Mark = 99, nil
	foreignPolicy := netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_BLOCK, Index: 99, Tmpls: []netlink.XfrmPolicyTmpl{{Reqid: 99}}}
	policies = append(policies, foreignPolicy)
	require.NoError(t, drainedInventory(reservation, ledger.BootID, []saLedger{ledger}, []netlink.XfrmState{foreign}, policies))
	reused := saStatesFixture("192.0.2.1", "192.0.2.2")[1]
	reused.Reqid, reused.Mark = 99, nil
	require.Error(t, drainedInventory(reservation, ledger.BootID, []saLedger{ledger}, []netlink.XfrmState{reused}, policies))
	newBoot := "75980000-0000-0000-0000-000000000004"
	require.NoError(t, drainedInventory(reservation, newBoot, []saLedger{ledger}, []netlink.XfrmState{reused}, policies), "a prior boot's tuple cannot identify a new kernel instance")
	reused.Reqid = int(reservation.Reqid)
	require.Error(t, drainedInventory(reservation, newBoot, []saLedger{ledger}, []netlink.XfrmState{reused}, policies), "unobserved installs after reboot still block drain")
	require.Equal(t, 99, foreign.Reqid)
	require.NotNil(t, foreign.Aead)
	require.Equal(t, foreignPolicy, policies[2])
}

func TestDrainInventoryRejectsRemainingPoliciesAndChangedGuards(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func([]netlink.XfrmPolicy, protectionReservation) []netlink.XfrmPolicy
	}{
		{"missing-family", func(policies []netlink.XfrmPolicy, _ protectionReservation) []netlink.XfrmPolicy { return policies[:1] }},
		{"replaced-guard", func(policies []netlink.XfrmPolicy, _ protectionReservation) []netlink.XfrmPolicy {
			policies[0].Action = netlink.XFRM_POLICY_ALLOW
			return policies
		}},
		{"guard-index-reused", func(policies []netlink.XfrmPolicy, _ protectionReservation) []netlink.XfrmPolicy {
			policies[0].Src = &net.IPNet{IP: net.ParseIP("192.0.2.0"), Mask: net.CIDRMask(24, 32)}
			return policies
		}},
		{"leftover-transport", func(policies []netlink.XfrmPolicy, lease protectionReservation) []netlink.XfrmPolicy {
			return append(policies, netlink.XfrmPolicy{Tmpls: []netlink.XfrmPolicyTmpl{{Reqid: int(lease.Reqid)}}})
		}},
		{"unknown-marked-policy", func(policies []netlink.XfrmPolicy, lease protectionReservation) []netlink.XfrmPolicy {
			return append(policies, netlink.XfrmPolicy{Mark: &netlink.XfrmMark{Value: lease.Mark, Mask: 0xff}})
		}},
		{"unmarked-bypass-with-intact-guards", func(policies []netlink.XfrmPolicy, _ protectionReservation) []netlink.XfrmPolicy {
			return append(policies, netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Priority: 1})
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reservation, policies, ledger := drainFixture(t)
			require.Error(t, drainedInventory(reservation, ledger.BootID, []saLedger{ledger}, nil, scenario.mutate(policies, reservation)))
		})
	}
}

func TestDrainEvidenceRetainsUnobservedSessionsAndRejectsForeignLease(t *testing.T) {
	r := connectionRuntimeFixture(t)
	first, err := r.prepareConnectionSession()
	require.NoError(t, err)
	second, err := r.prepareConnectionSession()
	require.NoError(t, err)
	reservation, err := r.store.loadProtection("node-uid")
	require.NoError(t, err)
	ledgers, err := r.store.drainLedgers(*reservation)
	require.NoError(t, err)
	require.Len(t, ledgers, 2, "absence of an intent/SA snapshot must not drop a crashed session")
	for _, ledger := range ledgers {
		require.Empty(t, ledger.Bindings)
	}
	second.Lease = "foreign-lease"
	data, err := json.Marshal(second)
	require.NoError(t, err)
	path := filepath.Join(filepath.Dir(second.intentPath(r.store)), "session.json")
	require.NoError(t, fileutil.AtomicWriteFile(path, data, 0o600))
	_, err = r.store.drainLedgers(*reservation)
	require.ErrorContains(t, err, "protection owner")
	second.Lease = reservation.Lease
	data, err = json.Marshal(second)
	require.NoError(t, err)
	require.NoError(t, fileutil.AtomicWriteFile(path, data, 0o600))
	// A link to another private store must never be followed during cleanup.
	link := filepath.Join(r.store.dir, "connections", "koAAAAAAAAAAAAAAAAAAAAAAAAAA-")
	require.NoError(t, os.Symlink(filepath.Dir(first.intentPath(r.store)), link))
	_, err = r.store.drainLedgers(*reservation)
	require.Error(t, err)
}
