package kohelper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cancellationRunner struct {
	started   chan struct{}
	cancelled chan struct{}
	finished  chan struct{}
}

func (r cancellationRunner) Run(ctx context.Context, _ Request, _, _ io.Writer) Result {
	close(r.started)
	<-ctx.Done()
	close(r.cancelled)
	<-r.finished
	return Result{Code: 130, Error: ctx.Err().Error()}
}

func TestServeWaitsForCancelledRunner(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	runner := cancellationRunner{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	finish := sync.OnceFunc(func() { close(runner.finished) })
	t.Cleanup(finish)
	serverDone := make(chan error, 1)
	go func() { serverDone <- Serve(t.Context(), serverConn, runner) }()
	client, err := Dial(clientConn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	requestDone := make(chan error, 1)
	go func() {
		requestDone <- Run(t.Context(), client, Request{Version: Version, Argv: []string{"wait"}}, io.Discard, io.Discard)
	}()
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
	// A lost exec connection must cancel the runner and wait for its cleanup.
	require.NoError(t, clientConn.Close())
	select {
	case <-runner.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("disconnected request did not cancel the runner")
	}
	select {
	case err := <-serverDone:
		t.Fatalf("helper returned before command cleanup: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	finish()
	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not return after command cleanup")
	}
	require.Error(t, <-requestDone)
}

type testRunner struct {
	result Result
}

func (r testRunner) Run(_ context.Context, request Request, stdout, stderr io.Writer) Result {
	_, _ = stdout.Write([]byte(request.Argv[0]))
	_, _ = stderr.Write([]byte("diagnostic"))
	return r.result
}

func TestRunOverAttachStream(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- Serve(context.Background(), serverConn, testRunner{}) }()
	client, err := Dial(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = Run(ctx, client, Request{Version: Version, Argv: []string{"show"}}, &stdout, &stderr)
	_ = client.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "show" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); got != "diagnostic" {
		t.Fatalf("stderr = %q", got)
	}
	if err := <-serverDone; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func TestRunPreservesRemoteExitCode(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- Serve(context.Background(), serverConn, testRunner{result: Result{Code: 17, Error: "command failed"}})
	}()
	client, err := Dial(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	err = Run(t.Context(), client, Request{Version: Version, Argv: []string{"show"}}, io.Discard, io.Discard)
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error = %v, want ExitError", err)
	}
	if exit.ExitStatus() != 17 {
		t.Fatalf("exit status = %d, want 17", exit.ExitStatus())
	}
	_ = client.Close()
	if err := <-serverDone; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func TestRunRejectsUnsupportedVersion(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- Serve(context.Background(), serverConn, testRunner{}) }()
	client, err := Dial(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	err = Run(t.Context(), client, Request{Version: Version + 1, Argv: []string{"show"}}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "without success") {
		t.Fatalf("error = %v, want unsupported-version stream failure", err)
	}
	_ = client.Close()
	if err := <-serverDone; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}
