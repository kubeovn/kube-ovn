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
	datapath    string
	bridge      string
	physicalNIC string
	vlanID      int
	kind        sourceKind
	annotations map[string]string
	addresses   []string
	pod         *corev1.Pod
}

type podInterface struct {
	name   string
	kind   string
	netns  string
	ofport int
}

func (a *Application) addNetworkCommands() {
	var captureNode, observeAt string
	capture := &cobra.Command{
		Use: "capture TARGET [flags] -- [CAPTURE_ARGS...]", DisableFlagsInUseLine: true, Short: "Capture packets from a Kubernetes resource network namespace",
		Args: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 {
				dash = len(args)
			}
			if dash != 1 {
				return errors.New("capture requires exactly one target before --")
			}
			ref, err := parseSourceRef(args[0])
			if err != nil {
				return err
			}
			if captureNode != "" && ref.kind != sourceKindLocalnet && ref.kind != sourceKindLSP && ref.kind != sourceKindLRP {
				return errors.New("--node is only valid for localnet and OVN port targets")
			}
			if ref.kind == sourceKindLRP && observeAt == "" {
				return errors.New("LRP capture requires --observe-at lsp/NAME")
			}
			if ref.kind != sourceKindLRP && observeAt != "" {
				return errors.New("--observe-at is only valid for LRP capture")
			}
			if observeAt != "" {
				observed, err := parseSourceRef(observeAt)
				if err != nil {
					return err
				}
				if observed.kind != sourceKindLSP {
					return errors.New("--observe-at must be lsp/NAME")
				}
			}
			if captureNode != "" {
				if err := validateResourceName("node", captureNode); err != nil {
					return err
				}
			}
			options, err := parseCaptureArguments(args[1:])
			if err != nil {
				return err
			}
			if ref.kind == sourceKindNode && options.iface == "" && !options.list {
				return errors.New("node capture requires -i INTERFACE (or -D to list interfaces)")
			}
			return nil
		},
		RunE: a.run(func(ctx context.Context, client *Client, args []string) error {
			ref, err := parseSourceRef(args[0])
			if err != nil {
				return err
			}
			target, err := client.captureTargetOnNode(ctx, ref, captureNode, observeAt)
			if err != nil {
				return err
			}
			if target.vmi != nil {
				if _, err := fmt.Fprintf(a.streams.ErrOut, "Capturing VMI %s/%s through Pod %s on node %s\n", target.vmi.GetNamespace(), target.vmi.GetName(), target.pod.Name, target.pod.Spec.NodeName); err != nil {
					return err
				}
			}
			if target.observation != "" {
				if _, err := fmt.Fprintln(a.streams.ErrOut, target.observation); err != nil {
					return err
				}
			}
			return a.tcpdump(ctx, client, target, args[1:])
		}),
	}
	capture.Flags().StringVar(&captureNode, "node", "", "Node for a localnet capture")
	capture.Flags().StringVar(&observeAt, "observe-at", "", "Adjacent bound LSP for an LRP capture")
	a.root.AddCommand(capture)
	a.addNetworkInspectCommand()
	a.addTraceCommand()
}

func (a *Application) addNetworkInspectCommand() {
	var output string
	var ref sourceRef
	command := &cobra.Command{
		Use:   "inspect TARGET [flags]",
		Short: "Show a resource network namespace, interfaces and peers",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			var err error
			ref, err = parseSourceRef(args[0])
			if err != nil {
				return err
			}
			if ref.kind == sourceKindLocalnet || ref.kind == sourceKindLSP || ref.kind == sourceKindLRP {
				return fmt.Errorf("network inspect does not support %s targets", ref.kind)
			}
			if output != "table" && output != "json" {
				return errors.New("--output must be table or json")
			}
			return nil
		},
		RunE: a.run(func(ctx context.Context, client *Client, _ []string) error {
			return a.networkInspect(ctx, client, ref, output)
		}),
	}
	command.Flags().StringVarP(&output, "output", "o", "table", "Output format: table or json")
	parent := &cobra.Command{Use: "network", Short: "Inspect resource network namespaces and interfaces"}
	parent.AddCommand(command)
	a.root.AddCommand(parent)
}

type traceOptions struct {
	target, mac, protocol, engine, arpOperation, sourceIP, sourceMAC, node string
	port                                                                   int
}

func (a *Application) addTraceCommand() {
	options := traceOptions{}
	var request traceRequest
	command := &cobra.Command{Use: "trace TARGET --target IP [flags]", Short: "Trace a packet through OVN and OVS"}
	flags := command.Flags()
	flags.StringVar(&options.target, "target", "", "Destination IP address (addr/IP also accepted)")
	flags.StringVar(&options.mac, "dst-mac", "", "Destination MAC address (auto-detected when omitted)")
	flags.StringVar(&options.sourceIP, "source-ip", "", "Source IP for an OVN logical port without an address")
	flags.StringVar(&options.sourceMAC, "source-mac", "", "Source MAC for an OVN logical port without an address")
	flags.StringVar(&options.node, "node", "", "Node for the OVS trace of a localnet port")
	flags.StringVar(&options.protocol, "protocol", "icmp", "Packet protocol: icmp, tcp, udp or arp")
	flags.IntVar(&options.port, "dst-port", 0, "Destination port for TCP or UDP")
	flags.StringVar(&options.arpOperation, "arp-op", "request", "ARP operation: request or reply")
	flags.StringVar(&options.engine, "engine", "all", "Trace engines: all (OVN then OVS) or ovn")
	command.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		if flags.Changed("arp-op") && options.protocol != "arp" {
			return errors.New("--arp-op requires --protocol=arp")
		}
		if flags.Changed("dst-port") && options.protocol != "tcp" && options.protocol != "udp" {
			return errors.New("--dst-port requires TCP or UDP")
		}
		var err error
		ref, err := parseSourceRef(args[0])
		if err != nil {
			return err
		}
		if options.node != "" && ref.kind != sourceKindLocalnet && ref.kind != sourceKindLSP {
			return errors.New("--node is only valid for localnet and localnet LSP targets")
		}
		if options.node != "" && options.engine == "ovn" {
			return errors.New("--node requires the OVS trace engine")
		}
		if options.node != "" {
			if err := validateResourceName("node", options.node); err != nil {
				return err
			}
		}
		if ref.kind == sourceKindLRP {
			if flags.Changed("engine") && options.engine != "ovn" {
				return errors.New("LRP has no OVS interface; use --engine ovn")
			}
			options.engine = "ovn"
		}
		request, err = options.request(ref)
		return err
	}
	command.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		return a.trace(ctx, client, request, options.engine == "ovn")
	})
	a.root.AddCommand(command)
}

func (o traceOptions) request(ref sourceRef) (traceRequest, error) {
	if o.engine != "all" && o.engine != "ovn" {
		return traceRequest{}, errors.New("--engine must be all or ovn")
	}
	if o.target == "" {
		return traceRequest{}, errors.New("--target IP is required")
	}
	destination, err := parseTargetAddress(o.target)
	if err != nil {
		return traceRequest{}, err
	}
	args := []string{ref.reference(), destination}
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
	request, err := parseTrace(args)
	if err != nil {
		return traceRequest{}, err
	}
	request.node = o.node
	if o.sourceIP != "" {
		ip, err := netip.ParseAddr(o.sourceIP)
		if err != nil || ip.Zone() != "" {
			return traceRequest{}, fmt.Errorf("invalid --source-ip %q", o.sourceIP)
		}
		request.sourceIP = ip.Unmap()
	}
	if o.sourceMAC != "" {
		mac, err := net.ParseMAC(o.sourceMAC)
		if err != nil || len(mac) != 6 {
			return traceRequest{}, fmt.Errorf("invalid --source-mac %q", o.sourceMAC)
		}
		request.sourceMAC = mac.String()
	}
	if ref.kind == sourceKindLocalnet && (!request.sourceIP.IsValid() || request.sourceMAC == "") {
		return traceRequest{}, errors.New("localnet trace requires --source-ip and --source-mac")
	}
	return request, nil
}

func (c *Client) networkSource(ctx context.Context, reference string) (*networkSource, error) {
	ref, err := parseSourceRef(reference)
	if err != nil {
		return nil, err
	}
	switch ref.kind {
	case sourceKindNode:
		return c.nodeSource(ctx, ref.name)
	case sourceKindVM, sourceKindVMI:
		target, err := c.captureTarget(ctx, ref)
		if err != nil {
			return nil, err
		}
		return podNetworkSource(target.pod), nil
	case sourceKindPod, sourceKindService, sourceKindDeployment, sourceKindDaemonSet, sourceKindStatefulSet, sourceKindReplicaSet, sourceKindReplicationController, sourceKindJob:
		target, err := c.captureTarget(ctx, ref)
		if err != nil {
			return nil, err
		}
		pod := target.pod
		if pod.Spec.HostNetwork {
			return c.nodeSource(ctx, pod.Spec.NodeName)
		}
		return podNetworkSource(pod), nil
	default:
		return nil, fmt.Errorf("unsupported source kind %q", ref.kind)
	}
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
	rows, err := c.ovsRows(ctx, target, "ovs-vsctl", "name,type,external_ids,ofport", "Interface", "external_ids:iface-id="+strconv.Quote(lsp))
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
	kind, _ := rows[0]["type"].(string)
	return podInterface{name: name, kind: kind, netns: ovsMap(rows[0]["external_ids"])["pod_netns"], ofport: int(ofport)}, nil
}

func (c *Client) hostInterface(ctx context.Context, target Target, name string) (podInterface, error) {
	rows, err := c.ovsRows(ctx, target, "ovs-vsctl", "name,ofport", "Interface", "name="+strconv.Quote(name))
	if err != nil {
		return podInterface{}, err
	}
	if len(rows) != 1 || rows[0]["name"] != name {
		return podInterface{}, fmt.Errorf("expected one OVS interface %q, found %d", name, len(rows))
	}
	ofport, _ := rows[0]["ofport"].(float64)
	return podInterface{name: name, ofport: int(ofport)}, nil
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
	if target.node != "" {
		if target.iface != "" {
			if options.iface != "" && options.iface != target.iface {
				return fmt.Errorf("capture interface %q does not match resolved interface %q", options.iface, target.iface)
			}
			options.iface = target.iface
		} else if options.iface == "" && !options.list {
			return errors.New("node capture requires -i INTERFACE (or -D to list interfaces)")
		}
		ovs, err := client.nodeTarget(ctx, target.node, "ovs")
		if err != nil {
			return err
		}
		netns := target.netns
		if netns == "" {
			netns = "/proc/1/ns/net"
		}
		return client.Executor.Exec(ctx, ovs, captureCommand(netns, options.iface, options), a.outputStreams())
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
	sourceIP    netip.Addr
	sourceMAC   string
	node        string
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
