package ovn_central_controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSPolicyArgs(t *testing.T) {
	tests := []struct {
		name             string
		min, max, suites string
		want             []string
		wantErrContains  string
	}{
		{name: "no policy"},
		{
			name: "min only",
			min:  "TLS12",
			want: []string{
				"--ovn-nb-db-ssl-protocols=TLSv1.2,TLSv1.3",
				"--ovn-sb-db-ssl-protocols=TLSv1.2,TLSv1.3",
			},
		},
		{
			name: "max only",
			max:  "1.1",
			want: []string{
				"--ovn-nb-db-ssl-protocols=TLSv1,TLSv1.1",
				"--ovn-sb-db-ssl-protocols=TLSv1,TLSv1.1",
			},
		},
		{
			name:   "versions and mixed suites",
			min:    "TLS 1.2",
			max:    "TLS13",
			suites: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, TLS_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
			want: []string{
				"--ovn-nb-db-ssl-protocols=TLSv1.2,TLSv1.3",
				"--ovn-sb-db-ssl-protocols=TLSv1.2,TLSv1.3",
				"--ovn-nb-db-ssl-ciphers=ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA",
				"--ovn-sb-db-ssl-ciphers=ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA",
				"--ovn-nb-db-ssl-ciphersuites=TLS_AES_256_GCM_SHA384",
				"--ovn-sb-db-ssl-ciphersuites=TLS_AES_256_GCM_SHA384",
			},
		},
		{name: "min above max", min: "TLS13", max: "TLS12", wantErrContains: "must be less than or equal"},
		{name: "bad version", min: "SSL3", wantErrContains: "unsupported TLS version"},
		{name: "bad suite", suites: "TLS_NOPE", wantErrContains: "unsupported TLS cipher suite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tlsPolicyArgs(tt.min, tt.max, tt.suites)
			if tt.wantErrContains != "" {
				require.ErrorContains(t, err, tt.wantErrContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildOvnCtlArgsCarriesTLSPolicyOnlyWithSSL(t *testing.T) {
	policy, err := tlsPolicyArgs("TLS12", "", "TLS_AES_128_GCM_SHA256")
	require.NoError(t, err)
	base := Config{DBClusterAddr: "10.0.0.1", DBAddr: "::", NBPort: 6641, SBPort: 6642, NBClusterPort: 6643, SBClusterPort: 6644, tlsArgs: policy}

	ssl := base
	ssl.EnableSSL = true
	require.Subset(t, buildOvnCtlArgs(&ssl, nil), policy)

	plain := base
	for _, a := range policy {
		require.NotContains(t, buildOvnCtlArgs(&plain, nil), a)
	}
}

func TestWipeDBIsFencedOnContext(t *testing.T) {
	dir := t.TempDir()
	d := dbInfo{dbFile: filepath.Join(dir, "db"), hdrFile: filepath.Join(dir, "hdr")}
	require.NoError(t, os.WriteFile(d.dbFile, []byte("data"), 0o600))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, wipeDB(ctx, d), context.Canceled)
	require.FileExists(t, d.dbFile, "a cancelled (lease-lost) ctx must not delete the DB")

	require.NoError(t, wipeDB(t.Context(), d))
	require.NoFileExists(t, d.dbFile)
	require.NoError(t, wipeDB(t.Context(), d), "idempotent")
}
