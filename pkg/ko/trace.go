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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type tracePlan struct {
	request        traceRequest
	sourceIP       netip.Addr
	sourceMAC      string
	destinationMAC string
	switchName     string
	lsp            string
}

func (a *Application) trace(ctx context.Context, client *Client, request traceRequest, ovnOnly bool) error {
	source, err := client.networkSource(ctx, request.reference)
	if err != nil {
		return err
	}
	plan, err := prepareTrace(source, request)
	if err != nil {
		return err
	}
	nb, err := client.leader(ctx, "nb")
	if err != nil {
		return err
	}
	ovs, err := client.nodeTarget(ctx, source.node, "ovs")
	if err != nil {
		return err
	}
	if plan.destinationMAC == "" {
		plan.destinationMAC, err = client.destinationMAC(ctx, source, nb, ovs, plan)
		if err != nil {
			return err
		}
	}
	sb, err := client.leader(ctx, "sb")
	if err != nil {
		return err
	}
	if err := client.checkSource(ctx, source); err != nil {
		return err
	}
	if err := client.Executor.Exec(ctx, sb, plan.ovnCommand(), a.outputStreams()); err != nil {
		return err
	}
	if ovnOnly {
		return nil
	}
	nic, err := client.podInterface(ctx, ovs, source.lsp)
	if err != nil {
		return err
	}
	if nic.ofport <= 0 {
		return fmt.Errorf("interface %s has no valid OpenFlow port", nic.name)
	}
	if _, err := fmt.Fprintln(a.streams.Out, "\n--------\nStart OVS Tracing"); err != nil {
		return err
	}
	return client.Executor.Exec(ctx, ovs, plan.ovsCommand(nic.ofport), a.outputStreams())
}

func prepareTrace(source *networkSource, request traceRequest) (tracePlan, error) {
	plan := tracePlan{request: request, lsp: source.lsp, switchName: source.annotations[annotationPrefix+"logical_switch"], destinationMAC: request.mac}
	if plan.lsp == "" || plan.switchName == "" {
		return plan, errors.New("source logical port or switch is not ready")
	}
	mac, err := net.ParseMAC(source.annotations[annotationPrefix+"mac_address"])
	if err != nil || len(mac) != 6 {
		return plan, errors.New("source MAC address is not ready")
	}
	plan.sourceMAC = mac.String()
	addresses := source.addresses
	// ARP originates from the OVN port, not a node external/InternalIP address.
	if request.protocol == "arp" {
		addresses = strings.Split(source.annotations[annotationPrefix+"ip_address"], ",")
	}
	for _, candidate := range addresses {
		ip, err := netip.ParseAddr(candidate)
		if err == nil && ip.Unmap().Is4() == request.destination.Is4() {
			plan.sourceIP = ip.Unmap()
			break
		}
	}
	if !plan.sourceIP.IsValid() {
		return plan, errors.New("source has no address in the destination family")
	}
	if request.protocol == "arp" && !request.arpReply && request.mac == "" {
		plan.destinationMAC = "ff:ff:ff:ff:ff:ff"
	}
	return plan, nil
}

func (p tracePlan) ovnCommand() []string {
	base := fmt.Sprintf("inport == %s && eth.src == %s && eth.dst == %s", strconv.Quote(p.lsp), p.sourceMAC, p.destinationMAC)
	if p.request.protocol == "arp" {
		op, targetMAC := 1, "00:00:00:00:00:00"
		if p.request.arpReply {
			op, targetMAC = 2, p.destinationMAC
		}
		match := fmt.Sprintf("%s && arp.op == %d && arp.sha == %s && arp.tha == %s && arp.spa == %s && arp.tpa == %s", base, op, p.sourceMAC, targetMAC, p.sourceIP, p.request.destination)
		return []string{"ovn-trace", p.switchName, match}
	}
	family := 4
	if p.request.destination.Is6() {
		family = 6
	}
	match := fmt.Sprintf("%s && ip.ttl == 255 && ip%d.src == %s && ip%d.dst == %s", base, family, p.sourceIP, family, p.request.destination)
	switch p.request.protocol {
	case "icmp":
		match += " && icmp"
		if family == 6 {
			match += "6.type == 128"
		}
	case "tcp", "udp":
		match += fmt.Sprintf(" && %s.src == 30000 && %s.dst == %d", p.request.protocol, p.request.protocol, p.request.port)
		if p.request.protocol == "tcp" {
			match += " && tcp.flags == 2"
		}
	}
	return []string{"ovn-trace", "--ct=new", "--ct=new", "--ct=new", "--ct=new", p.switchName, match}
}

func (p tracePlan) ovsCommand(port int) []string {
	base := fmt.Sprintf("in_port=%d,dl_src=%s,dl_dst=%s", port, p.sourceMAC, p.destinationMAC)
	var flow string
	if p.request.protocol == "arp" {
		op, targetMAC := 1, "00:00:00:00:00:00"
		if p.request.arpReply {
			op, targetMAC = 2, p.destinationMAC
		}
		flow = fmt.Sprintf("%s,arp,arp_op=%d,arp_spa=%s,arp_tpa=%s,arp_sha=%s,arp_tha=%s", base, op, p.sourceIP, p.request.destination, p.sourceMAC, targetMAC)
	} else {
		protocol, network := p.request.protocol, "nw"
		if p.request.destination.Is6() {
			protocol += "6"
			network = "ipv6"
		}
		flow = fmt.Sprintf("%s,%s,nw_ttl=64,%s_src=%s,%s_dst=%s", base, protocol, network, p.sourceIP, network, p.request.destination)
		if p.request.protocol != "icmp" {
			flow += fmt.Sprintf(",%s_src=1000,%s_dst=%d", p.request.protocol, p.request.protocol, p.request.port)
		}
	}
	return []string{"ovs-appctl", "ofproto/trace", "br-int", flow}
}

func sameSubnet(address netip.Addr, cidrs string) bool {
	for cidr := range strings.SplitSeq(cidrs, ",") {
		if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}

func validMAC(value string) (string, error) {
	mac, err := net.ParseMAC(strings.TrimSpace(value))
	if err != nil || len(mac) != 6 {
		return "", fmt.Errorf("invalid Ethernet MAC %q", value)
	}
	return mac.String(), nil
}

func (c *Client) destinationMAC(ctx context.Context, source *networkSource, nb, ovs Target, plan tracePlan) (string, error) {
	if sameSubnet(plan.request.destination, source.annotations[annotationPrefix+"cidr"]) {
		mac, err := c.logicalPortMAC(ctx, nb, plan.request.destination)
		if err != nil || mac != "" {
			return mac, err
		}
	}
	if plan.request.protocol == "arp" {
		return "", errors.New("ARP reply requires a destination MAC when the target has no logical port")
	}
	subnet, err := c.Dynamic.Resource(subnetResource).Get(ctx, plan.switchName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	vlan, _, err := unstructured.NestedString(subnet.Object, "spec", "vlan")
	if err != nil {
		return "", err
	}
	logical, _, err := unstructured.NestedBool(subnet.Object, "spec", "logicalGateway")
	if err != nil {
		return "", err
	}
	u2o, _, err := unstructured.NestedBool(subnet.Object, "spec", "u2oInterconnection")
	if err != nil {
		return "", err
	}
	if vlan != "" && !logical && !u2o {
		gateways, _, err := unstructured.NestedString(subnet.Object, "spec", "gateway")
		if err != nil {
			return "", err
		}
		return c.physicalGatewayMAC(ctx, source, ovs, gateways, plan.request.destination.Is4())
	}
	router := source.annotations[annotationPrefix+"logical_router"]
	if router == "" {
		router, _, err = unstructured.NestedString(subnet.Object, "spec", "vpc")
		if err != nil {
			return "", err
		}
	}
	rows, err := c.ovsRows(ctx, nb, "ovn-nbctl", "mac", "Logical_Router_Port", "name="+strconv.Quote(router+"-"+plan.switchName))
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("expected one router port for %s/%s, found %d", router, plan.switchName, len(rows))
	}
	mac, ok := rows[0]["mac"].(string)
	if !ok {
		return "", errors.New("router port has no MAC")
	}
	return validMAC(mac)
}

func (c *Client) logicalPortMAC(ctx context.Context, nb Target, address netip.Addr) (string, error) {
	rows, err := c.ovsRows(ctx, nb, "ovn-nbctl", "addresses,dynamic_addresses", "Logical_Switch_Port")
	if err != nil {
		return "", err
	}
	matches := map[string]bool{}
	for _, row := range rows {
		for _, entry := range append(ovsStrings(row["addresses"]), ovsStrings(row["dynamic_addresses"])...) {
			fields := strings.Fields(entry)
			if len(fields) < 2 {
				continue
			}
			for _, field := range fields[1:] {
				if ip, err := netip.ParseAddr(field); err == nil && ip.Unmap() == address {
					mac, err := validMAC(fields[0])
					if err != nil {
						return "", err
					}
					matches[mac] = true
				}
			}
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple MACs match %s; specify the destination MAC", address)
	}
	for mac := range matches {
		return mac, nil
	}
	return "", nil
}

type linkInfo struct {
	Index     int    `json:"ifindex"`
	Name      string `json:"ifname"`
	LinkIndex int    `json:"link_index"`
	Master    string `json:"master"`
}

func (c *Client) links(ctx context.Context, target Target, netns string, extra ...string) ([]linkInfo, error) {
	argv := namespaceCommand(netns, append([]string{"ip", "-s", "-j", "link", "show"}, extra...)...)
	output, err := c.capture(ctx, target, argv...)
	if err != nil {
		return nil, err
	}
	var links []linkInfo
	if err := json.Unmarshal([]byte(output), &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (c *Client) physicalGatewayMAC(ctx context.Context, source *networkSource, ovs Target, gateways string, ipv4 bool) (string, error) {
	gateway := ""
	for candidate := range strings.SplitSeq(gateways, ",") {
		if ip, err := netip.ParseAddr(candidate); err == nil && ip.Is4() == ipv4 {
			gateway = ip.String()
			break
		}
	}
	if gateway == "" {
		return "", errors.New("subnet has no gateway in the requested family")
	}
	nic, err := c.podInterface(ctx, ovs, source.lsp)
	if err != nil {
		return "", err
	}
	cni, err := c.nodeTarget(ctx, source.node, "kube-ovn-cni")
	if err != nil {
		return "", err
	}
	name, err := c.gatewayInterface(ctx, source, ovs, cni, nic)
	if err != nil {
		return "", err
	}
	argv := []string{"arping", "-c3", "-C1", "-i1", "-I", name, gateway}
	if !ipv4 {
		argv = []string{"ndisc6", "-q", gateway, name}
	}
	output, err := c.capture(ctx, cni, namespaceCommand(nic.netns, argv...)...)
	if err != nil {
		return "", err
	}
	for field := range strings.FieldsSeq(output) {
		if mac, err := validMAC(strings.Trim(field, "[](),")); err == nil {
			return mac, nil
		}
	}
	return "", errors.New("gateway neighbor discovery returned no MAC; specify the destination MAC")
}

func (c *Client) gatewayInterface(ctx context.Context, source *networkSource, ovs, cni Target, nic podInterface) (string, error) {
	name := nic.name
	if source.annotations[annotationPrefix+"pod_nic_type"] != "internal-port" && nic.netns != "" {
		hostLinks, err := c.links(ctx, ovs, "", "dev", nic.name)
		if err != nil {
			return "", err
		}
		if len(hostLinks) != 1 || hostLinks[0].LinkIndex == 0 {
			return "", errors.New("cannot resolve pod veth peer")
		}
		links, err := c.links(ctx, cni, nic.netns)
		if err != nil {
			return "", err
		}
		name = ""
		for _, link := range links {
			if link.Index == hostLinks[0].LinkIndex {
				name = link.Name
				break
			}
		}
		if name == "" {
			return "", errors.New("pod veth peer is absent from its network namespace")
		}
	}
	links, err := c.links(ctx, cni, nic.netns, "dev", name)
	if err != nil {
		return "", err
	}
	if len(links) != 1 || links[0].Master != "" {
		return "", errors.New("pod interface is missing or enslaved; specify the destination MAC")
	}
	return name, nil
}
