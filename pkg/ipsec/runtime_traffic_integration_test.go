package ipsec

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// These fixtures contain only ephemeral synthetic keys. The harness mounts
// one node's identity per test container and removes the volume afterwards.
func TestCandidateTrafficFixtures(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_TRAFFIC_FIXTURES") != "true" {
		t.Skip("requires the isolated candidate traffic harness")
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "candidate traffic CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	trust := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	ca, err = x509.ParseCertificate(der)
	require.NoError(t, err)
	for i := range 2 {
		dir := filepath.Join("/fixtures", fmt.Sprintf("node-%d", i))
		require.NoError(t, os.MkdirAll(dir, 0o700))
		key, err := newPrivateKey()
		require.NoError(t, err)
		parsed, err := privateKey(key)
		require.NoError(t, err)
		chassis := fmt.Sprintf("traffic-chassis-%d", i)
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: chassis}, DNSNames: []string{chassis}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &parsed.PublicKey, caKey)
		require.NoError(t, err)
		for name, data := range map[string][]byte{"certificate": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "private-key": key, "trust": trust} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
		}
	}
}

// This validates real IKE and Linux transport policies with synthetic UDP
// payloads. It is a prerequisite, not a substitute for actual OVN overlay,
// rollout, fail-closed protection and cluster identity E2E coverage.
func TestCandidateEncryptedTraffic(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_TRAFFIC_TEST") != "true" {
		t.Skip("requires the isolated candidate traffic harness")
	}
	local, peer := os.Getenv("IPSEC_LOCAL_IP"), os.Getenv("IPSEC_PEER_IP")
	node, peerNode := os.Getenv("IPSEC_NODE"), os.Getenv("IPSEC_PEER_NODE")
	tunnel := os.Getenv("IPSEC_TUNNEL")
	port := map[string]int{"geneve": 6081, "vxlan": 4789}[tunnel]
	require.NotZero(t, port)
	// Prototype only: reserve this synthetic source/UDP selector in an isolated
	// namespace. Production needs a verified ownership/conflict contract before
	// applying such a rule: other tunnels can share an underlay address/port.
	// The rule belongs to neither charon nor its dynamically allocated reqids.
	guard := []string{"xfrm", "policy", "add", "src", local, "dst", "0.0.0.0/0", "proto", "udp", "dport", strconv.Itoa(port), "dir", "out", "priority", "2147483647", "index", "759801", "action", "block"}
	if net.ParseIP(local).To4() == nil {
		guard[6] = "::/0"
	}
	require.NoError(t, command(t.Context(), "ip", guard...))
	assertGuard := func() {
		t.Helper()
		output, err := exec.CommandContext(t.Context(), "ip", "xfrm", "policy", "get", "index", "759801", "dir", "out").Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "action block")
		require.Contains(t, string(output), "priority 2147483647")
	}
	assertGuard()
	probeBlocked := func() {
		t.Helper()
		// An XFRM block can reject connect() before any packet is sent. Use an
		// unconnected socket so the probe exercises the blocked output path.
		probe, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(local)})
		require.NoError(t, err)
		defer func() { require.NoError(t, probe.Close()) }()
		// Linux may report a policy rejection synchronously or drop the packet.
		// The independent outer capture must contain no plaintext in either case.
		for range 10 {
			_, _ = probe.WriteToUDP([]byte("candidate-blocked-transport-"+node), &net.UDPAddr{IP: net.ParseIP(peer), Port: port})
		}
	}
	probeBlocked()
	cert, err := os.ReadFile("/fixtures/certificate")
	require.NoError(t, err)
	key, err := os.ReadFile("/fixtures/private-key")
	require.NoError(t, err)
	trust, err := os.ReadFile("/fixtures/trust")
	require.NoError(t, err)
	client := fake.NewClientset(
		&corev1.Node{Name: node, UID: types.UID("traffic-node-" + node)},
		&corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": trust}},
	)
	a, err := New(Configuration{NodeName: node, PodUID: "traffic-pod-" + node, Namespace: "kube-system", Kube: client, KeyDir: t.TempDir(), RuntimeDir: t.TempDir(), OVSSocket: "/run/openvswitch/db.sock", Duration: time.Hour, RequestTimeout: 30 * time.Second, Priority: -5})
	require.NoError(t, err)
	g := &generation{ID: digest(key), NodeUID: "traffic-node-" + node, Chassis: "traffic-chassis-" + node}
	require.NoError(t, a.store.write(g, "private-key", key))
	require.NoError(t, a.store.write(g, "certificate", cert))
	require.NoError(t, a.store.save("pending", g))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	stopped := false
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(20 * time.Second):
			t.Error("traffic runtime did not stop")
		}
	})
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "add-br", "br-test"))
	// Follow the production startup gate: the node reserves and publishes its
	// protection lease before OVN creates an encrypted tunnel. A fixed reqid
	// inserted concurrently with bootstrap races the ownership conflict check.
	var runErr error
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		select {
		case err := <-done:
			stopped = true
			runErr = err
		default:
		}
		require.False(collect, stopped, "traffic agent exited before readiness: %v", runErr)
		status := a.Status()
		require.True(collect, status.ProtectionArmed && status.RuntimeHealthy && status.ConfigurationApplied, "agent status: %+v", status)
	}, 60*time.Second, 200*time.Millisecond)
	a.protectionMu.Lock()
	lease := a.protection.owner.reservation
	a.protectionMu.Unlock()
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "add-port", "br-test", "ovn-peer", "--", "set", "Interface", "ovn-peer", "type="+tunnel, "options:local_ip="+local, "options:remote_ip="+peer, "options:remote_name=traffic-chassis-"+peerNode, "options:ipsec_reqid="+strconv.FormatUint(uint64(lease.Reqid), 10), "options:ipsec_mark_out="+strconv.FormatUint(uint64(lease.Mark), 10)+"/0xffffffff", "options:egress_pkt_mark="+strconv.FormatUint(uint64(lease.Mark), 10), "--", "set", "Port", "ovn-peer", "external_ids:ovn-chassis-id=traffic-chassis-"+peerNode))
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "/usr/sbin/ipsec", "statusall").Output()
		require.NoError(collect, err)
		// statusall contains public identities and selectors, never SA keys.
		require.True(collect, bytes.Contains(output, []byte("ROUTED")), "agent status: %+v; IKE status: %s", a.Status(), output)
	}, 60*time.Second, 200*time.Millisecond, "the real monitor must install transport trap policies")
	socket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(local), Port: port})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, socket.Close()) })
	// Set the production output mark before connect(): the separate synthetic
	// guard deliberately blocks an unmarked socket even before its first send.
	dialer := &net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var markErr error
		if err := raw.Control(func(fd uintptr) {
			markErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(lease.Mark))
		}); err != nil {
			return err
		}
		return markErr
	}}
	sender, err := dialer.DialContext(t.Context(), "udp", net.JoinHostPort(peer, strconv.Itoa(port)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sender.Close()) })
	payload := "candidate-IPsec-node-" + node
	want := "candidate-IPsec-node-" + peerNode
	buffer := make([]byte, 256)
	require.Eventually(t, func() bool {
		_, err := sender.Write([]byte(payload))
		if err != nil {
			return false
		}
		if err := socket.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
			return false
		}
		n, _, err := socket.ReadFromUDP(buffer)
		return err == nil && string(buffer[:n]) == want
	}, 60*time.Second, 200*time.Millisecond, "both minimal-capability runtimes must exchange the synthetic payload")
	stateCount := func() int {
		t.Helper()
		states, err := netlink.XfrmStateList(netlink.FAMILY_ALL)
		require.NoError(t, err)
		count := 0
		for _, state := range states {
			// Never log or serialize the netlink state: it includes SA keys.
			if state.Reqid == int(lease.Reqid) {
				require.Equal(t, netlink.XFRM_PROTO_ESP, state.Proto)
				require.Equal(t, netlink.XFRM_MODE_TRANSPORT, state.Mode)
				require.True(t, state.Src.Equal(net.ParseIP(local)) && state.Dst.Equal(net.ParseIP(peer)) || state.Src.Equal(net.ParseIP(peer)) && state.Dst.Equal(net.ParseIP(local)))
				count++
			}
		}
		return count
	}
	require.GreaterOrEqual(t, stateCount(), 2, "the real monitor must preserve an explicit connection reqid in both directions")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		paths, err := filepath.Glob(filepath.Join(a.store.dir, "connections", "*", "sa-ledger.json"))
		require.NoError(collect, err)
		observed := 0
		for _, path := range paths {
			data, err := readRegularFile(path)
			require.NoError(collect, err)
			var ledger saLedger
			require.NoError(collect, json.Unmarshal(data, &ledger))
			require.Equal(collect, lease.NodeUID, ledger.NodeUID)
			require.Equal(collect, lease.Lease, ledger.Lease)
			require.Equal(collect, lease.Mark, ledger.Mark)
			require.Equal(collect, lease.Reqid, ledger.Reqid)
			require.NotEmpty(collect, ledger.BootID)
			for _, binding := range ledger.Bindings {
				require.Equal(collect, lease.Reqid, binding.Instance.Reqid)
				require.NotZero(collect, binding.Instance.SPI)
				require.NotZero(collect, binding.Instance.Added)
				observed++
			}
		}
		require.GreaterOrEqual(collect, observed, 2, "the actual IKE SPIs must bind to durable kernel instance evidence")
	}, 30*time.Second, 200*time.Millisecond)
	require.NoError(t, os.WriteFile("/fixtures/traffic-complete", nil, 0o600))
	// Keep both peers alive until the harness has collected both outcomes.
	for {
		if _, err := os.Stat("/fixtures/finish"); err == nil {
			break
		}
		_, err := sender.Write([]byte(payload))
		require.NoError(t, err)
		select {
		case <-t.Context().Done():
			t.Fatal("traffic harness did not release the peer")
		case <-time.After(200 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		stopped = true
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("traffic runtime did not stop for the guard test")
	}
	assertGuard()
	require.Zero(t, stateCount(), "graceful shutdown must remove the synthetic reqid's SAs while preserving the separate guard")
	observations, err := filepath.Glob(filepath.Join(a.store.dir, "connections", "*", "drain.json"))
	require.NoError(t, err)
	require.NotEmpty(t, observations, "real IKE shutdown must confirm drain with both production guards still armed")
	for _, path := range observations {
		data, err := readRegularFile(path)
		require.NoError(t, err)
		var observation drainObservation
		require.NoError(t, json.Unmarshal(data, &observation))
		require.Equal(t, lease.NodeUID, observation.NodeUID)
		require.Equal(t, lease.Lease, observation.Lease)
		require.False(t, observation.Observed.IsZero())
	}
	probeBlocked()
	require.NoError(t, os.WriteFile("/fixtures/guard-complete", nil, 0o600))
	for {
		if _, err := os.Stat("/fixtures/release"); err == nil {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal("traffic harness did not release the guard test")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
