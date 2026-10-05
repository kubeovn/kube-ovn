package ko

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

type performanceOptions struct {
	duration  int
	bandwidth string
}
type performancePods struct {
	client, server, hostClient, hostServer *corev1.Pod
	service                                *corev1.Service
}

func (a *Application) addPerformanceCommand() {
	parent := &cobra.Command{Use: "perf", Short: "Measure network performance or deliberate leader recovery"}
	options := performanceOptions{}
	var image string
	var duration time.Duration
	run := &cobra.Command{Use: "run", Short: "Measure pod, host, Service and multicast performance"}
	run.Flags().StringVar(&image, "image", "docker.io/kubeovn/test:v1.13.0", "Test image (use an internal image in disconnected installations)")
	run.Flags().DurationVar(&duration, "duration", 5*time.Second, "Duration of each measurement (whole seconds, 1s to 5m)")
	run.Flags().StringVar(&options.bandwidth, "bandwidth", "1G", "iperf offered UDP bandwidth")
	run.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.NoArgs(cmd, args); err != nil {
			return err
		}
		if duration < time.Second || duration > 5*time.Minute || duration%time.Second != 0 {
			return errors.New("--duration must be whole seconds between 1s and 5m")
		}
		if image == "" || options.bandwidth == "" {
			return errors.New("--image and --bandwidth must not be empty")
		}
		options.duration = int(duration / time.Second)
		return nil
	}
	run.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		return a.performance(ctx, client, image, options)
	})
	var yes bool
	recovery := &cobra.Command{Use: "recovery --yes", Short: "Delete central leader pods and measure their recovery"}
	recovery.Flags().BoolVar(&yes, "yes", false, "Confirm deliberate leader pod disruption")
	recovery.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.NoArgs(cmd, args); err != nil {
			return err
		}
		if !yes {
			return errors.New("perf recovery requires --yes")
		}
		return nil
	}
	recovery.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		return a.leaderRecoveryPerformance(ctx, client)
	})
	parent.AddCommand(run, recovery)
	a.root.AddCommand(parent)
}

func (a *Application) performance(ctx context.Context, client *Client, image string, options performanceOptions) (resultErr error) {
	run := &resourceRun{client: client, id: runID()}
	defer func() { resultErr = errors.Join(resultErr, run.cleanup(ctx)) }()
	pods, err := run.performancePods(ctx, image)
	if err != nil {
		return err
	}
	measurements := []struct {
		name    string
		pod     *corev1.Pod
		address string
	}{
		{"Pod network", pods.client, pods.server.Status.PodIP},
		{"Host network", pods.hostClient, pods.hostServer.Status.PodIP},
	}
	for _, measurement := range measurements {
		if _, err := fmt.Fprintf(a.streams.Out, "=== %s ===\n", measurement.name); err != nil {
			return err
		}
		if err := a.unicastPerformance(ctx, client, measurement.pod, measurement.address, options); err != nil {
			return err
		}
	}
	if err := a.servicePerformance(ctx, run, pods, options); err != nil {
		return err
	}
	if err := a.multicastPerformance(ctx, client, pods.client, pods.server, options); err != nil {
		return err
	}
	if err := a.multicastPerformance(ctx, client, pods.hostClient, pods.hostServer, options); err != nil {
		return err
	}
	return nil
}

func perfPod(name, image, node string, host, server bool, labels map[string]string) *corev1.Pod {
	command := []string{"sleep", "infinity"}
	if server {
		command = []string{"sh", "-c", "qperf & exec ./test-server.sh"}
	}
	return &corev1.Pod{Name: name, Labels: labels, Spec: corev1.PodSpec{
		NodeName: node, HostNetwork: host, RestartPolicy: corev1.RestartPolicyNever,
		AutomountServiceAccountToken: new(false),
		SecurityContext:              &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers:                   []corev1.Container{{Name: "probe", Image: image, ImagePullPolicy: corev1.PullIfNotPresent, Command: command}},
	}}
}

func (r *resourceRun) performancePods(ctx context.Context, image string) (*performancePods, error) {
	nodes, err := r.client.Kubernetes.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var ready []string
	for _, node := range nodes.Items {
		if node.Spec.Unschedulable {
			continue
		}
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				ready = append(ready, node.Name)
			}
		}
	}
	if len(ready) == 0 {
		return nil, errors.New("no ready schedulable nodes for performance tests")
	}
	clientNode, serverNode := ready[0], ready[0]
	if len(ready) > 1 {
		clientNode = ready[1]
	}
	specs := []*corev1.Pod{
		perfPod("ko-client-"+r.id, image, clientNode, false, false, r.labels()),
		perfPod("ko-server-"+r.id, image, serverNode, false, true, r.labels()),
		perfPod("ko-host-client-"+r.id, image, clientNode, true, false, r.labels()),
		perfPod("ko-host-server-"+r.id, image, serverNode, true, true, r.labels()),
	}
	specs[1].Labels["kubeovn.io/ko-server"] = "true"
	var pods []*corev1.Pod
	for _, spec := range specs {
		pod, err := r.createPod(ctx, spec)
		if err != nil {
			return nil, err
		}
		pods = append(pods, pod)
	}
	for i, pod := range pods {
		pods[i], err = r.client.waitPod(ctx, pod.Name)
		if err != nil {
			return nil, err
		}
	}
	selector := r.labels()
	selector["kubeovn.io/ko-server"] = "true"
	service := &corev1.Service{Name: "ko-service-" + r.id, Labels: r.labels(), Spec: corev1.ServiceSpec{Selector: selector}}
	for _, protocol := range []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP} {
		for _, port := range []int32{19765, 5201} {
			service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{Name: strings.ToLower(string(protocol)) + strconv.Itoa(int(port)), Protocol: protocol, Port: port, TargetPort: intstr.FromInt32(port)})
		}
	}
	svc, err := r.createService(ctx, service)
	if err != nil {
		return nil, err
	}
	return &performancePods{client: pods[0], server: pods[1], hostClient: pods[2], hostServer: pods[3], service: svc}, nil
}

func probeTarget(pod *corev1.Pod) Target {
	return Target{Namespace: pod.Namespace, Pod: pod.Name, Container: "probe", Node: pod.Spec.NodeName}
}

func (a *Application) unicastPerformance(ctx context.Context, client *Client, pod *corev1.Pod, address string, options performanceOptions) error {
	if _, err := netip.ParseAddr(address); err != nil {
		return fmt.Errorf("invalid performance target: %w", err)
	}
	duration := strconv.Itoa(options.duration)
	for _, size := range []string{"64", "128", "512", "1k", "4k"} {
		commands := [][]string{
			{"qperf", "-t", duration, address, "-ub", "-oo", "msg_size:" + size, "-vu", "tcp_lat", "udp_lat"},
			{"iperf3", "-c", address, "-u", "-t", duration, "-i", "1", "-P", "10", "-b", options.bandwidth, "-l", size},
			{"iperf3", "-c", address, "-t", duration, "-i", "1", "-P", "10", "-l", size},
		}
		for _, argv := range commands {
			if _, err := fmt.Fprintf(a.streams.Out, "Message size %s: %s\n", size, argv[0]); err != nil {
				return err
			}
			if err := client.Executor.Exec(ctx, probeTarget(pod), argv, a.outputStreams()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Application) servicePerformance(ctx context.Context, run *resourceRun, pods *performancePods, options performanceOptions) (resultErr error) {
	target, err := run.client.leader(ctx, "nb")
	if err != nil {
		return err
	}
	name := "ko-perf-" + run.id
	subnet := pods.server.Annotations[annotationPrefix+"logical_switch"]
	if subnet == "" {
		return errors.New("performance server has no logical switch annotation")
	}
	// qperf uses dynamic data ports, which require the IP-wide LB on every
	// chassis before measurements begin. NB commit alone does not install flows.
	argv := []string{"ovn-nbctl", "--wait=hv", "--timeout=30", "--", "lb-add", name, pods.service.Spec.ClusterIP, pods.server.Status.PodIP, "--", "ls-lb-add", subnet, name}
	// Register cleanup before mutation: a transport error does not prove the
	// remote transaction failed. The random run identity belongs only to us.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, err := run.client.capture(cleanupCtx, target, "ovn-nbctl", "--if-exists", "lb-del", name)
		resultErr = errors.Join(resultErr, err)
	}()
	if _, err := run.client.capture(ctx, target, argv...); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(a.streams.Out, "=== Service network ==="); err != nil {
		return err
	}
	return a.unicastPerformance(ctx, run.client, pods.client, pods.service.Spec.ClusterIP, options)
}

func (a *Application) leaderRecoveryPerformance(ctx context.Context, client *Client) error {
	for _, role := range []string{"nb", "sb", "northd"} {
		target, err := client.leader(ctx, role)
		if err != nil {
			return err
		}
		pod, err := client.Kubernetes.CoreV1().Pods(target.Namespace).Get(ctx, target.Pod, metav1.GetOptions{})
		if err != nil {
			return err
		}
		started := time.Now()
		if err := client.Kubernetes.CoreV1().Pods(target.Namespace).Delete(ctx, target.Pod, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: new(pod.UID)}}); err != nil {
			return err
		}
		err = wait.PollUntilContextTimeout(ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			targets, err := client.targets(ctx, "ovn-"+role+"-leader=true", "", "ovn-central", true)
			if err != nil {
				return false, err
			}
			return len(targets) == 1 && targets[0].Pod != target.Pod, nil
		})
		if err != nil {
			return err
		}
		if err := client.waitDeployment(ctx, "ovn-central", 3*time.Minute); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(a.streams.Out, "%s leader recovered in %s\n", role, time.Since(started)); err != nil {
			return err
		}
	}
	return nil
}

type multicastTarget struct {
	target     Target
	netns, nic string
}

func (c *Client) multicastTarget(ctx context.Context, pod *corev1.Pod) (multicastTarget, error) {
	ovs, err := c.nodeTarget(ctx, pod.Spec.NodeName, "ovs")
	if err != nil {
		return multicastTarget{}, err
	}
	if !pod.Spec.HostNetwork {
		nic, err := c.podInterface(ctx, ovs, pod.Name+"."+pod.Namespace)
		if err != nil {
			return multicastTarget{}, err
		}
		if nic.netns == "" {
			return multicastTarget{}, errors.New("multicast pod has no network namespace")
		}
		name := "eth0"
		if pod.Annotations[annotationPrefix+"pod_nic_type"] == "internal-port" {
			name = nic.name
		}
		cni, err := c.nodeTarget(ctx, pod.Spec.NodeName, "kube-ovn-cni")
		if err != nil {
			return multicastTarget{}, err
		}
		return multicastTarget{target: cni, netns: nic.netns, nic: name}, nil
	}
	cni, err := c.nodeTarget(ctx, pod.Spec.NodeName, "kube-ovn-cni")
	if err != nil {
		return multicastTarget{}, err
	}
	output, err := c.capture(ctx, cni, "ip", "-s", "-o", "addr", "show")
	if err != nil {
		return multicastTarget{}, err
	}
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[3])
		if err == nil && prefix.Addr().String() == pod.Status.PodIP {
			return multicastTarget{target: cni, nic: strings.SplitN(fields[1], "@", 2)[0]}, nil
		}
	}
	return multicastTarget{}, errors.New("cannot locate host performance interface")
}

func (a *Application) multicastPerformance(ctx context.Context, client *Client, clientPod, serverPod *corev1.Pod, options performanceOptions) (resultErr error) {
	for _, pod := range []*corev1.Pod{clientPod, serverPod} {
		address, err := netip.ParseAddr(pod.Status.PodIP)
		if err != nil {
			return err
		}
		if !address.Is4() {
			_, err := fmt.Fprintln(a.streams.Out, "Skipping IPv4 multicast on an IPv6-only performance pair")
			return err
		}
	}
	var added []multicastTarget
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		for _, target := range added {
			_, err := client.capture(cleanupCtx, target.target, namespaceCommand(target.netns, "ip", "maddr", "del", "01:00:5e:00:00:64", "dev", target.nic)...)
			resultErr = errors.Join(resultErr, err)
		}
	}()
	for _, pod := range []*corev1.Pod{clientPod, serverPod} {
		target, err := client.multicastTarget(ctx, pod)
		if err != nil {
			return err
		}
		output, err := client.capture(ctx, target.target, namespaceCommand(target.netns, "ip", "maddr", "show", "dev", target.nic)...)
		if err != nil {
			return err
		}
		if strings.Contains(output, "01:00:5e:00:00:64") {
			continue
		}
		// A lost exec response can hide a successful add, including on a host
		// interface that outlives the probe pod. Record cleanup before mutation.
		added = append(added, target)
		if _, err := client.capture(ctx, target.target, namespaceCommand(target.netns, "ip", "maddr", "add", "01:00:5e:00:00:64", "dev", target.nic)...); err != nil {
			return err
		}
	}
	return a.runMulticastTraffic(ctx, client, clientPod, serverPod, options)
}

func (a *Application) runMulticastTraffic(ctx context.Context, client *Client, clientPod, serverPod *corev1.Pod, options performanceOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var output boundedBuffer
	done := make(chan error, 1)
	// The remote server has its own finite lifetime even if the transport disappears.
	lifetime := strconv.Itoa(options.duration*10 + 10)
	go func() {
		done <- client.Executor.Exec(ctx, probeTarget(serverPod), []string{"timeout", lifetime, "iperf", "-s", "-B", "224.0.0.100", "-i", "1", "-u"}, Streams{Out: &output, ErrOut: io.Discard})
	}()
	trafficErr := a.multicastClientTraffic(ctx, client, clientPod, options)
	cancel()
	serverErr := <-done
	if errors.Is(serverErr, context.Canceled) {
		serverErr = nil
	}
	if remoteCode := ExitCode(serverErr); remoteCode == 124 {
		serverErr = nil
	}
	if _, err := fmt.Fprintln(a.streams.Out, output.String()); err != nil {
		return errors.Join(trafficErr, serverErr, err)
	}
	return errors.Join(trafficErr, serverErr)
}

func (a *Application) multicastClientTraffic(ctx context.Context, client *Client, pod *corev1.Pod, options performanceOptions) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	for _, size := range []string{"64", "128", "512", "1k", "4k"} {
		for _, bandwidth := range []string{options.bandwidth, "1M"} {
			argv := []string{"iperf", "-c", "224.0.0.100", "-u", "-T", "32", "-t", strconv.Itoa(options.duration), "-i", "1", "-b", bandwidth, "-l", size}
			if err := client.Executor.Exec(ctx, probeTarget(pod), argv, a.outputStreams()); err != nil {
				return err
			}
		}
	}
	return nil
}
