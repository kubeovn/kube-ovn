package ko

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const annotationPrefix = "ovn.kubernetes.io/"

type networkSource struct {
	node        string
	lsp         string
	annotations map[string]string
	addresses   []string
	pod         *corev1.Pod
}

type podInterface struct {
	name   string
	netns  string
	ofport int
}

func (a *Application) addNetworkCommands() {
	options := captureOptions{}
	capture := &cobra.Command{
		Use: "capture (--pod POD | --vm VM | --vmi VMI) [flags] -- [TCPDUMP_ARGS...]", DisableFlagsInUseLine: true, Short: "Capture packets in a Pod or KubeVirt network namespace",
		Args: func(cmd *cobra.Command, args []string) error {
			kind, reference, err := options.reference()
			if err != nil {
				return err
			}
			if err := validateNamespacedReference(kind, reference); err != nil {
				return err
			}
			return remoteArguments(cmd, args)
		},
		RunE: a.run(func(ctx context.Context, client *Client, args []string) error {
			target, err := client.captureTarget(ctx, options)
			if err != nil {
				return err
			}
			if target.vmi != nil {
				if _, err := fmt.Fprintf(a.streams.ErrOut, "Capturing VMI %s/%s through Pod %s on node %s\n", target.vmi.GetNamespace(), target.vmi.GetName(), target.pod.Name, target.pod.Spec.NodeName); err != nil {
					return err
				}
			}
			return a.tcpdump(ctx, client, target, args)
		}),
	}
	capture.Flags().StringVar(&options.pod, "pod", "", "Pod to capture, optionally qualified by namespace")
	capture.Flags().StringVar(&options.vm, "vm", "", "KubeVirt VM to capture, optionally qualified by namespace")
	capture.Flags().StringVar(&options.vmi, "vmi", "", "KubeVirt VMI to capture, optionally qualified by namespace")
	a.root.AddCommand(capture)
	a.addNetworkInspectCommand()
	a.addTraceCommand()
}

func (a *Application) addNetworkInspectCommand() {
	var pod, output string
	command := &cobra.Command{
		Use:   "inspect --pod [NAMESPACE/]POD",
		Short: "Show a Pod network namespace, interfaces and veth peers",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			if err := validatePodReference(pod); err != nil {
				return err
			}
			if output != "table" && output != "json" {
				return errors.New("--output must be table or json")
			}
			return nil
		},
		RunE: a.run(func(ctx context.Context, client *Client, _ []string) error {
			return a.networkInspect(ctx, client, pod, output)
		}),
	}
	command.Flags().StringVar(&pod, "pod", "", "Pod to inspect, optionally qualified by namespace")
	command.Flags().StringVarP(&output, "output", "o", "table", "Output format: table or json")
	parent := &cobra.Command{Use: "network", Short: "Inspect Pod network namespaces and interfaces"}
	parent.AddCommand(command)
	a.root.AddCommand(parent)
}

type traceOptions struct {
	pod, node, destination, mac, protocol, engine, arpOperation string
	port                                                        int
}

func (a *Application) addTraceCommand() {
	options := traceOptions{}
	var request traceRequest
	command := &cobra.Command{Use: "trace --pod POD|--node NODE --dst-ip IP", Short: "Trace a packet through OVN and OVS"}
	flags := command.Flags()
	flags.StringVar(&options.pod, "pod", "", "Source pod, optionally qualified by namespace")
	flags.StringVar(&options.node, "node", "", "Source node (mutually exclusive with --pod)")
	flags.StringVar(&options.destination, "dst-ip", "", "Destination IPv4 or IPv6 address")
	flags.StringVar(&options.mac, "dst-mac", "", "Destination MAC address (auto-detected when omitted)")
	flags.StringVar(&options.protocol, "protocol", "icmp", "Packet protocol: icmp, tcp, udp or arp")
	flags.IntVar(&options.port, "dst-port", 0, "Destination port for TCP or UDP")
	flags.StringVar(&options.arpOperation, "arp-op", "request", "ARP operation: request or reply")
	flags.StringVar(&options.engine, "engine", "all", "Trace engines: all (OVN then OVS) or ovn")
	command.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.NoArgs(cmd, args); err != nil {
			return err
		}
		if flags.Changed("arp-op") && options.protocol != "arp" {
			return errors.New("--arp-op requires --protocol=arp")
		}
		if flags.Changed("dst-port") && options.protocol != "tcp" && options.protocol != "udp" {
			return errors.New("--dst-port requires TCP or UDP")
		}
		var err error
		request, err = options.request()
		return err
	}
	command.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		return a.trace(ctx, client, request, options.engine == "ovn")
	})
	a.root.AddCommand(command)
}

func (o traceOptions) request() (traceRequest, error) {
	if (o.pod == "") == (o.node == "") {
		return traceRequest{}, errors.New("choose exactly one of --pod or --node")
	}
	if o.engine != "all" && o.engine != "ovn" {
		return traceRequest{}, errors.New("--engine must be all or ovn")
	}
	reference := o.pod
	if o.node != "" {
		if err := validateResourceName("node", o.node); err != nil {
			return traceRequest{}, err
		}
		reference = "node//" + o.node
	} else if err := validatePodReference(o.pod); err != nil {
		return traceRequest{}, err
	}
	args := []string{reference, o.destination}
	if o.mac != "" {
		mac, err := net.ParseMAC(o.mac)
		if err != nil || len(mac) != 6 {
			return traceRequest{}, errors.New("--dst-mac must be a six-byte MAC address")
		}
		args = append(args, mac.String())
	}
	args = append(args, o.protocol)
	switch o.protocol {
	case "tcp", "udp":
		args = append(args, strconv.Itoa(o.port))
	case "arp":
		args = append(args, o.arpOperation)
	}
	return parseTrace(args)
}

func (c *Client) networkSource(ctx context.Context, reference string) (*networkSource, error) {
	if node, ok := strings.CutPrefix(reference, "node//"); ok {
		return c.nodeSource(ctx, node)
	}
	pod, err := c.pod(ctx, reference)
	if err != nil {
		return nil, err
	}
	if pod.Spec.HostNetwork {
		return c.nodeSource(ctx, pod.Spec.NodeName)
	}
	return podNetworkSource(pod), nil
}

func podNetworkSource(pod *corev1.Pod) *networkSource {
	name := pod.Name
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "VirtualMachineInstance" {
			name = owner.Name
			break
		}
	}
	return &networkSource{
		node: pod.Spec.NodeName, lsp: name + "." + pod.Namespace,
		annotations: pod.Annotations, pod: pod,
		addresses: strings.Split(pod.Annotations[annotationPrefix+"ip_address"], ","),
	}
}

func (c *Client) nodeSource(ctx context.Context, name string) (*networkSource, error) {
	node, err := c.Kubernetes.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	source := &networkSource{node: name, lsp: node.Annotations[annotationPrefix+"port_name"], annotations: node.Annotations}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			source.addresses = append(source.addresses, address.Address)
		}
	}
	source.addresses = append(source.addresses, strings.Split(node.Annotations[annotationPrefix+"ip_address"], ",")...)
	return source, nil
}

func (c *Client) checkSource(ctx context.Context, source *networkSource) error {
	if source.pod == nil {
		return nil
	}
	pod, err := c.Kubernetes.CoreV1().Pods(source.pod.Namespace).Get(ctx, source.pod.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if pod.UID != source.pod.UID || pod.Spec.NodeName != source.node || pod.DeletionTimestamp != nil {
		return fmt.Errorf("pod %s/%s changed while resolving its network; retry", pod.Namespace, pod.Name)
	}
	return nil
}

func (c *Client) ovsRows(ctx context.Context, target Target, binary, columns, table string, conditions ...string) ([]map[string]any, error) {
	argv := append([]string{binary, "--format=json", "--columns=" + columns, "find", table}, conditions...)
	output, err := c.capture(ctx, target, argv...)
	if err != nil {
		return nil, err
	}
	var result struct {
		Headings []string `json:"headings"`
		Data     [][]any  `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("decode %s output: %w", binary, err)
	}
	rows := make([]map[string]any, 0, len(result.Data))
	for _, values := range result.Data {
		if len(values) != len(result.Headings) {
			return nil, errors.New("OVSDB headings and row length differ")
		}
		row := make(map[string]any, len(values))
		for i, value := range values {
			row[result.Headings[i]] = value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func ovsStrings(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		if len(v) == 2 && v[0] == "set" {
			if elements, ok := v[1].([]any); ok {
				var values []string
				for _, element := range elements {
					values = append(values, ovsStrings(element)...)
				}
				return values
			}
		}
	}
	return nil
}

func ovsMap(value any) map[string]string {
	result := map[string]string{}
	pair, ok := value.([]any)
	if !ok || len(pair) != 2 || pair[0] != "map" {
		return result
	}
	elements, ok := pair[1].([]any)
	if !ok {
		return result
	}
	for _, element := range elements {
		kv, ok := element.([]any)
		if !ok || len(kv) != 2 {
			continue
		}
		key, keyOK := kv[0].(string)
		val, valOK := kv[1].(string)
		if keyOK && valOK {
			result[key] = val
		}
	}
	return result
}

func (c *Client) podInterface(ctx context.Context, target Target, lsp string) (podInterface, error) {
	rows, err := c.ovsRows(ctx, target, "ovs-vsctl", "name,external_ids,ofport", "Interface", "external_ids:iface-id="+strconv.Quote(lsp))
	if err != nil {
		return podInterface{}, err
	}
	if len(rows) != 1 {
		return podInterface{}, fmt.Errorf("expected one OVS interface for %q, found %d", lsp, len(rows))
	}
	name, ok := rows[0]["name"].(string)
	if !ok || name == "" {
		return podInterface{}, errors.New("OVS interface has no name")
	}
	ofport, _ := rows[0]["ofport"].(float64)
	return podInterface{name: name, netns: ovsMap(rows[0]["external_ids"])["pod_netns"], ofport: int(ofport)}, nil
}

func namespaceCommand(netns string, argv ...string) []string {
	if netns == "" {
		return argv
	}
	return append([]string{"nsenter", "--net=" + netns, "--"}, argv...)
}

func (a *Application) tcpdump(ctx context.Context, client *Client, target captureTarget, args []string) error {
	options, err := parseCaptureArguments(args)
	if err != nil {
		return err
	}
	pod := target.pod
	ovs, err := client.nodeTarget(ctx, pod.Spec.NodeName, "ovs")
	if err != nil {
		return err
	}
	name, netns := "eth0", "/proc/1/ns/net"
	if options.iface != "" {
		name = options.iface
	}
	if pod.Spec.HostNetwork {
		if err := client.checkCaptureTarget(ctx, target); err != nil {
			return err
		}
		return client.Executor.Exec(ctx, ovs, captureCommand(netns, name, options), a.outputStreams())
	}
	source := podNetworkSource(pod)
	nic, err := client.podInterface(ctx, ovs, source.lsp)
	if err != nil || nic.netns == "" {
		var fallbackErr error
		netns, fallbackErr = client.podNetNS(ctx, ovs, pod)
		if fallbackErr != nil {
			if err != nil {
				return fmt.Errorf("find capture interface for %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			return fmt.Errorf("resolve capture network namespace for %s/%s: %w", pod.Namespace, pod.Name, fallbackErr)
		}
		netns = strings.TrimSpace(netns)
		if netns == "" {
			return errors.New("pod network namespace path is empty")
		}
	} else {
		netns = nic.netns
	}
	if pod.Annotations[annotationPrefix+"pod_nic_type"] == "internal-port" {
		if options.iface == "" && nic.name != "" {
			name = nic.name
		}
	}
	if err := client.checkCaptureTarget(ctx, target); err != nil {
		return err
	}
	return client.Executor.Exec(ctx, ovs, captureCommand(netns, name, options), a.outputStreams())
}

func captureCommand(netns, iface string, options packetCaptureOptions) []string {
	argv := []string{"capture", "--netns", netns, "--interface", iface}
	if options.count != 0 {
		argv = append(argv, "--count", strconv.Itoa(options.count))
	}
	if options.snaplen != defaultCaptureSnaplen {
		argv = append(argv, "--snaplen", strconv.Itoa(options.snaplen))
	}
	if options.pcap {
		argv = append(argv, "--pcap")
	}
	if options.list {
		argv = append(argv, "--list-interfaces")
	}
	return argv
}

type traceRequest struct {
	reference   string
	destination netip.Addr
	mac         string
	protocol    string
	port        uint16
	arpReply    bool
}

func parseTrace(args []string) (traceRequest, error) {
	var request traceRequest
	if len(args) < 3 {
		return request, errors.New("trace requires a source, destination IP and protocol")
	}
	request.reference = args[0]
	ip, err := netip.ParseAddr(args[1])
	if err != nil || ip.Zone() != "" {
		return request, fmt.Errorf("invalid destination IP %q", args[1])
	}
	request.destination = ip.Unmap()
	args = args[2:]
	if mac, err := net.ParseMAC(args[0]); err == nil && len(mac) == 6 {
		request.mac = mac.String()
		args = args[1:]
	}
	if len(args) == 0 {
		return request, errors.New("missing trace protocol")
	}
	request.protocol, args = args[0], args[1:]
	switch request.protocol {
	case "icmp":
		if len(args) != 0 {
			return request, errors.New("icmp does not accept a port")
		}
	case "tcp", "udp":
		if len(args) != 1 {
			return request, errors.New("tcp/udp require exactly one destination port")
		}
		port, err := strconv.ParseUint(args[0], 10, 16)
		if err != nil || port == 0 {
			return request, fmt.Errorf("invalid destination port %q", args[0])
		}
		request.port = uint16(port)
	case "arp":
		if !request.destination.Is4() {
			return request, errors.New("ARP requires IPv4")
		}
		if len(args) > 1 || len(args) == 1 && args[0] != "request" && args[0] != "reply" {
			return request, errors.New("ARP operation must be request or reply")
		}
		request.arpReply = len(args) == 1 && args[0] == "reply"
	default:
		return request, fmt.Errorf("unsupported trace protocol %q", request.protocol)
	}
	return request, nil
}

var subnetResource = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "subnets"}
