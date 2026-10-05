package ipsec

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestInitialPreparationPreservesSigningNetwork(t *testing.T) {
	state := Coordination{
		Version: 1, Generation: "initial", Epoch: "prepare", Phase: PreparePhase,
		DaemonSetUID: "ds", TemplateHash: strings.Repeat("a", 64), TrustHash: strings.Repeat("b", 64), Targets: map[string]string{"node": "uid"},
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	client := fake.NewClientset(&corev1.ConfigMap{Name: CoordinationConfigMap, Namespace: "kube-system", Data: map[string]string{"state": string(data)}})
	a := &Agent{config: Configuration{NodeName: "node", Namespace: "kube-system", Kube: client, ProtectionDir: t.TempDir()}, store: store{dir: t.TempDir()}, runtime: &runtimeManager{}}
	preparing, err := a.initialPreparation(t.Context(), "uid", nil)
	require.NoError(t, err)
	require.True(t, preparing)
	_, err = a.initialPreparation(t.Context(), "replacement", nil)
	require.ErrorContains(t, err, "Node UID")
	cert, key, trust := testIdentity(t, "chassis")
	source := &generation{ID: digest(key), NodeName: "node", Namespace: "kube-system", NodeUID: "uid", Chassis: "chassis"}
	require.NoError(t, a.store.write(source, "private-key", key))
	require.NoError(t, a.store.write(source, "certificate", cert))
	g, err := a.store.prepareGeneration(source, trust)
	require.NoError(t, err)
	require.NoError(t, a.prepareIdentity(g, trust))
	require.Equal(t, "Prepared", a.Status().Phase)
	require.True(t, identityPrepared(a.Status(), time.Now()))
	require.False(t, a.runtime.enabled.Load(), "preparing an identity must not start IKE or publish protection")
	require.Nil(t, a.protection)
	for _, path := range []string{filepath.Join(a.store.dir, "protection.json"), filepath.Join(a.config.ProtectionDir, "required")} {
		_, err := os.Lstat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	for _, evidence := range []string{"ovs-mark", "private-reservation", "public-intent"} {
		t.Run(evidence, func(t *testing.T) {
			externalIDs := map[string]string{}
			switch evidence {
			case "ovs-mark":
				externalIDs["ovn-ipsec-protection-mark"] = "42"
			case "private-reservation":
				p := protectionOwner{store: a.store, reservation: protectionReservation{Version: 1, NodeUID: "uid", Lease: "lease", Mark: 42, Reqid: 99, Required: true}}
				require.NoError(t, p.save())
				t.Cleanup(func() { require.NoError(t, os.Remove(filepath.Join(a.store.dir, "protection.json"))) })
			case "public-intent":
				require.NoError(t, os.WriteFile(filepath.Join(a.config.ProtectionDir, "required"), []byte("required"), 0o640))
				t.Cleanup(func() { require.NoError(t, os.Remove(filepath.Join(a.config.ProtectionDir, "required"))) })
			}
			preparing, err := a.initialPreparation(t.Context(), "uid", externalIDs)
			require.NoError(t, err)
			require.False(t, preparing, "a new Prepare cannot relax previously armed protection")
		})
	}
	cm, err := client.CoreV1().ConfigMaps(a.config.Namespace).Get(t.Context(), CoordinationConfigMap, metav1.GetOptions{})
	require.NoError(t, err)
	state.Phase = ArmPhase
	data, err = json.Marshal(state)
	require.NoError(t, err)
	cm.Data["state"] = string(data)
	_, err = client.CoreV1().ConfigMaps(a.config.Namespace).Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	preparing, err = a.initialPreparation(t.Context(), "uid", nil)
	require.NoError(t, err)
	require.False(t, preparing, "after every identity is prepared, Arm must require guards")
}

func TestPrepareReceiptCannotEnableUnarmedNodes(t *testing.T) {
	_, keyPEM, _ := testIdentity(t, "chassis")
	key, err := privateKey(keyPEM)
	require.NoError(t, err)
	now := time.Now()
	state := &Coordination{Generation: "generation", Epoch: "prepare", Phase: PreparePhase, TrustHash: "trust", Targets: map[string]string{"node": "uid"}}
	claim := Receipt{
		Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, NodeName: "node", PodUID: "pod", CSRName: "request", CSRUID: "csr", Observed: now,
		Status: Status{Phase: "Prepared", NodeUID: "uid", Chassis: "chassis", Generation: "identity", CertificateHash: "cert", TrustHash: "trust", Expires: now.Add(time.Hour)},
	}
	data, err := signReceipt(claim, key)
	require.NoError(t, err)
	_, err = VerifyReceipt(data, &key.PublicKey, state, "node", "uid", now)
	require.NoError(t, err, "certificate preparation must work without closing the signing network")
	state.Phase, state.Epoch = ArmPhase, "arm"
	_, err = VerifyReceipt(data, &key.PublicKey, state, "node", "uid", now)
	require.Error(t, err, "a Prepare receipt cannot be replayed at Arm")
	claim.Phase, claim.Epoch = state.Phase, state.Epoch
	data, err = signReceipt(claim, key)
	require.NoError(t, err)
	_, err = VerifyReceipt(data, &key.PublicKey, state, "node", "uid", now)
	require.Error(t, err, "Arm requires applied runtime and live protection")
}
