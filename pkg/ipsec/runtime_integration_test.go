package ipsec

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCandidateWithoutProtection(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_UNSUPPORTED_TEST") != "true" {
		t.Skip("requires the isolated unpatched-image fixture")
	}
	client := fake.NewClientset()
	a, err := New(Configuration{
		NodeName: "unsupported-node", PodUID: "unsupported-pod", Namespace: "kube-system", Kube: client,
		KeyDir: t.TempDir(), RuntimeDir: t.TempDir(), OVSSocket: "/tmp/missing-ovs/db.sock", Duration: time.Hour, RequestTimeout: time.Second,
	})
	require.NoError(t, err)
	require.ErrorContains(t, a.Run(t.Context()), "does not support IPsec output protection")
	require.Empty(t, client.Actions(), "unsupported OVN must be rejected before signing or reading cluster identity")
	require.False(t, a.runtime.enabled.Load())
	_, err = os.Stat(filepath.Join(a.config.KeyDir, "protection.json"))
	require.True(t, os.IsNotExist(err), "unsupported OVN must not arm a new protection lease")
}

// TestCandidateRuntime exercises the actual image programs and host kernel.
// Kubernetes is simulated here; cluster rollout and encrypted traffic require
// separate E2E coverage. Run only in the isolated containers created by the
// runtime harness, never in an operator's host network namespace.
func TestCandidateRuntime(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_RUNTIME_TEST") != "true" {
		t.Skip("requires the isolated candidate-image runtime harness")
	}
	require.NoError(t, checkIKEPorts(), "the test network namespace must be isolated")
	require.NoError(t, command(t.Context(), "ip", "xfrm", "policy", "add", "src", "192.0.2.10", "dst", "192.0.2.20", "dir", "out", "priority", "99", "index", "759809", "action", "block"))
	foreignPolicy := func() []byte {
		t.Helper()
		// testing cancels t.Context before running cleanup; the post-shutdown
		// kernel check needs its own bounded context to remain meaningful.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "ip", "xfrm", "policy", "get", "index", "759809", "dir", "out").Output()
		require.NoError(t, err, "the runtime must preserve an unrelated kernel policy")
		return output
	}
	foreign := foreignPolicy()
	// An unrelated kernel SA must survive startup, crash recovery, offline
	// recovery and shutdown. Its synthetic key is never logged or serialized.
	foreignKey := make([]byte, 20)
	_, err := rand.Read(foreignKey)
	require.NoError(t, err)
	foreignSA := &netlink.XfrmState{
		Src: net.ParseIP("192.0.2.10"), Dst: net.ParseIP("192.0.2.20"),
		Proto: netlink.XFRM_PROTO_ESP, Mode: netlink.XFRM_MODE_TRANSPORT,
		Spi: 0x759801, Reqid: 759821, ReplayWindow: 32,
		Aead: &netlink.XfrmStateAlgo{Name: "rfc4106(gcm(aes))", Key: foreignKey, ICVLen: 128},
	}
	require.NoError(t, netlink.XfrmStateAdd(foreignSA))
	checkForeignSA := func() {
		t.Helper()
		actual, err := netlink.XfrmStateGet(&netlink.XfrmState{Src: foreignSA.Src, Dst: foreignSA.Dst, Proto: foreignSA.Proto, Spi: foreignSA.Spi})
		require.NoError(t, err, "the runtime must preserve an unrelated ESP state")
		require.Equal(t, foreignSA.Reqid, actual.Reqid)
		require.Equal(t, foreignSA.Mode, actual.Mode)
		require.NotNil(t, actual.Aead)
		require.Equal(t, foreignSA.Aead.Name, actual.Aead.Name)
		require.True(t, bytes.Equal(foreignKey, actual.Aead.Key), "the unrelated SA key must remain unchanged")
	}
	checkForeignSA()
	t.Cleanup(func() {
		require.Equal(t, foreign, foreignPolicy())
		checkForeignSA()
	})
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "add-br", "br-fixture"))
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "add-port", "br-fixture", "unrelated-ipsec", "--", "set", "Interface", "unrelated-ipsec", "type=geneve", "options:remote_ip=198.51.100.77", "options:remote_name=external-peer"))
	cert, key, trust := testIdentity(t, "runtime-test-chassis")
	state, err := json.Marshal(Coordination{Version: 1, Generation: "runtime", Epoch: "runtime", Phase: EnabledPhase, DaemonSetUID: "runtime-ds", TemplateHash: digest([]byte("template")), TrustHash: digest(trust), Targets: map[string]string{"runtime-node": "runtime-node-uid"}})
	require.NoError(t, err)
	client := fake.NewClientset(
		&corev1.ConfigMap{Name: CoordinationConfigMap, Namespace: "kube-system", Data: map[string]string{"state": string(state)}},
		&corev1.Node{Name: "runtime-node", UID: "runtime-node-uid"},
	)
	// Model the API-authenticated CSR binding used by periodic receipts even
	// when activation reuses the already signed pending certificate.
	client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		req := action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
		req.UID = "runtime-csr-uid"
		req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
		req.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"runtime-cni"}, "authentication.kubernetes.io/pod-uid": {"runtime-pod-uid"}}
		req.Status.Certificate = cert
		err := client.Tracker().Create(certv1.SchemeGroupVersion.WithResource("certificatesigningrequests"), req, "")
		return true, req, err
	})
	secrets := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, secrets.Add(&corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": trust}}))
	// TempDir's child directory is created with 0777 before umask. Match the
	// hostpath initializer explicitly, without weakening the endpoint check.
	protectionDir := t.TempDir()
	require.NoError(t, os.Chmod(protectionDir, 0o750))
	a, err := New(Configuration{
		NodeName: "runtime-node", PodUID: "runtime-pod-uid", Namespace: "kube-system", Kube: client,
		KeyDir: t.TempDir(), RuntimeDir: t.TempDir(), ProtectionDir: protectionDir, OVSSocket: "/run/openvswitch/db.sock", Duration: time.Hour, RequestTimeout: 30 * time.Second, Priority: -5,
	})
	require.NoError(t, err)
	g := &generation{ID: digest(key), NodeUID: "runtime-node-uid", Chassis: "runtime-test-chassis"}
	require.NoError(t, a.store.write(g, "private-key", key))
	require.NoError(t, a.store.write(g, "certificate", cert))
	require.NoError(t, a.store.save("pending", g))
	before, err := exec.CommandContext(t.Context(), "ovs-vsctl", "--format=json", "--columns=other_config", "list", "Open_vSwitch").Output()
	require.NoError(t, err)
	foreignIKE, err := net.ListenPacket("udp4", ":500")
	require.NoError(t, err)
	conflictCtx, conflictCancel := context.WithTimeout(t.Context(), 3*time.Second)
	err = a.Run(conflictCtx)
	conflictCancel()
	require.NoError(t, foreignIKE.Close())
	require.ErrorContains(t, err, "IKE port 500", "an occupied IKE port must reject ownership before reconciliation")
	after, err := exec.CommandContext(t.Context(), "ovs-vsctl", "--format=json", "--columns=other_config", "list", "Open_vSwitch").Output()
	require.NoError(t, err)
	require.Equal(t, before, after, "an unrelated IKE owner must not trigger shared OVSDB configuration changes")
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "set", "Open_vSwitch", ".", "external_ids:ovn-enable-flow-based-tunnels=true"))
	require.ErrorContains(t, a.reconcile(t.Context(), corelisters.NewSecretLister(secrets)), "does not support flow-based tunnels")
	require.Empty(t, client.Actions(), "an unsupported datapath must be rejected before reading Node identity or issuing a CSR")
	after, err = exec.CommandContext(t.Context(), "ovs-vsctl", "--format=json", "--columns=other_config", "list", "Open_vSwitch").Output()
	require.NoError(t, err)
	require.Equal(t, before, after, "unsupported activation must preserve the active OVSDB identity paths")
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "remove", "Open_vSwitch", ".", "external_ids", "ovn-enable-flow-based-tunnels"))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(20 * time.Second):
			t.Error("owned runtime did not stop")
		}
		require.Eventually(t, func() bool { return checkIKEPorts() == nil }, 10*time.Second, 100*time.Millisecond,
			"charon must release the IKE ports after shutdown")
	})
	ready := func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		return Check(ctx, a.config.RuntimeDir, "readyz") == nil
	}
	ovsUUID, err := exec.CommandContext(t.Context(), "ovs-vsctl", "get", "Open_vSwitch", ".", "_uuid").Output()
	require.NoError(t, err)
	protected := func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		return CheckProtection(ctx, a.config.ProtectionDir, strings.TrimSpace(string(ovsUUID))) == nil
	}
	require.Eventually(t, protected, 10*time.Second, 100*time.Millisecond, "protection must bootstrap before trust, certificate activation, or chassis registration")
	require.False(t, ready())
	require.False(t, a.runtime.enabled.Load())
	currentBeforeTrust, err := a.store.load("current")
	require.NoError(t, err)
	require.Nil(t, currentBeforeTrust)
	for _, action := range client.Actions() {
		require.NotEqual(t, "certificatesigningrequests", action.GetResource().Resource, "bootstrap must not depend on certificate signing")
	}
	require.Error(t, CheckProtection(t.Context(), a.config.ProtectionDir, "00000000-0000-0000-0000-000000000000"), "a stale OVS database UUID cannot pass the startup gate")
	_, err = client.CoreV1().Secrets("kube-system").Create(t.Context(), &corev1.Secret{Name: util.DefaultOVNIPSecCA, Data: map[string][]byte{"cacert": trust}}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, ready, 60*time.Second, 200*time.Millisecond, "candidate monitor and strongSwan must become ready")
	require.True(t, a.Status().ProtectionArmed)
	reservation, err := a.store.loadProtection("runtime-node-uid")
	require.NoError(t, err)
	require.NotNil(t, reservation)
	require.True(t, reservation.Required)
	// Removing a guard in this disposable namespace must revoke readiness.
	// The recovery loop repairs it with the same persistent lease.
	fixtureOwner := &protectionOwner{reservation: *reservation}
	require.NoError(t, netlink.XfrmPolicyDel(fixtureOwner.guard(0, reservation.Indexes[0])))
	require.False(t, a.Status().ProtectionArmed)
	require.False(t, ready())
	require.False(t, protected(), "a live endpoint must reject a missing guard instead of reusing a prior receipt")
	require.Eventually(t, ready, 45*time.Second, 200*time.Millisecond, "reconcile must restore a missing guard before restoring readiness")
	checkTrust := func() {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "/usr/sbin/ipsec", "listcacerts").Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "CN=test CA", "readiness must follow loading the configured trust")
	}
	checkTrust()
	checkForeignSA()
	require.Eventually(t, func() bool {
		config, err := os.ReadFile("/etc/ipsec.conf")
		return err == nil && strings.Contains(string(config), "ca ca_auth") && !strings.Contains(string(config), "unrelated-ipsec")
	}, 10*time.Second, 200*time.Millisecond, "the monitor must recognize the certificate and leave unowned IPsec interfaces out of its configuration")
	pidBytes, err := os.ReadFile(filepath.Join(a.config.RuntimeDir, "monitor.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, err)
	priority, err := unix.Getpriority(unix.PRIO_PROCESS, pid)
	require.NoError(t, err)
	// Linux's raw getpriority syscall returns 20 minus the nice value.
	require.Equal(t, -5, 20-priority, "SYS_NICE must be effective in the minimal-capability IPsec container")
	charonPIDBytes, err := os.ReadFile("/run/charon.pid")
	require.NoError(t, err)
	charonPID, err := strconv.Atoi(strings.TrimSpace(string(charonPIDBytes)))
	require.NoError(t, err)
	charonPriority, err := unix.Getpriority(unix.PRIO_PROCESS, charonPID)
	require.NoError(t, err)
	require.Equal(t, -5, 20-charonPriority, "charon must inherit the IPsec priority")
	status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	require.NoError(t, err)
	var effective uint64
	for line := range strings.SplitSeq(string(status), "\n") {
		if value, found := strings.CutPrefix(line, "CapEff:"); found {
			effective, err = strconv.ParseUint(strings.TrimSpace(value), 16, 64)
			require.NoError(t, err)
		}
	}
	for _, capability := range []uint{unix.CAP_NET_ADMIN, unix.CAP_NET_BIND_SERVICE, unix.CAP_SYS_NICE} {
		require.NotZero(t, effective&(uint64(1)<<capability), "the nice launcher must preserve runtime capabilities")
	}
	connectionIntent := func(pid int) (string, []byte) {
		t.Helper()
		cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
		require.NoError(t, err)
		var path string
		for argument := range strings.SplitSeq(string(cmdline), "\x00") {
			if value, found := strings.CutPrefix(argument, "--connection-intent="); found {
				path = value
			}
		}
		require.NotEmpty(t, path, "the actual monitor must use a persistent connection namespace")
		data, err := readRegularFile(path)
		require.NoError(t, err)
		var actual struct {
			connectionSession
			Connections map[string]any `json:"connections"`
		}
		require.NoError(t, json.Unmarshal(data, &actual))
		require.Equal(t, reservation.NodeUID, actual.NodeUID)
		require.Equal(t, reservation.Lease, actual.Lease)
		require.Equal(t, reservation.Mark, actual.Mark)
		require.Equal(t, reservation.Reqid, actual.Reqid)
		require.NotEmpty(t, actual.Prefix)
		require.NotNil(t, actual.Connections)
		return path, data
	}
	previousIntentPath, previousIntent := connectionIntent(pid)
	require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
	require.Eventually(t, func() bool { return !a.runtime.healthy.Load() }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, ready, 60*time.Second, 200*time.Millisecond, "the runtime must recover after a monitor crash")
	newPID, err := os.ReadFile(filepath.Join(a.config.RuntimeDir, "monitor.pid"))
	require.NoError(t, err)
	require.NotEqual(t, string(pidBytes), string(newPID))
	newMonitorPID, err := strconv.Atoi(strings.TrimSpace(string(newPID)))
	require.NoError(t, err)
	newIntentPath, _ := connectionIntent(newMonitorPID)
	require.NotEqual(t, previousIntentPath, newIntentPath, "a monitor crash must create a new namespace")
	retainedIntent, err := readRegularFile(previousIntentPath)
	require.NoError(t, err)
	require.Equal(t, previousIntent, retainedIntent, "restart must retain previous connection evidence")
	checkTrust()
	checkForeignSA()
	current, err := a.store.load("current")
	require.NoError(t, err)
	require.Equal(t, a.config.NodeName, current.NodeName)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("the first runtime did not stop before offline recovery")
	}
	offline := fake.NewClientset()
	offline.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	config := a.config
	config.Kube, config.PodUID = offline, "replacement-pod-uid"
	a, err = New(config)
	require.NoError(t, err)
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "set", "Open_vSwitch", ".", "external_ids:ovn-enable-flow-based-tunnels=true"))
	require.ErrorContains(t, a.restoreCurrent(t.Context()), "does not support flow-based tunnels", "offline recovery must not bypass datapath validation")
	require.False(t, a.runtime.enabled.Load())
	require.Empty(t, offline.Actions())
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "remove", "Open_vSwitch", ".", "external_ids", "ovn-enable-flow-based-tunnels"))
	offlineCtx, offlineCancel := context.WithCancel(t.Context())
	t.Cleanup(offlineCancel)
	done = make(chan error, 1)
	go func() { done <- a.Run(offlineCtx) }()
	require.Eventually(t, ready, 30*time.Second, 200*time.Millisecond, "a valid committed identity must restore without API trust synchronization")
	require.Equal(t, "Restored", a.Status().Phase)
	require.Equal(t, current.ID, a.Status().Generation)
	restored := a.Status()
	require.Equal(t, current.NodeUID, restored.NodeUID)
	require.Equal(t, current.Chassis, restored.Chassis)
	require.Equal(t, digest(cert), restored.CertificateHash)
	require.Equal(t, digest(trust), restored.TrustHash)
	require.True(t, restored.ConfigurationApplied)
	require.True(t, restored.RuntimeHealthy)
	require.True(t, restored.ProtectionArmed)
	restoredReservation, err := a.store.loadProtection(current.NodeUID)
	require.NoError(t, err)
	require.Equal(t, reservation.Lease, restoredReservation.Lease, "offline recovery must retain the same Node UID lease")
	require.Equal(t, reservation.Mark, restoredReservation.Mark)
	require.Equal(t, reservation.Reqid, restoredReservation.Reqid)
	checkTrust()
	require.Equal(t, foreign, foreignPolicy())
	checkForeignSA()
	for _, action := range offline.Actions() {
		require.Equal(t, "secrets", action.GetResource().Resource, "offline recovery must not issue requests or infer a fresh Node identity")
	}
}
