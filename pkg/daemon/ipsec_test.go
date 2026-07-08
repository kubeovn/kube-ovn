package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCSRCodeIncludesIPSecTunnelEKU(t *testing.T) {
	tempDir := t.TempDir()

	oldReqPath := ipsecReqPath
	ipsecReqPath = filepath.Join(tempDir, "ipsec-req.pem")
	t.Cleanup(func() {
		ipsecReqPath = oldReqPath
	})

	argsPath := filepath.Join(tempDir, "openssl.args")
	t.Setenv("TEST_OPENSSL_ARGS", argsPath)

	binDir := filepath.Join(tempDir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestScript(t, filepath.Join(binDir, "ovs-vsctl"), `#!/bin/sh
printf 'node-1\n'
`)
	writeTestScript(t, filepath.Join(binDir, "openssl"), `#!/bin/sh
printf '%s\n' "$*" > "$TEST_OPENSSL_ARGS"
out=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-out" ]; then
    shift
    out=$1
    break
  fi
  shift
done
[ -n "$out" ] || exit 1
printf 'csr-bytes' > "$out"
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	csr, err := generateCSRCode(filepath.Join(tempDir, "ipsec.key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(csr) != "csr-bytes" {
		t.Fatalf("unexpected csr bytes %q", csr)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	for _, want := range []string{
		"-addext subjectAltName = DNS:node-1",
		"-addext extendedKeyUsage = ipsecTunnel",
		"-subj /C=CN/O=kubeovn/OU=kube-ovn/CN=node-1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("openssl args %q do not contain %q", got, want)
		}
	}
}

func writeTestScript(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
