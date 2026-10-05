package controller

import (
	"context"
	"crypto/rsa"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func hasIPsecCleanupTemplate(ds *appsv1.DaemonSet) bool {
	return ds != nil && ds.UID != "" && ds.DeletionTimestamp == nil && !slices.ContainsFunc(ds.Spec.Template.Spec.Containers, func(c corev1.Container) bool { return c.Name == "ipsec" }) &&
		slices.ContainsFunc(ds.Spec.Template.Spec.InitContainers, func(c corev1.Container) bool {
			return c.Name == "ipsec-cleanup" && c.RestartPolicy == nil && slices.Equal(c.Command, []string{"/kube-ovn/kube-ovn-ipsec"}) && slices.Equal(c.Args, []string{"--cleanup-only"})
		})
}

// The barrier stays at Cleanup: successful claims are not authorization to
// withdraw protection until the separate release/Disabled protocol completes.
// No signer initialization, trust Secret or cert-manager client is used here.
func (c *Controller) verifyIPsecCleanupBarrier(ctx context.Context, ds *appsv1.DaemonSet, state *ipsec.Coordination) error {
	return c.verifyIPsecNodeBarrier(ctx, ds, state, ipsec.CleanupReceiptAnnotation)
}

func (c *Controller) verifyIPsecReleaseBarrier(ctx context.Context, ds *appsv1.DaemonSet, state *ipsec.Coordination) error {
	return c.verifyIPsecNodeBarrier(ctx, ds, state, ipsec.ReleaseReceiptAnnotation)
}

func (c *Controller) verifyIPsecNodeBarrier(ctx context.Context, ds *appsv1.DaemonSet, state *ipsec.Coordination, annotation string) error {
	nodes, err := c.config.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	pods, err := c.config.KubeClient.CoreV1().Pods(c.config.PodNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	requests, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{FieldSelector: "spec.signerName=" + ipsec.CleanupSignerName})
	if err != nil {
		return err
	}
	nodeByName := make(map[string]*corev1.Node, len(nodes.Items))
	for i := range nodes.Items {
		nodeByName[nodes.Items[i].Name] = &nodes.Items[i]
	}
	podByName := make(map[string]*corev1.Pod, len(pods.Items))
	for i := range pods.Items {
		podByName[pods.Items[i].Name] = &pods.Items[i]
	}
	requestByName := make(map[string]*certv1.CertificateSigningRequest, len(requests.Items))
	for i := range requests.Items {
		requestByName[requests.Items[i].Name] = &requests.Items[i]
	}
	for name, uid := range state.Targets {
		node := nodeByName[name]
		if node == nil || string(node.UID) != uid || node.DeletionTimestamp != nil {
			return fmt.Errorf("IPsec cleanup frozen target %s is missing or replaced", name)
		}
		if err := verifyIPsecCleanupReceipt(node, ds, state, podByName, requestByName, annotation); err != nil {
			return fmt.Errorf("IPsec cleanup frozen target %s: %w", name, err)
		}
	}
	return nil
}

func verifyIPsecCleanupReceipt(node *corev1.Node, ds *appsv1.DaemonSet, state *ipsec.Coordination, pods map[string]*corev1.Pod, requests map[string]*certv1.CertificateSigningRequest, annotation string) error {
	data := []byte(node.Annotations[annotation])
	if len(data) == 0 || len(data) > 16<<10 || !hasIPsecCleanupTemplate(ds) {
		return errors.New("missing cleanup receipt or init template")
	}
	template, err := json.Marshal(ds.Spec.Template, json.Deterministic(true))
	if err != nil {
		return err
	}
	if state.DaemonSetUID != string(ds.UID) || state.TemplateHash != ipsecPublicHash(template) {
		return errors.New("IPsec cleanup template is not the frozen template")
	}
	var signed ipsec.SignedReceipt
	if err := json.Unmarshal(data, &signed); err != nil {
		return err
	}
	var claim ipsec.CleanupReceipt
	if err := json.Unmarshal(signed.Payload, &claim); err != nil {
		return err
	}
	csr := requests[claim.CSRName]
	if csr == nil || csr.DeletionTimestamp != nil || string(csr.UID) != claim.CSRUID || csr.Spec.SignerName != ipsec.CleanupSignerName || csr.Spec.ExpirationSeconds != nil || !slices.Equal(csr.Spec.Usages, []certv1.KeyUsage{certv1.UsageIPsecTunnel}) || csr.Annotations[ipsec.NodeNameAnnotation] != node.Name || csr.Annotations[ipsec.NodeUIDAnnotation] != string(node.UID) || !slices.Equal(csr.Spec.Extra["authentication.kubernetes.io/pod-uid"], []string{claim.PodUID}) || len(csr.Status.Certificate) != 0 {
		return errors.New("IPsec cleanup binding CSR does not match the current node and purpose")
	}
	for _, condition := range csr.Status.Conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == certv1.CertificateDenied || condition.Type == certv1.CertificateFailed) {
			return errors.New("IPsec cleanup binding CSR is denied or failed")
		}
	}
	request, err := decodeCertificateRequest(csr.Spec.Request)
	if err != nil {
		return err
	}
	names := csr.Spec.Extra["authentication.kubernetes.io/pod-name"]
	if len(names) != 1 {
		return errors.New("IPsec cleanup CSR has no bound Pod")
	}
	pod := pods[names[0]]
	if err := validateIPsecBoundObjects(ds.Namespace, csr, request, pod, node, ds); err != nil {
		return err
	}
	key, ok := request.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("IPsec cleanup receipt key is not RSA")
	}
	verificationTime := time.Now()
	// Release is a completed local transition, not a renewable health lease.
	// Earlier init containers can finish long before a large zero-surge cohort.
	// Retain their signed result only while the same bound Pod/template is live
	// and Kubernetes confirms that this exact init exited successfully.
	if annotation == ipsec.ReleaseReceiptAnnotation {
		completed := false
		for _, status := range pod.Status.InitContainerStatuses {
			if status.Name == "ipsec-cleanup" && status.State.Terminated != nil && status.State.Terminated.ExitCode == 0 && !status.State.Terminated.FinishedAt.Time.Add(time.Second).Before(claim.Observed) {
				completed = true
			}
		}
		if !completed {
			return errors.New("IPsec cleanup init has not completed successfully")
		}
		if claim.Observed.After(verificationTime.Add(5 * time.Second)) {
			return errors.New("IPsec release receipt is from the future")
		}
		verificationTime = claim.Observed
	}
	receipt, err := ipsec.VerifyCleanupReceipt(data, key, state, node.Name, string(node.UID), verificationTime)
	if err != nil {
		return err
	}
	if receipt.Chassis != node.Annotations[util.ChassisAnnotation] || pod.Spec.NodeName != node.Name || string(pod.UID) != receipt.PodUID {
		return errors.New("IPsec cleanup Pod does not match the frozen node or security context")
	}
	return verifyIPsecPodTemplate(pod, ds)
}
