package util

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

const kubeOVNTLSOrganization = "kube-ovn"

// GenerateKubeOVNTLSSecretData returns the legacy cacert/cert/key Secret data
// used by OVN components.
func GenerateKubeOVNTLSSecretData(now time.Time, caDuration, certDuration time.Duration, commonName string) (map[string][]byte, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate kube-ovn-tls CA key: %w", err)
	}
	caSerial, err := newCertificateSerialNumber()
	if err != nil {
		return nil, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber: caSerial,
		Subject: pkix.Name{
			CommonName:   commonName + "-ca",
			Organization: []string{kubeOVNTLSOrganization},
		},
		NotBefore:             now.Add(-time.Second),
		NotAfter:              now.Add(caDuration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caCert, err := createCertificate(caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create kube-ovn-tls CA certificate: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate kube-ovn-tls key: %w", err)
	}
	serial, err := newCertificateSerialNumber()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{kubeOVNTLSOrganization},
		},
		NotBefore:             now.Add(-time.Second),
		NotAfter:              now.Add(certDuration),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:              []string{commonName},
		BasicConstraintsValid: true,
	}
	cert, err := createCertificate(template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create kube-ovn-tls certificate: %w", err)
	}

	caCertPEM, err := encodeCertificates(caCert)
	if err != nil {
		return nil, fmt.Errorf("encode kube-ovn-tls CA certificate: %w", err)
	}
	certPEM, err := encodeCertificates(cert)
	if err != nil {
		return nil, fmt.Errorf("encode kube-ovn-tls certificate: %w", err)
	}

	return map[string][]byte{
		"cacert": caCertPEM,
		"cert":   certPEM,
		"key":    encodeRSAPrivateKeyPEM(key),
	}, nil
}

func newCertificateSerialNumber() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}
	return serial, nil
}

func createCertificate(template, issuer *x509.Certificate, publicKey, issuerKey any) (*x509.Certificate, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, publicKey, issuerKey)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse generated certificate: %w", err)
	}
	return cert, nil
}

func encodeCertificates(certs ...*x509.Certificate) ([]byte, error) {
	b := bytes.Buffer{}
	for _, cert := range certs {
		if err := pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

func encodeRSAPrivateKeyPEM(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}
