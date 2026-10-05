package ipsec

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

// protectionReservation is an exclusive node-local mark/reqid lease. It is
// not an SA ownership ledger: an SA using these values alone is not deletable.
type protectionReservation struct {
	Version  int    `json:"version"`
	NodeUID  string `json:"nodeUID"`
	Lease    string `json:"lease"`
	Mark     uint32 `json:"mark"`
	Reqid    uint32 `json:"reqid"`
	Required bool   `json:"required,omitzero"`
	Released bool   `json:"released,omitzero"`
	Indexes  [2]int `json:"indexes"`
}

type protectionOwner struct {
	store       store
	reservation protectionReservation
	kernel      *netlink.Handle
}

func (s store) loadProtection(nodeUID string) (*protectionReservation, error) {
	data, err := readRegularFile(filepath.Join(s.dir, "protection.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reservation protectionReservation
	if err := json.Unmarshal(data, &reservation); err != nil {
		return nil, err
	}
	if reservation.Version != 1 || reservation.NodeUID == "" || reservation.NodeUID != nodeUID || reservation.Lease == "" || reservation.Mark == 0 || reservation.Reqid == 0 || reservation.Reqid > 1<<31-1 || reservation.Required && reservation.Released {
		return nil, errors.New("IPsec protection reservation is invalid or belongs to a different Node UID")
	}
	for _, index := range reservation.Indexes {
		if index < 0 || uint64(index) > 1<<32-1 || index != 0 && index&7 != int(netlink.XFRM_DIR_OUT) {
			return nil, errors.New("IPsec protection reservation has an invalid policy index")
		}
	}
	return &reservation, nil
}

func (p *protectionOwner) save() error {
	data, err := json.Marshal(p.reservation)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(p.store.dir, "protection.json"), data, 0o600)
}

// prepareProtection requires the existing node owner lock. A fresh lease must
// be disjoint from the kernel inventory before its intent is persisted. Other
// privileged writers must not reuse a lease; they are outside the owner lock.
func prepareProtection(s store, nodeUID string, kernel *netlink.Handle) (*protectionOwner, error) {
	if nodeUID == "" || kernel == nil {
		return nil, errors.New("IPsec protection requires a Node UID and kernel handle")
	}
	reservation, err := s.loadProtection(nodeUID)
	if err != nil {
		return nil, err
	}
	p := &protectionOwner{store: s, kernel: kernel}
	if reservation != nil {
		p.reservation = *reservation
		return p, nil
	}
	policies, err := kernel.XfrmPolicyList(netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("inventory XFRM policies before protection reservation: %w", err)
	}
	// State key material stays transient and is never logged or persisted.
	states, err := kernel.XfrmStateList(netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("inventory XFRM state before protection reservation: %w", err)
	}
	for range 64 {
		var data [8]byte
		if _, err := rand.Read(data[:]); err != nil {
			return nil, err
		}
		mark := binary.BigEndian.Uint32(data[:4])
		reqid := binary.BigEndian.Uint32(data[4:]) & (1<<31 - 1)
		if !protectionAvailable(mark, reqid, policies, states) {
			continue
		}
		p.reservation = protectionReservation{Version: 1, NodeUID: nodeUID, Lease: rand.Text(), Mark: mark, Reqid: reqid}
		if err := p.save(); err != nil {
			return nil, err
		}
		return p, nil
	}
	return nil, errors.New("cannot reserve IPsec protection without conflicting with existing XFRM resources")
}

func protectionAvailable(mark, reqid uint32, policies []netlink.XfrmPolicy, states []netlink.XfrmState) bool {
	if mark == 0 || reqid == 0 || reqid > 1<<31-1 {
		return false
	}
	for _, policy := range policies {
		for _, template := range policy.Tmpls {
			if template.Reqid == int(reqid) {
				return false
			}
		}
		if policy.Mark != nil {
			if mark&policy.Mark.Mask == policy.Mark.Value&policy.Mark.Mask {
				return false
			}
			continue
		}
		if protectionBypassed(mark, policy) {
			return false
		}
	}
	for _, state := range states {
		if state.Reqid == int(reqid) || state.Mark != nil && mark&state.Mark.Mask == state.Mark.Value&state.Mark.Mask {
			return false
		}
		if state.OutputMark != nil {
			// netlink represents an output mask of 0xffffffff as zero. An
			// input policy/state mask of zero retains its kernel meaning.
			mask := state.OutputMark.Mask
			if mask == 0 {
				mask = ^uint32(0)
			}
			if mark&mask == state.OutputMark.Value&mask {
				return false
			}
		}
	}
	return true
}

// A higher-priority allow can preempt the guard even when the guard itself is
// intact. Only a required ESP template prevents protected tunnel plaintext.
func protectionBypassed(mark uint32, policy netlink.XfrmPolicy) bool {
	if policy.Dir != netlink.XFRM_DIR_OUT || policy.Action != netlink.XFRM_POLICY_ALLOW || policy.Proto != 0 && policy.Proto != netlink.Proto(unix.IPPROTO_UDP) || policy.DstPort != 0 && policy.DstPort != 6081 && policy.DstPort != 4789 {
		return false
	}
	if policy.Mark != nil && mark&policy.Mark.Mask != policy.Mark.Value&policy.Mark.Mask {
		return false
	}
	for _, template := range policy.Tmpls {
		if template.Proto == netlink.XFRM_PROTO_ESP && template.Optional == 0 {
			return false
		}
	}
	return true
}

func (p *protectionOwner) guard(family, index int) *netlink.XfrmPolicy {
	ip, bits := net.IPv4zero, 32
	if family == 1 {
		ip, bits = net.IPv6zero, 128
	}
	return &netlink.XfrmPolicy{
		Src: new(net.IPNet{IP: ip, Mask: net.CIDRMask(0, bits)}), Dst: new(net.IPNet{IP: ip, Mask: net.CIDRMask(0, bits)}),
		Dir: netlink.XFRM_DIR_OUT, Action: netlink.XFRM_POLICY_BLOCK, Priority: 1<<31 - 1,
		Mark: &netlink.XfrmMark{Value: p.reservation.Mark, Mask: ^uint32(0)}, Index: index,
	}
}

func sameGuard(actual, expected *netlink.XfrmPolicy) bool {
	return actual != nil && actual.Src != nil && actual.Dst != nil && actual.Src.String() == expected.Src.String() && actual.Dst.String() == expected.Dst.String() &&
		actual.Dir == expected.Dir && actual.Action == expected.Action && actual.Priority == expected.Priority &&
		actual.Mark != nil && *actual.Mark == *expected.Mark && actual.Proto == 0 && actual.SrcPort == 0 && actual.DstPort == 0 &&
		actual.Ifid == 0 && actual.Ifindex == 0 && len(actual.Tmpls) == 0 && actual.Index > 0 && actual.Index&7 == int(netlink.XFRM_DIR_OUT) && (expected.Index == 0 || actual.Index == expected.Index)
}

func (p *protectionOwner) readGuard(expected *netlink.XfrmPolicy) (*netlink.XfrmPolicy, error) {
	family := netlink.FAMILY_V4
	if expected.Src.IP.To4() == nil {
		family = netlink.FAMILY_V6
	}
	// XfrmPolicyGet currently parses with FAMILY_ALL, which turns an IPv6
	// wildcard selector into IPv4. A family-filtered dump preserves the
	// selector's actual family and never normalizes a conflicting policy.
	policies, err := p.kernel.XfrmPolicyList(family)
	if err != nil {
		return nil, err
	}
	for _, policy := range policies {
		if expected.Index != 0 {
			if policy.Index == expected.Index {
				return &policy, nil
			}
			continue
		}
		if policy.Src != nil && policy.Dst != nil && policy.Src.String() == expected.Src.String() && policy.Dst.String() == expected.Dst.String() &&
			policy.Dir == expected.Dir && policy.Proto == expected.Proto && policy.SrcPort == expected.SrcPort && policy.DstPort == expected.DstPort &&
			policy.Ifid == expected.Ifid && policy.Ifindex == expected.Ifindex && policy.Mark != nil && *policy.Mark == *expected.Mark {
			return &policy, nil
		}
	}
	return nil, unix.ENOENT
}

// arm persists protection intent before installing either family. It never
// removes protection on normal exit or overwrites a conflicting policy. The
// caller may expose the mark to OVN only after both readbacks succeed.
func (p *protectionOwner) arm() error {
	p.reservation.Required = true
	p.reservation.Released = false
	if err := p.save(); err != nil {
		return err
	}
	// Recheck on recovery and every Arm, not just initial lease allocation.
	// This detects a bypass introduced between Prepare and Arm or while the
	// owner was stopped. Other privileged writers still cannot be serialized.
	policies, err := p.kernel.XfrmPolicyList(netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("inventory XFRM policies before arming protection: %w", err)
	}
	for _, policy := range policies {
		if protectionBypassed(p.reservation.Mark, policy) {
			return errors.New("IPsec protection is preempted by an existing tunnel bypass policy")
		}
	}
	for family, index := range p.reservation.Indexes {
		guard := p.guard(family, index)
		actual, err := p.readGuard(guard)
		if errors.Is(err, unix.ENOENT) {
			// A missing policy, including after a host reboot, gets a fresh
			// kernel index. Persist the pending insertion for crash recovery.
			p.reservation.Indexes[family] = 0
			if err := p.save(); err != nil {
				return err
			}
			guard.Index = 0
			if err := p.kernel.XfrmPolicyAdd(guard); err != nil {
				return fmt.Errorf("install IPsec protection guard: %w", err)
			}
			actual, err = p.readGuard(guard)
		}
		if err != nil {
			return fmt.Errorf("read IPsec protection guard: %w", err)
		}
		if !sameGuard(actual, guard) {
			return errors.New("IPsec protection guard conflicts with an existing kernel policy")
		}
		p.reservation.Indexes[family] = actual.Index
		if err := p.save(); err != nil {
			return err
		}
	}
	return nil
}

// verify is read-only: probes must never repair a missing guard and then report
// the stale receipt as proof that it had remained installed.
func (p *protectionOwner) verify() error {
	if !p.reservation.Required {
		return errors.New("IPsec protection is not required")
	}
	policies, err := p.kernel.XfrmPolicyList(netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if protectionBypassed(p.reservation.Mark, policy) {
			return errors.New("IPsec protection is preempted by an existing tunnel bypass policy")
		}
	}
	for family, index := range p.reservation.Indexes {
		if index == 0 {
			return errors.New("IPsec protection has no confirmed kernel index")
		}
		guard := p.guard(family, index)
		actual, err := p.readGuard(guard)
		if err != nil {
			return err
		}
		if !sameGuard(actual, guard) {
			return errors.New("IPsec protection guard conflicts with an existing kernel policy")
		}
	}
	return nil
}

// release removes only the two guards belonging to this durable lease. It is
// called after the controller has entered Release and the OVS identity/output
// references have already been removed. Any readback failure leaves Required
// and the remaining guard intact for retry.
func (p *protectionOwner) release() error {
	if !p.reservation.Required {
		return nil
	}
	if err := p.verify(); err != nil {
		return err
	}
	for family, index := range p.reservation.Indexes {
		if err := p.kernel.XfrmPolicyDel(p.guard(family, index)); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		policies, err := p.kernel.XfrmPolicyList(map[int]int{0: netlink.FAMILY_V4, 1: netlink.FAMILY_V6}[family])
		if err != nil {
			return err
		}
		for _, policy := range policies {
			if sameGuard(&policy, p.guard(family, index)) {
				return errors.New("IPsec protection guard remains after release")
			}
		}
	}
	p.reservation.Required = false
	p.reservation.Released = true
	p.reservation.Indexes = [2]int{}
	if err := p.save(); err != nil {
		return err
	}
	return nil
}
