package ko

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

type podNetworkInfo struct {
	Namespace   string                `json:"namespace"`
	Name        string                `json:"name"`
	Node        string                `json:"node"`
	NetNS       string                `json:"netns"`
	HostNetwork bool                  `json:"hostNetwork"`
	Interfaces  []podNetworkInterface `json:"interfaces"`
}

type podNetworkInterface struct {
	Name       string          `json:"name"`
	Index      int             `json:"index"`
	PeerIndex  int             `json:"peerIndex,omitzero"`
	Kind       string          `json:"kind,omitempty"`
	MAC        string          `json:"mac,omitempty"`
	MTU        int             `json:"mtu,omitzero"`
	OperState  string          `json:"operState,omitempty"`
	Flags      []string        `json:"flags,omitempty"`
	Addresses  []string        `json:"addresses,omitempty"`
	Parent     *networkLink    `json:"parent,omitempty"`
	HostPeer   *networkLink    `json:"hostPeer,omitempty"`
	Peer       *networkLink    `json:"peer,omitempty"`
	Statistics *linkStatistics `json:"statistics,omitempty"`
}

type networkLink struct {
	Name       string          `json:"name"`
	Index      int             `json:"index"`
	PeerIndex  int             `json:"peerIndex,omitzero"`
	Kind       string          `json:"kind,omitempty"`
	MAC        string          `json:"mac,omitempty"`
	MTU        int             `json:"mtu,omitzero"`
	OperState  string          `json:"operState,omitempty"`
	Flags      []string        `json:"flags,omitempty"`
	Addresses  []string        `json:"addresses,omitempty"`
	Statistics *linkStatistics `json:"statistics,omitempty"`
}

// Counter names follow iproute2, including driver-specific error counters.
// uint64 preserves counters above the precision limit of JSON float64 values.
type linkStatistics struct {
	RX map[string]uint64 `json:"rx"`
	TX map[string]uint64 `json:"tx"`
}

type ipJSONLink struct {
	Index       int             `json:"ifindex"`
	Name        string          `json:"ifname"`
	LinkIndex   int             `json:"link_index"`
	LinkNetNSID *int            `json:"link_netnsid"`
	MTU         int             `json:"mtu"`
	OperState   string          `json:"operstate"`
	Address     string          `json:"address"`
	LinkName    string          `json:"link"`
	Flags       []string        `json:"flags"`
	LinkType    string          `json:"link_type"`
	LinkInfo    ipJSONLinkInfo  `json:"linkinfo"`
	AddrInfo    []ipJSONAddr    `json:"addr_info"`
	Stats64     *linkStatistics `json:"stats64"`
	Stats       *linkStatistics `json:"stats"`
}

type ipJSONLinkInfo struct {
	InfoKind string `json:"info_kind"`
}

type ipJSONAddr struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	PrefixLen int    `json:"prefixlen"`
}

func (c *Client) podNetwork(ctx context.Context, reference string) (*podNetworkInfo, error) {
	pod, err := c.pod(ctx, reference)
	if err != nil {
		return nil, err
	}
	target, err := c.nodeTarget(ctx, pod.Spec.NodeName, "ovs")
	if err != nil {
		return nil, err
	}

	netns := "/proc/1/ns/net"
	if !pod.Spec.HostNetwork {
		netns, err = c.podNetNS(ctx, target, pod)
		if err != nil {
			return nil, err
		}
	}
	podLinks, err := c.ipLinks(ctx, target, netns)
	if err != nil {
		return nil, fmt.Errorf("inspect pod network namespace %s: %w", netns, err)
	}
	hostLinks := podLinks
	if !pod.Spec.HostNetwork {
		hostLinks, err = c.ipLinks(ctx, target, "")
		if err != nil {
			return nil, fmt.Errorf("inspect host network namespace: %w", err)
		}
	}
	hostByIndex := make(map[int]networkLink, len(hostLinks))
	hostByName := make(map[string]networkLink, len(hostLinks))
	for _, link := range hostLinks {
		hostByIndex[link.Index] = link.networkLink()
		hostByName[link.Name] = link.networkLink()
	}
	podByIndex := make(map[int]networkLink, len(podLinks))
	for _, link := range podLinks {
		podByIndex[link.Index] = link.networkLink()
	}

	result := &podNetworkInfo{
		Namespace:   pod.Namespace,
		Name:        pod.Name,
		Node:        pod.Spec.NodeName,
		NetNS:       netns,
		HostNetwork: pod.Spec.HostNetwork,
		Interfaces:  make([]podNetworkInterface, 0, len(podLinks)),
	}
	for _, link := range podLinks {
		item := link.podNetworkInterface()
		switch {
		case link.isMacvlanOrIPVLAN():
			if parent, ok := link.parentLink(hostByName, hostByIndex, podByIndex); ok && (!pod.Spec.HostNetwork || parent.Index != link.Index) {
				item.Parent = new(parent)
			}
		case pod.Spec.HostNetwork && link.LinkNetNSID == nil && link.kind() == "veth":
			if peer, ok := hostByIndex[link.LinkIndex]; ok && peer.Index != link.Index {
				item.Peer = new(peer)
			}
		case !pod.Spec.HostNetwork && link.kind() == "veth":
			if peer, ok := hostByIndex[link.LinkIndex]; ok && peer.Kind == "veth" {
				item.HostPeer = new(peer)
			}
		}
		result.Interfaces = append(result.Interfaces, item)
	}
	return result, nil
}

func (c *Client) podNetNS(ctx context.Context, target Target, pod *corev1.Pod) (string, error) {
	podName := pod.Name
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "VirtualMachineInstance" {
			podName = owner.Name
			break
		}
	}
	rows, err := c.ovsRows(ctx, target, "ovs-vsctl", "name,external_ids,ofport", "Interface",
		"external_ids:pod_name="+strconv.Quote(podName),
		"external_ids:pod_namespace="+strconv.Quote(pod.Namespace))
	if err != nil && !c.ComponentFree {
		return "", fmt.Errorf("find OVS interfaces for pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	paths := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		externalIDs := ovsMap(row["external_ids"])
		if path := externalIDs["pod_netns"]; path != "" {
			paths[path] = struct{}{}
		}
	}
	if len(paths) == 0 {
		if c.ComponentFree {
			path, err := c.capture(ctx, target, "/kube-ovn/kubectl-ko-node-agent", "netns", string(pod.UID))
			if err != nil {
				return "", fmt.Errorf("resolve pod netns from host processes: %w", err)
			}
			path = strings.TrimSpace(path)
			if !strings.HasPrefix(path, "/proc/") || !strings.HasSuffix(path, "/ns/net") {
				return "", errors.New("helper returned an invalid pod netns path")
			}
			return path, nil
		}
		return "", fmt.Errorf("no OVS interface contains pod netns for %s/%s", pod.Namespace, pod.Name)
	}
	if len(paths) != 1 {
		return "", fmt.Errorf("pod %s/%s has multiple network namespaces: %s", pod.Namespace, pod.Name, strings.Join(slices.Sorted(maps.Keys(paths)), ", "))
	}
	for path := range paths {
		return path, nil
	}
	return "", errors.New("pod netns path is empty")
}

func (c *Client) ipLinks(ctx context.Context, target Target, netns string) ([]ipJSONLink, error) {
	output, err := c.capture(ctx, target, namespaceCommand(netns, "ip", "-s", "-j", "-d", "addr", "show")...)
	if err != nil {
		return nil, err
	}
	var links []ipJSONLink
	if err := json.Unmarshal([]byte(output), &links); err != nil {
		return nil, fmt.Errorf("decode ip link JSON: %w", err)
	}
	return links, nil
}

func (link ipJSONLink) networkLink() networkLink {
	return networkLink{
		Name:       link.Name,
		Index:      link.Index,
		PeerIndex:  link.LinkIndex,
		Kind:       link.kind(),
		MAC:        link.Address,
		MTU:        link.MTU,
		OperState:  link.OperState,
		Flags:      slices.Clone(link.Flags),
		Addresses:  link.addresses(),
		Statistics: cmp.Or(link.Stats64, link.Stats),
	}
}

func (link ipJSONLink) podNetworkInterface() podNetworkInterface {
	return podNetworkInterface{
		Name:       link.Name,
		Index:      link.Index,
		PeerIndex:  link.LinkIndex,
		Kind:       link.kind(),
		MAC:        link.Address,
		MTU:        link.MTU,
		OperState:  link.OperState,
		Flags:      slices.Clone(link.Flags),
		Addresses:  link.addresses(),
		Statistics: cmp.Or(link.Stats64, link.Stats),
	}
}

func (link ipJSONLink) isMacvlanOrIPVLAN() bool {
	kind := strings.ToLower(link.kind())
	return kind == "macvlan" || kind == "ipvlan"
}

func (link ipJSONLink) parentLink(byName map[string]networkLink, byIndex, localByIndex map[int]networkLink) (networkLink, bool) {
	if link.LinkNetNSID == nil {
		// With no external netns ID, the lower interface belongs to the
		// inspected namespace. Host names and indexes can identify other NICs.
		if parent, ok := localByIndex[link.LinkIndex]; ok && parent.Index != link.Index {
			return parent, true
		}
		// iproute2 can emit only the local lower device's name, without
		// link_index. Resolve it here before considering host names.
		for _, parent := range localByIndex {
			if link.LinkName != "" && parent.Name == link.LinkName && parent.Index != link.Index {
				return parent, true
			}
		}
	}
	if link.LinkName != "" {
		if parent, ok := byName[link.LinkName]; ok {
			return parent, true
		}
	}
	if link.LinkIndex != 0 {
		if parent, ok := byIndex[link.LinkIndex]; ok {
			return parent, true
		}
	}
	return networkLink{}, false
}

func (link ipJSONLink) kind() string {
	if link.LinkInfo.InfoKind != "" {
		return link.LinkInfo.InfoKind
	}
	return link.LinkType
}

func (link ipJSONLink) addresses() []string {
	addresses := make([]string, 0, len(link.AddrInfo))
	for _, address := range link.AddrInfo {
		if address.Local == "" {
			continue
		}
		addresses = append(addresses, fmt.Sprintf("%s/%d", address.Local, address.PrefixLen))
	}
	return addresses
}

func (a *Application) networkInspect(ctx context.Context, client *Client, pod, output string) error {
	info, err := client.podNetwork(ctx, pod)
	if err != nil {
		return err
	}
	if output == "json" {
		if err := json.MarshalWrite(a.streams.Out, info); err != nil {
			return err
		}
		_, err := io.WriteString(a.streams.Out, "\n")
		return err
	}
	return writePodNetwork(a.streams.Out, info)
}

func writePodNetwork(out io.Writer, info *podNetworkInfo) error {
	if _, err := fmt.Fprintf(out, "Pod: %s/%s\nNode: %s\nNetwork namespace: %s\nHost network: %t\nInterfaces:\n", info.Namespace, info.Name, info.Node, info.NetNS, info.HostNetwork); err != nil {
		return err
	}
	if len(info.Interfaces) == 0 {
		_, err := io.WriteString(out, "  (none)\n")
		return err
	}
	for _, item := range info.Interfaces {
		if _, err := fmt.Fprintf(out, "  - %s (ifindex=%d", item.Name, item.Index); err != nil {
			return err
		}
		if item.Kind != "" {
			if _, err := fmt.Fprintf(out, ", kind=%s", item.Kind); err != nil {
				return err
			}
		}
		if item.MAC != "" {
			if _, err := fmt.Fprintf(out, ", mac=%s", item.MAC); err != nil {
				return err
			}
		}
		if item.MTU != 0 {
			if _, err := fmt.Fprintf(out, ", mtu=%d", item.MTU); err != nil {
				return err
			}
		}
		if item.OperState != "" {
			if _, err := fmt.Fprintf(out, ", state=%s", item.OperState); err != nil {
				return err
			}
		}
		if len(item.Flags) > 0 {
			if _, err := fmt.Fprintf(out, ", flags=%s", strings.Join(item.Flags, ",")); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, ")\n"); err != nil {
			return err
		}
		if len(item.Addresses) > 0 {
			if _, err := fmt.Fprintf(out, "      addresses: %s\n", strings.Join(item.Addresses, ", ")); err != nil {
				return err
			}
		}
		if err := item.Statistics.write(out, "      "); err != nil {
			return err
		}
		var related *networkLink
		switch {
		case item.Parent != nil:
			related = item.Parent
			if _, err := fmt.Fprintf(out, "      parent: %s (ifindex=%d, kind=%s, mac=%s, mtu=%d, state=%s, flags=%s)\n", item.Parent.Name, item.Parent.Index, item.Parent.Kind, item.Parent.MAC, item.Parent.MTU, item.Parent.OperState, strings.Join(item.Parent.Flags, ",")); err != nil {
				return err
			}
		case item.HostPeer != nil:
			related = item.HostPeer
			if _, err := fmt.Fprintf(out, "      host peer: %s (ifindex=%d, kind=%s, mac=%s, mtu=%d, state=%s, flags=%s)\n", item.HostPeer.Name, item.HostPeer.Index, item.HostPeer.Kind, item.HostPeer.MAC, item.HostPeer.MTU, item.HostPeer.OperState, strings.Join(item.HostPeer.Flags, ",")); err != nil {
				return err
			}
		case item.Peer != nil:
			related = item.Peer
			if _, err := fmt.Fprintf(out, "      peer: %s (ifindex=%d, kind=%s, mac=%s, mtu=%d, state=%s, flags=%s)\n", item.Peer.Name, item.Peer.Index, item.Peer.Kind, item.Peer.MAC, item.Peer.MTU, item.Peer.OperState, strings.Join(item.Peer.Flags, ",")); err != nil {
				return err
			}
		case item.PeerIndex != 0:
			if _, err := fmt.Fprintf(out, "      peer ifindex: %d\n", item.PeerIndex); err != nil {
				return err
			}
		}
		if related != nil {
			if err := related.Statistics.write(out, "        "); err != nil {
				return err
			}
		}
	}
	return nil
}

func (statistics *linkStatistics) write(out io.Writer, indent string) error {
	if statistics == nil {
		return nil
	}
	for _, direction := range []struct {
		name     string
		counters map[string]uint64
	}{{"RX", statistics.RX}, {"TX", statistics.TX}} {
		if len(direction.counters) == 0 {
			continue
		}
		fields := make([]string, 0, len(direction.counters))
		for _, name := range slices.Sorted(maps.Keys(direction.counters)) {
			fields = append(fields, name+"="+strconv.FormatUint(direction.counters[name], 10))
		}
		if _, err := fmt.Fprintf(out, "%s%s: %s\n", indent, direction.name, strings.Join(fields, " ")); err != nil {
			return err
		}
	}
	return nil
}
