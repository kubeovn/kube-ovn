package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPingerHelpDoesNotClaimExternalAddressDefault(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	bin := filepath.Join(tmpDir, CmdPinger)
	build := exec.Command("go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build %s: %v\n%s", CmdPinger, err, output)
	}

	cmd := exec.Command(bin, "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run %s --help: %v\n%s", CmdPinger, err, output)
	}

	help := string(output)
	if strings.Contains(help, "default: 1.1.1.1") {
		t.Fatalf("%s --help must not claim an external address default when the actual default is empty:\n%s", CmdPinger, help)
	}
}
