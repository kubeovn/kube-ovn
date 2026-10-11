package ipsec

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func TestCandidateInitialPreparation(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_RUNTIME_TEST") != "true" {
		t.Skip("requires the isolated candidate-image runtime harness")
	}
	cert, key, trust := testIdentity(t, "runtime-test-chassis")
	state := Coordination{
		Version: 1, Generation: "initial", Epoch: "prepare", Phase: PreparePhase, DaemonSetUID: "ds",
		TemplateHash: digest([]byte("template")), TrustHash: digest(trust), Targets: map[string]string{"node": "uid"},
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	client := fake.NewClientset(&corev1.ConfigMap{Name: CoordinationConfigMap, Namespace: "kube-system", Data: map[string]string{"state": string(data)}}, &corev1.Node{Name: "node", UID: "uid"})
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o750))
	a, err := New(Configuration{
		NodeName: "node", Namespace: "kube-system", PodUID: "pod", Kube: client,
		KeyDir: t.TempDir(), RuntimeDir: t.TempDir(), ProtectionDir: dir, OVSSocket: "/run/openvswitch/db.sock", Duration: time.Hour, RequestTimeout: time.Second,
	})
	require.NoError(t, err)
	lock, err := a.store.lock()
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, a.serveProtection(ctx))
	g := &generation{ID: digest(key), NodeUID: "uid", Chassis: "runtime-test-chassis"}
	require.NoError(t, a.store.write(g, "private-key", key))
	require.NoError(t, a.store.save("pending", g))
	issued := false
	client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		_, err := os.Stat(filepath.Join(dir, "required"))
		require.ErrorIs(t, err, os.ErrNotExist, "first issuance must precede durable protection intent")
		row, err := a.ovs.IPsecDatapathConfiguration()
		require.NoError(t, err)
		require.Empty(t, row.ExternalIDs["ovn-ipsec-protection-mark"], "first issuance must keep the signing network available")
		req := action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
		req.UID = "csr-uid"
		req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
		req.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-uid": {"pod"}}
		req.Status.Certificate = cert
		issued = true
		return true, req, nil
	})
	secrets := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, secrets.Add(&corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": trust}}))
	require.NoError(t, a.reconcile(t.Context(), corelisters.NewSecretLister(secrets)))
	defer a.ovs.Close()
	require.True(t, issued)
	require.Equal(t, "Prepared", a.Status().Phase)
	require.False(t, a.runtime.enabled.Load())
	row, err := a.ovs.IPsecDatapathConfiguration()
	require.NoError(t, err)
	require.NoError(t, CheckStartup(t.Context(), dir, row.UUID), "initial preparation must let OVS and cert-manager communicate")
	require.Error(t, CheckProtection(t.Context(), dir, row.UUID), "Prepare must not claim that guards exist")
	state.Phase, state.Epoch = ArmPhase, "arm"
	data, err = json.Marshal(state)
	require.NoError(t, err)
	cm, err := client.CoreV1().ConfigMaps("kube-system").Get(t.Context(), CoordinationConfigMap, metav1.GetOptions{})
	require.NoError(t, err)
	cm.Data["state"] = string(data)
	_, err = client.CoreV1().ConfigMaps("kube-system").Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Error(t, CheckStartup(t.Context(), dir, row.UUID), "Arm must stop accepting unprotected startup")
}
