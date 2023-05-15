package daemon

import (
	"time"

	"github.com/kubeovn/gonetworkmanager/v2"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
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

	running, err := nm.Running()
	if err != nil {
		klog.Warningf("failed to check NetworkManager running state: %v", err)
		return nil
	}
	if !running {
		klog.V(5).Info("NetworkManager is not running, ignore")
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

	klog.Infof(`setting device %s NetworkManager property "managed" to %v`, device, managed)
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
		return
	}

	// wait route event on the link for 50ms
	timer := time.NewTimer(50 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			// timeout, interface configuration is expected to be completed
			done <- struct{}{}
			return
		case event := <-ch:
			if event.LinkIndex == linkIndex {
				// received a route event on the link
				// stop the timer
				if !timer.Stop() {
					<-timer.C
				}
				// reset the timer, wait for another 50ms
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

	// set link unmanaged by NetworkManager
	if err = nmSetManaged(current, false); err != nil {
		klog.Errorf("failed set device %s unmanaged by NetworkManager: %v", current, err)
		return false, err
	}

	klog.Infof("renaming link %s as %s", current, target)
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

	index := link.Attrs().Index
	if link, err = netlink.LinkByIndex(index); err != nil {
		klog.Errorf("failed to get link %s by index %d: %v", target, index, err)
		return false, err
	}

	if util.ContainsString(link.Attrs().Properties.AlternativeIfnames, current) {
		if err = netlink.LinkDelAltName(link, current); err != nil {
			klog.Errorf("failed to delete alternative name %s from link %s: %v", current, link.Attrs().Name, err)
			return false, err
		}
	}

	return true, nil
}
