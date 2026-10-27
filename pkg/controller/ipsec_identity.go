package controller

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type ipsecIdentityError struct{ message string }

func (e *ipsecIdentityError) Error() string { return e.message }

func rejectIPsecIdentity(message string) error { return &ipsecIdentityError{message: message} }

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Validate server-populated authentication fields, not the request's mutable
// labels or its name. A shared service account alone does not identify a node.
func (c *Controller) validateIPsecRequester(csr *certv1.CertificateSigningRequest, req *x509.CertificateRequest) error {
	namespace := os.Getenv(util.EnvPodNamespace)
	if csr.Spec.Username != "system:serviceaccount:"+namespace+":kube-ovn-cni" {
		return rejectIPsecIdentity("unexpected IPsec requester service account")
	}
	name, uid := csr.Spec.Extra["authentication.kubernetes.io/pod-name"], csr.Spec.Extra["authentication.kubernetes.io/pod-uid"]
	if len(name) != 1 || len(uid) != 1 {
		return rejectIPsecIdentity("IPsec requester needs a bound Pod identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pod, err := c.config.KubeClient.CoreV1().Pods(namespace).Get(ctx, name[0], metav1.GetOptions{})
	if err != nil {
		return err
	}
	ds, err := c.config.KubeClient.AppsV1().DaemonSets(namespace).Get(ctx, "kube-ovn-cni", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := validateIPsecBoundPod(namespace, csr, pod, ds); err != nil {
		return err
	}
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return validateIPsecBoundObjects(namespace, csr, req, pod, node, ds)
}

// The signer fetches individual live objects. The coordinator uses bounded
// batch snapshots and this same identity contract, avoiding per-node API
// requests while verifying a large frozen cohort.
func validateIPsecBoundObjects(namespace string, csr *certv1.CertificateSigningRequest, req *x509.CertificateRequest, pod *corev1.Pod, node *corev1.Node, ds *appsv1.DaemonSet) error {
	if err := validateIPsecBoundPod(namespace, csr, pod, ds); err != nil {
		return err
	}
	if node == nil || node.DeletionTimestamp != nil || node.Name != pod.Spec.NodeName {
		return rejectIPsecIdentity("IPsec requester node identity is no longer valid")
	}
	if csr.Annotations[ipsec.NodeNameAnnotation] != "" && (csr.Annotations[ipsec.NodeNameAnnotation] != node.Name || csr.Annotations[ipsec.NodeUIDAnnotation] != string(node.UID)) {
		return rejectIPsecIdentity("IPsec request Node UID does not match bound Pod")
	}
	chassis := node.Annotations[util.ChassisAnnotation]
	if chassis == "" {
		// OVS registers after bootstrap protection; the Node annotation can lag
		// the first CSR. Keep the request pending without weakening CN checks.
		return errors.New("IPsec requester chassis has not registered")
	}
	if err := ipsec.ValidateRequestProfile(req, chassis); err != nil {
		return rejectIPsecIdentity(err.Error())
	}
	return nil
}

func validateIPsecBoundPod(namespace string, csr *certv1.CertificateSigningRequest, pod *corev1.Pod, ds *appsv1.DaemonSet) error {
	if csr.Spec.Username != "system:serviceaccount:"+namespace+":kube-ovn-cni" {
		return rejectIPsecIdentity("unexpected IPsec requester service account")
	}
	name, uid := csr.Spec.Extra["authentication.kubernetes.io/pod-name"], csr.Spec.Extra["authentication.kubernetes.io/pod-uid"]
	if len(name) != 1 || len(uid) != 1 {
		return rejectIPsecIdentity("IPsec requester needs a bound Pod identity")
	}
	if pod == nil || pod.Name != name[0] || pod.Namespace != namespace || string(pod.UID) != uid[0] || pod.Spec.ServiceAccountName != "kube-ovn-cni" || pod.DeletionTimestamp != nil {
		return rejectIPsecIdentity("IPsec requester Pod identity is no longer valid")
	}
	owner := metav1.GetControllerOf(pod)
	if ds == nil || ds.DeletionTimestamp != nil || ds.Namespace != namespace || owner == nil || owner.APIVersion != "apps/v1" || owner.Kind != "DaemonSet" || owner.Name != "kube-ovn-cni" || owner.UID != ds.UID || pod.Spec.NodeName == "" {
		return rejectIPsecIdentity("IPsec requester is not a live CNI DaemonSet Pod")
	}
	return nil
}
