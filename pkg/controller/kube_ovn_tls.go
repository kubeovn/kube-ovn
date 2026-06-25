package controller

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	kubeOVNTLSSecretName              = "kube-ovn-tls" // #nosec G101 -- Kubernetes Secret resource name, not a credential.
	kubeOVNTLSDefaultRotationInterval = 365 * 24 * time.Hour
	kubeOVNTLSCADuration              = 10 * 365 * 24 * time.Hour
	kubeOVNTLSCertDuration            = 10 * 365 * 24 * time.Hour
	kubeOVNTLSCommonName              = "ovn"

	kubeOVNTLSCertHashAnnotation = "kube-ovn.io/kube-ovn-tls-cert-hash"
)

func (c *Controller) startKubeOVNTLSManager(ctx context.Context) {
	if os.Getenv(util.EnvSSLEnabled) != "true" {
		return
	}
	interval, err := kubeOVNTLSRotationInterval()
	if err != nil {
		klog.Errorf("failed to parse kube-ovn TLS rotation interval: %v", err)
		return
	}
	if interval <= 0 {
		return
	}

	go wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := c.reconcileKubeOVNTLS(ctx); err != nil {
			klog.Errorf("failed to reconcile kube-ovn TLS secret: %v", err)
		}
	}, interval)
}

func kubeOVNTLSRotationInterval() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(util.EnvKubeOVNTLSRotationInterval))
	if value == "" {
		return kubeOVNTLSDefaultRotationInterval, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", util.EnvKubeOVNTLSRotationInterval, value, err)
	}
	return interval, nil
}

func (c *Controller) reconcileKubeOVNTLS(ctx context.Context) error {
	secret, err := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Get(ctx, kubeOVNTLSSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		data, hash, genErr := generateKubeOVNTLSData(time.Now(), kubeOVNTLSCADuration, kubeOVNTLSCertDuration)
		if genErr != nil {
			return genErr
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubeOVNTLSSecretName,
				Namespace: c.config.PodNamespace,
				Annotations: map[string]string{
					kubeOVNTLSCertHashAnnotation: hash,
				},
			},
			Data: data,
		}
		if _, err = c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}

	// kube-ovn-tls keeps the legacy cacert/cert/key schema. The manager only
	// adds metadata on first adoption so upgrades do not restart OVN workloads.
	hash, err := kubeOVNTLSHash(secret.Data)
	if err != nil {
		return err
	}

	if secret.Annotations[kubeOVNTLSCertHashAnnotation] != hash {
		if err = c.setKubeOVNTLSHash(ctx, hash); err != nil {
			return err
		}
		return nil
	}

	renew, err := kubeOVNTLSNeedsRenewal(time.Now(), secret.Data)
	if err != nil {
		return err
	}
	if renew {
		data, newHash, genErr := generateKubeOVNTLSData(time.Now(), kubeOVNTLSCADuration, kubeOVNTLSCertDuration)
		if genErr != nil {
			return genErr
		}
		if err = c.updateKubeOVNTLSSecretData(ctx, data, newHash); err != nil {
			return err
		}
	}

	return nil
}

func kubeOVNTLSNeedsRenewal(now time.Time, data map[string][]byte) (bool, error) {
	cert, err := parseKubeOVNTLSCert(data)
	if err != nil {
		return false, err
	}
	refreshTime := cert.NotBefore.Add(cert.NotAfter.Sub(cert.NotBefore) / 2)
	return !now.Before(refreshTime), nil
}

func parseKubeOVNTLSCert(data map[string][]byte) (*x509.Certificate, error) {
	cert, err := decodeCertificate(data["cert"])
	if err != nil {
		return nil, fmt.Errorf("parse kube-ovn-tls cert: %w", err)
	}
	return cert, nil
}

func generateKubeOVNTLSData(now time.Time, caDuration, certDuration time.Duration) (map[string][]byte, string, error) {
	data, err := util.GenerateKubeOVNTLSSecretData(now, caDuration, certDuration, kubeOVNTLSCommonName)
	if err != nil {
		return nil, "", err
	}
	hash, err := kubeOVNTLSHash(data)
	if err != nil {
		return nil, "", err
	}
	return data, hash, nil
}

func kubeOVNTLSHash(data map[string][]byte) (string, error) {
	for _, key := range []string{"cacert", "cert", "key"} {
		if len(data[key]) == 0 {
			return "", fmt.Errorf("kube-ovn-tls missing %s", key)
		}
	}
	h := sha256.New()
	for _, key := range []string{"cacert", "cert", "key"} {
		h.Write([]byte(key))
		h.Write([]byte{0})
		h.Write(data[key])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *Controller) updateKubeOVNTLSSecretData(ctx context.Context, data map[string][]byte, hash string) error {
	secrets := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := secrets.Get(ctx, kubeOVNTLSSecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		secret = secret.DeepCopy()
		secret.Data = data
		setKubeOVNTLSAnnotation(secret, kubeOVNTLSCertHashAnnotation, hash)
		_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
}

func (c *Controller) setKubeOVNTLSHash(ctx context.Context, hash string) error {
	secrets := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := secrets.Get(ctx, kubeOVNTLSSecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		secret = secret.DeepCopy()
		setKubeOVNTLSAnnotation(secret, kubeOVNTLSCertHashAnnotation, hash)
		_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
}

func setKubeOVNTLSAnnotation(secret *corev1.Secret, key, value string) {
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[key] = value
}
