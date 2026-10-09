package ko

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type diagnosticOptions struct {
	readOnly, skipKubeProxy bool
	tcpPort, udpPort        string
	externalAddresses       []string
}

func (a *Application) addDiagnosticCommands() {
	a.root.AddCommand(&cobra.Command{Use: "restart", Short: "Restart all Kube-OVN components in dependency order", Args: cobra.NoArgs, RunE: a.run(a.reload)})
	parent := &cobra.Command{Use: "diagnose", Short: "Check the environment, configuration and network connectivity"}
	parent.AddCommand(&cobra.Command{Use: "environment", Short: "Run the image environment checker on every node", Args: cobra.NoArgs, RunE: a.run(a.environmentCheck)})
	for _, mode := range []string{"cluster", "node", "subnet", "connectivity"} {
		a.addDiagnosticMode(parent, mode)
	}
	a.root.AddCommand(parent)
	a.addLogCommand()
}

func (a *Application) addDiagnosticMode(parent *cobra.Command, mode string) {
	options := diagnosticOptions{tcpPort: "8100", udpPort: "8101"}
	command := &cobra.Command{Use: mode, Short: "Check " + mode + " configuration and active connectivity"}
	flags := command.Flags()
	flags.BoolVar(&options.skipKubeProxy, "skip-kube-proxy", os.Getenv("WITHOUT_KUBE_PROXY") == "true", "Skip kube-proxy checks (default from WITHOUT_KUBE_PROXY)")
	if mode != "connectivity" {
		flags.BoolVar(&options.readOnly, "read-only", false, "Check configuration without creating probe resources or sending traffic")
		flags.StringArrayVar(&options.externalAddresses, "external-address", nil, "External IP address to ping (repeatable; disabled by default)")
	}
	if mode == "node" {
		command.Use += " NODE"
	}
	if mode == "subnet" {
		command.Use += " SUBNET"
		flags.StringVar(&options.tcpPort, "tcp-port", cmp.Or(os.Getenv("TCP_CONN_CHECK_PORT"), "8100"), "Subnet probe TCP port")
		flags.StringVar(&options.udpPort, "udp-port", cmp.Or(os.Getenv("UDP_CONN_CHECK_PORT"), "8101"), "Subnet probe UDP port")
	}
	var targets []string
	if mode == "connectivity" {
		command.Short = "Probe explicit TCP/UDP IP endpoints from independent probe pods"
		flags.StringArrayVar(&targets, "target", nil, "Responding endpoint such as tcp://192.0.2.1:8100 or udp://[2001:db8::1]:8101 (repeatable)")
	}
	var probeTargets string
	command.Args = func(cmd *cobra.Command, args []string) error {
		validate := cobra.NoArgs
		if mode == "node" || mode == "subnet" {
			validate = cobra.ExactArgs(1)
		}
		if err := validate(cmd, args); err != nil {
			return err
		}
		if mode == "node" || mode == "subnet" {
			if err := validateResourceName(mode, args[0]); err != nil {
				return err
			}
		}
		for _, port := range []*string{&options.tcpPort, &options.udpPort} {
			value, err := strconv.ParseUint(*port, 10, 16)
			if err != nil || value == 0 {
				return fmt.Errorf("invalid probe port %q", *port)
			}
			*port = strconv.FormatUint(value, 10)
		}
		for i, value := range options.externalAddresses {
			address, err := netip.ParseAddr(value)
			if err != nil || address.Zone() != "" {
				return fmt.Errorf("invalid external address %q", value)
			}
			options.externalAddresses[i] = address.Unmap().String()
		}
		if mode == "connectivity" {
			var err error
			probeTargets, err = diagnosticTargets(targets)
			return err
		}
		return nil
	}
	command.RunE = a.run(func(ctx context.Context, client *Client, args []string) error {
		internalMode, value := mode, ""
		if mode == "cluster" {
			internalMode = "all"
		}
		if len(args) != 0 {
			value = args[0]
		}
		if mode == "connectivity" {
			internalMode, value = "IPPorts", probeTargets
		}
		return a.diagnose(ctx, client, internalMode, value, options)
	})
	parent.AddCommand(command)
}

func diagnosticTargets(targets []string) (string, error) {
	if len(targets) == 0 {
		return "", errors.New("at least one --target is required")
	}
	var result []string
	for _, target := range targets {
		endpoint, err := url.Parse(target)
		if err != nil {
			return "", fmt.Errorf("invalid target %q: %w", target, err)
		}
		if endpoint.Scheme != "tcp" && endpoint.Scheme != "udp" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
			return "", fmt.Errorf("target %q must be tcp://IP:PORT or udp://IP:PORT", target)
		}
		address, err := netip.ParseAddrPort(endpoint.Host)
		if err != nil || address.Port() == 0 || address.Addr().Zone() != "" {
			return "", fmt.Errorf("invalid target address %q", target)
		}
		result = append(result, fmt.Sprintf("%s-%s-%d", endpoint.Scheme, address.Addr().Unmap(), address.Port()))
	}
	return strings.Join(result, ","), nil
}

func (a *Application) environmentCheck(ctx context.Context, client *Client, _ []string) error {
	targets, discoveryErr := client.linuxTargets(ctx)
	var failures []error
	if discoveryErr != nil {
		failures = append(failures, discoveryErr)
	}
	if len(targets) == 0 {
		return errors.Join(errors.New("no environment check targets found"), discoveryErr)
	}
	for _, target := range targets {
		if _, err := fmt.Fprintf(a.streams.Out, "Environment check on %s\n", target.Node); err != nil {
			return err
		}
		if err := client.Executor.Exec(ctx, target, []string{"bash", "/kube-ovn/env-check.sh"}, a.outputStreams()); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (a *Application) diagnose(ctx context.Context, client *Client, mode, value string, options diagnosticOptions) (resultErr error) {
	var err error
	if mode == "node" {
		if _, err := client.Kubernetes.CoreV1().Nodes().Get(ctx, value, metav1.GetOptions{}); err != nil {
			return err
		}
	}
	if mode == "subnet" {
		if _, err := client.Dynamic.Resource(subnetResource).Get(ctx, value, metav1.GetOptions{}); err != nil {
			return err
		}
	}
	configurationErr := errors.Join(a.checkConfiguration(ctx, client, options.skipKubeProxy), a.diagnoseOVN(ctx, client))
	if options.readOnly {
		return configurationErr
	}
	run := &resourceRun{client: client, id: runID()}
	defer func() { resultErr = errors.Join(resultErr, run.cleanup(ctx)) }()
	targets := value
	if mode != "IPPorts" {
		targets, err = run.nodePortProbe(ctx)
		if err != nil {
			return errors.Join(configurationErr, err)
		}
	}
	if mode == "subnet" {
		if err := run.subnetProbe(ctx, value, options); err != nil {
			return errors.Join(configurationErr, err)
		}
	}
	node := ""
	if mode == "node" {
		node = value
	}
	selector, container := "app=kube-ovn-pinger", "pinger"
	if mode == "subnet" {
		selector, container = labels.Set(run.labels()).String(), "probe"
	}
	pingers, err := client.targets(ctx, selector, node, container, true)
	if err != nil {
		return errors.Join(configurationErr, err)
	}
	if len(pingers) == 0 {
		return errors.Join(configurationErr, errors.New("no ready pinger containers matched the diagnostic target"))
	}
	return errors.Join(configurationErr, a.runDiagnosticProbes(ctx, client, pingers, mode, targets, options))
}

func (a *Application) runDiagnosticProbes(ctx context.Context, client *Client, pingers []Target, mode, targets string, options diagnosticOptions) (resultErr error) {
	run := &resourceRun{client: client, id: runID()}
	defer func() { resultErr = errors.Join(resultErr, run.cleanup(ctx)) }()
	if mode == "subnet" {
		peers, err := client.subnetProbeTargets(ctx, pingers, options)
		if err != nil {
			return err
		}
		targets += "," + peers
	}
	var failures []error
	for _, target := range pingers {
		if _, err := fmt.Fprintf(a.streams.Out, "Diagnosing node %s\n", target.Node); err != nil {
			return err
		}
		if err := client.validateExternalProbeFamilies(ctx, target, options.externalAddresses); err != nil {
			failures = append(failures, fmt.Errorf("probe on %s: %w", target.Node, err))
			continue
		}
		execTarget := target
		probeTarget := target
		var prefix []string
		if client.ComponentFree && mode != "subnet" {
			var err error
			execTarget, err = client.agentTarget(ctx, target.Node)
			if err == nil {
				probeTarget, prefix, err = run.diagnosticProbe(ctx, target, execTarget)
			}
			if err != nil {
				failures = append(failures, fmt.Errorf("probe on %s: %w", target.Node, err))
				continue
			}
		}
		if mode == "all" || mode == "node" {
			for _, argv := range [][]string{{"tail", "/var/log/ovn/ovn-controller.log"}, {"tail", "/var/log/openvswitch/ovs-vswitchd.log"}, {"ovs-vsctl", "show"}} {
				if err := client.Executor.Exec(ctx, execTarget, argv, a.outputStreams()); err != nil {
					failures = append(failures, err)
				}
			}
		}
		argv := []string{"/kube-ovn/kube-ovn-pinger", "--mode=job", "--exit-code=1", "--target-ip-ports=" + targets}
		if mode != "IPPorts" {
			argv = append(argv, "--external-address="+strings.Join(options.externalAddresses, ","))
		}
		if mode == "subnet" {
			// The temporary pods listen on the probe ports; CNI node listeners
			// are optional. Probe peers explicitly and retain ICMP node checks.
			argv = append(argv, "--network-mode=diagnostic")
		}
		if err := client.Executor.Exec(ctx, probeTarget, append(prefix, argv...), a.outputStreams()); err != nil {
			failures = append(failures, fmt.Errorf("probe on %s: %w", target.Node, err))
		}
	}
	return errors.Join(failures...)
}

func (r *resourceRun) diagnosticProbe(ctx context.Context, source, agent Target) (Target, []string, error) {
	c := r.client
	pod, err := c.Kubernetes.CoreV1().Pods(source.Namespace).Get(ctx, source.Pod, metav1.GetOptions{})
	if err != nil {
		return Target{}, nil, err
	}
	if pod.Spec.NodeName != source.Node {
		return Target{}, nil, errors.New("pinger Pod moved to another node")
	}
	if _, ready := podTarget(pod, "pinger", true); !ready || pod.UID == "" || pod.Status.PodIP == "" {
		return Target{}, nil, errors.New("pinger Pod is no longer ready with a live network identity")
	}
	netns := "/proc/1/ns/net"
	if !pod.Spec.HostNetwork {
		netns, err = c.podNetNS(ctx, agent, pod)
		if err != nil {
			return Target{}, nil, err
		}
	}
	probe, err := diagnosticProbePod(pod, "ko-diagnostic-"+r.id+"-"+strconv.Itoa(len(r.resources)), r.labels())
	if err != nil {
		return Target{}, nil, err
	}
	created, err := r.createPod(ctx, probe)
	if err != nil {
		return Target{}, nil, err
	}
	if _, err := c.waitPod(ctx, created.Name); err != nil {
		return Target{}, nil, err
	}
	current, err := c.Kubernetes.CoreV1().Pods(source.Namespace).Get(ctx, source.Pod, metav1.GetOptions{})
	if err != nil {
		return Target{}, nil, err
	}
	if current.UID != pod.UID || current.Status.PodIP != pod.Status.PodIP || current.Spec.NodeName != source.Node {
		return Target{}, nil, errors.New("pinger Pod network identity changed while preparing the probe")
	}
	if _, ready := podTarget(current, "pinger", true); !ready {
		return Target{}, nil, errors.New("pinger Pod is no longer ready")
	}
	// The independent probe uses the existing pinger service account and its
	// original network identity; the node agent remains credential-free.
	prefix := namespaceCommand(netns, "env",
		"POD_NAME="+pod.Name, "POD_NAMESPACE="+pod.Namespace,
		"POD_IP="+pod.Status.PodIP, "HOST_IP="+pod.Status.HostIP,
		"NODE_NAME="+pod.Spec.NodeName)
	return Target{Namespace: c.Namespace, Pod: created.Name, Container: "probe", Node: source.Node}, prefix, nil
}

func diagnosticProbePod(pod *corev1.Pod, name string, runLabels map[string]string) (*corev1.Pod, error) {
	probe := &corev1.Pod{Name: name, Namespace: pod.Namespace, Labels: runLabels, Spec: *pod.Spec.DeepCopy()}
	index := slices.IndexFunc(probe.Spec.Containers, func(container corev1.Container) bool { return container.Name == "pinger" })
	if index < 0 {
		return nil, errors.New("pinger Pod has no pinger container")
	}
	container := probe.Spec.Containers[index]
	container.Name, container.Command, container.Args = "probe", []string{"sleep", "600"}, nil
	container.LivenessProbe, container.ReadinessProbe, container.StartupProbe = nil, nil, nil
	container.Lifecycle, container.Ports = nil, nil
	container.SecurityContext = container.SecurityContext.DeepCopy()
	if container.SecurityContext == nil {
		container.SecurityContext = &corev1.SecurityContext{}
	}
	container.SecurityContext.RunAsUser = new(int64(0))
	container.SecurityContext.RunAsNonRoot = new(false)
	if container.SecurityContext.Capabilities == nil {
		container.SecurityContext.Capabilities = &corev1.Capabilities{}
	}
	if !slices.Contains(container.SecurityContext.Capabilities.Add, corev1.Capability("SYS_ADMIN")) {
		container.SecurityContext.Capabilities.Add = append(container.SecurityContext.Capabilities.Add, "SYS_ADMIN")
	}
	container.VolumeMounts = slices.DeleteFunc(container.VolumeMounts, func(mount corev1.VolumeMount) bool {
		return mount.MountPath == "/var/log/kube-ovn" || mount.Name == "ko-probe-log"
	})
	probe.Spec.Volumes = slices.DeleteFunc(probe.Spec.Volumes, func(volume corev1.Volume) bool { return volume.Name == "ko-probe-log" })
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "ko-probe-log", MountPath: "/var/log/kube-ovn"})
	probe.Spec.Volumes = append(probe.Spec.Volumes, corev1.Volume{Name: "ko-probe-log", EmptyDir: &corev1.EmptyDirVolumeSource{}})
	if !slices.ContainsFunc(container.VolumeMounts, func(mount corev1.VolumeMount) bool { return mount.MountPath == "/var/run/netns" }) {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "ko-probe-netns", MountPath: "/var/run/netns", MountPropagation: new(corev1.MountPropagationHostToContainer)})
		probe.Spec.Volumes = slices.DeleteFunc(probe.Spec.Volumes, func(volume corev1.Volume) bool { return volume.Name == "ko-probe-netns" })
		probe.Spec.Volumes = append(probe.Spec.Volumes, corev1.Volume{Name: "ko-probe-netns", HostPath: &corev1.HostPathVolumeSource{Path: "/var/run/netns", Type: new(corev1.HostPathDirectory)}})
	}
	probe.Spec.HostNetwork, probe.Spec.HostPID = true, true
	probe.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
	probe.Spec.ShareProcessNamespace, probe.Spec.InitContainers, probe.Spec.EphemeralContainers = nil, nil, nil
	probe.Spec.ReadinessGates, probe.Spec.SchedulingGates, probe.Spec.HostUsers = nil, nil, nil
	probe.Spec.RestartPolicy, probe.Spec.ActiveDeadlineSeconds = corev1.RestartPolicyNever, new(int64(600))
	probe.Spec.Containers = []corev1.Container{container}
	probe.Spec.Volumes = slices.DeleteFunc(probe.Spec.Volumes, func(volume corev1.Volume) bool {
		return !slices.ContainsFunc(container.VolumeMounts, func(mount corev1.VolumeMount) bool { return mount.Name == volume.Name })
	})
	return probe, nil
}

func (c *Client) validateExternalProbeFamilies(ctx context.Context, target Target, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	pod, err := c.Kubernetes.CoreV1().Pods(target.Namespace).Get(ctx, target.Pod, metav1.GetOptions{})
	if err != nil {
		return err
	}
	families := make(map[bool]bool)
	for _, ip := range pod.Status.PodIPs {
		address, err := netip.ParseAddr(ip.IP)
		if err != nil {
			return fmt.Errorf("invalid probe Pod address: %w", err)
		}
		families[address.Unmap().Is4()] = true
	}
	for _, value := range addresses {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return err
		}
		if !families[address.Unmap().Is4()] {
			return fmt.Errorf("external address %s has no matching IP family on probe %s/%s", value, target.Namespace, target.Pod)
		}
	}
	return nil
}

func (c *Client) subnetProbeTargets(ctx context.Context, pingers []Target, options diagnosticOptions) (string, error) {
	var endpoints []string
	for _, target := range pingers {
		pod, err := c.Kubernetes.CoreV1().Pods(target.Namespace).Get(ctx, target.Pod, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if len(pod.Status.PodIPs) == 0 {
			return "", fmt.Errorf("subnet probe %s/%s has no IP addresses", target.Namespace, target.Pod)
		}
		for _, ip := range pod.Status.PodIPs {
			address, err := netip.ParseAddr(ip.IP)
			if err != nil {
				return "", fmt.Errorf("invalid subnet probe address: %w", err)
			}
			endpoints = append(endpoints, "tcp-"+address.Unmap().String()+"-"+options.tcpPort, "udp-"+address.Unmap().String()+"-"+options.udpPort)
		}
	}
	return strings.Join(endpoints, ","), nil
}

type diagnosticCheck struct {
	name string
	run  func() error
}

func (a *Application) checkConfiguration(ctx context.Context, client *Client, skipKubeProxy bool) error {
	checks := []diagnosticCheck{
		{"Kubernetes service", func() error {
			_, err := client.Kubernetes.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
			return err
		}},
		{"Kube-OVN subnets", func() error {
			_, err := client.Dynamic.Resource(subnetResource).List(ctx, metav1.ListOptions{})
			return err
		}},
	}
	checks = append(checks, client.configurationChecks(ctx)...)
	for _, name := range []string{"ovn-central", "kube-ovn-controller"} {
		checks = append(checks, diagnosticCheck{name, func() error { return client.waitDeployment(ctx, name, 30*time.Second) }})
	}
	for _, name := range []string{"kube-ovn-cni", "ovs-ovn"} {
		checks = append(checks, diagnosticCheck{name, func() error { return client.waitDaemonSet(ctx, name, 30*time.Second) }})
	}
	for _, role := range []string{"nb", "sb", "northd"} {
		checks = append(checks, diagnosticCheck{role + " leader", func() error { _, err := client.leader(ctx, role); return err }})
	}
	if !skipKubeProxy {
		checks = append(checks, diagnosticCheck{"kube-proxy", func() error { return client.checkKubeProxy(ctx) }})
	}
	var failures []error
	for _, check := range checks {
		if err := check.run(); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", check.name, err))
			if _, writeErr := fmt.Fprintf(a.streams.Out, "FAIL %s: %v\n", check.name, err); writeErr != nil {
				return writeErr
			}
		} else if _, err := fmt.Fprintf(a.streams.Out, "PASS %s\n", check.name); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

func (c *Client) checkKubeProxy(ctx context.Context) error {
	ds, err := c.Kubernetes.AppsV1().DaemonSets("kube-system").Get(ctx, "kube-proxy", metav1.GetOptions{})
	if err == nil {
		if !daemonSetReady(ds) {
			return errors.New("kube-proxy DaemonSet is not ready")
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	var targets []Target
	var discoveryErr error
	if c.ComponentFree {
		targets, discoveryErr = c.linuxTargets(ctx)
	} else {
		targets, discoveryErr = c.targets(ctx, "app=kube-ovn-cni", "", "cni-server", true)
	}
	if len(targets) == 0 {
		return errors.Join(errors.New("no targets available to probe embedded kube-proxy"), discoveryErr)
	}
	var failures []error
	if discoveryErr != nil {
		failures = append(failures, discoveryErr)
	}
	for _, target := range targets {
		pod, err := c.Kubernetes.CoreV1().Pods(c.Namespace).Get(ctx, target.Pod, metav1.GetOptions{})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		address := "http://" + net.JoinHostPort(pod.Status.PodIP, "10256") + "/healthz"
		if _, err := c.capture(ctx, target, "curl", "--globoff", "--fail", "--silent", "--show-error", "--max-time", "3", address); err != nil {
			failures = append(failures, fmt.Errorf("embedded kube-proxy on %s: %w", target.Node, err))
		}
	}
	return errors.Join(failures...)
}

func (c *Client) configurationChecks(ctx context.Context) []diagnosticCheck {
	checks := []diagnosticCheck{
		{"ovn service account", func() error {
			_, err := c.Kubernetes.CoreV1().ServiceAccounts(c.Namespace).Get(ctx, "ovn", metav1.GetOptions{})
			return err
		}},
		{"ovn cluster role", func() error {
			_, err := c.Kubernetes.RbacV1().ClusterRoles().Get(ctx, "system:ovn", metav1.GetOptions{})
			return err
		}},
		{"ovn cluster role binding", func() error {
			_, err := c.Kubernetes.RbacV1().ClusterRoleBindings().Get(ctx, "ovn", metav1.GetOptions{})
			return err
		}},
		{"cluster DNS", func() error {
			_, err := c.Kubernetes.CoreV1().Services("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{})
			return err
		}},
		{"CoreDNS", func() error {
			dns := *c
			dns.Namespace = "kube-system"
			return dns.waitDeployment(ctx, "coredns", 30*time.Second)
		}},
	}
	resource := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	for _, name := range []string{"vpcs", "vpc-nat-gateways", "vpc-egress-gateways", "subnets", "ips", "vlans", "provider-networks", "security-groups", "vips", "vpc-dnses", "switch-lb-rules", "ippools", "ovn-eips", "ovn-fips", "ovn-dnat-rules", "ovn-snat-rules", "iptables-eips", "iptables-fip-rules", "iptables-snat-rules", "iptables-dnat-rules"} {
		checks = append(checks, diagnosticCheck{name + " CRD", func() error {
			_, err := c.Dynamic.Resource(resource).Get(ctx, name+".kubeovn.io", metav1.GetOptions{})
			return err
		}})
	}
	return checks
}

func (a *Application) diagnoseOVN(ctx context.Context, client *Client) error {
	var failures []error
	nodes, err := client.Kubernetes.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		failures = append(failures, err)
	} else {
		for _, node := range nodes.Items {
			if _, err := fmt.Fprintf(a.streams.Out, "Node %s: %v\n", node.Name, node.Status.Addresses); err != nil {
				return err
			}
		}
	}
	for _, role := range []string{"nb", "sb"} {
		target, err := client.leader(ctx, role)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		commands := [][]string{{"ovn-" + role + "ctl", "show"}, databaseCommand(role, "cluster/status"), databaseCommand(role, "ovsdb-server/get-db-storage-status")}
		if role == "nb" {
			for _, args := range [][]string{{"lr-policy-list", "ovn-cluster"}, {"lr-route-list", "ovn-cluster"}, {"ls-lb-list", "ovn-default"}, {"list", "address_set"}, {"list", "acl"}} {
				commands = append(commands, append([]string{"ovn-nbctl"}, args...))
			}
		}
		for _, argv := range commands {
			if err := client.Executor.Exec(ctx, target, argv, a.outputStreams()); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (r *resourceRun) nodePortProbe(ctx context.Context) (string, error) {
	service := &corev1.Service{Name: "ko-nodeport-" + r.id, Labels: r.labels(), Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeNodePort, Selector: map[string]string{"app": "kube-ovn-pinger"},
		IPFamilyPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
		Ports:          []corev1.ServicePort{{Name: "probe", Protocol: corev1.ProtocolTCP, Port: 60001, TargetPort: intstr.FromInt32(8080)}},
	}}
	result, err := r.createService(ctx, service)
	if err != nil {
		return "", err
	}
	if len(result.Spec.Ports) != 1 || result.Spec.Ports[0].NodePort == 0 {
		return "", errors.New("probe Service has no allocated NodePort")
	}
	if len(result.Spec.IPFamilies) == 0 {
		return "", errors.New("probe Service has no allocated IP families")
	}
	nodes, err := r.client.Kubernetes.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	var targets []string
	probedFamilies := make(map[corev1.IPFamily]bool)
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				ip, err := netip.ParseAddr(address.Address)
				if err != nil {
					return "", fmt.Errorf("node %s has invalid internal address %q: %w", node.Name, address.Address, err)
				}
				family := corev1.IPv6Protocol
				if ip.Unmap().Is4() {
					family = corev1.IPv4Protocol
				}
				if !slices.Contains(result.Spec.IPFamilies, family) {
					continue
				}
				targets = append(targets, fmt.Sprintf("tcp-%s-%d", address.Address, result.Spec.Ports[0].NodePort))
				probedFamilies[family] = true
			}
		}
	}
	for _, family := range result.Spec.IPFamilies {
		if !probedFamilies[family] {
			return "", fmt.Errorf("nodes have no internal addresses for probe Service family %s", family)
		}
	}
	return strings.Join(targets, ","), nil
}

func (r *resourceRun) subnetProbe(ctx context.Context, subnet string, options diagnosticOptions) error {
	pinger, err := r.client.Kubernetes.AppsV1().DaemonSets(r.client.Namespace).Get(ctx, "kube-ovn-pinger", metav1.GetOptions{})
	if err != nil {
		return err
	}
	image := ""
	for _, container := range pinger.Spec.Template.Spec.Containers {
		if container.Name == "pinger" {
			image = container.Image
		}
	}
	if image == "" {
		return errors.New("pinger DaemonSet has no pinger image")
	}
	tcp, err := strconv.ParseInt(options.tcpPort, 10, 32)
	if err != nil || tcp < 1 || tcp > 65535 {
		return errors.New("invalid subnet TCP probe port")
	}
	udp, err := strconv.ParseInt(options.udpPort, 10, 32)
	if err != nil || udp < 1 || udp > 65535 {
		return errors.New("invalid subnet UDP probe port")
	}
	ds := &appsv1.DaemonSet{Name: "ko-subnet-" + r.id, Labels: r.labels(), Spec: appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: r.labels()},
		Template: corev1.PodTemplateSpec{Labels: r.labels(), Annotations: map[string]string{annotationPrefix + "logical_switch": subnet}, Spec: corev1.PodSpec{
			AutomountServiceAccountToken: new(true),
			ServiceAccountName:           "kube-ovn-app", SecurityContext: &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			// Pinger initializes its log file even when logging to stderr.
			Volumes: []corev1.Volume{{Name: "probe-log", EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			Containers: []corev1.Container{{
				Name: "probe", Image: image, Command: []string{"/kube-ovn/kube-ovn-pinger"},
				VolumeMounts:   []corev1.VolumeMount{{Name: "probe-log", MountPath: "/var/log/kube-ovn"}},
				Args:           []string{"--enable-verbose-conn-check=true", fmt.Sprintf("--tcp-conn-check-port=%d", tcp), fmt.Sprintf("--udp-conn-check-port=%d", udp)},
				Env:            []corev1.EnvVar{{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}}, {Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}},
				ReadinessProbe: &corev1.Probe{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(int32(tcp))}, InitialDelaySeconds: 3, PeriodSeconds: 5},
			}},
		}},
	}}
	return r.createDaemonSet(ctx, ds)
}
