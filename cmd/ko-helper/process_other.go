//go:build !linux

package main

import "os/exec"

// The node agent runs on Linux; other platforms compile it for protocol tests.
func configureProcessGroup(*exec.Cmd) {}
