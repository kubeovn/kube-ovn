package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func prepareExecutionTools() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate CNI execution bundle: %w", err)
	}
	return configureExecutionTools(executable)
}

// configureExecutionTools sets up the installed CNI execution environment.
// Running invocations keep their matching tools across a CNI symlink replacement.
func configureExecutionTools(executable string) error {
	// Container runtimes may invoke a CNI plugin after removing the directory
	// that was its current working directory. Shell-based tool wrappers emit a
	// getcwd error in that situation, and the diagnostic text can corrupt the
	// command's output. CNI paths are absolute, so use a stable directory before
	// starting any bundled helper.
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("reset CNI working directory: %w", err)
	}

	toolsDir := filepath.Join(filepath.Dir(executable), "tools")
	for _, tool := range []string{"ovs-vsctl", "ethtool"} {
		info, err := os.Stat(filepath.Join(toolsDir, tool))
		if err != nil {
			return fmt.Errorf("CNI execution bundle is incomplete (%s): %w", tool, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("CNI execution tool %s is not executable", tool)
		}
	}
	return os.Setenv("PATH", toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
