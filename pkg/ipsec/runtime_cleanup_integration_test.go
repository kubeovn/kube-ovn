package ipsec

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func TestCandidateCleanup(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_RUNTIME_TEST") != "true" {
		t.Skip("requires the isolated candidate-image runtime harness")
	}
	// This separate container intentionally has no SYS_NICE. Cleanup must not
	// depend on either a signer, trust bundle, or an IKE/monitor runtime.
	kernel, err := netlink.NewHandle(unix.NETLINK_XFRM)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, kernel.Close()) })
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o750))
	client := fake.NewClientset(&corev1.Node{Name: "cleanup-node", UID: "cleanup-node-uid"})
	config := Configuration{
		NodeName: "cleanup-node", PodUID: "cleanup-pod-uid", Namespace: "kube-system", Kube: client,
		KeyDir: t.TempDir(), RuntimeDir: t.TempDir(), ProtectionDir: dir,
		OVSSocket: "/run/openvswitch/db.sock", Duration: time.Hour, RequestTimeout: time.Second, CleanupOnly: true,
	}
	client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		req := action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
		req.UID = types.UID(digest([]byte(req.Name)))
		req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
		req.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cleanup-cni"}, "authentication.kubernetes.io/pod-uid": {config.PodUID}}
		err := client.Tracker().Create(certv1.SchemeGroupVersion.WithResource("certificatesigningrequests"), req, "")
		return true, req, err
	})
	start := func(config Configuration) (*Agent, func(), <-chan struct{}) {
		t.Helper()
		a, err := New(config)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		exited := make(chan struct{})
		go func() { done <- a.Run(ctx); close(exited) }()
		stop := sync.OnceFunc(func() {
			t.Helper()
			cancel()
			select {
			case err := <-done:
				require.True(t, err == nil || errors.Is(err, context.Canceled), "unexpected cleanup result: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("cleanup owner did not stop")
			}
		})
		t.Cleanup(stop)
		return a, stop, exited
	}
	a, stop, exited := start(config)
	require.Eventually(t, func() bool { return a.Status().Phase == "CleanupIdle" }, 10*time.Second, 100*time.Millisecond)
	// A fresh disabled init exits successfully instead of serving forever.
	require.Eventually(t, func() bool {
		select {
		case <-exited:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Error(t, Check(t.Context(), config.RuntimeDir, "readyz"), "idle cleanup is not an encryption readiness receipt")
	for _, path := range []string{filepath.Join(config.KeyDir, "protection.json"), filepath.Join(dir, "required"), filepath.Join(dir, "protection.sock")} {
		_, err := os.Lstat(path)
		require.True(t, os.IsNotExist(err), "fresh disabled installation must not acquire protection")
	}
	stop()

	storage := store{dir: config.KeyDir}
	lock, err := storage.lock()
	require.NoError(t, err)
	owner, err := prepareProtection(storage, "cleanup-node-uid", kernel)
	require.NoError(t, err)
	require.NoError(t, owner.arm())
	reservation := owner.reservation
	keyPEM, err := newPrivateKey()
	require.NoError(t, err)
	source := &generation{ID: digest(keyPEM), NodeName: config.NodeName, Namespace: config.Namespace, NodeUID: reservation.NodeUID, Chassis: "fixture-chassis"}
	database, err := ovs.NewCNIVswitchClient("unix:" + config.OVSSocket)
	require.NoError(t, err)
	row, err := database.IPsecConfiguration()
	require.NoError(t, err)
	source.Chassis = row.ExternalIDs["system-id"]
	require.NoError(t, storage.write(source, "private-key", keyPEM))
	require.NoError(t, storage.write(source, "certificate", []byte("expired owned identity")))
	g, err := storage.prepareGeneration(source, []byte("committed public trust"))
	require.NoError(t, err)
	identityPaths := map[string]string{"certificate": storage.path(g, "certificate"), "private_key": storage.path(g, "private-key"), "ca_cert": storage.path(g, "ca-bundle")}
	require.NoError(t, database.SetIPsecConfiguration(row.UUID, identityPaths))
	database.Close()
	require.NoError(t, lock.Close())
	state := Coordination{
		Version: 1, Generation: "cleanup-generation", Epoch: "cleanup-epoch", Phase: CleanupPhase,
		DaemonSetUID: "cleanup-daemonset", TemplateHash: strings.Repeat("a", 64), TrustHash: strings.Repeat("b", 64),
		Targets: map[string]string{"cleanup-node": "cleanup-node-uid"}, NBGlobalUUID: uuid.New().String(), SBGlobalUUID: uuid.New().String(),
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = client.CoreV1().ConfigMaps(config.Namespace).Create(t.Context(), &corev1.ConfigMap{Name: CoordinationConfigMap, Data: map[string]string{"state": string(data)}}, metav1.CreateOptions{})
	require.NoError(t, err)
	config.PodUID = "replacement-cleanup-pod"
	a, stop, exited = start(config)
	require.Eventually(t, func() bool { return a.Status().Phase == "CleanupDrained" }, 10*time.Second, 100*time.Millisecond)
	require.True(t, a.Status().ProtectionArmed)
	require.False(t, a.runtime.enabled.Load())
	require.NoError(t, checkIKEPorts())
	row, err = a.ovs.IPsecDatapathConfiguration()
	require.NoError(t, err)
	for key, path := range identityPaths {
		require.Empty(t, row.OtherConfig[key], "guarded cleanup must remove its owned database reference")
		_, err := os.Stat(path)
		require.NoError(t, err, "this stage must preserve private evidence and committed files")
	}
	require.NoError(t, CheckProtection(t.Context(), dir, row.UUID), "the disabled helper must unblock protected OVS startup")
	actual, err := storage.loadProtection("cleanup-node-uid")
	require.NoError(t, err)
	require.Equal(t, reservation, *actual, "cleanup must reuse its existing durable lease")
	for _, action := range client.Actions() {
		require.NotEqual(t, "secrets", action.GetResource().Resource, "cleanup cannot access a CA")
	}
	requests, err := client.CertificatesV1().CertificateSigningRequests().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, requests.Items, 1)
	require.Equal(t, CleanupSignerName, requests.Items[0].Spec.SignerName)
	require.Empty(t, requests.Items[0].Status.Certificate)
	node, err := client.CoreV1().Nodes().Get(t.Context(), config.NodeName, metav1.GetOptions{})
	require.NoError(t, err)
	keyPEM, err = storage.cleanupKey(reservation)
	require.NoError(t, err)
	key, err := privateKey(keyPEM)
	require.NoError(t, err)
	receipt, err := VerifyCleanupReceipt([]byte(node.Annotations[CleanupReceiptAnnotation]), &key.PublicKey, &state, config.NodeName, reservation.NodeUID, time.Now())
	require.NoError(t, err, "the real guarded preflight must produce a signed current-Pod observation")
	require.Equal(t, config.PodUID, receipt.PodUID)
	// A changed frozen target revokes the preflight without withdrawing guards.
	state.Targets["cleanup-node"] = "replaced-node-uid"
	data, err = json.Marshal(state)
	require.NoError(t, err)
	cm, err := client.CoreV1().ConfigMaps(config.Namespace).Get(t.Context(), CoordinationConfigMap, metav1.GetOptions{})
	require.NoError(t, err)
	cm.Data["state"] = string(data)
	_, err = client.CoreV1().ConfigMaps(config.Namespace).Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return a.Status().Phase == "CleanupBlocked" }, 5*time.Second, 100*time.Millisecond)
	require.NoError(t, CheckProtection(t.Context(), dir, row.UUID))
	stop()
	require.NoError(t, owner.verify())
	_, err = os.Stat(filepath.Join(dir, "required"))
	require.NoError(t, err, "cleanup preflight shutdown must retain required intent")

	// The real Release challenge has a new epoch. Publish its final receipt
	// after guard withdrawal; the finite init exits and can restart in Disabled.
	state.Targets[config.NodeName] = reservation.NodeUID
	state.Phase, state.Epoch = ReleasePhase, "new-release-epoch"
	data, err = json.Marshal(state)
	require.NoError(t, err)
	cm.Data["state"] = string(data)
	_, err = client.CoreV1().ConfigMaps(config.Namespace).Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	a, stop, exited = start(config)
	require.Eventually(t, func() bool { return a.Status().Phase == "Disabled" && a.Status().Reason == "" }, 10*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		current, err := client.CoreV1().Nodes().Get(t.Context(), config.NodeName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		_, err = VerifyCleanupReceipt([]byte(current.Annotations[ReleaseReceiptAnnotation]), &key.PublicKey, &state, config.NodeName, reservation.NodeUID, time.Now())
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-exited:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "successful release must exit without waiting for other nodes")
	actual, err = storage.loadProtection(reservation.NodeUID)
	require.NoError(t, err)
	require.NotNil(t, actual)
	require.False(t, actual.Required)
	require.Zero(t, actual.Indexes)
	require.FileExists(t, identityPaths["private_key"], "released ownership must retain private history")
	_, err = os.Stat(filepath.Join(dir, "required"))
	require.ErrorIs(t, err, os.ErrNotExist)
	stop()
	state.Phase, state.Epoch = DisabledPhase, "disabled-epoch"
	state.NBGlobalUUID, state.SBGlobalUUID = "", ""
	data, err = json.Marshal(state)
	require.NoError(t, err)
	cm.Data["state"] = string(data)
	_, err = client.CoreV1().ConfigMaps(config.Namespace).Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	a, stop, exited = start(config)
	require.Eventually(t, func() bool { return a.Status().Phase == "Disabled" && a.Status().Reason == "" }, 5*time.Second, 100*time.Millisecond)
	require.False(t, a.Status().ProtectionArmed)
	stop()

	// Reset only this test's OVS fixture before the separate runtime tests.
	require.NoError(t, command(t.Context(), "ovs-vsctl", "--timeout=5", "--no-wait", "remove", "Open_vSwitch", ".", "external_ids",
		"ovn-ipsec-protection-node-uid", "ovn-ipsec-protection-lease", "ovn-ipsec-protection-mark", "ovn-ipsec-protection-reqid"))
}

func TestCandidateCancelledPreparation(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_RUNTIME_TEST") != "true" {
		t.Skip("requires the isolated candidate-image runtime harness")
	}
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			state := Coordination{
				Version: 1, Generation: "cancelled-prepare", Epoch: "release", Phase: ReleasePhase,
				DaemonSetUID: "ds", TemplateHash: strings.Repeat("a", 64), TrustHash: strings.Repeat("b", 64),
				Targets: map[string]string{"node": "uid"}, NBGlobalUUID: uuid.New().String(), SBGlobalUUID: uuid.New().String(),
			}
			data, err := json.Marshal(state)
			require.NoError(t, err)
			client := fake.NewClientset(&corev1.Node{Name: "node", UID: "uid"},
				&corev1.ConfigMap{Name: CoordinationConfigMap, Namespace: "kube-system", Data: map[string]string{"state": string(data)}})
			client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
				req := action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
				req.UID = types.UID(digest([]byte(req.Name)))
				req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
				req.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cni"}, "authentication.kubernetes.io/pod-uid": {"pod"}}
				err := client.Tracker().Create(certv1.SchemeGroupVersion.WithResource("certificatesigningrequests"), req, "")
				return true, req, err
			})
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0o750))
			a, err := New(Configuration{
				NodeName: "node", Namespace: "kube-system", PodUID: "pod", Kube: client,
				KeyDir: t.TempDir(), RuntimeDir: t.TempDir(), ProtectionDir: dir, OVSSocket: "/run/openvswitch/db.sock",
				Duration: time.Hour, RequestTimeout: time.Second, CleanupOnly: true,
			})
			require.NoError(t, err)
			if pending {
				lock, err := a.store.lock()
				require.NoError(t, err)
				require.NoError(t, a.reservePreparation("uid"))
				require.NoError(t, a.store.save("pending", &generation{ID: strings.Repeat("c", 64), NodeUID: "uid", Chassis: "runtime-test-chassis"}))
				reservation, err := a.store.loadProtection("uid")
				require.NoError(t, err)
				require.False(t, reservation.Required)
				require.False(t, reservation.Released)
				require.Equal(t, [2]int{}, reservation.Indexes)
				require.NoError(t, lock.Close())
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			require.NoError(t, a.Run(ctx), "cancelled Prepare must release without a certificate or a resident helper")
			require.Equal(t, "Disabled", a.Status().Phase)
			require.False(t, a.runtime.enabled.Load())
			reservation, err := a.store.loadProtection("uid")
			require.NoError(t, err)
			require.False(t, reservation.Required)
			require.True(t, reservation.Released)
			node, err := client.CoreV1().Nodes().Get(t.Context(), "node", metav1.GetOptions{})
			require.NoError(t, err)
			require.NotEmpty(t, node.Annotations[ReleaseReceiptAnnotation])
			_, err = os.Lstat(filepath.Join(dir, "required"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
