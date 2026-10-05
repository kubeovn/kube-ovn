package ipsec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	certv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIssuerRejectsCollisionAndTerminalFailure(t *testing.T) {
	csr := []byte("synthetic-request")
	for _, tc := range []struct {
		name    string
		change  func(*certv1.CertificateSigningRequest)
		message string
	}{
		{"different-key", func(req *certv1.CertificateSigningRequest) {
			req.Spec.Request = []byte("stale-request")
			req.Status.Certificate = []byte("stale-certificate")
		}, "conflicts"},
		{"denied", func(req *certv1.CertificateSigningRequest) {
			req.Status.Conditions = []certv1.CertificateSigningRequestCondition{{Type: certv1.CertificateDenied, Status: "True", Reason: "Forbidden"}}
		}, "Denied"},
		{"failed", func(req *certv1.CertificateSigningRequest) {
			req.Status.Conditions = []certv1.CertificateSigningRequestCondition{{Type: certv1.CertificateFailed, Status: "True", Reason: "Invalid"}}
		}, "Failed"},
		{"another-pod", func(req *certv1.CertificateSigningRequest) {
			req.Spec.Extra["authentication.kubernetes.io/pod-uid"] = certv1.ExtraValue{"another-pod"}
		}, "not authenticated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := issuer{node: "node-a", nodeUID: "uid-a", podUID: "pod-a", namespace: "kube-system", duration: time.Hour}
			req := &certv1.CertificateSigningRequest{Name: i.name(csr), UID: "request-uid", Annotations: map[string]string{NodeNameAnnotation: "node-a", NodeUIDAnnotation: "uid-a"}, Spec: certv1.CertificateSigningRequestSpec{Request: csr, SignerName: util.SignerName, Usages: []certv1.KeyUsage{certv1.UsageIPsecTunnel}, ExpirationSeconds: new(int32(3600))}}
			req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
			req.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-uid": {i.podUID}}
			tc.change(req)
			client := fake.NewClientset(req)
			i.kube = client
			_, err := i.sign(t.Context(), csr)
			require.ErrorContains(t, err, tc.message)
			retained, err := client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), req.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, req.UID, retained.UID)
		})
	}
}

func TestIssuerPodRecreationPreservesKeyButReplacesBoundRequest(t *testing.T) {
	key, err := newPrivateKey()
	require.NoError(t, err)
	csr, err := newCSR(key, "chassis-a")
	require.NoError(t, err)
	old := issuer{node: "node-a", nodeUID: "node-uid", podUID: "old-pod", namespace: "kube-system", duration: time.Hour}
	oldRequest := &certv1.CertificateSigningRequest{Name: old.name(csr), UID: "old-request", Status: certv1.CertificateSigningRequestStatus{Certificate: []byte("old-certificate")}}
	client := fake.NewClientset(oldRequest)
	client.PrependReactor("create", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		req := action.(k8stesting.CreateAction).GetObject().(*certv1.CertificateSigningRequest).DeepCopy()
		req.UID = "new-request"
		req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
		req.Spec.Extra = map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-uid": {"new-pod"}}
		req.Status.Certificate = []byte("new-certificate")
		require.NotEqual(t, oldRequest.Name, req.Name)
		require.Equal(t, csr, req.Spec.Request)
		return true, req, nil
	})
	next := old
	next.podUID, next.kube = "new-pod", client
	cert, err := next.sign(t.Context(), csr)
	require.NoError(t, err)
	require.Equal(t, "new-certificate", string(cert))
	retained, err := client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), oldRequest.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, oldRequest.UID, retained.UID)
}

func TestIssuerTrustChangeReplacesAStaleSignedRequest(t *testing.T) {
	csr := []byte("persisted-key-csr")
	i := issuer{nodeUID: "node", podUID: "pod", duration: time.Hour, trustHash: "previous-trust"}
	previous := i.name(csr)
	i.trustHash = "replacement-trust"
	require.NotEqual(t, previous, i.name(csr), "a pending key must be signable again after its old issuer is no longer trusted")
}
