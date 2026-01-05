package util

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const envHelperProcess = "GO_WANT_HELPER_PROCESS"

func TestLogFatalAndExit(t *testing.T) {
	expectedMessage := "An error occurred: test error"

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(), envHelperProcess+"=1")
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()

	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ExitCode() != 1 {
			t.Fatalf("expected exit code 1, got %d", exitError.ExitCode())
		}
		if !strings.Contains(cmd.Stderr.(*bytes.Buffer).String(), expectedMessage) {
			t.Fatalf("expected error message %q, got %q", expectedMessage, cmd.Stderr.(*bytes.Buffer).String())
		}
	} else {
		t.Fatalf("expected an exit error, got %v", err)
	}
}

func TestHelperProcess(*testing.T) {
	if os.Getenv(envHelperProcess) != "1" {
		return
	}
	err := errors.New("test error")
	LogFatalAndExit(err, "An error occurred: %s", err.Error())
}
