package controller

import (
	"bytes"
	"context"
	"crypto/x509"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestReconcileKubeOVNTLSAddsBaselineAnnotation(t *testing.T) {
	const namespace = "kube-system"
	data, hash, err := generateKubeOVNTLSData(time.Now(), kubeOVNTLSCADuration, kubeOVNTLSCertDuration)
	if err != nil {
		t.Fatalf("generateKubeOVNTLSData returned error: %v", err)
	}
	client := fake.NewSimpleClientset(
		testKubeOVNTLSSecret(namespace, data, nil),
	)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: namespace}}

	if err = c.reconcileKubeOVNTLS(context.Background()); err != nil {
		t.Fatalf("reconcileKubeOVNTLS returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(context.Background(), kubeOVNTLSSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get kube-ovn-tls secret: %v", err)
	}
	if got := secret.Annotations[kubeOVNTLSCertHashAnnotation]; got != hash {
		t.Fatalf("cert hash annotation = %q, want %q", got, hash)
	}
}

func TestReconcileKubeOVNTLSOnlyAdoptsExpiredLegacySecret(t *testing.T) {
	const namespace = "kube-system"
	expiredData, hash, err := generateKubeOVNTLSData(time.Now().Add(-20*24*time.Hour), 10*24*time.Hour, 10*24*time.Hour)
	if err != nil {
		t.Fatalf("generateKubeOVNTLSData returned error: %v", err)
	}
	client := fake.NewSimpleClientset(
		testKubeOVNTLSSecret(namespace, expiredData, nil),
	)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: namespace}}

	if err = c.reconcileKubeOVNTLS(context.Background()); err != nil {
		t.Fatalf("reconcileKubeOVNTLS returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(context.Background(), kubeOVNTLSSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get kube-ovn-tls secret: %v", err)
	}
	if got := secret.Annotations[kubeOVNTLSCertHashAnnotation]; got != hash {
		t.Fatalf("cert hash annotation = %q, want %q", got, hash)
	}
	assertSecretDataEqual(t, secret.Data, expiredData)
}

func TestReconcileKubeOVNTLSRotatesExpiredSecret(t *testing.T) {
	const namespace = "kube-system"
	expiredData, oldHash, err := generateKubeOVNTLSData(time.Now().Add(-20*24*time.Hour), 10*24*time.Hour, 10*24*time.Hour)
	if err != nil {
		t.Fatalf("generateKubeOVNTLSData returned error: %v", err)
	}
	client := fake.NewSimpleClientset(
		testKubeOVNTLSSecret(namespace, expiredData, map[string]string{
			kubeOVNTLSCertHashAnnotation: oldHash,
		}),
	)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: namespace}}

	if err = c.reconcileKubeOVNTLS(context.Background()); err != nil {
		t.Fatalf("reconcileKubeOVNTLS returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(context.Background(), kubeOVNTLSSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get kube-ovn-tls secret: %v", err)
	}
	newHash := secret.Annotations[kubeOVNTLSCertHashAnnotation]
	if newHash == "" || newHash == oldHash {
		t.Fatalf("cert hash annotation = %q, want non-empty value different from %q", newHash, oldHash)
	}
}

func TestGenerateKubeOVNTLSDataAddsServingAndClientIdentity(t *testing.T) {
	data, _, err := generateKubeOVNTLSData(time.Now(), kubeOVNTLSCADuration, kubeOVNTLSCertDuration)
	if err != nil {
		t.Fatalf("generateKubeOVNTLSData returned error: %v", err)
	}

	cert, err := parseKubeOVNTLSCert(data)
	if err != nil {
		t.Fatalf("parseKubeOVNTLSCert returned error: %v", err)
	}

	if !containsExtKeyUsage(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Fatalf("leaf certificate ExtKeyUsage = %v, want ServerAuth", cert.ExtKeyUsage)
	}
	if !containsExtKeyUsage(cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		t.Fatalf("leaf certificate ExtKeyUsage = %v, want ClientAuth", cert.ExtKeyUsage)
	}
	if !containsString(cert.DNSNames, kubeOVNTLSCommonName) {
		t.Fatalf("leaf certificate DNSNames = %v, want %q", cert.DNSNames, kubeOVNTLSCommonName)
	}
}

func TestStartKubeOVNTLSManagerPeriodicallyRotatesExpiredSecret(t *testing.T) {
	const namespace = "kube-system"
	expiredData, oldHash, err := generateKubeOVNTLSData(time.Now().Add(-20*24*time.Hour), 10*24*time.Hour, 10*24*time.Hour)
	if err != nil {
		t.Fatalf("generateKubeOVNTLSData returned error: %v", err)
	}
	client := fake.NewSimpleClientset(
		testKubeOVNTLSSecret(namespace, expiredData, map[string]string{
			kubeOVNTLSCertHashAnnotation: oldHash,
		}),
	)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: namespace}}
	t.Setenv(util.EnvSSLEnabled, "true")
	t.Setenv(util.EnvKubeOVNTLSRotationInterval, "10ms")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c.startKubeOVNTLSManager(ctx)

	err = wait.PollUntilContextTimeout(context.Background(), 10*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, kubeOVNTLSSecretName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		newHash, err := kubeOVNTLSHash(secret.Data)
		if err != nil {
			return false, err
		}
		return newHash != oldHash && secret.Annotations[kubeOVNTLSCertHashAnnotation] == newHash, nil
	})
	if err != nil {
		t.Fatalf("startKubeOVNTLSManager did not rotate expired kube-ovn-tls secret: %v", err)
	}
}

func TestKubeOVNTLSRotationInterval(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty uses default", value: "", want: 365 * 24 * time.Hour},
		{name: "zero disables rotation", value: "0", want: 0},
		{name: "custom interval", value: "12h", want: 12 * time.Hour},
		{name: "invalid interval", value: "bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUBE_OVN_TLS_ROTATION_INTERVAL", tt.value)
			got, err := kubeOVNTLSRotationInterval()
			if tt.wantErr {
				if err == nil {
					t.Fatal("kubeOVNTLSRotationInterval returned nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("kubeOVNTLSRotationInterval returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("interval = %v, want %v", got, tt.want)
			}
		})
	}
}

func assertSecretDataEqual(t *testing.T, got, want map[string][]byte) {
	t.Helper()
	for _, key := range []string{"cacert", "cert", "key"} {
		if !bytes.Equal(got[key], want[key]) {
			t.Fatalf("secret data %s changed during adoption", key)
		}
	}
}

func containsExtKeyUsage(usages []x509.ExtKeyUsage, want x509.ExtKeyUsage) bool {
	return slices.Contains(usages, want)
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func testKubeOVNTLSSecret(namespace string, data map[string][]byte, annotations map[string]string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        kubeOVNTLSSecretName,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Data: data,
	}
}
