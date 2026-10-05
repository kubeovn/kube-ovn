package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/kohelper"
)

func TestHelperProcess(_ *testing.T) {
	if os.Getenv("KO_HELPER_TEST_PROCESS") != "1" {
		return
	}
	os.Args = []string{"kubectl-ko-node-agent", "--stdio"}
	main()
}

func TestRemoteTool(_ *testing.T) {
	if os.Getenv("KO_HELPER_TEST_PROCESS") != "1" {
		return
	}
	_, _ = os.Stdout.Write([]byte{0, 255, 13, 10, 1})
	_, _ = os.Stderr.WriteString("tool diagnostic")
	os.Exit(17)
}

func TestHelperStdioProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, executable, "-test.run=^TestHelperProcess$")
	process.Env = append(os.Environ(), "KO_HELPER_TEST_PROCESS=1")
	stdin, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	process.Stderr = &diagnostics
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	connection := &kohelper.StreamConn{Reader: io.NopCloser(stdout), Writer: stdin}
	client, err := kohelper.Dial(connection)
	if err != nil {
		_ = connection.Close()
		_ = process.Wait()
		t.Fatal(err)
	}
	var output, stderr bytes.Buffer
	err = kohelper.Run(ctx, client, kohelper.Request{Version: kohelper.Version, Argv: []string{executable, "-test.run=^TestRemoteTool$"}}, &output, &stderr)
	_ = client.Close()
	_ = connection.Close()
	// Keep stdout open until the helper exits after receiving stdin EOF.
	_, _ = io.Copy(io.Discard, stdout)
	if exit, ok := errors.AsType[*kohelper.ExitError](err); !ok || exit.Code != 17 {
		t.Errorf("remote status = %v, want exit 17", err)
	}
	if !bytes.Equal(output.Bytes(), []byte{0, 255, 13, 10, 1}) || stderr.String() != "tool diagnostic" {
		t.Errorf("corrupted tool streams: %v / %q", output.Bytes(), stderr.String())
	}
	if err = process.Wait(); err != nil {
		t.Fatalf("helper did not exit cleanly: %v (%s)", err, diagnostics.String())
	}
}
