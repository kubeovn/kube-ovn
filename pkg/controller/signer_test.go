package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"
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
