package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func TestCNIExecutionFindsBundledToolsWithoutHostTools(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, "tools")
	if err := os.Mkdir(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"ovs-vsctl", "ethtool"} {
		if err := os.WriteFile(filepath.Join(toolsDir, tool), []byte("#!/bin/sh\nprintf 'bundled-tool\\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", "/missing-host-tools")
	if err := configureExecutionTools(filepath.Join(dir, "kube-ovn")); err != nil {
		t.Fatal(err)
	}
	output, err := ovs.Exec("show")
	if err != nil || output != "bundled-tool" {
		t.Fatalf("OVS execution failed: output=%q error=%v", output, err)
	}
	outputBytes, err := exec.Command("ethtool", "--version").CombinedOutput()
	if err != nil || string(outputBytes) != "bundled-tool\n" {
		t.Fatalf("ethtool execution failed: output=%q error=%v", outputBytes, err)
	}
}

func TestCNIExecutionRepairsDeletedWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, "tools")
	if err := os.Mkdir(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"ovs-vsctl", "ethtool"} {
		if err := os.WriteFile(filepath.Join(toolsDir, tool), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkingDirectory) })
	deletedWorkingDirectory := t.TempDir()
	if err := os.Chdir(deletedWorkingDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(deletedWorkingDirectory); err != nil {
		t.Fatal(err)
	}

	if err := configureExecutionTools(filepath.Join(dir, "kube-ovn")); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if workingDirectory != "/" {
		t.Fatalf("working directory = %q, want /", workingDirectory)
	}
}
