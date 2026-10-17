package ipsec

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Certificates parses the entire bundle. Invalid blocks must not silently
// replace an already working trust configuration with a partial bundle.
func Certificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for len(bytes.TrimSpace(data)) != 0 {
		data = bytes.TrimSpace(data)
		if !bytes.HasPrefix(data, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, errors.New("unexpected data in certificate PEM bundle")
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, errors.New("invalid certificate PEM bundle")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		certs = append(certs, cert)
		data = rest
	}
	if len(certs) == 0 {
		return nil, errors.New("empty certificate bundle")
	}
	return certs, nil
}

func privateKey(data []byte) (*rsa.PrivateKey, error) {
	data = bytes.TrimSpace(data)
	if !bytes.HasPrefix(data, []byte("-----BEGIN RSA PRIVATE KEY-----")) && !bytes.HasPrefix(data, []byte("-----BEGIN PRIVATE KEY-----")) {
		return nil, errors.New("unexpected data in private key PEM")
	}
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid private key PEM")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		var err error
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		var ok bool
		if key, ok = parsed.(*rsa.PrivateKey); !ok {
			return nil, errors.New("IPsec requires an RSA private key")
		}
	default:
		return nil, errors.New("unsupported private key format")
	}
	if key.N.BitLen() < 2048 {
		return nil, errors.New("IPsec RSA key must be at least 2048 bits")
	}
	return key, key.Validate()
}

func newPrivateKey() ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func newCSR(keyPEM []byte, chassis string) ([]byte, error) {
	key, err := privateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: chassis, Country: []string{"CN"}, Organization: []string{"kubeovn"}, OrganizationalUnit: []string{"kube-ovn"}},
		DNSNames: []string{chassis},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func validateIdentity(certPEM, keyPEM, trustPEM []byte, chassis string, now time.Time) (*x509.Certificate, error) {
	key, err := privateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return ValidateCertificate(certPEM, trustPEM, &key.PublicKey, chassis, now)
}

// ValidateCertificate checks an issuer result before the controller publishes
// it or the node activates it. The controller never needs a node private key.
func ValidateCertificate(certPEM, trustPEM []byte, publicKey *rsa.PublicKey, chassis string, now time.Time) (*x509.Certificate, error) {
	certs, err := Certificates(certPEM)
	if err != nil {
		return nil, err
	}
	leaf := certs[0]
	pub, ok := leaf.PublicKey.(*rsa.PublicKey)
	if !ok || publicKey == nil || !pub.Equal(publicKey) {
		return nil, errors.New("certificate does not match the private key")
	}
	if leaf.IsCA || leaf.Subject.CommonName != chassis || !slices.Equal(leaf.DNSNames, []string{chassis}) || len(leaf.IPAddresses)+len(leaf.URIs)+len(leaf.EmailAddresses) != 0 {
		return nil, errors.New("certificate does not match the IPsec chassis identity")
	}
	if err := validateChassisNames(leaf.Subject, leaf.Extensions, chassis); err != nil {
		return nil, err
	}
	if leaf.KeyUsage != 0 && leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return nil, errors.New("IPsec certificate does not allow digital signatures")
	}
	if leaf.KeyUsage&(x509.KeyUsageCertSign|x509.KeyUsageCRLSign) != 0 {
		return nil, errors.New("IPsec leaf certificate must not allow CA signing")
	}
	for _, usage := range leaf.ExtKeyUsage {
		if usage != x509.ExtKeyUsageIPSECTunnel {
			return nil, errors.New("certificate has a non-IPsec extended key usage")
		}
	}
	for _, usage := range leaf.UnknownExtKeyUsage {
		if usage.String() != "1.3.6.1.5.5.7.3.17" && usage.String() != "1.3.6.1.5.5.8.2.2" {
			return nil, errors.New("certificate has an unsupported extended key usage")
		}
	}
	trust, err := Certificates(trustPEM)
	if err != nil {
		return nil, err
	}
	roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
	for _, ca := range trust {
		if !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
			return nil, errors.New("trust bundle contains a non-CA certificate")
		}
		roots.AddCert(ca)
	}
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return nil, fmt.Errorf("verify IPsec identity: %w", err)
	}
	return leaf, nil
}

// ValidateRequestProfile is shared by both signing backends. Go's parsed SAN
// fields alone cannot reject unsupported GeneralName types in the raw CSR.
func ValidateRequestProfile(req *x509.CertificateRequest, chassis string) error {
	if err := req.CheckSignature(); err != nil {
		return err
	}
	if chassis == "" || req.Subject.CommonName != chassis || !slices.Equal(req.DNSNames, []string{chassis}) || len(req.IPAddresses)+len(req.URIs)+len(req.EmailAddresses) != 0 {
		return errors.New("IPsec CSR must request only its bound node chassis")
	}
	key, ok := req.PublicKey.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < 2048 {
		return errors.New("IPsec CSR requires an RSA key of at least 2048 bits")
	}
	for _, ext := range req.Extensions {
		if ext.Id.String() != "2.5.29.17" {
			return errors.New("unexpected IPsec CSR extension")
		}
	}
	return validateChassisNames(req.Subject, req.Extensions, chassis)
}

func validateChassisNames(subject pkix.Name, extensions []pkix.Extension, chassis string) error {
	cnCount := 0
	for _, name := range subject.Names {
		if name.Type.String() == "2.5.4.3" {
			cnCount++
		}
	}
	if cnCount > 1 {
		return errors.New("IPsec identity must not contain multiple common names")
	}
	sanCount := 0
	for _, ext := range extensions {
		if ext.Id.String() != "2.5.29.17" {
			continue
		}
		sanCount++
		var names []asn1.RawValue
		rest, err := asn1.Unmarshal(ext.Value, &names)
		if err != nil || len(rest) != 0 || len(names) != 1 || names[0].Class != asn1.ClassContextSpecific || names[0].Tag != 2 || names[0].IsCompound || string(names[0].Bytes) != chassis {
			return errors.New("IPsec SAN must contain exactly one chassis DNS name")
		}
	}
	if sanCount != 1 {
		return errors.New("IPsec identity must contain exactly one SAN extension")
	}
	return nil
}
