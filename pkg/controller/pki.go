package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const ipsecSignerVersionAnnotation = "kube-ovn.io/ipsec-signer-version"

// InitDefaultOVNIPsecCA prepares the private signer before publishing trust.
// An interrupted migration is resumed with the original root, never a new CA.
func (c *Controller) InitDefaultOVNIPsecCA() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	secrets := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace)
	signer, err := secrets.Get(ctx, util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	trust, trustErr := secrets.Get(ctx, util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if trustErr != nil && !k8serrors.IsNotFound(trustErr) {
		return trustErr
	}
	if k8serrors.IsNotFound(err) {
		var cert, key []byte
		if trustErr == nil {
			if len(trust.Data["cakey"]) == 0 {
				return errors.New("existing IPsec trust has no private signer; refusing to replace the CA")
			}
			key = trust.Data["cakey"]
			cert, err = matchingIPsecCA(trust.Data["cacert"], key)
			if err != nil {
				return err
			}
		} else {
			cert, key, err = newIPsecCA()
			if err != nil {
				return err
			}
		}
		if err := validateIPsecCA(cert, key); err != nil {
			return err
		}
		signer, err = secrets.Create(ctx, &corev1.Secret{Name: util.DefaultOVNIPSecSigner, Namespace: c.config.PodNamespace, Data: map[string][]byte{"cacert": cert, "cakey": key}}, metav1.CreateOptions{})
		if k8serrors.IsAlreadyExists(err) {
			signer, err = secrets.Get(ctx, util.DefaultOVNIPSecSigner, metav1.GetOptions{})
		}
		if err != nil {
			return err
		}
	}
	if err := validateIPsecCA(signer.Data["cacert"], signer.Data["cakey"]); err != nil {
		return err
	}
	if k8serrors.IsNotFound(trustErr) {
		trust, err = secrets.Create(ctx, &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: c.config.PodNamespace, Data: map[string][]byte{"cacert": signer.Data["cacert"]}}, metav1.CreateOptions{})
		if k8serrors.IsAlreadyExists(err) {
			trust, err = secrets.Get(ctx, util.DefaultOVNIPSecCA, metav1.GetOptions{})
		}
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(trust.Data["cacert"], signer.Data["cacert"]) {
		// Existing bundles may contain additional roots during a rotation. They
		// must include the active signer, but their overlap must not be discarded.
		bundle, err := ipsec.Certificates(trust.Data["cacert"])
		if err != nil {
			return err
		}
		active, err := decodeCertificate(signer.Data["cacert"])
		if err != nil {
			return err
		}
		found := false
		for _, cert := range bundle {
			if bytes.Equal(cert.Raw, active.Raw) {
				found = true
			}
		}
		if !found {
			return errors.New("public IPsec trust does not contain the active signer CA")
		}
	}
	klog.Info("IPsec private signer and public trust are ready")
	return nil
}

func matchingIPsecCA(bundle, keyPEM []byte) ([]byte, error) {
	key, err := decodePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	certs, err := ipsec.Certificates(bundle)
	if err != nil {
		return nil, err
	}
	for _, cert := range certs {
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok || !pub.Equal(&key.PublicKey) {
			continue
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
		if err := validateIPsecCA(certPEM, keyPEM); err != nil {
			return nil, err
		}
		return certPEM, nil
	}
	return nil, errors.New("IPsec trust has no CA matching the legacy signing key")
}

func validateIPsecCA(certPEM, keyPEM []byte) error {
	cert, err := decodeCertificate(certPEM)
	if err != nil {
		return err
	}
	key, err := decodePrivateKey(keyPEM)
	if err != nil {
		return err
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < 2048 || !pub.Equal(&key.PublicKey) || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return errors.New("IPsec CA certificate and signing key are invalid or mismatched")
	}
	if now := time.Now(); now.Before(cert.NotBefore) || !now.Add(10*time.Minute).Before(cert.NotAfter) {
		return errors.New("IPsec signing CA is not currently valid for issuance")
	}
	return nil
}

func newIPsecCA() ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Kube-OVN IPsec CA", Organization: []string{"kubeovn"}}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

// MarkIPsecSignerCompatibility runs before leader election, including on
// followers. It acknowledges the running binary, not just a new Pod template.
func MarkIPsecSignerCompatibility(ctx context.Context, config *Configuration) error {
	if !config.EnableOVNIPSec || config.CertManagerIPSecCert {
		return nil
	}
	pods := config.KubeClient.CoreV1().Pods(config.PodNamespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod, err := pods.Get(ctx, config.PodName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pod.DeletionTimestamp != nil {
			return errors.New("cannot acknowledge a terminating IPsec signer Pod")
		}
		if pod.Annotations[ipsecSignerVersionAnnotation] == "2" {
			return nil
		}
		// UID and resourceVersion preconditions prevent acknowledging a new
		// Pod incarnation or replacing unrelated fields during a conflict.
		patch := fmt.Appendf(nil, `{"metadata":{"uid":%q,"resourceVersion":%q,"annotations":{%q:"2"}}}`, pod.UID, pod.ResourceVersion, ipsecSignerVersionAnnotation)
		_, err = pods.Patch(ctx, pod.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	})
}

// finalizeIPsecCAFormat keeps old controllers working during a mixed rollout.
// It removes the legacy public key field only after every live controller Pod
// has acknowledged the new signer schema. Root rotation is a separate action.
func (c *Controller) finalizeIPsecCAFormat(ctx context.Context) error {
	secrets := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace)
	trust, err := secrets.Get(ctx, util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(trust.Data["cakey"]) == 0 {
		return nil
	}
	deployment, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Get(ctx, "kube-ovn-controller", metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return err
	}
	pods, err := c.config.KubeClient.CoreV1().Pods(c.config.PodNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return errors.New("no controller Pods available for CA migration")
	}
	live := 0
	self := false
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		live++
		self = self || pod.Name == c.config.PodName
		if pod.Spec.ServiceAccountName != "ovn" || pod.Annotations[ipsecSignerVersionAnnotation] != "2" {
			return nil
		}
		controller := metav1.GetControllerOf(&pod)
		if controller == nil || controller.Kind != "ReplicaSet" {
			return errors.New("unexpected controller Pod owner during IPsec CA migration")
		}
		rs, err := c.config.KubeClient.AppsV1().ReplicaSets(c.config.PodNamespace).Get(ctx, controller.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		rsOwner := metav1.GetControllerOf(rs)
		if rs.UID != controller.UID || rsOwner == nil || rsOwner.APIVersion != "apps/v1" || rsOwner.Kind != "Deployment" || rsOwner.Name != deployment.Name || rsOwner.UID != deployment.UID {
			return fmt.Errorf("controller Pod %s has inconsistent deployment ownership", pod.Name)
		}
	}
	if live == 0 || !self || deployment.Spec.Replicas == nil || live < int(*deployment.Spec.Replicas) {
		return errors.New("incomplete live controller acknowledgements for CA migration")
	}
	signer, err := secrets.Get(ctx, util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := validateIPsecCA(signer.Data["cacert"], signer.Data["cakey"]); err != nil {
		return err
	}
	if !bytes.Equal(signer.Data["cakey"], trust.Data["cakey"]) {
		return errors.New("legacy IPsec signer key changed during migration")
	}
	trust = trust.DeepCopy()
	delete(trust.Data, "cakey")
	_, err = secrets.Update(ctx, trust, metav1.UpdateOptions{})
	return err
}
