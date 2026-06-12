package ovndbtls

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	CASecretName     = "ovn-db-tls-ca"     // #nosec G101 -- Kubernetes Secret name, not a credential
	ServerSecretName = "ovn-db-tls-server" // #nosec G101 -- Kubernetes Secret name, not a credential
	ClientSecretName = "ovn-db-tls-client" // #nosec G101 -- Kubernetes Secret name, not a credential

	KeyCACert      = "ca.crt"
	KeyCAKey       = "ca.key"
	KeyTrustBundle = "ca-bundle.crt"
	KeyServerCert  = "server.crt"
	KeyServerKey   = "server.key"
	KeyClientCert  = "client.crt"
	KeyClientKey   = "client.key"

	keyNextCACert = "next-ca.crt"
	keyNextCAKey  = "next-ca.key"
)

const (
	AnnotationCertHash      = "kube-ovn.io/ovn-db-tls-cert-hash"
	AnnotationCAHash        = "kube-ovn.io/ovn-db-tls-ca-hash"
	AnnotationRotationStage = "kube-ovn.io/ovn-db-tls-rotation-stage"
	AnnotationStageStarted  = "kube-ovn.io/ovn-db-tls-stage-started-at"
	AnnotationNotBefore     = "kube-ovn.io/ovn-db-tls-not-before"
	AnnotationNotAfter      = "kube-ovn.io/ovn-db-tls-not-after"
)

const (
	StageStable         = "stable"
	StageTrustExpanding = "trust-expanding"
	StageLeafReissuing  = "leaf-reissuing"
	StageTrustPruning   = "trust-pruning"
)

func newSecret(namespace, name string, data map[string][]byte, annotations map[string]string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
}

func secretHash(data map[string][]byte, keys ...string) string {
	h := sha256.New()
	for _, key := range keys {
		h.Write([]byte(key))
		h.Write([]byte{0})
		h.Write(data[key])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func certificateAnnotations(certPEM []byte, extra map[string]string) (map[string]string, error) {
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}
	annotations := map[string]string{
		AnnotationNotBefore: cert.NotBefore.UTC().Format(time.RFC3339),
		AnnotationNotAfter:  cert.NotAfter.UTC().Format(time.RFC3339),
	}
	maps.Copy(annotations, extra)
	return annotations, nil
}

func parseAnnotationTime(secret *corev1.Secret, key string) (time.Time, error) {
	value := secret.Annotations[key]
	if value == "" {
		return time.Time{}, fmt.Errorf("missing annotation %s", key)
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse annotation %s: %w", key, err)
	}
	return t, nil
}

func rotationStage(secret *corev1.Secret) string {
	if secret == nil || secret.Annotations == nil || secret.Annotations[AnnotationRotationStage] == "" {
		return StageStable
	}
	return secret.Annotations[AnnotationRotationStage]
}

func setAnnotation(secret *corev1.Secret, key, value string) {
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[key] = value
}
