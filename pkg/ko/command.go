package ko

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/util/term"

	"github.com/kubeovn/kube-ovn/versions"
)

// Application owns configuration and clients for one invocation.
type Application struct {
	root             *cobra.Command
	config           *genericclioptions.ConfigFlags
	streams          genericiooptions.IOStreams
	namespace        string
	discoveryTimeout time.Duration
	timeout          time.Duration
	client           *Client
	// newClient is replaced by tests to exercise command parsing without a cluster.
	newClient func() (*Client, error)
}

// New constructs a command tree without accessing kubeconfig or the API server.
func New(streams genericiooptions.IOStreams) *Application {
	a := &Application{streams: streams, config: genericclioptions.NewConfigFlags(true)}
	a.root = &cobra.Command{
		Use: "kubectl-ko", Short: "Operate and diagnose Kube-OVN through the Kubernetes API",
		SilenceErrors: true, SilenceUsage: true, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	a.root.SetIn(streams.In)
	a.root.SetOut(streams.Out)
	a.root.SetErr(streams.ErrOut)
	flags := a.root.PersistentFlags()
	a.config.AddFlags(flags)
	flags.StringVar(&a.namespace, "kube-ovn-namespace", cmp.Or(os.Getenv("KUBE_OVN_NS"), "kube-system"), "Namespace containing Kube-OVN components")
	flags.DurationVar(&a.discoveryTimeout, "discovery-timeout", 10*time.Second, "Time to wait for a unique ready target")
	flags.DurationVar(&a.timeout, "timeout", 0, "Overall command timeout (zero allows long-running streams)")
	a.root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &usageError{err} })
	a.newClient = a.connect
	a.addControlCommands()
	a.addDatabaseCommands()
	a.addNetworkCommands()
	a.addDiagnosticCommands()
	a.addPerformanceCommand()
	a.addACLCommands()
	a.root.AddCommand(&cobra.Command{
		Use: "version", Short: "Print the client build version without contacting a cluster", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(streams.Out, "kubectl-ko %s (%s)\n", versions.VERSION, versions.COMMIT)
			return err
		},
	})
	wrapArgumentErrors(a.root)
	return a
}

// Execute allows standard flags anywhere before a remote-command separator.
func (a *Application) Execute(ctx context.Context, args []string) error {
	started := false
	a.root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		started = true
		if a.discoveryTimeout <= 0 || a.timeout < 0 {
			return &usageError{errors.New("timeouts must be positive (overall timeout may be zero)")}
		}
		return nil
	}
	a.root.SetArgs(args)
	err := a.root.ExecuteContext(ctx)
	if err != nil && !started {
		return &usageError{err}
	}
	return err
}

// Argument errors are consistently distinguished from remote execution failures.
func wrapArgumentErrors(command *cobra.Command) {
	if validate := command.Args; validate != nil {
		command.Args = func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, args); err != nil {
				return &usageError{err}
			}
			return nil
		}
	}
	for _, child := range command.Commands() {
		wrapArgumentErrors(child)
	}
}

func remoteArguments(cmd *cobra.Command, args []string) error {
	if cmd.ArgsLenAtDash() > 0 || len(args) != 0 && cmd.ArgsLenAtDash() < 0 {
		return errors.New("put all remote arguments after --; use -- --help for remote tool help")
	}
	return nil
}

func (a *Application) connect() (*Client, error) {
	config, err := a.config.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	namespace, _, err := a.config.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return nil, err
	}
	return &Client{
		Kubernetes: client, Dynamic: dynamicClient,
		Executor:  &helperExecutor{client: client, legacy: &remoteExecutor{client: client, config: config}},
		Namespace: a.namespace, WorkloadNamespace: namespace, DiscoveryTimeout: a.discoveryTimeout,
		ComponentFree: true,
	}, nil
}

func (a *Application) run(handler func(context.Context, *Client, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if a.timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, a.timeout)
			defer cancel()
		}
		if a.client == nil {
			var err error
			a.client, err = a.newClient()
			if err != nil {
				return err
			}
		}
		return handler(ctx, a.client, args)
	}
}

func (a *Application) outputStreams() Streams {
	return Streams{Out: a.streams.Out, ErrOut: a.streams.ErrOut}
}

func (a *Application) addControlCommands() {
	var container, selector string
	var stdin, tty, quiet bool
	var podRunningTimeout time.Duration
	parent := &cobra.Command{
		Use:   "exec (POD | TYPE/NAME) [-c CONTAINER] [flags] -- COMMAND [args...]",
		Short: "Execute a command in a container or run a Kube-OVN tool",
		Args: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 {
				return errors.New("put the command after --")
			}
			if len(args[dash:]) == 0 {
				return errors.New("you must specify at least one command for the container")
			}
			if selector != "" {
				if dash != 0 {
					return errors.New("--selector cannot be combined with a resource reference")
				}
			} else if dash != 1 {
				return errors.New("pod or type/name must be specified before --")
			}
			if podRunningTimeout <= 0 {
				return errors.New("pod-running-timeout must be positive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			return a.run(func(ctx context.Context, client *Client, args []string) error {
				reference := ""
				if selector == "" {
					reference = args[0]
				}
				streams := a.outputStreams()
				if stdin {
					streams.In = a.streams.In
				}
				streams.TTY = tty && stdin
				if streams.TTY && !(term.TTY{In: streams.In}).IsTerminalIn() {
					if !quiet && streams.ErrOut != nil {
						_, _ = fmt.Fprintln(streams.ErrOut, "Unable to use a TTY - input is not a terminal or the right kind of file")
					}
					streams.TTY = false
				}
				return client.exec(ctx, reference, selector, container, quiet, podRunningTimeout, streams, args[dash:])
			})(cmd, args)
		},
	}
	parent.Flags().StringVarP(&container, "container", "c", "", "Container name. If omitted, use the first container")
	parent.Flags().StringVarP(&selector, "selector", "l", "", "Selector (label query) to filter Pods")
	parent.Flags().BoolVarP(&stdin, "stdin", "i", false, "Pass stdin to the container")
	parent.Flags().BoolVarP(&tty, "tty", "t", false, "Stdin is a TTY")
	parent.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only print output from the remote session")
	parent.Flags().DurationVar(&podRunningTimeout, "pod-running-timeout", time.Minute, "The length of time to wait until at least one Pod is running")
	for _, role := range []string{"nb", "sb", "ic-nb", "ic-sb"} {
		name := role + "ctl"
		binary := "ovn-" + name
		parent.AddCommand(&cobra.Command{
			Use: name + " [flags] -- [TOOL_ARGS...]", DisableFlagsInUseLine: true, Short: "Invoke " + binary + " on its leader", Args: remoteArguments,
			RunE: a.run(func(ctx context.Context, client *Client, args []string) error {
				target, err := client.leader(ctx, role)
				if err != nil {
					return err
				}
				return client.Executor.Exec(ctx, target, append([]string{binary}, args...), a.outputStreams())
			}),
		})
	}
	for _, name := range []string{"vsctl", "ofctl", "dpctl", "appctl"} {
		var node string
		command := &cobra.Command{
			Use: name + " --node NODE [flags] -- [TOOL_ARGS...]", DisableFlagsInUseLine: true, Short: "Invoke ovs-" + name + " on a node",
			Args: func(cmd *cobra.Command, args []string) error {
				if err := validateResourceName("node", node); err != nil {
					return err
				}
				return remoteArguments(cmd, args)
			},
			RunE: a.run(func(ctx context.Context, client *Client, args []string) error {
				target, err := client.nodeTarget(ctx, node, "ovs")
				if err != nil {
					return err
				}
				return client.Executor.Exec(ctx, target, append([]string{"ovs-" + name}, args...), a.outputStreams())
			}),
		}
		command.Flags().StringVar(&node, "node", "", "Node hosting the OVS container")
		parent.AddCommand(command)
	}
	a.root.AddCommand(parent)
}
