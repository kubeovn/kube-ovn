// Command ko-helper is the process installed in the independent node-agent
// Pod. Each --stdio process receives a request over its own stdin/stdout and never needs a
// kube-ovn-cni or ovs-ovn container.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/kohelper"
)

type stdioConn struct{}

func (stdioConn) Read(p []byte) (int, error)       { return os.Stdin.Read(p) }
func (stdioConn) Write(p []byte) (int, error)      { return os.Stdout.Write(p) }
func (stdioConn) Close() error                     { return nil }
func (stdioConn) LocalAddr() net.Addr              { return netAddr("stdin") }
func (stdioConn) RemoteAddr() net.Addr             { return netAddr("kubectl-ko") }
func (stdioConn) SetDeadline(time.Time) error      { return nil }
func (stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (stdioConn) SetWriteDeadline(time.Time) error { return nil }

type netAddr string

func (a netAddr) Network() string { return "attach" }
func (a netAddr) String() string  { return string(a) }

type runner struct{}

func (runner) Run(ctx context.Context, request kohelper.Request, stdout, stderr io.Writer) kohelper.Result {
	if len(request.Argv) == 0 || request.Argv[0] == "" {
		return kohelper.Result{Code: 2, Error: "helper request has no command"}
	}
	// Remote argv execution is intentional: pods/exec authorizes access to this
	// privileged tool runner. Arguments are passed directly, without a shell.
	command := exec.CommandContext(ctx, request.Argv[0], request.Argv[1:]...) // #nosec G204 -- Kubernetes-authorized remote tool execution.
	configureProcessGroup(command)
	command.WaitDelay = 5 * time.Second
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if ctx.Err() != nil {
		return kohelper.Result{Code: 130, Error: ctx.Err().Error()}
	}
	if err == nil {
		return kohelper.Result{}
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return kohelper.Result{Code: exit.ExitCode(), Error: err.Error()}
	}
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, context.Canceled) {
		return kohelper.Result{Code: 130, Error: err.Error()}
	}
	return kohelper.Result{Code: 1, Error: err.Error()}
}

func main() {
	os.Exit(runHelper())
}

func runHelper() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) == 1 {
		// The DaemonSet stays idle; Kubernetes exec starts isolated RPC processes.
		<-ctx.Done()
		return 0
	}
	if len(os.Args) == 3 && os.Args[1] == "netns" {
		path, err := kohelper.ResolvePodNetns(ctx, "/host/proc", os.Args[2])
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_, err = fmt.Fprintln(os.Stdout, path)
		if err != nil {
			return 1
		}
		return 0
	}
	if len(os.Args) == 3 && os.Args[1] == "ipsec" {
		if err := collectIPsec(ctx, "/host/proc", os.Args[2], os.Stdout, os.Stderr); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(os.Args) != 2 || os.Args[1] != "--stdio" {
		return 2
	}
	if err := kohelper.Serve(ctx, stdioConn{}, runner{}); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		return 1
	}
	return 0
}
