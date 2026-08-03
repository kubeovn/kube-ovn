package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandHelpDescriptions(t *testing.T) {
	tmpDir := t.TempDir()

	bins := buildCommandBinaries(t, tmpDir)
	for _, name := range []string{
		CmdController,
		CmdPinger,
		"kube-ovn-daemon",
		"kube-ovn-monitor",
		"kube-ovn-speaker",
		"kube-ovn-webhook",
		"kube-ovn-leader-checker",
		"kube-ovn-ic-controller",
	} {
		t.Run(name, func(t *testing.T) {
			help := runHelp(t, bins[name], name)
			if strings.Contains(help, "default:") {
				t.Fatalf("%s --help must rely on pflag's generated defaults instead of manual \"default:\" descriptions:\n%s", name, help)
			}
		})
	}

	for name, wants := range map[string][]string{
		CmdPinger: {
			"empty disables external dns check",
			"empty disables external ping check",
		},
		CmdController: {
			"when empty, the first IP in default-cidr is used",
			"when empty, the first IP in node-switch-cidr is used",
		},
		"kube-ovn-daemon": {
			"when empty, the interface that owns POD_IP or a node internal IP is used",
			"the node tunnel interface annotation and DPDK mode take precedence",
			"when set to 0, it is derived from the selected interface MTU based on the network type and IP family",
		},
		"kube-ovn-speaker": {
			"when empty, the IPv4 address from POD_IPS is used; if POD_IPS is empty, POD_IP is used; if no IPv4 address is available, 0.0.0.0 is used",
		},
	} {
		help := runHelp(t, bins[name], name)
		normalizedHelp := strings.ToLower(help)
		for _, want := range wants {
			if !strings.Contains(normalizedHelp, strings.ToLower(want)) {
				t.Fatalf("%s --help must contain %q:\n%s", name, want, help)
			}
		}
	}
}

func buildCommandBinaries(t *testing.T, tmpDir string) map[string]string {
	t.Helper()

	controllerBin := buildBinary(t, tmpDir, CmdController, ".")
	rootBin := buildBinary(t, tmpDir, "kube-ovn-monitor", "..")
	daemonBin := buildBinary(t, tmpDir, "kube-ovn-daemon", "../daemon")

	bins := map[string]string{
		CmdController:             controllerBin,
		CmdPinger:                 linkCommand(t, tmpDir, controllerBin, CmdPinger),
		"kube-ovn-daemon":         daemonBin,
		"kube-ovn-monitor":        rootBin,
		"kube-ovn-speaker":        linkCommand(t, tmpDir, rootBin, "kube-ovn-speaker"),
		"kube-ovn-webhook":        linkCommand(t, tmpDir, rootBin, "kube-ovn-webhook"),
		"kube-ovn-leader-checker": linkCommand(t, tmpDir, rootBin, "kube-ovn-leader-checker"),
		"kube-ovn-ic-controller":  linkCommand(t, tmpDir, rootBin, "kube-ovn-ic-controller"),
	}
	return bins
}

func buildBinary(t *testing.T, tmpDir, name, packageDir string) string {
	t.Helper()

	bin := filepath.Join(tmpDir, name)
	build := exec.Command("go", "build", "-o", bin, packageDir)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build %s from %s: %v\n%s", name, packageDir, err, output)
	}
	return bin
}

func linkCommand(t *testing.T, tmpDir, target, name string) string {
	t.Helper()

	link := filepath.Join(tmpDir, name)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("failed to link %s to %s: %v", link, target, err)
	}
	return link
}

func runHelp(t *testing.T, bin, name string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--help")
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("timed out running %s --help\n%s", name, output)
	}
	if err != nil && !strings.Contains(string(output), "Usage of") {
		t.Fatalf("failed to run %s --help: %v\n%s", name, err, output)
	}

	return string(output)
}
