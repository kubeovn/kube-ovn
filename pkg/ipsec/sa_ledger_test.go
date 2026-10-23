package ipsec

import (
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

func saIntentFixture(t *testing.T, local, remote string) (*runtimeManager, *connectionIntent, string) {
	t.Helper()
	r := connectionRuntimeFixture(t)
	session, err := r.prepareConnectionSession()
	require.NoError(t, err)
	name := session.Prefix + "ovn-peer2-0-out-3"
	intent := &connectionIntent{connectionSession: *session, Connections: map[string]connectionClaim{
		name: {InterfaceUUID: "75980000-0000-0000-0000-000000000001", LocalIP: local, RemoteIP: remote, Reqid: session.Reqid, MarkOut: "759811/0xffffffff", TunnelType: "geneve"},
	}}
	data, err := json.Marshal(intent)
	require.NoError(t, err)
	require.NoError(t, fileutil.AtomicWriteFile(session.intentPath(r.store), data, 0o600))
	return r, intent, name
}

func saStatesFixture(local, remote string) []netlink.XfrmState {
	host := func(address string) *net.IPNet {
		ip := net.ParseIP(address)
		bits := 128
		if ip.To4() != nil {
			bits = 32
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
	}
	states := []netlink.XfrmState{
		{Src: net.ParseIP(remote), Dst: net.ParseIP(local), Spi: 0x11223344, Selector: &netlink.XfrmPolicy{Src: host(remote), Dst: host(local), Proto: unix.IPPROTO_UDP, SrcPort: 6081}},
		{Src: net.ParseIP(local), Dst: net.ParseIP(remote), Spi: 0xaabbccdd, Mark: &netlink.XfrmMark{Value: 759811, Mask: ^uint32(0)}, Selector: &netlink.XfrmPolicy{Src: host(local), Dst: host(remote), Proto: unix.IPPROTO_UDP, DstPort: 6081}},
	}
	for i := range states {
		states[i].Proto, states[i].Mode, states[i].Reqid = netlink.XFRM_PROTO_ESP, netlink.XFRM_MODE_TRANSPORT, 759811
		states[i].Statistics.AddTime = 1000
		states[i].Aead = &netlink.XfrmStateAlgo{Name: "rfc4106(gcm(aes))", Key: []byte("never-persist-this-SA-key")}
	}
	return states
}

func TestSALedgerBindsBothSPIsAndRetainsRekeyedInstancesWithoutKeys(t *testing.T) {
	for _, addresses := range [][2]string{{"192.0.2.1", "192.0.2.2"}, {"2001:db8::1", "2001:db8::2"}} {
		t.Run(addresses[0], func(t *testing.T) {
			r, intent, name := saIntentFixture(t, addresses[0], addresses[1])
			loaded, err := intent.loadIntent(r.store)
			require.NoError(t, err)
			require.Equal(t, intent, loaded)
			status := []byte(name + "{11}:  INSTALLED, TRANSPORT, reqid 759811, ESP SPIs: 11223344_i aabbccdd_o\nforeign{1}: INSTALLED, TRANSPORT, reqid 759811, ESP SPIs: 11223344_i aabbccdd_o")
			children, err := intent.installedChildren(status)
			require.NoError(t, err)
			require.Len(t, children, 1, "a lease or SPI alone cannot adopt a foreign connection")
			states := saStatesFixture(addresses[0], addresses[1])
			bindings, err := intent.bindChildren(children, states)
			require.NoError(t, err)
			require.Len(t, bindings, 2)
			ledger, err := intent.loadLedger(r.store)
			require.NoError(t, err)
			ledger.record(bindings)
			ledger.record(bindings)
			require.Len(t, ledger.Bindings, 2, "repeated observation cannot grow duplicate evidence")
			states[1].Spi, states[1].Statistics.AddTime = 0xaabbccde, 1001
			child := children[name+"{11}"]
			child.OutboundSPI, child.UniqueID = 0xaabbccde, 12
			binding, err := intent.bindChild(child, "out", states)
			require.NoError(t, err)
			ledger.record([]saBinding{binding})
			require.NoError(t, ledger.save(r.store))
			retained, err := intent.loadLedger(r.store)
			require.NoError(t, err)
			require.Equal(t, ledger, retained)
			require.Len(t, retained.Bindings, 3, "rekey must retain old kernel instance evidence")
			path := filepath.Join(filepath.Dir(intent.intentPath(r.store)), "sa-ledger.json")
			data, err := readRegularFile(path)
			require.NoError(t, err)
			require.NotContains(t, string(data), "never-persist")
			require.NotContains(t, string(data), "Aead")
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}

func TestSALedgerRejectsUnsupportedOrConflictingOwnedStatus(t *testing.T) {
	_, intent, name := saIntentFixture(t, "192.0.2.1", "192.0.2.2")
	line := name + "{11}:  INSTALLED, TRANSPORT, reqid 759811, ESP SPIs: 11223344_i aabbccdd_o"
	for _, status := range []string{
		strings.ReplaceAll(line, "TRANSPORT", "TUNNEL"),
		strings.ReplaceAll(line, "reqid 759811", "reqid 99"),
		strings.ReplaceAll(line, "11223344_i", "00000000_i"),
		strings.ReplaceAll(line, "out-3", "out-2"),
		line + ", IPCOMP CPIs: 0010_i 0011_o",
		line + "\n" + line,
	} {
		children, err := intent.installedChildren([]byte(status))
		require.Error(t, err)
		require.Nil(t, children)
	}
}

func TestSALedgerRefusesAmbiguousOrChangedKernelInstances(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]netlink.XfrmState) []netlink.XfrmState
	}{
		{"foreign-reqid", func(states []netlink.XfrmState) []netlink.XfrmState { states[1].Reqid++; return states }},
		{"foreign-mark", func(states []netlink.XfrmState) []netlink.XfrmState { states[1].Mark.Value++; return states }},
		{"missing-mark", func(states []netlink.XfrmState) []netlink.XfrmState { states[1].Mark = nil; return states }},
		{"foreign-selector", func(states []netlink.XfrmState) []netlink.XfrmState { states[1].Selector.DstPort = 4789; return states }},
		{"missing-lifetime", func(states []netlink.XfrmState) []netlink.XfrmState { states[1].Statistics.AddTime = 0; return states }},
		{"ambiguous", func(states []netlink.XfrmState) []netlink.XfrmState { return append(states, states[1]) }},
		{"absent", func(states []netlink.XfrmState) []netlink.XfrmState { return states[:1] }},
		{"unconfigured-output-mark", func(states []netlink.XfrmState) []netlink.XfrmState {
			states[1].OutputMark = &netlink.XfrmMark{Value: 759811}
			return states
		}},
		{"unexpected-encapsulation", func(states []netlink.XfrmState) []netlink.XfrmState {
			states[1].Encap = &netlink.XfrmStateEncap{Type: netlink.XFRM_ENCAP_ESPINUDP, SrcPort: 4500, DstPort: 4500}
			return states
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, intent, name := saIntentFixture(t, "192.0.2.1", "192.0.2.2")
			child := childAssociation{Connection: name, UniqueID: 11, InboundSPI: 0x11223344, OutboundSPI: 0xaabbccdd}
			_, err := intent.bindChild(child, "out", test.mutate(saStatesFixture("192.0.2.1", "192.0.2.2")))
			require.Error(t, err)
		})
	}
}

func TestSALedgerRejectsDifferentNodeLeaseSessionAndBoot(t *testing.T) {
	r, intent, _ := saIntentFixture(t, "192.0.2.1", "192.0.2.2")
	ledger, err := intent.loadLedger(r.store)
	require.NoError(t, err)
	require.NoError(t, ledger.save(r.store))
	for _, change := range []func(*connectionSession){
		func(session *connectionSession) { session.NodeUID = "replacement-node" },
		func(session *connectionSession) { session.Lease = "replacement-lease" },
		func(session *connectionSession) { session.Mark++ },
		func(session *connectionSession) { session.BootID = "75980000-0000-0000-0000-000000000003" },
	} {
		session := intent.connectionSession
		change(&session)
		_, err := session.loadLedger(r.store)
		require.Error(t, err)
		_, err = session.loadIntent(r.store)
		require.Error(t, err)
	}
}

func TestSALedgerTransportRolesAndUDPEncapsulation(t *testing.T) {
	for _, tunnel := range []string{"geneve", "vxlan"} {
		for _, role := range []string{"in", "out"} {
			t.Run(tunnel+"/"+role, func(t *testing.T) {
				_, intent, name := saIntentFixture(t, "192.0.2.1", "192.0.2.2")
				claim := intent.Connections[name]
				claim.TunnelType = tunnel
				delete(intent.Connections, name)
				name = strings.ReplaceAll(name, "out-3", role+"-3")
				intent.Connections[name] = claim
				children, err := intent.installedChildren([]byte(name + "{11}:  INSTALLED, TRANSPORT, reqid 759811, ESP in UDP SPIs: 11223344_i aabbccdd_o"))
				require.NoError(t, err)
				states := saStatesFixture("192.0.2.1", "192.0.2.2")
				port := 6081
				if tunnel == "vxlan" {
					port = 4789
				}
				states[0].Selector.SrcPort, states[1].Selector.DstPort = port, port
				if role == "in" {
					for _, state := range states {
						state.Selector.SrcPort, state.Selector.DstPort = state.Selector.DstPort, state.Selector.SrcPort
					}
				}
				for i := range states {
					states[i].Encap = &netlink.XfrmStateEncap{Type: netlink.XFRM_ENCAP_ESPINUDP, SrcPort: 4500, DstPort: 4500}
				}
				bindings, err := intent.bindChildren(children, states)
				require.NoError(t, err)
				require.Len(t, bindings, 2)
				for _, binding := range bindings {
					require.True(t, binding.Instance.HasEncapsulation)
					require.Equal(t, 4500, binding.Instance.Encapsulation.DestinationPort)
				}
				states = append(states, states[1])
				_, err = intent.bindChildren(children, states)
				require.ErrorContains(t, err, "ambiguous", "indexing cannot overwrite a duplicate kernel candidate")
			})
		}
	}
}

func TestSALedgerRejectsMultipleChildrenClaimingOneInstance(t *testing.T) {
	_, intent, name := saIntentFixture(t, "192.0.2.1", "192.0.2.2")
	child := childAssociation{Connection: name, UniqueID: 11, InboundSPI: 0x11223344, OutboundSPI: 0xaabbccdd}
	other := child
	other.UniqueID++
	_, err := intent.bindChildren(map[string]childAssociation{"first": child, "second": other}, saStatesFixture("192.0.2.1", "192.0.2.2"))
	require.ErrorContains(t, err, "multiple CHILD_SAs")
}
