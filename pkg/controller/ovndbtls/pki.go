package ovndbtls

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"time"
)

const (
	defaultKeySize = 2048
)

type CA struct {
	CertPEM []byte
	KeyPEM  []byte
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
}

type LeafSpec struct {
	CommonName  string
	DNSNames    []string
	IPAddresses []net.IP
	ExtKeyUsage []x509.ExtKeyUsage
	Duration    time.Duration
}

func GenerateCA(now time.Time, commonName string, duration time.Duration) (*CA, error) {
	key, err := rsa.GenerateKey(rand.Reader, defaultKeySize)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}

	serialNumber, err := serialNumber()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"kube-ovn"},
		},
		NotBefore:             now.Add(-time.Second),
		NotAfter:              now.Add(duration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse generated CA certificate: %w", err)
	}

	keyPEM, err := encodePrivateKey(key)
	if err != nil {
		return nil, err
	}

	return &CA{
		CertPEM: encodeCertificate(certDER),
		KeyPEM:  keyPEM,
		Cert:    cert,
		Key:     key,
	}, nil
}

func ParseCA(certPEM, keyPEM []byte) (*CA, error) {
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}
	if !cert.IsCA {
		return nil, errors.New("certificate is not a CA")
	}

	key, err := ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}

	return &CA{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
		Cert:    cert,
		Key:     key,
	}, nil
}

func SignLeaf(now time.Time, ca *CA, spec LeafSpec) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, defaultKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("generate leaf key: %w", err)
	}

	serialNumber, err := serialNumber()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   spec.CommonName,
			Organization: []string{"kube-ovn"},
		},
		NotBefore:             now.Add(-time.Second),
		NotAfter:              now.Add(spec.Duration),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           spec.ExtKeyUsage,
		DNSNames:              spec.DNSNames,
		IPAddresses:           spec.IPAddresses,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, nil, fmt.Errorf("create leaf certificate: %w", err)
	}

	keyPEM, err = encodePrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	certPEM = encodeCertificate(certDER)
	if err := ValidateKeyPair(certPEM, keyPEM, spec.ExtKeyUsage); err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

func ValidateKeyPair(certPEM, keyPEM []byte, expectedUsages []x509.ExtKeyUsage) error {
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return err
	}
	key, err := ParsePrivateKey(keyPEM)
	if err != nil {
		return err
	}
	if !cert.PublicKey.(*rsa.PublicKey).Equal(key.Public()) {
		return errors.New("certificate public key does not match private key")
	}
	for _, expected := range expectedUsages {
		if !slices.Contains(cert.ExtKeyUsage, expected) {
			return fmt.Errorf("certificate ExtKeyUsage %v does not contain %v", cert.ExtKeyUsage, expected)
		}
	}
	return nil
}

func ParseCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate PEM block type must be CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

func ParsePrivateKey(keyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("private key PEM block type must be PRIVATE KEY")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rsaKey, nil
}

func encodeCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodePrivateKey(key *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func serialNumber() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}
	return serial, nil
}

func certBundle(certs ...[]byte) []byte {
	var b bytes.Buffer
	for _, cert := range certs {
		b.Write(bytes.TrimSpace(cert))
		b.WriteByte('\n')
	}
	return b.Bytes()
}
