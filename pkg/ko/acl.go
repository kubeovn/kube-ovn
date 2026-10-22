package ko

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const aclHelper = "/kube-ovn/kube-ovn-acl-sample"

func (a *Application) addACLCommands() {
	command := &cobra.Command{Use: "acl", Short: "Decode and listen for NetworkPolicy ACL samples"}
	command.AddCommand(&cobra.Command{
		Use: "decode COOKIE", Args: cobra.ExactArgs(1), Short: "Decode one cookie or metadata value",
		RunE: a.run(func(ctx context.Context, client *Client, args []string) error {
			target, err := client.leader(ctx, "nb")
			if err != nil {
				return err
			}
			return client.Executor.Exec(ctx, target, aclDecodeArgs(args[0]), a.outputStreams())
		}),
	})
	var node string
	listen := &cobra.Command{Use: "listen --node NODE", Args: cobra.NoArgs, Short: "Stream decoded samples from a node"}
	listen.Flags().StringVar(&node, "node", "", "Node to listen on")
	listen.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.NoArgs(cmd, args); err != nil {
			return err
		}
		return validateResourceName("node", node)
	}
	listen.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		return a.listenACL(ctx, client, node)
	})
	command.AddCommand(listen)
	a.root.AddCommand(command)
}

func aclDecodeArgs(cookie string) []string {
	return []string{aclHelper, "decode", "--ovn-nb-addr=unix:/var/run/ovn/ovnnb_db.sock", cookie}
}

func (a *Application) listenACL(ctx context.Context, client *Client, node string) error {
	ds, err := client.Kubernetes.AppsV1().DaemonSets(client.Namespace).Get(ctx, "kube-ovn-cni", metav1.GetOptions{})
	if err != nil {
		return err
	}
	enabled, group := false, ""
	for _, container := range ds.Spec.Template.Spec.Containers {
		if container.Name != "cni-server" {
			continue
		}
		for _, arg := range container.Args {
			if value, ok := strings.CutPrefix(arg, "--enable-acl-sampling="); ok {
				enabled = value == "true"
			}
			if value, ok := strings.CutPrefix(arg, "--acl-sampling-local-group-id="); ok {
				group = value
			}
		}
	}
	if !enabled {
		return errors.New("ACL sampling is not enabled on the kube-ovn-cni DaemonSet")
	}
	if _, err := strconv.ParseUint(group, 10, 32); err != nil {
		return fmt.Errorf("invalid ACL sampling group ID: %w", err)
	}
	source, err := client.nodeTarget(ctx, node, "kube-ovn-cni")
	if err != nil {
		return err
	}
	target, err := client.leader(ctx, "nb")
	if err != nil {
		return err
	}
	return a.streamACL(ctx, client, source, target, group)
}

func (a *Application) streamACL(ctx context.Context, client *Client, source, target Target, group string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := client.Executor.Exec(ctx, source, []string{aclHelper, "listen", "--group-id=" + group}, Streams{Out: writer, ErrOut: a.streams.ErrOut})
		done <- errors.Join(err, writer.CloseWithError(err))
	}()
	scanErr := a.decodeACLStream(ctx, client, target, reader)
	cancel()
	closeErr := reader.Close()
	return errors.Join(scanErr, closeErr, <-done)
}

func (a *Application) decodeACLStream(ctx context.Context, client *Client, target Target, input io.Reader) error {
	scanner := bufio.NewScanner(input)
	// Decode synchronously to apply bounded backpressure, rather than queueing unbounded execs.
	for scanner.Scan() {
		cookie := strings.TrimSpace(scanner.Text())
		if cookie == "" {
			continue
		}
		output, err := client.capture(ctx, target, aclDecodeArgs(cookie)...)
		if err != nil {
			if _, writeErr := fmt.Fprintf(a.streams.ErrOut, "decode sample %q: %v\n", cookie, err); writeErr != nil {
				return writeErr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if _, err := fmt.Fprintf(a.streams.Out, "---\n%s", output); err != nil {
			return err
		}
		if !strings.HasSuffix(output, "\n") {
			if _, err := io.WriteString(a.streams.Out, "\n"); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
