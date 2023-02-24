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
		return nil, fmt.Errorf("failed to get link %s: %v", iface.Name, err)
	}
	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes on link %s: %v", iface.Name, err)
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
	links, err := netlink.LinkList()
	if err != nil {
		return "", 0, err
	}

	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return "", 0, fmt.Errorf("failed to get addresses of link %s: %v", link.Attrs().Name, err)
		}
		for _, addr := range addrs {
			if addr.IPNet.Contains(net.ParseIP(ip)) && addr.IP.String() == ip {
				return link.Attrs().Name, link.Attrs().MTU, nil
			}
		}
	}

	return "", 0, fmt.Errorf("failed to find interface by address %s", ip)
}

func (config *Configuration) initRuntimeConfig(node *corev1.Node) error {
	// nothing to do on Linux
	return nil
}
