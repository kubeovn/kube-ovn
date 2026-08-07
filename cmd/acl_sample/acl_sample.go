package acl_sample

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"sigs.k8s.io/yaml"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

const (
	CommandName                   = "kube-ovn-acl-sample"
	defaultOVNTimeout             = 60
	defaultOVSDBConnectTimeout    = 3
	defaultOVSDBInactivityTimeout = 10
)

type sampleResolver interface {
	ResolveNetworkPolicyACLSample(aclsampling.SampleReference) (*aclsampling.Event, error)
	Close()
}

type dependencies struct {
	newResolver func(string) (sampleResolver, error)
	listen      func(context.Context, uint32, func(aclsampling.PacketSample) error) error
}

func defaultDependencies() dependencies {
	return dependencies{
		newResolver: func(address string) (sampleResolver, error) {
			return ovs.NewOvnNbClient(
				address,
				defaultOVNTimeout,
				defaultOVSDBConnectTimeout,
				defaultOVSDBInactivityTimeout,
				0,
			)
		},
		listen: aclsampling.ListenPSamples,
	}
}

// CmdMain runs the ACL sampling debug command selected through the
// kube-ovn-acl-sample image symlink.
func CmdMain() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, defaultDependencies()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, deps dependencies) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("an ACL sample subcommand is required")
	}

	switch args[0] {
	case "decode":
		return runDecode(args[1:], stdout, stderr, deps.newResolver)
	case "listen":
		return runListen(ctx, args[1:], stdout, stderr, deps.listen)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown ACL sample subcommand %q", args[0])
	}
}

func runDecode(args []string, stdout, stderr io.Writer, newResolver func(string) (sampleResolver, error)) error {
	flags := flag.NewFlagSet("decode", flag.ContinueOnError)
	flags.SetOutput(stderr)
	ovnNBAddress := flags.String("ovn-nb-addr", "", "OVN northbound database address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *ovnNBAddress == "" {
		return errors.New("--ovn-nb-addr is required")
	}
	if flags.NArg() != 1 {
		return errors.New("decode requires exactly one cookie or metadata value")
	}

	reference, err := aclsampling.ParseSampleReference(flags.Arg(0))
	if err != nil {
		return err
	}
	resolver, err := newResolver(*ovnNBAddress)
	if err != nil {
		return fmt.Errorf("connect to OVN northbound database: %w", err)
	}
	defer resolver.Close()

	event, err := resolver.ResolveNetworkPolicyACLSample(reference)
	if err != nil {
		return fmt.Errorf("resolve ACL sample: %w", err)
	}
	output, err := yaml.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode ACL sample event: %w", err)
	}
	_, err = stdout.Write(output)
	return err
}

func runListen(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	listen func(context.Context, uint32, func(aclsampling.PacketSample) error) error,
) error {
	flags := flag.NewFlagSet("listen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	groupID := flags.Uint("group-id", uint(aclsampling.DefaultLocalGroupID), "local psample multicast group ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("listen does not accept positional arguments")
	}
	if uint64(*groupID) > uint64(^uint32(0)) {
		return fmt.Errorf("psample group ID %d exceeds the uint32 range", *groupID)
	}

	handle := func(sample aclsampling.PacketSample) error {
		cookie, err := sample.Reference.Cookie()
		if err != nil {
			return fmt.Errorf("format psample cookie: %w", err)
		}
		_, err = fmt.Fprintf(stdout, "0x%016x\n", cookie)
		return err
	}
	if err := listen(ctx, uint32(*groupID), handle); err != nil {
		return fmt.Errorf("listen for ACL psamples: %w", err)
	}
	return nil
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintf(output, "  %s decode --ovn-nb-addr <address> <cookie-or-metadata>\n", CommandName)
	fmt.Fprintf(output, "  %s listen [--group-id <id>]\n", CommandName)
}
