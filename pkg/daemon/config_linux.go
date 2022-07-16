package daemon

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
)

const defaultBindSocket = "/run/openvswitch/kube-ovn-daemon.sock"

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
