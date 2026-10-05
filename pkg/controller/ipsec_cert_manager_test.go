package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	cmfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	certlisters "k8s.io/client-go/listers/certificates/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func testIPsecIssuerPolicy(namespace, issuer string) (*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding) {
	match, authorized := ipsecIssuerPolicyExpressions(namespace, issuer)
	policy := &admissionv1.ValidatingAdmissionPolicy{Name: ipsecIssuerPolicyName, Spec: admissionv1.ValidatingAdmissionPolicySpec{
		FailurePolicy:    new(admissionv1.Fail),
		MatchConstraints: &admissionv1.MatchResources{ResourceRules: []admissionv1.NamedRuleWithOperations{{APIGroups: []string{"cert-manager.io"}, APIVersions: []string{"v1"}, Operations: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update}, Resources: []string{"certificaterequests"}}}},
		MatchConditions:  []admissionv1.MatchCondition{{Name: "dedicated-ipsec-issuer", Expression: match}},
		Validations:      []admissionv1.Validation{{Expression: authorized}},
	}}
	binding := &admissionv1.ValidatingAdmissionPolicyBinding{Name: ipsecIssuerPolicyName, Spec: admissionv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: ipsecIssuerPolicyName, ValidationActions: []admissionv1.ValidationAction{admissionv1.Deny}}}
	return policy, binding
}

func TestIPsecCertManagerSharesCSRIdentityAuthorization(t *testing.T) {
	t.Setenv(util.EnvPodNamespace, "kube-system")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "chassis-a"}, DNSNames: []string{"chassis-a"}}, key)
	require.NoError(t, err)
	base := &certv1.CertificateSigningRequest{
		Name: "ovn-ipsec-node-a", UID: "request-uid", Annotations: map[string]string{ipsec.NodeNameAnnotation: "node-a", ipsec.NodeUIDAnnotation: "node-uid"},
		Spec: certv1.CertificateSigningRequestSpec{
			Request: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), SignerName: util.SignerName,
			Usages: []certv1.KeyUsage{certv1.UsageIPsecTunnel}, ExpirationSeconds: new(int32(3600)),
			Username: "system:serviceaccount:kube-system:kube-ovn-cni",
			Extra:    map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-name": {"cni-a"}, "authentication.kubernetes.io/pod-uid": {"pod-uid"}},
		},
		Status: certv1.CertificateSigningRequestStatus{Conditions: []certv1.CertificateSigningRequestCondition{{Type: certv1.CertificateApproved, Status: "True"}}},
	}
	trust, caKeyPEM, err := newIPsecCA()
	require.NoError(t, err)
	ca, err := decodeCertificate(trust)
	require.NoError(t, err)
	caKey, err := decodePrivateKey(caKeyPEM)
	require.NoError(t, err)
	request, err := decodeCertificateRequest(base.Spec.Request)
	require.NoError(t, err)
	template, err := newCertificateTemplate(request)
	require.NoError(t, err)
	template.NotAfter = time.Now().Add(time.Hour)
	leaf, err := signCSR(template, request.PublicKey, ca, caKey)
	require.NoError(t, err)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	for _, scenario := range []string{"authorized", "forged-node", "unbound-token", "pending", "denied", "collision", "wrong-creator"} {
		t.Run(scenario, func(t *testing.T) {
			csr := base.DeepCopy()
			if scenario == "forged-node" {
				csr.Annotations[ipsec.NodeUIDAnnotation] = "other-node-uid"
			}
			if scenario == "unbound-token" {
				csr.Spec.Extra = nil
			}
			policy, binding := testIPsecIssuerPolicy("kube-system", "kube-ovn")
			client := fake.NewClientset(csr,
				policy, binding,
				&appsv1.DaemonSet{Name: "kube-ovn-cni", Namespace: "kube-system", UID: "ds-uid"},
				&corev1.Pod{Name: "cni-a", Namespace: "kube-system", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "kube-ovn-cni", UID: "ds-uid", Controller: new(true)}}, Spec: corev1.PodSpec{NodeName: "node-a", ServiceAccountName: "kube-ovn-cni"}},
				&corev1.Node{Name: "node-a", UID: "node-uid", Annotations: map[string]string{util.ChassisAnnotation: "chassis-a"}},
				&corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": trust}},
			)
			cm := cmfake.NewClientset()
			cm.PrependReactor("create", "certificaterequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
				req := action.(k8stesting.CreateAction).GetObject().(*cmv1.CertificateRequest).DeepCopy()
				req.UID = "cm-request-uid"
				req.Spec.Username = "system:serviceaccount:kube-system:ovn"
				req.Status.Certificate = certificate
				req.Status.Conditions = []cmv1.CertificateRequestCondition{
					{Type: cmv1.CertificateRequestConditionApproved, Status: cmmeta.ConditionTrue},
					{Type: cmv1.CertificateRequestConditionReady, Status: cmmeta.ConditionTrue},
				}
				switch scenario {
				case "pending":
					req.Status = cmv1.CertificateRequestStatus{}
				case "denied":
					req.Status.Conditions = append(req.Status.Conditions, cmv1.CertificateRequestCondition{Type: cmv1.CertificateRequestConditionDenied, Status: cmmeta.ConditionTrue})
				case "collision":
					req.Spec.Request = []byte("other-csr")
				case "wrong-creator":
					req.Spec.Username = "system:serviceaccount:kube-system:kube-ovn-cni"
				}
				return true, req, nil
			})
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(csr))
			queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
			t.Cleanup(queue.ShutDown)
			c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system", CertManagerIPSecCert: true, CertManagerIssuerName: "kube-ovn", CertManagerClient: cm}, csrLister: certlisters.NewCertificateSigningRequestLister(indexer), addOrUpdateCsrQueue: queue}
			err := c.handleAddOrUpdateCsr(csr.Name)
			if scenario == "collision" || scenario == "wrong-creator" {
				require.ErrorContains(t, err, "conflicts")
			} else {
				require.NoError(t, err)
			}
			after, err := client.CertificatesV1().CertificateSigningRequests().Get(t.Context(), csr.Name, metav1.GetOptions{})
			require.NoError(t, err)
			if scenario == "authorized" {
				require.Equal(t, certificate, after.Status.Certificate)
			} else {
				require.Empty(t, after.Status.Certificate)
			}
			if scenario == "forged-node" || scenario == "unbound-token" {
				require.Empty(t, cm.Actions(), "even a previously approved CSR needs node identity validation before forwarding")
				require.Equal(t, certv1.CertificateFailed, after.Status.Conditions[len(after.Status.Conditions)-1].Type)
			}
		})
	}
}
