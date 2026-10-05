package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/kubeovn/kube-ovn/pkg/ko"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx)
	stop()
	os.Exit(code)
}

func run(ctx context.Context) int {
	streams := genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	if err := ko.New(streams).Execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ko.ExitCode(err)
	}
	return 0
}
