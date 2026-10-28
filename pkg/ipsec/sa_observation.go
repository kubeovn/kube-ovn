package ipsec

import (
	"context"
	"errors"
	"maps"
	"net"
	"os/exec"
	"time"

	"github.com/vishvananda/netlink"
)

func privateIKEStatus(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// statusall contains public identities, selectors and SPIs, never SA keys.
	// Do not return command output on failure or pass it through diagnostics.
	return exec.CommandContext(ctx, "/usr/sbin/ipsec", "statusall").Output()
}

func (r *runtimeManager) observeSAs(ctx context.Context, session connectionSession, kernel *netlink.Handle) error {
	intent, err := session.loadIntent(r.store)
	if err != nil {
		return err
	}
	before, err := privateIKEStatus(ctx)
	if err != nil {
		return err
	}
	children, err := intent.installedChildren(before)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return nil // This empty observation never authorizes removal of old SAs.
	}
	// Netlink returns key material. Keep it transient: only kernelInstance's
	// explicit public fields cross the persistence seam; never log states.
	states, err := kernel.XfrmStateList(netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	bindings, err := intent.bindChildren(children, states)
	if err != nil {
		return err
	}
	after, err := privateIKEStatus(ctx)
	if err != nil {
		return err
	}
	confirmed, err := intent.installedChildren(after)
	if err != nil {
		return err
	}
	latest, err := session.loadIntent(r.store)
	if err != nil {
		return err
	}
	if !maps.Equal(children, confirmed) || !maps.Equal(intent.Connections, latest.Connections) {
		return errors.New("IPsec connections changed during ownership observation")
	}
	ledger, err := session.loadLedger(r.store)
	if err != nil {
		return err
	}
	previous := len(ledger.Bindings)
	ledger.record(bindings)
	if previous == len(ledger.Bindings) {
		return nil
	}
	// Retain retired/rekeyed instances. Asynchronous IKE deletion is not proof
	// that an earlier SPI has disappeared from the kernel after a crash.
	return ledger.save(r.store)
}

func (ledger *saLedger) record(bindings []saBinding) {
	seen := make(map[saBinding]struct{}, len(ledger.Bindings)+len(bindings))
	for _, binding := range ledger.Bindings {
		seen[binding] = struct{}{}
	}
	for _, binding := range bindings {
		if _, exists := seen[binding]; !exists {
			ledger.Bindings = append(ledger.Bindings, binding)
			seen[binding] = struct{}{}
		}
	}
}

type saLookup struct {
	Source, Destination string
	SPI                 uint32
}

func (intent *connectionIntent) bindChildren(children map[string]childAssociation, states []netlink.XfrmState) ([]saBinding, error) {
	// Index only public lookup fields, keeping duplicate candidates so an
	// ambiguous SPI is rejected rather than silently overwritten in a map.
	inventory := make(map[saLookup][]netlink.XfrmState, len(states))
	for _, state := range states {
		key := saLookup{Source: state.Src.String(), Destination: state.Dst.String(), SPI: stateSPI(state)}
		inventory[key] = append(inventory[key], state)
	}
	bindings := make([]saBinding, 0, 2*len(children))
	instances := make(map[saInstance]struct{})
	for _, child := range children {
		claim := intent.Connections[child.Connection]
		for _, direction := range []string{"in", "out"} {
			key := saLookup{Source: net.ParseIP(claim.RemoteIP).String(), Destination: net.ParseIP(claim.LocalIP).String(), SPI: child.InboundSPI}
			if direction == "out" {
				key.Source, key.Destination, key.SPI = key.Destination, key.Source, child.OutboundSPI
			}
			binding, err := intent.bindChild(child, direction, inventory[key])
			if err != nil {
				return nil, err
			}
			if _, exists := instances[binding.Instance]; exists {
				return nil, errors.New("multiple CHILD_SAs claim the same kernel instance")
			}
			instances[binding.Instance] = struct{}{}
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}
