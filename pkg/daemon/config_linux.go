package daemon

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
)

const defaultBindSocket = "/run/openvswitch/kube-ovn-daemon.sock"

func getSrcIPsByRoutes(iface *net.Interface) ([]string, error) {
	link, err := netlink.LinkByName(iface.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get link %s: %w", iface.Name, err)
	}
	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes on link %s: %w", iface.Name, err)
	}

	srcIPs := make([]string, 0, 2)
	for _, r := range routes {
		if r.Src != nil && r.Scope == netlink.SCOPE_LINK {
			srcIPs = append(srcIPs, r.Src.String())
		}
	}
	return srcIPs, nil
}

func getIfaceByIP(ip string) (string, int, error) {
	target := net.ParseIP(ip)
	if target == nil {
		return "", 0, fmt.Errorf("invalid IP address %q", ip)
	}

	// Use a single AddrList dump instead of LinkList + per-link AddrList (N+1 dumps → 1+1)
	addrs, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return "", 0, fmt.Errorf("failed to list all addresses: %w", err)
	}

	for _, addr := range addrs {
		if addr.Contains(target) && addr.IP.Equal(target) {
			link, err := netlink.LinkByIndex(addr.LinkIndex)
			if err != nil {
				return "", 0, fmt.Errorf("failed to get link by index %d: %w", addr.LinkIndex, err)
			}
			return link.Attrs().Name, link.Attrs().MTU, nil
		}
	}

	return "", 0, fmt.Errorf("failed to find interface by address %s", ip)
}

func (config *Configuration) initRuntimeConfig(_ *corev1.Node) error {
	// nothing to do on Linux
	return nil
}
