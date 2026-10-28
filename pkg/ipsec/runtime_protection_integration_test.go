package ipsec

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// A mark can distinguish protected traffic from an unrelated tunnel using the
// same underlay/UDP selector. This kernel prerequisite does not prove that
// ovn-controller preserves the mark on every actual overlay output path.
func TestCandidateMarkedGuard(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_RUNTIME_TEST") != "true" {
		t.Skip("requires the isolated candidate-image runtime harness")
	}
	// Linux disables XFRM on loopback by default. The harness enables it only
	// inside this disposable network namespace so the probe reaches the policy.
	disableXFRM, err := os.ReadFile("/proc/sys/net/ipv4/conf/lo/disable_xfrm")
	require.NoError(t, err)
	require.Equal(t, "0", strings.TrimSpace(string(disableXFRM)), "the loopback fixture must perform outbound XFRM lookups")
	storage := store{dir: t.TempDir()}
	lock, err := storage.lock()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	kernel, err := netlink.NewHandle(unix.NETLINK_XFRM)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, kernel.Close()) })
	require.NoError(t, kernel.SetSocketTimeout(3*time.Second))
	owner, err := prepareProtection(storage, "fixture-node-uid", kernel)
	require.NoError(t, err)
	require.NoError(t, owner.arm())
	reservation := owner.reservation
	// A new in-memory owner must reuse its durable lease and actual kernel
	// policies. Repeating Arm neither flushes nor replaces other resources.
	owner, err = prepareProtection(storage, "fixture-node-uid", kernel)
	require.NoError(t, err)
	require.NoError(t, owner.arm())
	require.Equal(t, reservation, owner.reservation)
	_, err = prepareProtection(storage, "replaced-node-uid", kernel)
	require.Error(t, err, "a same-name replacement Node must not inherit the old lease")
	// Exercise the insertion journal after a crash before indexes were saved.
	owner.reservation.Indexes = [2]int{}
	require.NoError(t, owner.save())
	owner, err = prepareProtection(storage, "fixture-node-uid", kernel)
	require.NoError(t, err)
	require.NoError(t, owner.arm())
	require.Equal(t, reservation, owner.reservation)
	for _, scenario := range []struct {
		network, address string
		port             int
	}{
		{"udp4", "127.0.0.1", 6081},
		{"udp4", "127.0.0.1", 4789},
		{"udp6", "::1", 6081},
		{"udp6", "::1", 4789},
	} {
		t.Run(scenario.network+":"+net.JoinHostPort(scenario.address, strconv.Itoa(scenario.port)), func(t *testing.T) {
			assertMarkedTunnelIsolation(t, owner.reservation.Mark, scenario.network, scenario.address, scenario.port)
		})
	}
	bypass := owner.guard(0, 0)
	bypass.Mark = nil
	bypass.Action = netlink.XFRM_POLICY_ALLOW
	bypass.Priority = 1
	bypass.Proto, bypass.DstPort = netlink.Proto(unix.IPPROTO_UDP), 4789
	require.NoError(t, kernel.XfrmPolicyAdd(bypass))
	require.Error(t, owner.arm(), "an intact guard does not authorize a preempting plaintext bypass")
	require.NoError(t, kernel.XfrmPolicyDel(bypass))
	require.NoError(t, owner.arm())
	conflict := owner.guard(0, owner.reservation.Indexes[0])
	conflict.Priority = 1
	require.NoError(t, kernel.XfrmPolicyUpdate(conflict))
	require.Error(t, owner.arm(), "a changed policy must not be silently overwritten")
	actual, err := kernel.XfrmPolicyGet(conflict)
	require.NoError(t, err)
	require.Equal(t, 1, actual.Priority, "failed reconciliation must preserve the conflicting policy")
	conflict.Priority = 1<<31 - 1
	require.NoError(t, kernel.XfrmPolicyUpdate(conflict))
	// Simulate policy loss, then verify exact readback restoration. Ordinary
	// owner destruction leaves the guard in the isolated kernel namespace.
	require.NoError(t, kernel.XfrmPolicyDel(owner.guard(0, owner.reservation.Indexes[0])))
	require.NoError(t, owner.arm())
	for family, addressFamily := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		policies, err := kernel.XfrmPolicyList(addressFamily)
		require.NoError(t, err)
		found := false
		for _, policy := range policies {
			if policy.Index == owner.reservation.Indexes[family] {
				require.True(t, sameGuard(&policy, owner.guard(family, policy.Index)))
				found = true
			}
		}
		require.True(t, found, "each family must retain its actual kernel guard")
	}
}

func assertMarkedTunnelIsolation(t *testing.T, mark uint32, network, address string, port int) {
	t.Helper()
	listener, err := net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP(address), Port: port})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	sender, err := net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP(address)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sender.Close()) })
	peer := listener.LocalAddr().(*net.UDPAddr)
	buffer := make([]byte, 64)
	assertUnmarked := func() {
		t.Helper()
		_, err := sender.WriteToUDP([]byte("unrelated-tunnel"), peer)
		require.NoError(t, err)
		require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))
		n, _, err := listener.ReadFromUDP(buffer)
		require.NoError(t, err, "the same unmarked underlay/UDP selector must remain usable")
		require.Equal(t, "unrelated-tunnel", string(buffer[:n]))
	}
	assertUnmarked()
	raw, err := sender.SyscallConn()
	require.NoError(t, err)
	setMark := func(value int) {
		t.Helper()
		var markErr error
		err := raw.Control(func(fd uintptr) { markErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, value) })
		require.NoError(t, err)
		require.NoError(t, markErr)
	}
	setMark(int(mark))
	// The kernel can reject sendto synchronously or silently discard the packet.
	// In either case the listener must receive no protected plaintext payload.
	_, _ = sender.WriteToUDP([]byte("protected-tunnel"), peer)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, _, err = listener.ReadFromUDP(buffer)
	networkErr, ok := errors.AsType[net.Error](err)
	require.True(t, ok && networkErr.Timeout(), "marked plaintext must be blocked")
	setMark(0)
	assertUnmarked()
}
