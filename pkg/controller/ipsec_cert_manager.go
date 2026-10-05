package controller

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	cminformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"
	certv1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const ipsecRequestLabel = "kube-ovn.io/ipsec-request"

func (c *Controller) watchIPsecCertificateRequests(ctx context.Context) {
	factory := cminformers.NewSharedInformerFactoryWithOptions(c.config.CertManagerClient, 0,
		cminformers.WithNamespace(c.config.PodNamespace),
		cminformers.WithTweakListOptions(func(opts *metav1.ListOptions) { opts.LabelSelector = ipsecRequestLabel + "=true" }))
	requests := factory.Certmanager().V1().CertificateRequests().Informer()
	enqueue := func(obj any) {
		request := obj.(*cmv1.CertificateRequest)
		for _, owner := range request.OwnerReferences {
			if owner.APIVersion == "certificates.k8s.io/v1" && owner.Kind == "CertificateSigningRequest" {
				c.addOrUpdateCsrQueue.Add(owner.Name)
			}
		}
	}
	if _, err := requests.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: enqueue, UpdateFunc: func(_, obj any) { enqueue(obj) }}); err != nil {
		klog.ErrorS(err, "Watch IPsec CertificateRequests")
		return
	}
	factory.Start(ctx.Done())
	defer factory.Shutdown()
	<-ctx.Done()
}

// Node requests always cross the built-in CSR authorization path. Only the
// controller can forward an authorized request to the cert-manager issuer.
// This keeps cert-manager's generic approver out of node identity decisions.
func (c *Controller) signIPsecWithCertManager(csr *certv1.CertificateSigningRequest) error {
	if c.config.CertManagerClient == nil || c.config.CertManagerIssuerName == "" || csr.UID == "" {
		return errors.New("IPsec cert-manager backend is not configured")
	}
	duration := 2 * 365 * 24 * time.Hour
	if csr.Spec.ExpirationSeconds != nil {
		duration = min(duration, time.Duration(*csr.Spec.ExpirationSeconds)*time.Second)
	}
	if duration < 10*time.Minute {
		return c.updateCSRStatusConditions(csr, "InvalidDuration", "IPsec requested lifetime is too short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.verifyIPsecIssuerPolicy(ctx); err != nil {
		return err
	}
	client := c.config.CertManagerClient.CertmanagerV1().CertificateRequests(c.config.PodNamespace)
	hash := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%s:%s", csr.UID, csr.Spec.Request, c.config.CertManagerIssuerName, duration))
	req := &cmv1.CertificateRequest{
		Name: fmt.Sprintf("ovn-ipsec-%x", hash[:24]), Namespace: c.config.PodNamespace,
		Labels:          map[string]string{ipsecRequestLabel: "true"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "certificates.k8s.io/v1", Kind: "CertificateSigningRequest", Name: csr.Name, UID: csr.UID}},
		Spec: cmv1.CertificateRequestSpec{
			Request: csr.Spec.Request, Duration: &metav1.Duration{Duration: duration},
			IssuerRef: cmmeta.IssuerReference{Name: c.config.CertManagerIssuerName, Kind: "ClusterIssuer", Group: "cert-manager.io"},
			Usages:    []cmv1.KeyUsage{cmv1.UsageIPsecTunnel},
		},
	}
	created, err := client.Get(ctx, req.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		created, err = client.Create(ctx, req, metav1.CreateOptions{})
		if k8serrors.IsAlreadyExists(err) {
			created, err = client.Get(ctx, req.Name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(created.Spec.Request, req.Spec.Request) || created.Spec.IsCA || created.Spec.IssuerRef != req.Spec.IssuerRef || !slices.Equal(created.Spec.Usages, req.Spec.Usages) || created.Spec.Duration == nil || *created.Spec.Duration != *req.Spec.Duration || !slices.Equal(created.OwnerReferences, req.OwnerReferences) || created.Spec.Username != "system:serviceaccount:"+c.config.PodNamespace+":ovn" {
		return errors.New("IPsec CertificateRequest conflicts with the authorized controller request")
	}
	approved, ready := false, false
	for _, condition := range created.Status.Conditions {
		if condition.Type == cmv1.CertificateRequestConditionDenied && condition.Status == cmmeta.ConditionTrue || condition.Type == cmv1.CertificateRequestConditionReady && condition.Reason == "Failed" {
			return c.updateCSRStatusConditions(csr, "IssuerRejected", "cert-manager rejected the authorized IPsec request")
		}
		approved = approved || condition.Type == cmv1.CertificateRequestConditionApproved && condition.Status == cmmeta.ConditionTrue
		ready = ready || condition.Type == cmv1.CertificateRequestConditionReady && condition.Status == cmmeta.ConditionTrue
	}
	if created.Status.FailureTime != nil {
		return c.updateCSRStatusConditions(csr, "IssuerFailed", "cert-manager failed the authorized IPsec request")
	}
	if !approved || !ready || len(created.Status.Certificate) == 0 {
		// Do not block a CSR worker while the external signer is unavailable.
		c.addOrUpdateCsrQueue.AddAfter(csr.Name, time.Minute)
		return nil
	}
	request, err := decodeCertificateRequest(csr.Spec.Request)
	if err != nil {
		return err
	}
	trust, err := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Get(ctx, util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err != nil {
		return err
	}
	leaf, err := ipsec.ValidateCertificate(created.Status.Certificate, trust.Data["cacert"], request.PublicKey.(*rsa.PublicKey), request.Subject.CommonName, time.Now())
	if err != nil {
		return err
	}
	if leaf.NotAfter.After(leaf.NotBefore.Add(duration + time.Minute)) {
		return c.updateCSRStatusConditions(csr, "InvalidDuration", "cert-manager certificate exceeds the authorized lifetime")
	}
	csr.Status.Certificate = slices.Clone(created.Status.Certificate)
	return c.updateCsrStatus(csr)
}
