package ipsec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestProtectionReservationRejectsAliasesAndBypasses(t *testing.T) {
	const mark, reqid = uint32(0xab01), uint32(4000)
	for _, scenario := range []struct {
		name     string
		policies []netlink.XfrmPolicy
		states   []netlink.XfrmState
		allowed  bool
	}{
		{name: "empty", allowed: true},
		{name: "masked-policy-alias", policies: []netlink.XfrmPolicy{{Mark: &netlink.XfrmMark{Value: 0xab00, Mask: 0xffffff00}}}},
		{name: "template-reqid-alias", policies: []netlink.XfrmPolicy{{Tmpls: []netlink.XfrmPolicyTmpl{{Reqid: int(reqid)}}}}},
		{name: "state-reqid-alias", states: []netlink.XfrmState{{Reqid: int(reqid)}}},
		{name: "state-mark-alias", states: []netlink.XfrmState{{Mark: &netlink.XfrmMark{Value: mark, Mask: ^uint32(0)}}}},
		{name: "state-output-mark-alias", states: []netlink.XfrmState{{OutputMark: &netlink.XfrmMark{Value: mark, Mask: ^uint32(0)}}}},
		{name: "state-default-output-mark-alias", states: []netlink.XfrmState{{OutputMark: &netlink.XfrmMark{Value: mark}}}},
		{name: "state-disjoint-default-output-mark", states: []netlink.XfrmState{{OutputMark: &netlink.XfrmMark{Value: 1}}}, allowed: true},
		{name: "global-bypass", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW}}},
		{name: "geneve-bypass", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Proto: netlink.Proto(unix.IPPROTO_UDP), DstPort: 6081}}},
		{name: "vxlan-optional-esp", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, DstPort: 4789, Tmpls: []netlink.XfrmPolicyTmpl{{Proto: netlink.XFRM_PROTO_ESP, Optional: 1}}}}},
		{name: "required-esp", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Tmpls: []netlink.XfrmPolicyTmpl{{Proto: netlink.XFRM_PROTO_ESP, Reqid: 123}}}}, allowed: true},
		{name: "unrelated-tcp", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Proto: netlink.Proto(unix.IPPROTO_TCP)}}, allowed: true},
		{name: "unrelated-ike", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, DstPort: 500}}, allowed: true},
		{name: "disjoint-marked-bypass", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Mark: &netlink.XfrmMark{Value: 1, Mask: ^uint32(0)}}}, allowed: true},
		{name: "unrelated-block", policies: []netlink.XfrmPolicy{{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_BLOCK}}, allowed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.allowed, protectionAvailable(mark, reqid, scenario.policies, scenario.states))
		})
	}
	require.False(t, protectionAvailable(0, reqid, nil, nil))
	require.False(t, protectionAvailable(mark, 0, nil, nil))
}

func TestProtectionStorageRejectsInvalidOwnerOrSymlink(t *testing.T) {
	for _, scenario := range []string{"valid", "version", "node", "lease", "mark", "reqid", "large-reqid", "negative-index", "large-index", "wrong-direction", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			s := store{dir: t.TempDir()}
			owner := &protectionOwner{store: s, reservation: protectionReservation{Version: 1, NodeUID: "node-uid", Lease: "fixture-lease", Mark: 123, Reqid: 456, Required: true, Indexes: [2]int{9, 17}}}
			switch scenario {
			case "version":
				owner.reservation.Version = 2
			case "node":
				owner.reservation.NodeUID = "replaced-node-uid"
			case "lease":
				owner.reservation.Lease = ""
			case "mark":
				owner.reservation.Mark = 0
			case "reqid":
				owner.reservation.Reqid = 0
			case "large-reqid":
				owner.reservation.Reqid = 1 << 31
			case "negative-index":
				owner.reservation.Indexes[0] = -1
			case "large-index":
				if ^uint(0)>>32 == 0 {
					t.Skip("requires a 64-bit policy index fixture")
				}
				index := uint64(1<<32 + 1)
				owner.reservation.Indexes[0] = int(index)
			case "wrong-direction":
				owner.reservation.Indexes[0] = 10
			}
			require.NoError(t, owner.save())
			if scenario == "symlink" {
				path := filepath.Join(s.dir, "protection.json")
				require.NoError(t, os.Rename(path, path+".target"))
				require.NoError(t, os.Symlink(path+".target", path))
			}
			loaded, err := s.loadProtection("node-uid")
			if scenario == "valid" {
				require.NoError(t, err)
				require.Equal(t, owner.reservation, *loaded)
			} else {
				require.Error(t, err)
				require.Nil(t, loaded)
			}
		})
	}
}

func TestProtectionReadbackRejectsPreemptingAllow(t *testing.T) {
	const mark = uint32(123)
	for _, scenario := range []struct {
		name     string
		policy   netlink.XfrmPolicy
		bypassed bool
	}{
		{name: "unmarked-allow", policy: netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW}, bypassed: true},
		{name: "own-mark-allow", policy: netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Mark: &netlink.XfrmMark{Value: mark, Mask: ^uint32(0)}}, bypassed: true},
		{name: "disjoint-mark", policy: netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Mark: &netlink.XfrmMark{Value: 456, Mask: ^uint32(0)}}},
		{name: "own-required-esp", policy: netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Mark: &netlink.XfrmMark{Value: mark, Mask: ^uint32(0)}, Tmpls: []netlink.XfrmPolicyTmpl{{Proto: netlink.XFRM_PROTO_ESP}}}},
		{name: "own-optional-esp", policy: netlink.XfrmPolicy{Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_ALLOW, Mark: &netlink.XfrmMark{Value: mark, Mask: ^uint32(0)}, Tmpls: []netlink.XfrmPolicyTmpl{{Proto: netlink.XFRM_PROTO_ESP, Optional: 1}}}, bypassed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.bypassed, protectionBypassed(mark, scenario.policy))
		})
	}
}
