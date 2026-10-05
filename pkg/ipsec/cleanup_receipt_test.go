package ipsec

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestCleanupReceiptRejectsForgeryPurposeAndReplay(t *testing.T) {
	keyPEM, err := newPrivateKey()
	require.NoError(t, err)
	key, err := privateKey(keyPEM)
	require.NoError(t, err)
	now := time.Now()
	state := &Coordination{Version: 1, Generation: "generation", Epoch: "cleanup-epoch", Phase: CleanupPhase, DaemonSetUID: "ds-uid", TemplateHash: digest([]byte("template")), TrustHash: digest([]byte("unused trust")), Targets: map[string]string{"node": "node-uid"}, NBGlobalUUID: uuid.New().String(), SBGlobalUUID: uuid.New().String()}
	base := CleanupReceipt{
		Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, DaemonSetUID: state.DaemonSetUID, TemplateHash: state.TemplateHash,
		NBGlobalUUID: state.NBGlobalUUID, SBGlobalUUID: state.SBGlobalUUID, NodeName: "node", NodeUID: "node-uid", PodUID: "pod-uid", CSRName: "binding", CSRUID: "binding-uid",
		Chassis: "chassis", Lease: "owned-lease", Mark: 42, Reqid: 99, OVSUUID: uuid.New().String(), BootID: uuid.New().String(), IdentityCleared: true, Observed: now,
	}
	data, err := signCleanupReceipt(base, key)
	require.NoError(t, err)
	_, err = VerifyCleanupReceipt(data, &key.PublicKey, state, "node", "node-uid", now)
	require.NoError(t, err)
	var envelope SignedReceipt
	require.NoError(t, json.Unmarshal(data, &envelope))
	changed := base
	changed.Lease = "forged"
	envelope.Payload, err = json.Marshal(changed)
	require.NoError(t, err)
	forged, err := json.Marshal(envelope)
	require.NoError(t, err)
	_, err = VerifyCleanupReceipt(forged, &key.PublicKey, state, "node", "node-uid", now)
	require.Error(t, err)
	for _, tc := range []struct {
		name   string
		change func(*Coordination)
	}{
		{"next-generation", func(s *Coordination) { s.Generation = "next" }},
		{"next-epoch", func(s *Coordination) { s.Epoch = "next" }},
		{"enable-purpose", func(s *Coordination) { s.Phase = EnabledPhase }},
		{"replaced-node", func(s *Coordination) { s.Targets["node"] = "new-uid" }},
		{"replaced-daemonset", func(s *Coordination) { s.DaemonSetUID = "next" }},
		{"next-template", func(s *Coordination) { s.TemplateHash = digest([]byte("next")) }},
		{"rebuilt-nb", func(s *Coordination) { s.NBGlobalUUID = uuid.New().String() }},
		{"rebuilt-sb", func(s *Coordination) { s.SBGlobalUUID = uuid.New().String() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := *state
			s.Targets = maps.Clone(state.Targets)
			tc.change(&s)
			_, err := VerifyCleanupReceipt(data, &key.PublicKey, &s, "node", "node-uid", now)
			require.Error(t, err)
		})
	}
	for _, change := range []func(*CleanupReceipt){
		func(r *CleanupReceipt) { r.Observed = now.Add(-3 * time.Minute) },
		func(r *CleanupReceipt) { r.Observed = now.Add(time.Minute) },
		func(r *CleanupReceipt) { r.BootID = "invalid" },
		func(r *CleanupReceipt) { r.OVSUUID = "invalid" },
		func(r *CleanupReceipt) { r.Mark = 0 },
		func(r *CleanupReceipt) { r.Reqid = 1 << 31 },
		func(r *CleanupReceipt) { r.CSRUID = "" },
		func(r *CleanupReceipt) { r.IdentityCleared = false },
	} {
		r := base
		change(&r)
		data, err := signCleanupReceipt(r, key)
		require.NoError(t, err)
		_, err = VerifyCleanupReceipt(data, &key.PublicKey, state, "node", "node-uid", now)
		require.Error(t, err)
	}
	_, err = VerifyReceipt(data, &key.PublicKey, state, "node", "node-uid", now)
	require.Error(t, err, "a cleanup signature must not cross into enable acknowledgement")
	enable, err := signReceipt(Receipt{}, key)
	require.NoError(t, err)
	_, err = VerifyCleanupReceipt(enable, &key.PublicKey, state, "node", "node-uid", now)
	require.Error(t, err, "an enable signature must not cross into cleanup")
	releaseState := *state
	releaseState.Phase = ReleasePhase
	release := base
	release.Phase, release.Released = ReleasePhase, true
	releaseData, err := signCleanupReceipt(release, key)
	require.NoError(t, err)
	_, err = VerifyCleanupReceipt(releaseData, &key.PublicKey, &releaseState, "node", "node-uid", now)
	require.NoError(t, err, "a released receipt must bind the separate Release phase")
	release.Released = false
	releaseData, err = signCleanupReceipt(release, key)
	require.NoError(t, err)
	_, err = VerifyCleanupReceipt(releaseData, &key.PublicKey, &releaseState, "node", "node-uid", now)
	require.Error(t, err)
}

func TestCleanupBindingNeedsBoundPodAndRetainsItsKey(t *testing.T) {
	s := store{dir: t.TempDir()}
	owner := protectionReservation{NodeUID: "node-uid", Lease: "lease"}
	keyPEM, err := s.cleanupKey(owner)
	require.NoError(t, err)
	retained, err := s.cleanupKey(owner)
	require.NoError(t, err)
	require.True(t, bytes.Equal(keyPEM, retained))
	info, err := os.Stat(filepath.Join(s.dir, "cleanup-key-"+digest([]byte("node-uid:lease"))+".pem"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	claim := CleanupReceipt{NodeName: "node", NodeUID: "node-uid", PodUID: "current-pod", Generation: "gen", Epoch: "epoch", Chassis: "chassis"}
	client := fake.NewClientset()
	var created *certv1.CertificateSigningRequest
	client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		created = action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
		created.UID = "binding-uid"
		created.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
		created.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cni"}, "authentication.kubernetes.io/pod-uid": {claim.PodUID}}
		return true, created.DeepCopy(), nil
	})
	a := &Agent{config: Configuration{Kube: client, Namespace: "kube-system"}}
	req, err := a.cleanupBinding(t.Context(), claim, keyPEM)
	require.NoError(t, err)
	require.Equal(t, CleanupSignerName, req.Spec.SignerName)
	require.Nil(t, req.Spec.ExpirationSeconds)
	require.Empty(t, req.Status.Certificate, "cleanup cannot require issuance")
	client.PrependReactor("create", "certificatesigningrequests", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewAlreadyExists(certv1.Resource("certificatesigningrequests"), req.Name)
	})
	_, err = client.CertificatesV1().CertificateSigningRequests().Create(t.Context(), created, metav1.CreateOptions{})
	require.Error(t, err)
	// Seed the server-populated request directly; the create reactor is only
	// simulating AlreadyExists so the production path must fetch and validate.
	require.NoError(t, client.Tracker().Create(certv1.SchemeGroupVersion.WithResource("certificatesigningrequests"), created, ""))
	_, err = a.cleanupBinding(t.Context(), claim, keyPEM)
	require.NoError(t, err)
	created.Spec.Extra["authentication.kubernetes.io/pod-uid"] = certv1.ExtraValue{"foreign-pod"}
	require.NoError(t, client.Tracker().Update(certv1.SchemeGroupVersion.WithResource("certificatesigningrequests"), created, ""))
	_, err = a.cleanupBinding(t.Context(), claim, keyPEM)
	require.ErrorContains(t, err, "current bound Pod", "an existing CSR name cannot override API-authenticated identity")
	claim.PodUID = "replacement-pod"
	_, err = a.cleanupBinding(t.Context(), claim, keyPEM)
	require.Error(t, err, "a rebuilt Pod cannot reuse another Pod's authenticated binding")
}

func TestPublishReleaseReceiptBindsNewChallenge(t *testing.T) {
	storage := store{dir: t.TempDir()}
	owner := protectionReservation{Version: 1, NodeUID: "node-uid", Lease: "lease", Mark: 42, Reqid: 99}
	keyPEM, err := storage.cleanupKey(owner)
	require.NoError(t, err)
	key, err := privateKey(keyPEM)
	require.NoError(t, err)
	state := &Coordination{Version: 1, Generation: "generation", Epoch: "release-epoch", Phase: ReleasePhase, DaemonSetUID: "ds-uid", TemplateHash: digest([]byte("template")), Targets: map[string]string{"node": "node-uid"}, NBGlobalUUID: uuid.New().String(), SBGlobalUUID: uuid.New().String()}
	claim := CleanupReceipt{Generation: state.Generation, Epoch: "cleanup-epoch", Phase: CleanupPhase, DaemonSetUID: state.DaemonSetUID, TemplateHash: state.TemplateHash, NBGlobalUUID: state.NBGlobalUUID, SBGlobalUUID: state.SBGlobalUUID, NodeName: "node", NodeUID: owner.NodeUID, PodUID: "pod-uid", Chassis: "chassis", Lease: owner.Lease, Mark: owner.Mark, Reqid: owner.Reqid, OVSUUID: uuid.New().String(), BootID: uuid.New().String(), IdentityCleared: true, Observed: time.Now()}
	client := fake.NewClientset(&corev1.Node{Name: "node", UID: "node-uid"})
	client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		request := action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
		request.UID = "binding-uid"
		request.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
		request.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cni"}, "authentication.kubernetes.io/pod-uid": {claim.PodUID}}
		return true, request, nil
	})
	agent := &Agent{store: storage, config: Configuration{Kube: client, Namespace: "kube-system", NodeName: "node"}}
	request, err := agent.cleanupBinding(t.Context(), claim, keyPEM)
	require.NoError(t, err)
	claim.CSRName, claim.CSRUID = request.Name, string(request.UID)
	cleanup, err := signCleanupReceipt(claim, key)
	require.NoError(t, err)
	node, err := client.CoreV1().Nodes().Get(t.Context(), "node", metav1.GetOptions{})
	require.NoError(t, err)
	node.Annotations = map[string]string{CleanupReceiptAnnotation: string(cleanup)}
	_, err = client.CoreV1().Nodes().Update(t.Context(), node, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, agent.publishReleaseReceipt(t.Context(), state, owner))
	node, err = client.CoreV1().Nodes().Get(t.Context(), "node", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = VerifyCleanupReceipt([]byte(node.Annotations[ReleaseReceiptAnnotation]), &key.PublicKey, state, "node", "node-uid", time.Now())
	require.NoError(t, err, "the actual publisher must replace the Cleanup epoch with the Release challenge")
	for _, tc := range []struct {
		name   string
		change func(*Coordination)
	}{
		{"wrong-phase", func(s *Coordination) { s.Phase = CleanupPhase }},
		{"new-generation", func(s *Coordination) { s.Generation = "replacement-generation" }},
		{"new-template", func(s *Coordination) { s.TemplateHash = digest([]byte("replacement")) }},
		{"replaced-nb", func(s *Coordination) { s.NBGlobalUUID = uuid.New().String() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := *state
			tc.change(&changed)
			require.Error(t, agent.publishReleaseReceipt(t.Context(), &changed, owner))
		})
	}
	foreign := owner
	foreign.Mark++
	require.Error(t, agent.publishReleaseReceipt(t.Context(), state, foreign))
}
