package ipsec

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/vishvananda/netlink"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

// A drain observation is private, historical evidence, never authorization to
// remove a guard. Coordinated disable must repeat the live kernel/OVSDB checks.
type drainObservation struct {
	connectionSession
	Observed time.Time `json:"observed"`
}

var connectionPrefix = regexp.MustCompile(`^ko[A-Za-z0-9]{20,64}-$`)

// drainLedgers retains evidence from every supervised pair, including a pair
// that crashed before its first periodic SA observation. Missing intent/ledger
// is not proof of an empty kernel; the live lease inventory is checked too.
func (s store) drainLedgers(reservation protectionReservation) ([]saLedger, error) {
	parent := filepath.Join(s.dir, "connections")
	info, err := os.Lstat(parent)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("connection evidence directory is not an owned directory")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	ledgers := make([]saLedger, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !connectionPrefix.MatchString(entry.Name()) {
			return nil, errors.New("unexpected connection evidence directory entry")
		}
		data, err := readRegularFile(filepath.Join(parent, entry.Name(), "session.json"))
		if err != nil {
			return nil, err
		}
		var session connectionSession
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, err
		}
		if session.Version != 1 || session.Prefix != entry.Name() || session.NodeUID != reservation.NodeUID || session.Lease != reservation.Lease || session.Mark != reservation.Mark || session.Reqid != reservation.Reqid || session.BootID == "" {
			return nil, errors.New("connection evidence does not bind the protection owner")
		}
		boot, err := uuid.Parse(session.BootID)
		if err != nil || boot == uuid.Nil() || boot.String() != session.BootID {
			return nil, errors.New("connection evidence has an invalid kernel boot identity")
		}
		ledger, err := session.loadLedger(s)
		if err != nil {
			return nil, err
		}
		ledgers = append(ledgers, *ledger)
	}
	return ledgers, nil
}

func matchesLeaseMark(mark *netlink.XfrmMark, value uint32, output bool) bool {
	if mark == nil {
		return false
	}
	mask := mark.Mask
	if output && mask == 0 {
		mask = ^uint32(0) // netlink's encoding of a full output mask.
	}
	return value&mask == mark.Value&mask
}

// This is a negative inventory check, not an ownership predicate. A lease or
// historical tuple collision blocks completion; it never authorizes deletion.
// AddTime has second precision, so even a different AddTime cannot make tuple
// reuse safely deletable. Unknown/unobserved installations remain blockers.
func drainedInventory(reservation protectionReservation, bootID string, ledgers []saLedger, states []netlink.XfrmState, policies []netlink.XfrmPolicy) error {
	if reservation.Version != 1 || reservation.NodeUID == "" || reservation.Lease == "" || !reservation.Required || reservation.Mark == 0 || reservation.Reqid == 0 || reservation.Reqid > 1<<31-1 || bootID == "" {
		return errors.New("drain inventory requires an armed protection owner")
	}
	known := make(map[saLookup]struct{})
	for _, ledger := range ledgers {
		if ledger.NodeUID != reservation.NodeUID || ledger.Lease != reservation.Lease || ledger.Mark != reservation.Mark || ledger.Reqid != reservation.Reqid {
			return errors.New("drain ledger belongs to a different protection owner")
		}
		if ledger.BootID != bootID {
			continue // The previous boot's tuples cannot identify this kernel.
		}
		for _, binding := range ledger.Bindings {
			instance := binding.Instance
			known[saLookup{Source: instance.Source, Destination: instance.Destination, SPI: instance.SPI}] = struct{}{}
		}
	}
	for _, state := range states {
		_, collision := known[saLookup{Source: state.Src.String(), Destination: state.Dst.String(), SPI: stateSPI(state)}]
		if state.Reqid == int(reservation.Reqid) || matchesLeaseMark(state.Mark, reservation.Mark, false) || matchesLeaseMark(state.OutputMark, reservation.Mark, true) || state.Proto == netlink.XFRM_PROTO_ESP && collision {
			return errors.New("IPsec drain is blocked by a remaining or ambiguous kernel SA")
		}
	}
	owner := protectionOwner{reservation: reservation}
	var guards [2]bool
	for _, policy := range policies {
		if protectionBypassed(reservation.Mark, policy) {
			return errors.New("IPsec drain found a policy that bypasses output protection")
		}
		family := 0
		if policy.Src != nil && policy.Src.IP.To4() == nil {
			family = 1
		}
		if reservation.Indexes[family] != 0 && sameGuard(&policy, owner.guard(family, reservation.Indexes[family])) {
			if guards[family] {
				return errors.New("IPsec drain found duplicate protection guards")
			}
			guards[family] = true
			continue
		}
		if matchesLeaseMark(policy.Mark, reservation.Mark, false) {
			return errors.New("IPsec drain is blocked by a remaining or ambiguous marked policy")
		}
		for _, template := range policy.Tmpls {
			if template.Reqid == int(reservation.Reqid) {
				return errors.New("IPsec drain is blocked by a remaining transport policy")
			}
		}
	}
	if !guards[0] || !guards[1] {
		return errors.New("IPsec drain cannot confirm both unchanged protection guards")
	}
	return nil
}

// Called only after this pair's monitor and IKE process groups have stopped,
// with the node owner lock still held. It neither flushes nor deletes XFRM
// resources. The private observation is written only after successful readback.
func (r *runtimeManager) recordDrain(session connectionSession, kernel *netlink.Handle) error {
	reservation, err := r.store.loadProtection(session.NodeUID)
	if err != nil {
		return err
	}
	if reservation == nil || reservation.Lease != session.Lease || reservation.Mark != session.Mark || reservation.Reqid != session.Reqid {
		return errors.New("protection owner changed before IPsec drain observation")
	}
	bootID, err := r.store.liveDrainInventory(*reservation, kernel)
	if err != nil {
		return err
	}
	if bootID != session.BootID {
		return errors.New("kernel boot changed before IPsec drain observation")
	}
	data, err := json.Marshal(drainObservation{connectionSession: session, Observed: time.Now()})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(filepath.Dir(session.intentPath(r.store)), "drain.json"), data, 0o600)
}

func (s store) liveDrainInventory(reservation protectionReservation, kernel *netlink.Handle) (string, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(boot))
	ledgers, err := s.drainLedgers(reservation)
	if errors.Is(err, os.ErrNotExist) {
		// An owner can crash after Arm and before its first runtime session.
		// Only a missing parent is an empty history; a damaged session is not.
		if _, parentErr := os.Lstat(filepath.Join(s.dir, "connections")); errors.Is(parentErr, os.ErrNotExist) {
			ledgers, err = nil, nil
		}
	}
	if err != nil {
		return "", err
	}
	// Keep key material transient. Neither these structs nor their String
	// methods may cross the persistence or diagnostic seam.
	states, err := kernel.XfrmStateList(netlink.FAMILY_ALL)
	if err != nil {
		return "", err
	}
	// FAMILY_ALL normalizes an IPv6 wildcard selector to IPv4 in netlink's
	// policy parser. Preserve each family exactly, as readGuard does; never
	// normalize a conflicting policy to make it match an owned guard.
	var policies []netlink.XfrmPolicy
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		familyPolicies, err := kernel.XfrmPolicyList(family)
		if err != nil {
			return "", err
		}
		policies = append(policies, familyPolicies...)
	}
	if err := drainedInventory(reservation, bootID, ledgers, states, policies); err != nil {
		return "", err
	}
	return bootID, nil
}
