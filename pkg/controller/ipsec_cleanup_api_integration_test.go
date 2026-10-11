package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/v2"
	"encoding/pem"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authentication/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// This tests real bound authentication and sidecar template admission;
// execution/drain of the actual binary is covered by the candidate overlay.
func TestIPsecAPIServerCleanupBinding(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_API_TEST") != "true" {
		t.Skip("requires the disposable API Server harness")
	}
	const namespace, chassis = "ipsec-cleanup-api-test", "cleanup-api-chassis"
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	require.NoError(t, err)
	admin, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	ctx := t.Context()
	_, err = admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{Name: namespace}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = admin.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{Name: "kube-ovn-cni"}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = admin.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{Name: namespace, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"certificates.k8s.io"}, Resources: []string{"certificatesigningrequests"}, Verbs: []string{"create"}}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = admin.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{Name: namespace, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: namespace}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: namespace, Name: "kube-ovn-cni"}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	labels := map[string]string{"app": namespace}
	ds, err := admin.AppsV1().DaemonSets(namespace).Create(ctx, &appsv1.DaemonSet{
		Name: "kube-ovn-cni", Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{
			Labels: labels, Spec: corev1.PodSpec{
				ServiceAccountName: "kube-ovn-cni", HostNetwork: true, Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
				InitContainers: []corev1.Container{{Name: "ipsec-cleanup", Image: "registry.k8s.io/pause:3.10.1", Command: []string{"/kube-ovn/kube-ovn-ipsec"}, Args: []string{"--cleanup-only"}}},
				Containers: []corev1.Container{
					{
						Name: "cni-server", Image: "registry.k8s.io/pause:3.10.1",
						Ports: []corev1.ContainerPort{{ContainerPort: 10665}}, Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")}},
						ReadinessProbe: &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(10665)}},
					},
				},
			},
		}},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	var pod *corev1.Pod
	require.Eventually(t, func() bool {
		pods, err := admin.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=" + namespace})
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
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: chassis}, DNSNames: []string{chassis}}, key)
	require.NoError(t, err)
	request := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	create := func(bound bool, name string) *certv1.CertificateSigningRequest {
		t.Helper()
		tokenRequest := &authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: new(int64(600))}}
		if bound {
			tokenRequest.Spec.BoundObjectRef = &authv1.BoundObjectReference{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}
		}
		token, err := admin.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, "kube-ovn-cni", tokenRequest, metav1.CreateOptions{})
		require.NoError(t, err)
		identity := rest.AnonymousClientConfig(config)
		identity.BearerToken = token.Status.Token
		client, err := kubernetes.NewForConfig(identity)
		require.NoError(t, err)
		csr, err := client.CertificatesV1().CertificateSigningRequests().Create(ctx, &certv1.CertificateSigningRequest{
			Name: name, Annotations: map[string]string{ipsec.NodeNameAnnotation: node.Name, ipsec.NodeUIDAnnotation: string(node.UID)},
			Spec: certv1.CertificateSigningRequestSpec{Request: request, SignerName: ipsec.CleanupSignerName, Usages: []certv1.KeyUsage{certv1.UsageIPsecTunnel}, Username: "forged", Extra: map[string]certv1.ExtraValue{"authentication.kubernetes.io/pod-uid": {"forged"}}},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		return csr
	}
	csr := create(true, "ovn-ipsec-cleanup-api-bound")
	require.Equal(t, certv1.ExtraValue{string(pod.UID)}, csr.Spec.Extra["authentication.kubernetes.io/pod-uid"])
	require.Empty(t, csr.Status.Certificate)
	require.Nil(t, csr.Spec.ExpirationSeconds)
	template, err := json.Marshal(ds.Spec.Template, json.Deterministic(true))
	require.NoError(t, err)
	state := &ipsec.Coordination{Version: 1, Generation: "cleanup-api", Epoch: "cleanup-epoch", Phase: ipsec.CleanupPhase, DaemonSetUID: string(ds.UID), TemplateHash: ipsecPublicHash(template), TrustHash: ipsecPublicHash([]byte("unused trust")), Targets: map[string]string{node.Name: string(node.UID)}, NBGlobalUUID: uuid.New().String(), SBGlobalUUID: uuid.New().String()}
	claim := ipsec.CleanupReceipt{
		Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, DaemonSetUID: state.DaemonSetUID, TemplateHash: state.TemplateHash, NBGlobalUUID: state.NBGlobalUUID, SBGlobalUUID: state.SBGlobalUUID,
		NodeName: node.Name, NodeUID: string(node.UID), PodUID: string(pod.UID), CSRName: csr.Name, CSRUID: string(csr.UID), Chassis: chassis, Lease: "cleanup-api-lease", Mark: 42, Reqid: 99, OVSUUID: uuid.New().String(), BootID: uuid.New().String(), IdentityCleared: true, Observed: time.Now(),
	}
	node.Annotations[ipsec.CleanupReceiptAnnotation] = cleanupTestSignature(t, claim, key)
	require.NoError(t, verifyIPsecCleanupReceipt(node, ds, state, map[string]*corev1.Pod{pod.Name: pod}, map[string]*certv1.CertificateSigningRequest{csr.Name: csr}, ipsec.CleanupReceiptAnnotation))
	other := create(false, "ovn-ipsec-cleanup-api-unbound")
	claim.CSRName, claim.CSRUID = other.Name, string(other.UID)
	node.Annotations[ipsec.CleanupReceiptAnnotation] = cleanupTestSignature(t, claim, key)
	require.Error(t, verifyIPsecCleanupReceipt(node, ds, state, map[string]*corev1.Pod{pod.Name: pod}, map[string]*certv1.CertificateSigningRequest{other.Name: other}, ipsec.CleanupReceiptAnnotation))
}
