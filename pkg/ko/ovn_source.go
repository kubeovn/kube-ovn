package ko

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	vlanResource            = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vlans"}
	providerNetworkResource = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "providernetworks"}
)

func (c *Client) ovnSource(ctx context.Context, ref sourceRef, nodeHint string, needOVS bool) (*networkSource, error) {
	nb, err := c.leader(ctx, "nb")
	if err != nil {
		return nil, err
	}
	if ref.kind == sourceKindLRP {
		return c.routerPortSource(ctx, nb, ref.name)
	}
	portName := ref.name
	if ref.kind == sourceKindLocalnet {
		portName = "localnet." + ref.name
	}
	rows, err := c.ovsRows(ctx, nb, "ovn-nbctl", "name,type,addresses,dynamic_addresses,external_ids,options", "Logical_Switch_Port", "name="+strconv.Quote(portName))
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("expected one logical switch port %q, found %d", portName, len(rows))
	}
	row := rows[0]
	if row["name"] != portName {
		return nil, fmt.Errorf("logical switch port %q changed during lookup", portName)
	}
	portType, _ := row["type"].(string)
	if ref.kind == sourceKindLocalnet && portType != "localnet" {
		return nil, fmt.Errorf("logical switch port %q is %q, not localnet", portName, portType)
	}
	if portType == "localnet" {
		subnet := ovsMap(row["external_ids"])["ls"]
		if subnet == "" || ref.kind == sourceKindLocalnet && subnet != ref.name {
			return nil, fmt.Errorf("localnet port %q does not identify the requested subnet", portName)
		}
		return c.localnetSource(ctx, subnet, portName, ovsMap(row["options"])["network_name"], nodeHint, needOVS)
	}
	if ref.kind != sourceKindLSP {
		return nil, fmt.Errorf("%s requires a localnet logical switch port", ref.reference())
	}
	if nodeHint != "" {
		return nil, errors.New("--node is only valid for a localnet LSP")
	}
	switchName := ovsMap(row["external_ids"])["ls"]
	if switchName == "" {
		return nil, fmt.Errorf("logical switch port %q has no logical switch identity", portName)
	}
	source := &networkSource{kind: sourceKindLSP, lsp: portName, datapath: switchName, bridge: "br-int", annotations: map[string]string{annotationPrefix + "logical_switch": switchName}}
	for _, value := range append(ovsStrings(row["addresses"]), ovsStrings(row["dynamic_addresses"])...) {
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		if mac, err := validMAC(fields[0]); err == nil {
			source.annotations[annotationPrefix+"mac_address"] = mac
			for _, field := range fields[1:] {
				if ip, err := netip.ParseAddr(field); err == nil {
					source.addresses = append(source.addresses, ip.Unmap().String())
				}
			}
			break
		}
	}
	source.annotations[annotationPrefix+"ip_address"] = strings.Join(source.addresses, ",")
	if needOVS {
		source.node, err = c.boundLSPNode(ctx, portName)
		if err != nil {
			return nil, err
		}
	}
	return source, nil
}

func (c *Client) populateLSPSubnet(ctx context.Context, source *networkSource) error {
	subnet, err := c.Dynamic.Resource(subnetResource).Get(ctx, source.datapath, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cidr, _, err := unstructured.NestedString(subnet.Object, "spec", "cidrBlock")
	if err != nil {
		return err
	}
	source.annotations[annotationPrefix+"cidr"] = cidr
	router, _, err := unstructured.NestedString(subnet.Object, "spec", "vpc")
	if err != nil {
		return err
	}
	source.annotations[annotationPrefix+"logical_router"] = router
	return nil
}

func (c *Client) localnetSource(ctx context.Context, subnetName, portName, networkName, nodeHint string, needOVS bool) (*networkSource, error) {
	subnet, err := c.Dynamic.Resource(subnetResource).Get(ctx, subnetName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get localnet subnet %q: %w", subnetName, err)
	}
	vlanName, _, err := unstructured.NestedString(subnet.Object, "spec", "vlan")
	if err != nil || vlanName == "" {
		return nil, fmt.Errorf("subnet %q has no valid VLAN: %w", subnetName, err)
	}
	vlan, err := c.Dynamic.Resource(vlanResource).Get(ctx, vlanName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get VLAN %q: %w", vlanName, err)
	}
	provider, _, err := unstructured.NestedString(vlan.Object, "spec", "provider")
	if err != nil || provider == "" || provider != networkName {
		return nil, fmt.Errorf("localnet port %q provider %q does not match VLAN %q provider %q", portName, networkName, vlanName, provider)
	}
	providerNetwork, err := c.Dynamic.Resource(providerNetworkResource).Get(ctx, provider, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get ProviderNetwork %q: %w", provider, err)
	}
	exchange, _, err := unstructured.NestedBool(providerNetwork.Object, "spec", "exchangeLinkName")
	if err != nil {
		return nil, err
	}
	cidr, _, err := unstructured.NestedString(subnet.Object, "spec", "cidrBlock")
	if err != nil {
		return nil, err
	}
	source := &networkSource{
		kind: sourceKindLocalnet, lsp: portName, datapath: subnetName, bridge: util.ExternalBridgeName(provider),
		annotations: map[string]string{annotationPrefix + "logical_switch": subnetName, annotationPrefix + "cidr": cidr},
	}
	if !needOVS {
		return source, nil
	}
	source.node, source.physicalNIC, err = c.providerNodeInterface(ctx, provider, nodeHint)
	if err != nil {
		return nil, err
	}
	if exchange {
		source.bridge, source.physicalNIC = source.physicalNIC, source.bridge
	}
	id, found, err := unstructured.NestedInt64(vlan.Object, "spec", "id")
	if err != nil {
		return nil, err
	}
	if found {
		source.vlanID = int(id)
	}
	return source, nil
}

func (c *Client) providerNodeInterface(ctx context.Context, provider, hint string) (string, string, error) {
	readyKey := fmt.Sprintf(util.ProviderNetworkReadyTemplate, provider)
	interfaceKey := fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, provider)
	var nodes []corev1.Node
	if hint != "" {
		node, err := c.Kubernetes.CoreV1().Nodes().Get(ctx, hint, metav1.GetOptions{})
		if err != nil {
			return "", "", err
		}
		nodes = append(nodes, *node)
	} else {
		list, err := c.Kubernetes.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: readyKey + "=true"})
		if err != nil {
			return "", "", err
		}
		nodes = list.Items
	}
	if len(nodes) != 1 {
		return "", "", fmt.Errorf("ProviderNetwork %q has %d candidate nodes; specify --node", provider, len(nodes))
	}
	node := nodes[0]
	if node.Labels[readyKey] != "true" || node.Labels[interfaceKey] == "" {
		return "", "", fmt.Errorf("ProviderNetwork %q is not ready on node %q", provider, node.Name)
	}
	return node.Name, node.Labels[interfaceKey], nil
}

func (c *Client) routerPortSource(ctx context.Context, nb Target, name string) (*networkSource, error) {
	rows, err := c.ovsRows(ctx, nb, "ovn-nbctl", "name,mac,networks,external_ids", "Logical_Router_Port", "name="+strconv.Quote(name))
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 || rows[0]["name"] != name {
		return nil, fmt.Errorf("expected one logical router port %q, found %d", name, len(rows))
	}
	row := rows[0]
	router := ovsMap(row["external_ids"])["lr"]
	if router == "" {
		return nil, fmt.Errorf("logical router port %q has no router identity", name)
	}
	mac, _ := row["mac"].(string)
	mac, err = validMAC(mac)
	if err != nil {
		return nil, err
	}
	source := &networkSource{kind: sourceKindLRP, lsp: name, datapath: router, bridge: "br-int", annotations: map[string]string{annotationPrefix + "mac_address": mac, annotationPrefix + "logical_router": router}}
	for _, value := range ovsStrings(row["networks"]) {
		prefix, err := netip.ParsePrefix(value)
		if err == nil {
			source.addresses = append(source.addresses, prefix.Addr().Unmap().String())
		}
	}
	source.annotations[annotationPrefix+"ip_address"] = strings.Join(source.addresses, ",")
	return source, nil
}

func (c *Client) boundLSPNode(ctx context.Context, name string) (string, error) {
	sb, err := c.leader(ctx, "sb")
	if err != nil {
		return "", err
	}
	rows, err := c.ovsRows(ctx, sb, "ovn-sbctl", "logical_port,chassis", "Port_Binding", "logical_port="+strconv.Quote(name))
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("expected one Port_Binding for %q, found %d", name, len(rows))
	}
	uuid := ovsUUID(rows[0]["chassis"])
	if uuid == "" {
		return "", fmt.Errorf("logical switch port %q is not bound to a chassis", name)
	}
	chassis, err := c.ovsRows(ctx, sb, "ovn-sbctl", "hostname", "Chassis", "_uuid="+uuid)
	if err != nil {
		return "", err
	}
	if len(chassis) != 1 {
		return "", fmt.Errorf("expected one chassis %q, found %d", uuid, len(chassis))
	}
	hostname, _ := chassis[0]["hostname"].(string)
	if hostname == "" {
		return "", fmt.Errorf("chassis %q has no hostname", uuid)
	}
	return hostname, nil
}

func (c *Client) checkLRPObservation(ctx context.Context, routerPort, observedPort string) error {
	nb, err := c.leader(ctx, "nb")
	if err != nil {
		return err
	}
	if _, err := c.routerPortSource(ctx, nb, routerPort); err != nil {
		return err
	}
	peers, err := c.ovsRows(ctx, nb, "ovn-nbctl", "name,options,external_ids", "Logical_Switch_Port", "type="+strconv.Quote("router"))
	if err != nil {
		return err
	}
	peerSwitch := ""
	for _, peer := range peers {
		if ovsMap(peer["options"])["router-port"] == routerPort {
			if peerSwitch != "" {
				return fmt.Errorf("LRP %q has multiple adjacent switches", routerPort)
			}
			peerSwitch = ovsMap(peer["external_ids"])["ls"]
		}
	}
	if peerSwitch == "" {
		return fmt.Errorf("LRP %q has no adjacent logical switch", routerPort)
	}
	ports, err := c.ovsRows(ctx, nb, "ovn-nbctl", "name,type,external_ids", "Logical_Switch_Port", "name="+strconv.Quote(observedPort))
	if err != nil {
		return err
	}
	if len(ports) != 1 || ports[0]["name"] != observedPort || ports[0]["type"] == "router" || ovsMap(ports[0]["external_ids"])["ls"] != peerSwitch {
		return fmt.Errorf("LSP %q is not a capturable port adjacent to LRP %q", observedPort, routerPort)
	}
	return nil
}

func ovsUUID(value any) string {
	if parts, ok := value.([]any); ok && len(parts) == 2 && parts[0] == "uuid" {
		uuid, _ := parts[1].(string)
		return uuid
	}
	return ""
}
