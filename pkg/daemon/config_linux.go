package daemon

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

func getIfaceOwnPodIP(podIP string) (*net.Interface, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, fmt.Errorf("failed to get a list of IP addresses %v", err)
		}
		for _, addr := range addrs {
			if addr.IPNet.Contains(net.ParseIP(podIP)) && addr.IP.String() == podIP {
				return &net.Interface{
					Index:        link.Attrs().Index,
					MTU:          link.Attrs().MTU,
					Name:         link.Attrs().Name,
					HardwareAddr: link.Attrs().HardwareAddr,
					Flags:        link.Attrs().Flags,
				}, nil
			}
		}
	}

	return nil, errors.New("unable to find podIP interface")
}
