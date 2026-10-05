package controller

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/v2"
	"encoding/pem"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIPsecFrozenTargetsIncludeOfflineNodesAndRequiredAffinity(t *testing.T) {
	ds := &appsv1.DaemonSet{UID: "ds-uid", Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ipsec"}}, NodeSelector: map[string]string{"os": "linux"}, Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "pool", Operator: corev1.NodeSelectorOpIn, Values: []string{"network"}}}}}}}}}}}}
	nodes := []corev1.Node{
		{Name: "online", UID: "online-uid", Labels: map[string]string{"os": "linux", "pool": "network"}},
		{Name: "offline", UID: "offline-uid", Labels: map[string]string{"os": "linux", "pool": "network"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}}},
		{Name: "wrong-pool", UID: "other-uid", Labels: map[string]string{"os": "linux", "pool": "compute"}},
		{Name: "windows", UID: "windows-uid", Labels: map[string]string{"os": "windows", "pool": "network"}},
	}
	state, err := freezeIPsecTargets(ds, nodes, []byte("trust"), false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"online": "online-uid", "offline": "offline-uid"}, state.Targets)
	require.Equal(t, ipsec.PreparePhase, state.Phase)
	for range 8 {
		repeated, err := freezeIPsecTargets(ds, nodes, []byte("trust"), false)
		require.NoError(t, err)
		require.Equal(t, state.TemplateHash, repeated.TemplateHash, "template maps must have a deterministic digest")
	}
	state, err = freezeIPsecTargets(ds, nodes, []byte("trust"), true)
	require.NoError(t, err)
	require.Equal(t, ipsec.EnabledPhase, state.Phase, "a legacy enabled cluster is recorded without toggling NB")
	_, err = freezeIPsecTargets(ds, nil, []byte("trust"), false)
	require.Error(t, err)
}

func TestIPsecCoordinationRequiresFreshBoundReceiptsBeforeEnable(t *testing.T) {
	t.Setenv(util.EnvPodNamespace, "kube-system")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "chassis-a"}, DNSNames: []string{"chassis-a"}}, key)
	require.NoError(t, err)
	request, err := x509.ParseCertificateRequest(csrDER)
	require.NoError(t, err)
	trust, caKeyPEM, err := newIPsecCA()
	require.NoError(t, err)
	ca, err := decodeCertificate(trust)
	require.NoError(t, err)
	caKey, err := decodePrivateKey(caKeyPEM)
	require.NoError(t, err)
	template, err := newCertificateTemplate(request)
	require.NoError(t, err)
	cert, err := signCSR(template, &key.PublicKey, ca, caKey)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	ds := &appsv1.DaemonSet{Name: "kube-ovn-cni", Namespace: "kube-system", UID: "ds-uid", Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{HostNetwork: true, HostPID: true, Containers: []corev1.Container{{Name: "ipsec", Image: "candidate"}}}}}}
	pod := &corev1.Pod{Name: "cni-a", Namespace: "kube-system", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: ds.Name, UID: ds.UID, Controller: new(true)}}, Spec: corev1.PodSpec{NodeName: "node-a", ServiceAccountName: "kube-ovn-cni", HostNetwork: true, HostPID: true, Containers: []corev1.Container{{Name: "ipsec", Image: "candidate", VolumeMounts: []corev1.VolumeMount{{Name: "kube-api-access-test", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true}}}}}}
	node := &corev1.Node{Name: "node-a", UID: "node-uid", Annotations: map[string]string{util.ChassisAnnotation: "chassis-a"}}
	pod.Spec.Volumes = []corev1.Volume{ipsecTestAPITokenVolume("kube-api-access-test")}
	csr := &certv1.CertificateSigningRequest{Name: "ovn-ipsec-receipt", UID: "csr-uid", Annotations: map[string]string{ipsec.NodeNameAnnotation: node.Name, ipsec.NodeUIDAnnotation: string(node.UID)}, Spec: certv1.CertificateSigningRequestSpec{SignerName: util.SignerName, Request: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), Username: "system:serviceaccount:kube-system:kube-ovn-cni", Extra: map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {pod.Name}, "authentication.kubernetes.io/pod-uid": {string(pod.UID)}}}, Status: certv1.CertificateSigningRequestStatus{Certificate: certPEM}}
	kube := fake.NewClientset(ds, pod, node, csr, &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": trust}})
	nb := &ovnnb.NBGlobal{}
	ovs := mockovs.NewMockNbClient(gomock.NewController(t))
	ovs.EXPECT().GetNbGlobal().Return(nb, nil).AnyTimes()
	// No transition before fresh Arm receipts may call the enabling operation.
	ovs.EXPECT().SetOVNIPSec(true).DoAndReturn(func(bool) error { nb.Ipsec = true; return nil }).Times(1)
	c := &Controller{config: &Configuration{KubeClient: kube, PodNamespace: "kube-system", EnableOVNIPSec: true}, OVNNbClient: ovs}
	readState := func() *ipsec.Coordination {
		cm, err := kube.CoreV1().ConfigMaps("kube-system").Get(t.Context(), ipsec.CoordinationConfigMap, metav1.GetOptions{})
		require.NoError(t, err)
		state, err := ipsec.DecodeCoordination([]byte(cm.Data["state"]))
		require.NoError(t, err)
		return state
	}
	publish := func(state *ipsec.Coordination) {
		now := time.Now()
		claim := ipsec.Receipt{Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, NodeName: node.Name, PodUID: string(pod.UID), CSRName: csr.Name, CSRUID: string(csr.UID), Observed: now, Status: ipsec.Status{NodeUID: string(node.UID), Chassis: "chassis-a", Generation: "identity", CertificateHash: ipsecPublicHash(certPEM), TrustHash: state.TrustHash, RuntimeHealthy: true, ConfigurationApplied: true, ProtectionArmed: true, Expires: cert.NotAfter}}
		payload, err := json.Marshal(claim)
		require.NoError(t, err)
		hash := sha256.Sum256(append([]byte("kube-ovn IPsec coordination receipt v1\x00"), payload...))
		signature, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, hash[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
		require.NoError(t, err)
		data, err := json.Marshal(ipsec.SignedReceipt{Payload: payload, Signature: signature})
		require.NoError(t, err)
		node.Annotations[ipsec.ReceiptAnnotation] = string(data)
		_, err = kube.CoreV1().Nodes().Update(t.Context(), node, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
	prepared := readState()
	require.Equal(t, ipsec.PreparePhase, prepared.Phase)
	require.ErrorContains(t, c.reconcileIPsecCoordination(t.Context()), "missing or oversized")
	require.False(t, nb.Ipsec)
	replaced := node.DeepCopy()
	replaced.UID = "replacement-node-uid"
	_, err = kube.CoreV1().Nodes().Update(t.Context(), replaced, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, c.reconcileIPsecCoordination(t.Context()), "removed or replaced")
	require.Equal(t, prepared.Targets, readState().Targets, "reconciliation cannot silently rewrite a frozen Node UID")
	_, err = kube.CoreV1().Nodes().Update(t.Context(), node, metav1.UpdateOptions{})
	require.NoError(t, err)
	publish(prepared)
	replacedPod := pod.DeepCopy()
	replacedPod.UID = "replacement-pod-uid"
	_, err = kube.CoreV1().Pods("kube-system").Update(t.Context(), replacedPod, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, c.reconcileIPsecCoordination(t.Context()), "no longer valid")
	_, err = kube.CoreV1().Pods("kube-system").Update(t.Context(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
	armed := readState()
	require.Equal(t, ipsec.ArmPhase, armed.Phase)
	require.Equal(t, prepared.Targets, armed.Targets)
	require.Equal(t, prepared.Generation, armed.Generation)
	require.NotEqual(t, prepared.Epoch, armed.Epoch)
	require.ErrorContains(t, c.reconcileIPsecCoordination(t.Context()), "current barrier")
	require.False(t, nb.Ipsec, "a valid Prepare signature cannot be replayed at Arm")
	publish(armed)
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
	require.True(t, nb.Ipsec)
	require.Equal(t, ipsec.EnabledPhase, readState().Phase)
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()), "enabled reconciliation must not toggle IPsec during rolling migration")
	ds.Spec.Template.Spec.Containers[0].Image = "next-candidate"
	_, err = kube.AppsV1().DaemonSets("kube-system").Update(t.Context(), ds, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
	rolled := readState()
	require.Equal(t, prepared.Targets, rolled.Targets)
	require.NotEqual(t, prepared.Generation, rolled.Generation)
	require.Equal(t, ipsec.EnabledPhase, rolled.Phase)
	require.True(t, nb.Ipsec)
	_, err = kube.CoreV1().Nodes().Create(t.Context(), &corev1.Node{Name: "new-node", UID: "new-node-uid"}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
	joined := readState()
	require.Equal(t, map[string]string{"node-a": "node-uid", "new-node": "new-node-uid"}, joined.Targets)
	require.NotEqual(t, rolled.Generation, joined.Generation, "new members invalidate older cohort acknowledgements")
	require.True(t, nb.Ipsec, "joining an already protected new node cannot toggle encryption off")
	require.NoError(t, kube.CoreV1().Nodes().Delete(t.Context(), "new-node", metav1.DeleteOptions{}))
	require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
	require.Equal(t, joined.Targets, readState().Targets, "a removed member still needs explicit retirement")
	_, err = kube.CoreV1().Nodes().Create(t.Context(), &corev1.Node{Name: "new-node", UID: "replacement-uid"}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, c.reconcileIPsecCoordination(t.Context()), "explicit retirement")
	require.Equal(t, joined.Targets, readState().Targets, "same-name replacement cannot overwrite a frozen identity")
}

func TestIPsecChangedBarrierRestartsChallengeWithoutDroppingTargets(t *testing.T) {
	trust, _, err := newIPsecCA()
	require.NoError(t, err)
	for _, phase := range []string{ipsec.PreparePhase, ipsec.ArmPhase} {
		for _, enabled := range []bool{false, true} {
			t.Run(phase+"/"+strconv.FormatBool(enabled), func(t *testing.T) {
				state := &ipsec.Coordination{Version: 1, Generation: "old-generation", Epoch: "old-epoch", Phase: phase, DaemonSetUID: "old-ds", TemplateHash: ipsecPublicHash([]byte("old-template")), TrustHash: ipsecPublicHash([]byte("old-trust")), Targets: map[string]string{"offline": "offline-uid"}}
				cm := &corev1.ConfigMap{Name: ipsec.CoordinationConfigMap, Namespace: "kube-system"}
				require.NoError(t, encodeIPsecCoordination(cm, state))
				ds := &appsv1.DaemonSet{Name: "kube-ovn-cni", Namespace: "kube-system", UID: "new-ds", Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ipsec", Image: "new-candidate"}}, NodeSelector: map[string]string{"pool": "new-pool"}}}}}
				kube := fake.NewClientset(cm, ds, &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": trust}})
				nb := mockovs.NewMockNbClient(gomock.NewController(t))
				// The cached value intentionally differs. Recovery must use a live
				// read before choosing Prepare or preserving a committed enable.
				nb.EXPECT().GetNbGlobal().Return(&ovnnb.NBGlobal{Ipsec: !enabled}, nil)
				nb.EXPECT().GetIPsecGlobal(gomock.Any()).DoAndReturn(func(context.Context) (*ovs.IPsecGlobalState, error) {
					return &ovs.IPsecGlobalState{UUID: "75980000-0000-0000-0000-000000000001", Enabled: enabled}, nil
				})
				c := &Controller{config: &Configuration{KubeClient: kube, PodNamespace: "kube-system", EnableOVNIPSec: true}, OVNNbClient: nb}
				require.NoError(t, c.reconcileIPsecCoordination(t.Context()))
				actual, err := kube.CoreV1().ConfigMaps("kube-system").Get(t.Context(), cm.Name, metav1.GetOptions{})
				require.NoError(t, err)
				restarted, err := ipsec.DecodeCoordination([]byte(actual.Data["state"]))
				require.NoError(t, err)
				require.Equal(t, state.Targets, restarted.Targets, "selector, template and trust changes retain the entire original cohort")
				require.NotEqual(t, state.Generation, restarted.Generation)
				require.NotEqual(t, state.Epoch, restarted.Epoch)
				require.Equal(t, string(ds.UID), restarted.DaemonSetUID)
				require.Equal(t, ipsecPublicHash(trust), restarted.TrustHash)
				expectedPhase := ipsec.PreparePhase
				if enabled {
					expectedPhase = ipsec.EnabledPhase
				}
				require.Equal(t, expectedPhase, restarted.Phase)
			})
		}
	}
}
