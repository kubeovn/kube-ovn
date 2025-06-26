package daemon

import (
	"fmt"
	"math"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const defaultBindSocket = "/run/openvswitch/kube-ovn-daemon.sock"

func getSrcIPsByRoutes(iface string) ([]string, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, fmt.Errorf("failed to get link %s: %w", iface, err)
	}
	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes on link %s: %w", iface, err)
	}

	srcIPs := make([]string, 0, 2)
	for _, r := range routes {
		if r.Src != nil && r.Scope == netlink.SCOPE_LINK {
			srcIPs = append(srcIPs, r.Src.String())
		}
	}
	return srcIPs, nil
}

func getIPsConfiguredByDHCP(iface string) ([]string, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, fmt.Errorf("failed to get link %s: %w", iface, err)
	}
	addresses, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses of link %s: %w", iface, err)
	}

	var ips []string
	for _, addr := range addresses {
		if addr.Flags&unix.IFA_F_PERMANENT == 0 &&
			(addr.ValidLft > 0 && addr.ValidLft != math.MaxUint32) {
			klog.Infof("Found temporary address %s configured by DHCP", addr.IPNet.String())
			ips = append(ips, addr.IPNet.IP.String())
		}
	}

	return ips, nil
}

func getIfaceByIP(ip string) (string, int, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return "", 0, err
	}

	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return "", 0, fmt.Errorf("failed to get addresses of link %s: %w", link.Attrs().Name, err)
		}
		for _, addr := range addrs {
			if addr.Contains(net.ParseIP(ip)) && addr.IP.String() == ip {
				return link.Attrs().Name, link.Attrs().MTU, nil
			}
		}
	}

	return "", 0, fmt.Errorf("failed to find interface by address %s", ip)
}

func (config *Configuration) initRuntimeConfig(_ *corev1.Node) error {
	// nothing to do on Linux
	return nil
}
