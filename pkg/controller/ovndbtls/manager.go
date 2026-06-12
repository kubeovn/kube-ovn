package ovndbtls

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultCADuration    = 10 * 365 * 24 * time.Hour
	DefaultLeafDuration  = 365 * 24 * time.Hour
	DefaultRotationDelay = 10 * time.Minute
	defaultCACommonName  = "kube-ovn-ovn-db-ca"
	defaultServerCN      = "ovn-db"
	defaultClientCN      = "kube-ovn-ovn-db-client"
)

type Manager struct {
	client          kubernetes.Interface
	namespace       string
	now             func() time.Time
	caDuration      time.Duration
	leafDuration    time.Duration
	transitionDelay time.Duration
	serverDNSNames  []string
}

type Config struct {
	Client          kubernetes.Interface
	Namespace       string
	Now             func() time.Time
	CADuration      time.Duration
	LeafDuration    time.Duration
	TransitionDelay time.Duration
	ServerDNSNames  []string
}

func NewManager(config Config) *Manager {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	caDuration := config.CADuration
	if caDuration == 0 {
		caDuration = DefaultCADuration
	}
	leafDuration := config.LeafDuration
	if leafDuration == 0 {
		leafDuration = DefaultLeafDuration
	}
	transitionDelay := config.TransitionDelay
	if transitionDelay == 0 {
		transitionDelay = DefaultRotationDelay
	}
	serverDNSNames := config.ServerDNSNames
	if len(serverDNSNames) == 0 {
		serverDNSNames = []string{
			"ovn-nb",
			"ovn-nb." + config.Namespace,
			"ovn-nb." + config.Namespace + ".svc",
			"ovn-nb." + config.Namespace + ".svc.cluster.local",
			"ovn-sb",
			"ovn-sb." + config.Namespace,
			"ovn-sb." + config.Namespace + ".svc",
			"ovn-sb." + config.Namespace + ".svc.cluster.local",
		}
	}
	return &Manager{
		client:          config.Client,
		namespace:       config.Namespace,
		now:             now,
		caDuration:      caDuration,
		leafDuration:    leafDuration,
		transitionDelay: transitionDelay,
		serverDNSNames:  serverDNSNames,
	}
}

func (m *Manager) Reconcile(ctx context.Context) error {
	ca, err := m.ensureCASecret(ctx)
	if err != nil {
		return err
	}
	if ca, err = m.reconcileCARotation(ctx, ca); err != nil {
		return err
	}
	if err = m.ensureLeafSecret(ctx, ServerSecretName, KeyServerCert, KeyServerKey, LeafSpec{
		CommonName:  defaultServerCN,
		DNSNames:    m.serverDNSNames,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		Duration:    m.leafDuration,
	}, ca); err != nil {
		return err
	}
	return m.ensureLeafSecret(ctx, ClientSecretName, KeyClientCert, KeyClientKey, LeafSpec{
		CommonName:  defaultClientCN,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		Duration:    m.leafDuration,
	}, ca)
}

func (m *Manager) ensureCASecret(ctx context.Context) (*CA, error) {
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, CASecretName, metav1.GetOptions{})
	if err == nil {
		return ParseCA(secret.Data[KeyCACert], secret.Data[KeyCAKey])
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	ca, err := GenerateCA(m.now(), defaultCACommonName, m.caDuration)
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		KeyCACert:      ca.CertPEM,
		KeyCAKey:       ca.KeyPEM,
		KeyTrustBundle: ca.CertPEM,
	}
	annotations := map[string]string{
		AnnotationCAHash:        secretHash(data, KeyCACert, KeyCAKey, KeyTrustBundle),
		AnnotationRotationStage: StageStable,
		AnnotationNotBefore:     ca.Cert.NotBefore.UTC().Format(time.RFC3339),
		AnnotationNotAfter:      ca.Cert.NotAfter.UTC().Format(time.RFC3339),
	}
	if _, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, newSecret(m.namespace, CASecretName, data, annotations), metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return m.ensureCASecret(ctx)
		}
		return nil, err
	}
	return ca, nil
}

func (m *Manager) ensureLeafSecret(ctx context.Context, secretName, certKey, keyKey string, spec LeafSpec, ca *CA) error {
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	notFound := apierrors.IsNotFound(err)
	if err == nil {
		notBefore, beforeErr := parseAnnotationTime(secret, AnnotationNotBefore)
		notAfter, afterErr := parseAnnotationTime(secret, AnnotationNotAfter)
		if beforeErr == nil && afterErr == nil && !NeedsRenewal(m.now(), notBefore, notAfter) {
			return nil
		}
	}

	certPEM, keyPEM, err := SignLeaf(m.now(), ca, spec)
	if err != nil {
		return err
	}
	data := map[string][]byte{
		KeyCACert:      ca.CertPEM,
		KeyTrustBundle: ca.CertPEM,
		certKey:        certPEM,
		keyKey:         keyPEM,
	}
	annotations, err := certificateAnnotations(certPEM, map[string]string{
		AnnotationCertHash: secretHash(data, KeyCACert, KeyTrustBundle, certKey, keyKey),
		AnnotationCAHash:   secretHash(map[string][]byte{KeyCACert: ca.CertPEM}, KeyCACert),
	})
	if err != nil {
		return err
	}

	if notFound {
		_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, newSecret(m.namespace, secretName, data, annotations), metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return m.ensureLeafSecret(ctx, secretName, certKey, keyKey, spec, ca)
		}
		return err
	}
	secret = secret.DeepCopy()
	secret.Data = data
	secret.Annotations = annotations
	_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func (m *Manager) reconcileCARotation(ctx context.Context, activeCA *CA) (*CA, error) {
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, CASecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	stage := rotationStage(secret)
	switch stage {
	case StageStable:
		if !NeedsRenewal(m.now(), activeCA.Cert.NotBefore, activeCA.Cert.NotAfter) {
			return activeCA, nil
		}
		nextCA, err := GenerateCA(m.now(), defaultCACommonName, m.caDuration)
		if err != nil {
			return nil, err
		}
		secret = secret.DeepCopy()
		secret.Data[keyNextCACert] = nextCA.CertPEM
		secret.Data[keyNextCAKey] = nextCA.KeyPEM
		secret.Data[KeyTrustBundle] = certBundle(activeCA.CertPEM, nextCA.CertPEM)
		setAnnotation(secret, AnnotationRotationStage, StageTrustExpanding)
		setAnnotation(secret, AnnotationStageStarted, m.now().UTC().Format(time.RFC3339))
		setAnnotation(secret, AnnotationCAHash, secretHash(secret.Data, KeyCACert, KeyCAKey, KeyTrustBundle, keyNextCACert, keyNextCAKey))
		if _, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return nil, err
		}
		return activeCA, nil
	case StageTrustExpanding:
		if !m.stageDelayElapsed(secret) {
			return activeCA, nil
		}
		setAnnotation(secret, AnnotationRotationStage, StageLeafReissuing)
		setAnnotation(secret, AnnotationStageStarted, m.now().UTC().Format(time.RFC3339))
		if _, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return nil, err
		}
		nextCA, err := ParseCA(secret.Data[keyNextCACert], secret.Data[keyNextCAKey])
		if err != nil {
			return nil, err
		}
		if err := m.reissueAllLeafSecrets(ctx, nextCA, secret.Data[KeyTrustBundle]); err != nil {
			return nil, err
		}
		return nextCA, nil
	case StageLeafReissuing:
		if !m.stageDelayElapsed(secret) {
			nextCA, err := ParseCA(secret.Data[keyNextCACert], secret.Data[keyNextCAKey])
			if err != nil {
				return nil, err
			}
			return nextCA, nil
		}
		setAnnotation(secret, AnnotationRotationStage, StageTrustPruning)
		setAnnotation(secret, AnnotationStageStarted, m.now().UTC().Format(time.RFC3339))
		if _, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return nil, err
		}
		return ParseCA(secret.Data[keyNextCACert], secret.Data[keyNextCAKey])
	case StageTrustPruning:
		nextCA, err := ParseCA(secret.Data[keyNextCACert], secret.Data[keyNextCAKey])
		if err != nil {
			return nil, err
		}
		secret = secret.DeepCopy()
		secret.Data[KeyCACert] = nextCA.CertPEM
		secret.Data[KeyCAKey] = nextCA.KeyPEM
		secret.Data[KeyTrustBundle] = nextCA.CertPEM
		delete(secret.Data, keyNextCACert)
		delete(secret.Data, keyNextCAKey)
		setAnnotation(secret, AnnotationRotationStage, StageStable)
		delete(secret.Annotations, AnnotationStageStarted)
		setAnnotation(secret, AnnotationNotBefore, nextCA.Cert.NotBefore.UTC().Format(time.RFC3339))
		setAnnotation(secret, AnnotationNotAfter, nextCA.Cert.NotAfter.UTC().Format(time.RFC3339))
		setAnnotation(secret, AnnotationCAHash, secretHash(secret.Data, KeyCACert, KeyCAKey, KeyTrustBundle))
		if _, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return nil, err
		}
		return nextCA, nil
	default:
		return nil, fmt.Errorf("unknown OVN DB TLS CA rotation stage %q", stage)
	}
}

func (m *Manager) stageDelayElapsed(secret *corev1.Secret) bool {
	started, err := parseAnnotationTime(secret, AnnotationStageStarted)
	if err != nil {
		return true
	}
	return stageReady(m.now(), started, m.transitionDelay)
}

func (m *Manager) reissueAllLeafSecrets(ctx context.Context, ca *CA, trustBundle []byte) error {
	if err := m.reissueLeafSecret(ctx, ServerSecretName, KeyServerCert, KeyServerKey, LeafSpec{
		CommonName:  defaultServerCN,
		DNSNames:    m.serverDNSNames,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		Duration:    m.leafDuration,
	}, ca, trustBundle); err != nil {
		return err
	}
	return m.reissueLeafSecret(ctx, ClientSecretName, KeyClientCert, KeyClientKey, LeafSpec{
		CommonName:  defaultClientCN,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		Duration:    m.leafDuration,
	}, ca, trustBundle)
}

func (m *Manager) reissueLeafSecret(ctx context.Context, secretName, certKey, keyKey string, spec LeafSpec, ca *CA, trustBundle []byte) error {
	certPEM, keyPEM, err := SignLeaf(m.now(), ca, spec)
	if err != nil {
		return err
	}
	data := map[string][]byte{
		KeyCACert:      trustBundle,
		KeyTrustBundle: trustBundle,
		certKey:        certPEM,
		keyKey:         keyPEM,
	}
	annotations, err := certificateAnnotations(certPEM, map[string]string{
		AnnotationCertHash: secretHash(data, KeyCACert, KeyTrustBundle, certKey, keyKey),
		AnnotationCAHash:   secretHash(map[string][]byte{KeyCACert: ca.CertPEM}, KeyCACert),
	})
	if err != nil {
		return err
	}
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, newSecret(m.namespace, secretName, data, annotations), metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	secret = secret.DeepCopy()
	secret.Data = data
	secret.Annotations = annotations
	_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}
