package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	cmclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authentication/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	certlisters "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/yaml"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// This test creates synthetic identities only in the disposable cluster owned
// by hack/test-ipsec-api.sh. Tokens, private keys and kubeconfig are never logged.
func TestIPsecAPIServerSigningContract(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_API_TEST") != "true" {
		t.Skip("requires the disposable API Server harness")
	}
	const namespace, chassis, issuer = "ipsec-api-test", "api-test-chassis", "api-test-issuer"
	t.Setenv(util.EnvPodNamespace, namespace)
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	require.NoError(t, err)
	admin, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	cmAdmin, err := cmclient.NewForConfig(config)
	require.NoError(t, err)
	ctx := t.Context()
	_, err = admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{Name: namespace}, metav1.CreateOptions{})
	require.NoError(t, err)
	for _, name := range []string{"ovn", "kube-ovn-cni"} {
		_, err = admin.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{Name: name}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	_, err = admin.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{Name: "ipsec-api-test", Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{"certificates.k8s.io"}, Resources: []string{"certificatesigningrequests"}, Verbs: []string{"create"}},
		{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificaterequests"}, Verbs: []string{"create", "get", "update"}},
	}}, metav1.CreateOptions{})
	require.NoError(t, err)
	// Deliberately grant both test identities CertificateRequest creation so
	// admission rejection proves more than the production CNI's narrower RBAC.
	_, err = admin.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{Name: "ipsec-api-test", RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "ipsec-api-test"}, Subjects: []rbacv1.Subject{
		{Kind: "ServiceAccount", Namespace: namespace, Name: "ovn"}, {Kind: "ServiceAccount", Namespace: namespace, Name: "kube-ovn-cni"},
	}}, metav1.CreateOptions{})
	require.NoError(t, err)
	labels := map[string]string{"app": "ipsec-api-test"}
	_, err = admin.AppsV1().DaemonSets(namespace).Create(ctx, &appsv1.DaemonSet{Name: "kube-ovn-cni", Spec: appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: labels},
		Template: corev1.PodTemplateSpec{Labels: labels, Spec: corev1.PodSpec{ServiceAccountName: "kube-ovn-cni", Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}}, Containers: []corev1.Container{{Name: "ipsec", Image: "registry.k8s.io/pause:3.10.1"}}}},
	}}, metav1.CreateOptions{})
	require.NoError(t, err)
	var pod *corev1.Pod
	require.Eventually(t, func() bool {
		pods, err := admin.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=ipsec-api-test"})
		if err == nil && len(pods.Items) == 1 && pods.Items[0].Spec.NodeName != "" {
			pod = &pods.Items[0]
			return true
		}
		return false
	}, time.Minute, 200*time.Millisecond)
	node, err := admin.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	require.NoError(t, err)
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[util.ChassisAnnotation] = chassis
	node, err = admin.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	require.NoError(t, err)
	boundConfig := func(account string, bound bool) *rest.Config {
		t.Helper()
		request := &authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: new(int64(600))}}
		if bound {
			request.Spec.BoundObjectRef = &authv1.BoundObjectReference{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}
		}
		token, err := admin.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, account, request, metav1.CreateOptions{})
		require.NoError(t, err)
		anonymous := rest.AnonymousClientConfig(config)
		anonymous.BearerToken = token.Status.Token
		return anonymous
	}
	cni, err := kubernetes.NewForConfig(boundConfig("kube-ovn-cni", true))
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: chassis}, DNSNames: []string{chassis}}, key)
	require.NoError(t, err)
	request := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	createCSR := func(client kubernetes.Interface, name string) *certv1.CertificateSigningRequest {
		t.Helper()
		csr, err := client.CertificatesV1().CertificateSigningRequests().Create(ctx, &certv1.CertificateSigningRequest{
			Name:        "ovn-ipsec-" + name,
			Annotations: map[string]string{ipsec.NodeNameAnnotation: node.Name, ipsec.NodeUIDAnnotation: string(node.UID)},
			Spec:        certv1.CertificateSigningRequestSpec{Request: request, SignerName: util.SignerName, Usages: []certv1.KeyUsage{certv1.UsageIPsecTunnel}, ExpirationSeconds: new(int32(3600)), Username: "forged-user", Extra: map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-uid": {"forged-uid"}}},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		return csr
	}
	parsed, err := decodeCertificateRequest(request)
	require.NoError(t, err)
	c := &Controller{config: &Configuration{KubeClient: admin, PodNamespace: namespace}, addOrUpdateCsrQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())}
	t.Cleanup(c.addOrUpdateCsrQueue.ShutDown)
	csr := createCSR(cni, "bound")
	require.Equal(t, "system:serviceaccount:"+namespace+":kube-ovn-cni", csr.Spec.Username)
	require.Equal(t, certv1.ExtraValue{string(pod.UID)}, csr.Spec.Extra["authentication.kubernetes.io/pod-uid"])
	require.NoError(t, c.validateIPsecRequester(csr, parsed))
	unbound, err := kubernetes.NewForConfig(boundConfig("kube-ovn-cni", false))
	require.NoError(t, err)
	require.Error(t, c.validateIPsecRequester(createCSR(unbound, "unbound"), parsed))
	forged := csr.DeepCopy()
	forged.Annotations[ipsec.NodeUIDAnnotation] = "replaced-node-uid"
	require.Error(t, c.validateIPsecRequester(forged, parsed))

	trust, privateKey, err := newIPsecCA()
	require.NoError(t, err)
	_, err = admin.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{Name: util.DefaultOVNIPSecSigner, Data: map[string][]byte{"cacert": trust, "cakey": privateKey}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = admin.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{Name: util.DefaultOVNIPSecCA, Data: map[string][]byte{"cacert": trust}}, metav1.CreateOptions{})
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	c.csrLister = certlisters.NewCertificateSigningRequestLister(indexer)
	process := func(csr *certv1.CertificateSigningRequest) *certv1.CertificateSigningRequest {
		t.Helper()
		require.NoError(t, indexer.Update(csr))
		require.NoError(t, c.handleAddOrUpdateCsr(csr.Name))
		current, err := admin.CertificatesV1().CertificateSigningRequests().Get(ctx, csr.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return current
	}
	csr = process(process(csr))
	_, err = ipsec.ValidateCertificate(csr.Status.Certificate, trust, &key.PublicKey, chassis, time.Now())
	require.NoError(t, err, "built-in issuance must work with real authentication and CSR status endpoints")

	// Apply the actual rendered policies and wait for API Server type-checking.
	output, err := exec.CommandContext(ctx, "helm", "template", "api-test", "../../charts/kube-ovn-v2", "--kube-version=1.37.0", "--set=namespace="+namespace, "--set=ipsec.certManager.enabled=true", "--set=ipsec.certManager.issuerName="+issuer).Output()
	require.NoError(t, err)
	for document := range strings.SplitSeq(string(output), "\n---\n") {
		var header struct{ Kind string }
		require.NoError(t, yaml.Unmarshal([]byte(document), &header))
		switch header.Kind {
		case "ValidatingAdmissionPolicy":
			var policy admissionv1.ValidatingAdmissionPolicy
			require.NoError(t, yaml.UnmarshalStrict([]byte(document), &policy))
			_, err = admin.AdmissionregistrationV1().ValidatingAdmissionPolicies().Create(ctx, &policy, metav1.CreateOptions{})
			require.NoError(t, err)
		case "ValidatingAdmissionPolicyBinding":
			var binding admissionv1.ValidatingAdmissionPolicyBinding
			require.NoError(t, yaml.UnmarshalStrict([]byte(document), &binding))
			_, err = admin.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Create(ctx, &binding, metav1.CreateOptions{})
			require.NoError(t, err)
		}
	}
	c.config.CertManagerIssuerName = issuer
	var policyError error
	valid := false
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		policyError = c.verifyIPsecIssuerPolicy(ctx)
		if policyError == nil {
			valid = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("API contract test cancelled during policy validation")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !valid {
		policy, err := admin.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(ctx, ipsecIssuerPolicyName, metav1.GetOptions{})
		require.NoError(t, err)
		t.Logf("policy generation=%d observed=%d type-check=%v", policy.Generation, policy.Status.ObservedGeneration, policy.Status.TypeChecking)
	}
	require.NoError(t, policyError)
	controllerConfig := boundConfig("ovn", false)
	cmController, err := cmclient.NewForConfig(controllerConfig)
	require.NoError(t, err)
	cmCNI, err := cmclient.NewForConfig(boundConfig("kube-ovn-cni", true))
	require.NoError(t, err)
	cmRequest := &cmv1.CertificateRequest{Name: "unauthorized", Spec: cmv1.CertificateRequestSpec{Request: request, IssuerRef: cmmeta.IssuerReference{Name: issuer, Kind: "ClusterIssuer", Group: "cert-manager.io"}, Usages: []cmv1.KeyUsage{cmv1.UsageIPsecTunnel}}}
	// Policy type-checking can precede binding enforcement. Establish the
	// actual rejection barrier before creating the dedicated signing issuer.
	// Dry-run avoids leaving requests that might be signed after provisioning.
	initiallyAccepted := 0
	require.Eventually(t, func() bool {
		_, err := cmCNI.CertmanagerV1().CertificateRequests(namespace).Create(ctx, cmRequest, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		if err == nil {
			initiallyAccepted++
			return false
		}
		if !k8serrors.IsInvalid(err) && !k8serrors.IsForbidden(err) || !strings.Contains(err.Error(), "The IPsec ClusterIssuer accepts only authorized") {
			t.Errorf("unexpected response while establishing issuer admission: %v", err)
			return false
		}
		return true
	}, time.Minute, 200*time.Millisecond, "the dedicated issuer must remain absent until actual admission rejects its unauthorized requests")
	t.Logf("issuer admission established after %d accepted dry-run probes", initiallyAccepted)
	_, err = cmCNI.CertmanagerV1().CertificateRequests(namespace).Create(ctx, cmRequest, metav1.CreateOptions{})
	require.Error(t, err, "the API Server must reject an unauthorized issuer request")
	require.True(t, k8serrors.IsInvalid(err) || k8serrors.IsForbidden(err), "unexpected admission response: %v", err)
	require.ErrorContains(t, err, "The IPsec ClusterIssuer accepts only authorized")
	_, err = cmController.CertmanagerV1().CertificateRequests("default").Create(ctx, cmRequest, metav1.CreateOptions{})
	require.True(t, k8serrors.IsInvalid(err) || k8serrors.IsForbidden(err), "controller identity must not bypass the namespace constraint")
	require.ErrorContains(t, err, "The IPsec ClusterIssuer accepts only authorized")
	unrelated := cmRequest.DeepCopy()
	unrelated.Name, unrelated.Spec.IssuerRef.Name = "unrelated", "unrelated-issuer"
	_, err = cmCNI.CertmanagerV1().CertificateRequests(namespace).Create(ctx, unrelated, metav1.CreateOptions{})
	require.NoError(t, err, "the policy must leave unrelated issuers usable")
	_, err = admin.CoreV1().Secrets("cert-manager").Create(ctx, &corev1.Secret{Name: "ipsec-api-test-ca", Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": trust, "tls.key": privateKey}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = cmAdmin.CertmanagerV1().ClusterIssuers().Create(ctx, &cmv1.ClusterIssuer{Name: issuer, Spec: cmv1.IssuerSpec{CA: &cmv1.CAIssuer{SecretName: "ipsec-api-test-ca"}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	c.config.CertManagerClient, c.config.CertManagerIPSecCert = cmController, true
	external := process(createCSR(cni, "cert-manager"))
	for deadline := time.Now().Add(90 * time.Second); len(external.Status.Certificate) == 0 && time.Now().Before(deadline); {
		external = process(external)
		select {
		case <-ctx.Done():
			t.Fatal("API contract test cancelled")
		case <-time.After(time.Second):
		}
	}
	require.NotEmpty(t, external.Status.Certificate, "the real cert-manager backend must finish issuance")
	_, err = ipsec.ValidateCertificate(external.Status.Certificate, trust, &key.PublicKey, chassis, time.Now())
	require.NoError(t, err, "authorized forwarding must obtain a real cert-manager certificate")
	requests, err := cmAdmin.CertmanagerV1().CertificateRequests(namespace).List(ctx, metav1.ListOptions{LabelSelector: ipsecRequestLabel + "=true"})
	require.NoError(t, err)
	require.Len(t, requests.Items, 1)
	issued := requests.Items[0].DeepCopy()
	require.Equal(t, "system:serviceaccount:"+namespace+":ovn", issued.Spec.Username)
	issued.Labels["metadata-update"] = "allowed"
	issued, err = cmCNI.CertmanagerV1().CertificateRequests(namespace).Update(ctx, issued, metav1.UpdateOptions{})
	require.NoError(t, err, "a metadata-only update must remain possible")
	issued.Spec.Duration = &metav1.Duration{Duration: 2 * time.Hour}
	_, err = cmCNI.CertmanagerV1().CertificateRequests(namespace).Update(ctx, issued, metav1.UpdateOptions{})
	require.True(t, k8serrors.IsInvalid(err) || k8serrors.IsForbidden(err), "another requester must not mutate the authorized spec")
	// Kubernetes' CSR cleaner owns request retention. The forwarded request
	// must disappear with its exact parent UID, without node delete privileges
	// or a second, name-based cleanup controller.
	require.Equal(t, []metav1.OwnerReference{{APIVersion: "certificates.k8s.io/v1", Kind: "CertificateSigningRequest", Name: external.Name, UID: external.UID}}, issued.OwnerReferences)
	require.NoError(t, admin.CertificatesV1().CertificateSigningRequests().Delete(ctx, external.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: new(external.UID)}}))
	require.Eventually(t, func() bool {
		_, err := cmAdmin.CertmanagerV1().CertificateRequests(namespace).Get(ctx, issued.Name, metav1.GetOptions{})
		return k8serrors.IsNotFound(err)
	}, time.Minute, time.Second, "forwarded requests must be garbage collected with their CSR parent")
	_, err = cmAdmin.CertmanagerV1().CertificateRequests(namespace).Get(ctx, unrelated.Name, metav1.GetOptions{})
	require.NoError(t, err, "garbage collection must preserve the unrelated issuer request")
	t.Log("real bound-token CSR, built-in issuance, issuer admission, cert-manager forwarding and request garbage collection passed")
}
