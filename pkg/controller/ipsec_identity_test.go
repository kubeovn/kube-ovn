package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	certlisters "k8s.io/client-go/listers/certificates/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIPsecSignerBindsRequesterToLiveNode(t *testing.T) {
	t.Setenv(util.EnvPodNamespace, "kube-system")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "chassis-a"}, DNSNames: []string{"chassis-a"}}, key)
	require.NoError(t, err)
	request, err := x509.ParseCertificateRequest(der)
	require.NoError(t, err)
	client := fake.NewClientset(
		&appsv1.DaemonSet{Name: "kube-ovn-cni", Namespace: "kube-system", UID: "ds-uid"},
		&corev1.Pod{Name: "cni-a", Namespace: "kube-system", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "kube-ovn-cni", UID: "ds-uid", Controller: new(true)}}, Spec: corev1.PodSpec{NodeName: "node-a", ServiceAccountName: "kube-ovn-cni"}},
		&corev1.Node{Name: "node-a", UID: "node-uid", Annotations: map[string]string{util.ChassisAnnotation: "chassis-a"}},
	)
	c := &Controller{config: &Configuration{KubeClient: client}}
	base := &certv1.CertificateSigningRequest{Annotations: map[string]string{ipsec.NodeNameAnnotation: "node-a", ipsec.NodeUIDAnnotation: "node-uid"}, Spec: certv1.CertificateSigningRequestSpec{Username: "system:serviceaccount:kube-system:kube-ovn-cni", Extra: map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cni-a"}, "authentication.kubernetes.io/pod-uid": {"pod-uid"}}}}
	require.NoError(t, c.validateIPsecRequester(base, request))
	for _, tc := range []struct {
		name   string
		mutate func(*certv1.CertificateSigningRequest)
	}{
		{"wrong-SA", func(req *certv1.CertificateSigningRequest) {
			req.Spec.Username = "system:serviceaccount:kube-system:other"
		}},
		{"unbound-token", func(req *certv1.CertificateSigningRequest) { req.Spec.Extra = nil }},
		{"replaced-pod", func(req *certv1.CertificateSigningRequest) {
			req.Spec.Extra["authentication.kubernetes.io/pod-uid"] = certv1.ExtraValue{"old-pod-uid"}
		}},
		{"forged-node", func(req *certv1.CertificateSigningRequest) { req.Annotations[ipsec.NodeNameAnnotation] = "node-b" }},
		{"replaced-node", func(req *certv1.CertificateSigningRequest) { req.Annotations[ipsec.NodeUIDAnnotation] = "old-node-uid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base.DeepCopy()
			tc.mutate(req)
			require.Error(t, c.validateIPsecRequester(req, request))
		})
	}
	other := *request
	other.DNSNames = []string{"chassis-a", "chassis-b"}
	require.Error(t, c.validateIPsecRequester(base, &other))
	other = *request
	other.Subject.Names = []pkix.AttributeTypeAndValue{
		{Type: asn1.ObjectIdentifier{2, 5, 4, 3}, Value: "chassis-b"},
		{Type: asn1.ObjectIdentifier{2, 5, 4, 3}, Value: "chassis-a"},
	}
	require.Error(t, c.validateIPsecRequester(base, &other))
	other = *request
	san, err := asn1.Marshal([]asn1.RawValue{{Class: asn1.ClassContextSpecific, Tag: 2, Bytes: []byte("chassis-a")}, {Class: asn1.ClassContextSpecific, Tag: 8, Bytes: []byte("ignored-by-x509")}})
	require.NoError(t, err)
	other.Extensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 17}, Value: san}}
	require.Error(t, c.validateIPsecRequester(base, &other), "x509 DNSNames alone does not expose every SAN type")
	client.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	replaced := base.DeepCopy()
	replaced.Spec.Extra["authentication.kubernetes.io/pod-uid"] = certv1.ExtraValue{"old-pod-uid"}
	err = c.validateIPsecRequester(replaced, request)
	_, rejected := errors.AsType[*ipsecIdentityError](err)
	require.True(t, rejected, "a proven invalid Pod must remain terminal even when the node API is unavailable")
	err = c.validateIPsecRequester(base, request)
	require.ErrorIs(t, err, context.DeadlineExceeded, "valid Pod identity with an unavailable node lookup remains retryable")
}

func TestIPsecSignerDoesNotApproveDeniedOrFailedRequest(t *testing.T) {
	for _, condition := range []certv1.RequestConditionType{certv1.CertificateDenied, certv1.CertificateFailed} {
		t.Run(string(condition), func(t *testing.T) {
			req := &certv1.CertificateSigningRequest{Name: "ovn-ipsec-node", Status: certv1.CertificateSigningRequestStatus{Conditions: []certv1.CertificateSigningRequestCondition{{Type: condition, Status: "True"}}}}
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(req))
			client := fake.NewClientset(req)
			c := &Controller{config: &Configuration{KubeClient: client}, csrLister: certlisters.NewCertificateSigningRequestLister(indexer)}
			require.NoError(t, c.handleAddOrUpdateCsr(req.Name))
			require.Empty(t, client.Actions())
			require.Len(t, req.Status.Conditions, 1)
		})
	}
}

func TestIPsecSignerChecksCSRSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "chassis"}, DNSNames: []string{"chassis"}}, key)
	require.NoError(t, err)
	_, err = decodeCertificateRequest(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	require.NoError(t, err)
	der[len(der)-1] ^= 1
	_, err = decodeCertificateRequest(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	require.Error(t, err)
}

func TestIPsecSignerRetriesCAOutageWithoutFailingPendingRequest(t *testing.T) {
	t.Setenv(util.EnvPodNamespace, "kube-system")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "chassis-a"}, DNSNames: []string{"chassis-a"}}, key)
	require.NoError(t, err)
	req := &certv1.CertificateSigningRequest{Name: "ovn-ipsec-node-a", Annotations: map[string]string{ipsec.NodeNameAnnotation: "node-a", ipsec.NodeUIDAnnotation: "node-uid"}, Spec: certv1.CertificateSigningRequestSpec{
		Request:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}),
		Username: "system:serviceaccount:kube-system:kube-ovn-cni", Extra: map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cni-a"}, "authentication.kubernetes.io/pod-uid": {"pod-uid"}},
	}, Status: certv1.CertificateSigningRequestStatus{Conditions: []certv1.CertificateSigningRequestCondition{{Type: certv1.CertificateApproved, Status: "True"}}}}
	client := fake.NewClientset(req,
		&appsv1.DaemonSet{Name: "kube-ovn-cni", Namespace: "kube-system", UID: "ds-uid"},
		&corev1.Pod{Name: "cni-a", Namespace: "kube-system", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "kube-ovn-cni", UID: "ds-uid", Controller: new(true)}}, Spec: corev1.PodSpec{NodeName: "node-a", ServiceAccountName: "kube-ovn-cni"}},
		&corev1.Node{Name: "node-a", UID: "node-uid", Annotations: map[string]string{util.ChassisAnnotation: "chassis-a"}},
	)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(req))
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system"}, csrLister: certlisters.NewCertificateSigningRequestLister(indexer)}
	node, err := client.CoreV1().Nodes().Get(t.Context(), "node-a", metav1.GetOptions{})
	require.NoError(t, err)
	delete(node.Annotations, util.ChassisAnnotation)
	_, err = client.CoreV1().Nodes().Update(t.Context(), node, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, c.handleAddOrUpdateCsr(req.Name), "chassis has not registered")
	pending, err := client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), req.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, req.Status.Conditions, pending.Status.Conditions, "registration lag must not permanently fail the first CSR")
	node.Annotations[util.ChassisAnnotation] = "chassis-a"
	_, err = client.CoreV1().Nodes().Update(t.Context(), node, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Error(t, c.handleAddOrUpdateCsr(req.Name), "missing private CA must be retryable")
	after, err := client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), req.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, req.Status.Conditions, after.Status.Conditions)
	require.Empty(t, after.Status.Certificate)
	cert, caKey, err := newIPsecCA()
	require.NoError(t, err)
	_, err = client.CoreV1().Secrets("kube-system").Create(t.Context(), &corev1.Secret{Name: util.DefaultOVNIPSecSigner, Data: map[string][]byte{"cacert": cert, "cakey": caKey}}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.handleAddOrUpdateCsr(req.Name))
	after, err = client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), req.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, after.Status.Certificate)
	require.Equal(t, req.Status.Conditions, after.Status.Conditions)
	leaf, err := decodeCertificate(after.Status.Certificate)
	require.NoError(t, err)
	require.True(t, key.PublicKey.Equal(leaf.PublicKey))

	// A failed live identity lookup also must not become an InvalidIdentity
	// terminal condition. No API failure proves that the requester is forged.
	client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	require.Error(t, c.handleAddOrUpdateCsr(req.Name))
	after, err = client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), req.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, req.Status.Conditions, after.Status.Conditions)
}
