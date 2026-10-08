package ko

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type collectionOptions struct {
	output      string
	concurrency int
	timeout     time.Duration
	maxBytes    int64
	strict      bool
}

type collectionTask struct {
	Target   Target `json:"target"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationNanoseconds"`
	collect  func(context.Context) error
}

func (a *Application) addLogCommand() {
	options := collectionOptions{}
	var component string
	command := &cobra.Command{Use: "logs", Aliases: []string{"log"}, Short: "Collect component logs and node state into a local directory"}
	command.Flags().StringVar(&component, "component", "all", "Component: all, kube-ovn, ovn, ovs or linux")
	command.Flags().StringVar(&options.output, "output-dir", "kubectl-ko-log", "Output directory")
	command.Flags().IntVar(&options.concurrency, "concurrency", 4, "Maximum concurrent collection requests")
	command.Flags().DurationVar(&options.timeout, "item-timeout", 30*time.Second, "Time limit for each collection item")
	command.Flags().Int64Var(&options.maxBytes, "max-bytes", 256<<20, "Maximum bytes per collection item")
	command.Flags().BoolVar(&options.strict, "strict", false, "Return a nonzero status if any collection item fails")
	command.Args = func(cmd *cobra.Command, args []string) error {
		if cmd.CalledAs() == "log" {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if cmd.Flags().Changed("component") {
				return errors.New("log COMPONENT cannot be combined with --component; use logs --component COMPONENT")
			}
			component = args[0]
		} else if err := cobra.NoArgs(cmd, args); err != nil {
			return err
		}
		if options.output == "" {
			return errors.New("--output-dir must not be empty")
		}
		if options.concurrency < 1 || options.timeout <= 0 || options.maxBytes < 1 {
			return errors.New("collection limits must be positive")
		}
		if !slices.Contains([]string{"kube-ovn", "ovn", "ovs", "linux", "all"}, component) {
			return fmt.Errorf("unknown log component %q", component)
		}
		return nil
	}
	command.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		return a.collectLogs(ctx, client, component, options)
	})
	a.root.AddCommand(command)
}

func (a *Application) collectLogs(ctx context.Context, client *Client, component string, options collectionOptions) error {
	if err := os.MkdirAll(options.output, 0o700); err != nil {
		return err
	}
	tasks, discoveryErr := client.collectionTasks(ctx, component, options)
	slots := make(chan struct{}, options.concurrency)
	var wg sync.WaitGroup
	for i := range tasks {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				tasks[i].Error = ctx.Err().Error()
				return
			}
			defer func() { <-slots }()
			itemCtx, cancel := context.WithTimeout(ctx, options.timeout)
			defer cancel()
			started := time.Now()
			if err := tasks[i].collect(itemCtx); err != nil {
				tasks[i].Error = err.Error()
			}
			tasks[i].Duration = time.Since(started).Nanoseconds()
		})
	}
	wg.Wait()
	var failures []error
	if discoveryErr != nil {
		failures = append(failures, discoveryErr)
	}
	for _, task := range tasks {
		if task.Error != "" {
			failures = append(failures, fmt.Errorf("%s/%s %s: %s", task.Target.Namespace, task.Target.Pod, task.Name, task.Error))
		}
	}
	manifest := struct {
		SchemaVersion  string           `json:"schemaVersion"`
		Items          []collectionTask `json:"items"`
		DiscoveryError string           `json:"discoveryError,omitempty"`
	}{SchemaVersion: "v1", Items: tasks}
	if discoveryErr != nil {
		manifest.DiscoveryError = discoveryErr.Error()
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(options.output, "manifest.json"), data, 0o600); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.streams.Out, "Collected %d items into %s (%d failures; see manifest.json)\n", len(tasks), options.output, len(failures)); err != nil {
		return err
	}
	for _, failure := range failures {
		if _, err := fmt.Fprintln(a.streams.ErrOut, failure); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(tasks) == 0 {
		return errors.Join(errors.New("no collectable containers found"), discoveryErr)
	}
	if options.strict {
		return errors.Join(failures...)
	}
	return nil
}

func (c *Client) collectionTasks(ctx context.Context, component string, options collectionOptions) ([]collectionTask, error) {
	var tasks []collectionTask
	var failures []error
	groups := []struct{ component, selector, container, directory string }{
		{"kube-ovn", "app=kube-ovn-cni", "cni-server", "kube-ovn"},
		{"ovn", "app=ovs", "openvswitch", "ovn"},
		{"ovn", "app=ovn-central", "ovn-central", "ovn"},
		{"ovs", "app=ovs", "openvswitch", "openvswitch"},
		{"linux", "app=kube-ovn-cni", "cni-server", "linux"},
	}
	for _, group := range groups {
		if component != "all" && component != group.component {
			continue
		}
		if c.ComponentFree && group.component == "linux" {
			linuxTasks, err := c.linuxCollectionTasks(ctx, options)
			tasks = append(tasks, linuxTasks...)
			if err != nil {
				failures = append(failures, err)
			}
			continue
		}
		targets, err := c.targets(ctx, group.selector, "", group.container, false)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if len(targets) == 0 {
			failures = append(failures, fmt.Errorf("no running containers for %s", group.selector))
		}
		for _, target := range targets {
			source := target
			if c.ComponentFree {
				target, err = c.agentTarget(ctx, target.Node)
				if err != nil {
					failures = append(failures, err)
					continue
				}
			}
			directory := filepath.Join(options.output, target.Node, group.directory)
			if group.component == "linux" {
				tasks = append(tasks, c.linuxTasks(target, directory, options.maxBytes)...)
				continue
			}
			// Central and OVS may share a node and /var/log/ovn. Give central its own
			// child directory to preserve both archives without concurrent overwrites.
			if group.selector == "app=ovn-central" {
				directory = filepath.Join(directory, "central-"+source.Pod)
			}
			tasks = append(tasks, collectionTask{
				Target: target, Name: group.directory + " files", Path: directory,
				collect: func(ctx context.Context) error {
					return c.collectDirectory(ctx, target, "/var/log/"+group.directory, directory, options.maxBytes)
				},
			})
			destination := filepath.Join(directory, source.Pod+".stdout.log")
			tasks = append(tasks, collectionTask{
				Target: source, Name: "container stdout", Path: destination,
				collect: func(ctx context.Context) error { return c.collectPodLogs(ctx, source, destination, options.maxBytes) },
			})
		}
	}
	return tasks, errors.Join(failures...)
}

func (c *Client) linuxCollectionTasks(ctx context.Context, options collectionOptions) ([]collectionTask, error) {
	targets, discoveryErr := c.linuxTargets(ctx)
	var tasks []collectionTask
	for _, target := range targets {
		directory := filepath.Join(options.output, target.Node, "linux")
		tasks = append(tasks, c.linuxTasks(target, directory, options.maxBytes)...)
		sources, err := c.targets(ctx, "app=kube-ovn-cni", target.Node, "cni-server", false)
		if err == nil && len(sources) != 1 {
			err = fmt.Errorf("expected one running CNI container, found %d", len(sources))
		}
		if err != nil {
			sourceErr := fmt.Errorf("resolve IPsec source on node %q: %w", target.Node, err)
			tasks = append(tasks, collectionTask{
				Target: target, Name: "ipsec", Path: filepath.Join(directory, "ipsec.log"),
				collect: func(context.Context) error { return sourceErr },
			})
			continue
		}
		tasks = append(tasks, c.ipsecTask(target, sources[0], directory, options.maxBytes))
	}
	return tasks, discoveryErr
}

func (c *Client) collectPodLogs(ctx context.Context, target Target, destination string, limit int64) error {
	stream, err := c.Kubernetes.CoreV1().Pods(target.Namespace).GetLogs(target.Pod, &corev1.PodLogOptions{Container: target.Container, LimitBytes: new(limit)}).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	return writeCollectionFile(destination, func(writer io.Writer) error {
		_, err := io.Copy(writer, io.LimitReader(stream, limit))
		return err
	})
}

func writeCollectionFile(destination string, write func(io.Writer) error) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".ko-collect-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	writeErr := write(file)
	closeErr := file.Close()
	if closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	return errors.Join(writeErr, os.Rename(file.Name(), destination))
}

func (c *Client) linuxTasks(target Target, directory string, limit int64) []collectionTask {
	commands := map[string][][]string{
		"dmesg": {{"dmesg"}}, "route": {{"ip", "-4", "route", "show"}, {"ip", "-6", "route", "show"}},
		"link": {{"ip", "-d", "-s", "link", "show"}}, "neigh": {{"ip", "-4", "neigh"}, {"ip", "-6", "neigh"}},
		"memory": {{"free", "-m"}}, "top": {{"top", "-b", "-n", "1"}}, "sysctl": {{"sysctl", "-a"}},
		"netstat": {{"netstat", "-tunlp"}}, "addr": {{"ip", "-s", "addr", "show"}}, "ipset": {{"ipset", "list"}},
		"tcp": {{"cat", "/proc/net/sockstat"}}, "ipsec": {{"cat", "/etc/ipsec.conf"}, {"ipsec", "statusall"}},
		"xfrm": {{"ip", "xfrm", "policy"}, {"ip", "xfrm", "state", "list", "nokeys"}},
	}
	if c.ComponentFree {
		delete(commands, "ipsec")
	}
	for _, backend := range []string{"legacy", "nft"} {
		for _, binary := range []string{"iptables-", "ip6tables-"} {
			for _, table := range []string{"filter", "nat"} {
				commands["iptables-"+backend] = append(commands["iptables-"+backend], []string{binary + backend, "-S", "-t", table})
			}
		}
	}
	var tasks []collectionTask
	for name, argvs := range commands {
		destination := filepath.Join(directory, name+".log")
		tasks = append(tasks, collectionTask{Target: target, Name: name, Path: destination, collect: func(ctx context.Context) error {
			return writeCollectionFile(destination, func(writer io.Writer) error {
				limited := &limitedWriter{writer: writer, remaining: limit}
				var failures []error
				for _, argv := range argvs {
					if _, err := fmt.Fprintf(limited, "Command: %q\n", argv); err != nil {
						return err
					}
					if err := c.Executor.Exec(ctx, target, argv, Streams{Out: limited, ErrOut: limited}); err != nil {
						failures = append(failures, err)
					}
				}
				return errors.Join(failures...)
			})
		}})
	}
	slices.SortFunc(tasks, func(a, b collectionTask) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return tasks
}

func (c *Client) ipsecTask(target, source Target, directory string, limit int64) collectionTask {
	destination := filepath.Join(directory, "ipsec.log")
	return collectionTask{Target: target, Name: "ipsec", Path: destination, collect: func(ctx context.Context) error {
		return writeCollectionFile(destination, func(writer io.Writer) error {
			pod, err := c.Kubernetes.CoreV1().Pods(source.Namespace).Get(ctx, source.Pod, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if pod.UID == "" || pod.Spec.NodeName != target.Node || pod.DeletionTimestamp != nil {
				return errors.New("IPsec source Pod is missing its UID, moved nodes or terminating")
			}
			limited := &limitedWriter{writer: writer, remaining: limit}
			return c.Executor.Exec(ctx, target, []string{"/kube-ovn/kubectl-ko-node-agent", "ipsec", string(pod.UID)}, Streams{Out: limited, ErrOut: limited})
		})
	}}
}

type limitedWriter struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if int64(len(p)) > w.remaining {
		return 0, errors.New("collection item exceeds byte limit")
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}
