package daemon

import (
	"time"

	"k8s.io/klog/v2"

	"github.com/Wifx/gonetworkmanager"
	"github.com/vishvananda/netlink"
)

var routeScopeOrders = [...]netlink.Scope{
	netlink.SCOPE_HOST,
	netlink.SCOPE_LINK,
	netlink.SCOPE_SITE,
	netlink.SCOPE_UNIVERSE,
}

func nmSetManaged(device string, managed bool) error {
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		klog.V(5).Infof("failed to connect to NetworkManager: %v", err)
		return nil
	}

	d, err := nm.GetDeviceByIpIface(device)
	if err != nil {
		klog.Errorf("failed to get device by IP iface %s: %v", device, err)
		return err
	}
	current, err := d.GetPropertyManaged()
	if err != nil {
		klog.Errorf("failed to get device property managed: %v", err)
		return err
	}
	if current == managed {
		return nil
	}

	if err = d.SetPropertyManaged(managed); err != nil {
		klog.Errorf("failed to set device property managed to %v: %v", managed, err)
		return err
	}

	return nil
}

// wait systemd-networkd to finish interface configuration
func waitNetworkdConfiguration(linkIndex int) {
	done := make(chan struct{})
	ch := make(chan netlink.RouteUpdate)
	if err := netlink.RouteSubscribe(ch, done); err != nil {
		klog.Warningf("failed to subscribe route update events: %v", err)
		klog.Info("Waiting 100ms ...")
		time.Sleep(100 * time.Millisecond)
	}

	timer := time.NewTimer(50 * time.Millisecond)
LOOP:
	for {
		select {
		case <-timer.C:
			done <- struct{}{}
			break LOOP
		case event := <-ch:
			if event.LinkIndex == linkIndex {
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(50 * time.Millisecond)
			}
		}
	}
}

func changeProvideNicName(current, target string) (bool, error) {
	link, err := netlink.LinkByName(current)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			klog.Infof("link %s not found, skip", current)
			return false, nil
		}
		klog.Errorf("failed to get link %s: %v", current, err)
		return false, err
	}
	if link.Type() == "openvswitch" {
		klog.V(3).Infof("%s is an openvswitch interface, skip", current)
		return true, nil
	}

	// set link unmanaged by NetworkManager
	if err = nmSetManaged(current, false); err != nil {
		klog.Errorf("failed set device %s to unmanaged by NetworkManager: %v", current, err)
		return false, err
	}

	klog.Infof("renaming link %s as %s", current, target)
	addresses, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		klog.Errorf("failed to list addresses of link %s: %v", current, err)
		return false, err
	}
	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		klog.Errorf("failed to list routes of link %s: %v", current, err)
		return false, err
	}

	if err = netlink.LinkSetDown(link); err != nil {
		klog.Errorf("failed to set link %s down: %v", current, err)
		return false, err
	}
	if err = netlink.LinkSetName(link, target); err != nil {
		klog.Errorf("failed to set name of link %s to %s: %v", current, target, err)
		return false, err
	}
	if err = netlink.LinkSetUp(link); err != nil {
		klog.Errorf("failed to set link %s up: %v", target, err)
		return false, err
	}
	klog.Infof("link %s has been renamed as %s", current, target)

	waitNetworkdConfiguration(link.Attrs().Index)

	for _, addr := range addresses {
		if addr.IP.IsLinkLocalUnicast() {
			continue
		}
		addr.Label = ""
		if err = netlink.AddrReplace(link, &addr); err != nil {
			klog.Errorf("failed to replace address %q: %v", addr.String(), err)
			return false, err
		}
		klog.Infof("address %q has been added/replaced to link %s", addr.String(), target)
	}

	for _, scope := range routeScopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.IP.IsLinkLocalUnicast() {
				continue
			}
			if route.Scope == scope {
				if err = netlink.RouteReplace(&route); err != nil {
					klog.Errorf("failed to replace route %q to %s: %v", route.String(), target, err)
					return false, err
				}
				klog.Infof("route %q has been added/replaced to link %s", route.String(), target)
			}
		}
	}

	return true, nil
}
