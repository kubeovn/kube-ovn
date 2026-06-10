package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	csrv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestDecodePrivateKeyReturnsRSAPrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	got, err := decodePrivateKey(pemBytes)
	require.NoError(t, err)
	require.Equal(t, key.N, got.N)
}

func TestDecodePrivateKeyReturnsErrorForInvalidPEM(t *testing.T) {
	require.NotPanics(t, func() {
		_, err := decodePrivateKey([]byte("not pem"))
		require.Error(t, err)
	})
}

func TestCSRProfileDetection(t *testing.T) {
	tests := []struct {
		name    string
		csr     *csrv1.CertificateSigningRequest
		want    csrSignerProfile
		wantOK  bool
		wantCA  string
		wantUse []csrv1.KeyUsage
	}{
		{
			name: "ipsec csr",
			csr: newTestCSR("ovn-ipsec-node-a", []csrv1.KeyUsage{
				csrv1.UsageIPsecTunnel,
			}),
			want:   csrSignerProfileIPSec,
			wantOK: true,
			wantCA: util.DefaultOVNIPSecCA,
			wantUse: []csrv1.KeyUsage{
				csrv1.UsageIPsecTunnel,
			},
		},
		{
			name: "ovn db server csr",
			csr: newTestCSR("ovn-db-tls-server-ovn-central-0", []csrv1.KeyUsage{
				csrv1.UsageServerAuth,
			}),
			want:   csrSignerProfileOVNDBTLSServer,
			wantOK: true,
			wantCA: util.DefaultOVNDBTLSCA,
			wantUse: []csrv1.KeyUsage{
				csrv1.UsageServerAuth,
			},
		},
		{
			name: "ovn db client csr",
			csr: newTestCSR("ovn-db-tls-client-ovs-ovn-node-a", []csrv1.KeyUsage{
				csrv1.UsageClientAuth,
			}),
			want:   csrSignerProfileOVNDBTLSClient,
			wantOK: true,
			wantCA: util.DefaultOVNDBTLSCA,
			wantUse: []csrv1.KeyUsage{
				csrv1.UsageClientAuth,
			},
		},
		{
			name: "ovn db server csr rejects client auth usage",
			csr: newTestCSR("ovn-db-tls-server-ovn-central-0", []csrv1.KeyUsage{
				csrv1.UsageClientAuth,
			}),
			wantOK: false,
		},
		{
			name: "unknown csr",
			csr: newTestCSR("other-node-a", []csrv1.KeyUsage{
				csrv1.UsageClientAuth,
			}),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := getCSRSignerProfile(tt.csr)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.name != tt.want {
				t.Fatalf("profile = %q, want %q", got.name, tt.want)
			}
			if got.caSecretName != tt.wantCA {
				t.Fatalf("caSecretName = %q, want %q", got.caSecretName, tt.wantCA)
			}
			if len(got.usages) != len(tt.wantUse) {
				t.Fatalf("usages = %v, want %v", got.usages, tt.wantUse)
			}
			for i := range got.usages {
				if got.usages[i] != tt.wantUse[i] {
					t.Fatalf("usages = %v, want %v", got.usages, tt.wantUse)
				}
			}
		})
	}
}

func TestNewCertificateTemplateForProfile(t *testing.T) {
	certReq := &x509.CertificateRequest{
		DNSNames:    []string{"ovn-sb.kube-system.svc"},
		IPAddresses: []net.IP{net.ParseIP("10.96.0.10")},
	}

	serverTemplate := newCertificateTemplateForProfile(certReq, csrSignerProfileOVNDBTLSServer)
	if len(serverTemplate.ExtKeyUsage) != 1 || serverTemplate.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatalf("server ExtKeyUsage = %v, want serverAuth", serverTemplate.ExtKeyUsage)
	}
	if len(serverTemplate.DNSNames) != 1 || serverTemplate.DNSNames[0] != certReq.DNSNames[0] {
		t.Fatalf("server DNSNames = %v, want %v", serverTemplate.DNSNames, certReq.DNSNames)
	}
	if len(serverTemplate.IPAddresses) != 1 || !serverTemplate.IPAddresses[0].Equal(certReq.IPAddresses[0]) {
		t.Fatalf("server IPAddresses = %v, want %v", serverTemplate.IPAddresses, certReq.IPAddresses)
	}

	clientTemplate := newCertificateTemplateForProfile(certReq, csrSignerProfileOVNDBTLSClient)
	if len(clientTemplate.ExtKeyUsage) != 1 || clientTemplate.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("client ExtKeyUsage = %v, want clientAuth", clientTemplate.ExtKeyUsage)
	}

	ipsecTemplate := newCertificateTemplateForProfile(certReq, csrSignerProfileIPSec)
	if len(ipsecTemplate.ExtKeyUsage) != 0 {
		t.Fatalf("ipsec ExtKeyUsage = %v, want empty to preserve existing behavior", ipsecTemplate.ExtKeyUsage)
	}
}

func newTestCSR(name string, usages []csrv1.KeyUsage) *csrv1.CertificateSigningRequest {
	return &csrv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: csrv1.CertificateSigningRequestSpec{
			SignerName: util.SignerName,
			Usages:     usages,
		},
	}
}
