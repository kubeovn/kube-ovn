package metrics

import (
	"crypto/tls"
	"slices"
	"testing"
)

func TestTLSVersionFromString(t *testing.T) {
	tests := []struct {
		input   string
		want    uint16
		wantErr bool
	}{
		{"1.0", tls.VersionTLS10, false},
		{"1.1", tls.VersionTLS11, false},
		{"1.2", tls.VersionTLS12, false},
		{"1.3", tls.VersionTLS13, false},
		{"TLS 1.0", tls.VersionTLS10, false},
		{"TLS 1.1", tls.VersionTLS11, false},
		{"TLS 1.2", tls.VersionTLS12, false},
		{"TLS 1.3", tls.VersionTLS13, false},
		{"TLS10", tls.VersionTLS10, false},
		{"TLS11", tls.VersionTLS11, false},
		{"TLS12", tls.VersionTLS12, false},
		{"TLS13", tls.VersionTLS13, false},
		{"", 0, false},
		{"auto", 0, false},
		{"default", 0, false},
		{"SSLv3", 0, true},
		{"foobar", 0, true},
	}
	for _, tt := range tests {
		version, err := TLSVersionFromString(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("TLSVersionFromString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if version != tt.want {
			t.Errorf("TLSVersionFromString(%q) = %d, want %d", tt.input, version, tt.want)
		}
	}
}

func TestCipherSuiteFromName(t *testing.T) {
	tests := []struct {
		input   string
		want    uint16
		wantErr bool
	}{
		{"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, false},
		{"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256, false},
		{"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256", tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256, false},
		{"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256", tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256, false},
		{"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256", tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256, false},
		{"", 0, true},
		{"foobar", 0, true},
	}
	for _, tt := range tests {
		suite, err := CipherSuiteFromName(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("CipherSuiteFromName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if suite != tt.want {
			t.Errorf("CipherSuiteFromName(%q) = %d, want %d", tt.input, suite, tt.want)
		}
	}
}

func TestCipherSuitesFromNames(t *testing.T) {
	tests := []struct {
		input   []string
		want    []uint16
		wantErr bool
	}{
		{nil, nil, false},
		{[]string{}, nil, false},
		{[]string{""}, nil, true},
		{[]string{"foobar"}, nil, true},
		{[]string{"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"}, []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384}, false},
		{[]string{"", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"}, nil, true},
		{[]string{"foobar", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"}, nil, true},
		{
			[]string{
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
			},
			[]uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			},
			false,
		},
	}
	for _, tt := range tests {
		suites, err := CipherSuitesFromNames(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("CipherSuitesFromNames(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if !slices.Equal(suites, tt.want) {
			t.Errorf("CipherSuitesFromNames(%v) = %v, want %v", tt.input, suites, tt.want)
		}
	}
}
