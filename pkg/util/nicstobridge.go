package util

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
	"k8s.io/klog"
)

func getBridgeName(iface string) string {
	return fmt.Sprintf("gw%s", iface)
}

// GetNicName returns the physical NIC name, given an OVS bridge name
// configured by NicToBridge()
func GetNicName(brName string) string {
	stdout, err := exec.Command("ovs-vsctl",
		"br-get-external-id", brName, "bridge-uplink").CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to get the bridge-uplink for the bridge %q:, stderr: %q, error: %v",
			brName, string(stdout), err)
		return ""
	}
	if string(stdout) == "" && strings.HasPrefix(brName, "br") {
		// This would happen if the bridge was created before the bridge-uplink
		// changes got integrated.
		return fmt.Sprintf("%s", brName[len("br"):])
	}
	return string(stdout)
}

func saveIPAddress(oldLink, newLink netlink.Link, addrs []netlink.Addr) error {
	for i := range addrs {
		addr := addrs[i]

		// Remove from oldLink
		if err := netlink.AddrDel(oldLink, &addr); err != nil {
			klog.Errorf("Remove addr from %q failed: %v", oldLink.Attrs().Name, err)
			return err
		}

		// Add to newLink
		addr.Label = newLink.Attrs().Name
		if err := netlink.AddrAdd(newLink, &addr); err != nil {
			klog.Errorf("Add addr to newLink %q failed: %v", newLink.Attrs().Name, err)
			return err
		}
		klog.Infof("Successfully saved addr %q to newLink %q", addr.String(), newLink.Attrs().Name)
	}

	return netlink.LinkSetUp(newLink)
}

// delAddRoute removes 'route' from 'oldLink' and moves to 'newLink'
func delAddRoute(oldLink, newLink netlink.Link, route netlink.Route) error {
	// Remove route from old interface
	if err := netlink.RouteDel(&route); err != nil && !strings.Contains(err.Error(), "no such process") {
		klog.Errorf("Remove route from %q failed: %v", oldLink.Attrs().Name, err)
		return err
	}

	// Add route to newLink
	route.LinkIndex = newLink.Attrs().Index
	if err := netlink.RouteAdd(&route); err != nil && !os.IsExist(err) {
		klog.Errorf("Add route to newLink %q failed: %v", newLink.Attrs().Name, err)
		return err
	}

	klog.Infof("Successfully saved route %q", route.String())
	return nil
}

func saveRoute(oldLink, newLink netlink.Link, routes []netlink.Route) error {
	for i := range routes {
		route := routes[i]

		// Handle routes for default gateway later.  This is a special case for
		// GCE where we have /32 IP addresses and we can't add the default
		// gateway before the route to the gateway.
		if route.Dst == nil && route.Gw != nil && route.LinkIndex > 0 {
			continue
		}

		err := delAddRoute(oldLink, newLink, route)
		if err != nil {
			return err
		}
	}

	// Now add the default gateway (if any) via this interface.
	for i := range routes {
		route := routes[i]
		if route.Dst == nil && route.Gw != nil && route.LinkIndex > 0 {
			// Remove route from 'oldLink' and move it to 'newLink'
			err := delAddRoute(oldLink, newLink, route)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// NicToBridge creates a OVS bridge for the 'iface' and also moves the IP
// address and routes of 'iface' to OVS bridge.
func NicToBridge(iface string) (netlink.Link, error) {
	ifaceLink, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, err
	}

	bridge := getBridgeName(iface)
	stdout, err := exec.Command("ovs-vsctl",
		"--", "--may-exist", "add-br", bridge,
		"--", "br-set-external-id", bridge, "bridge-id", bridge,
		"--", "br-set-external-id", bridge, "bridge-uplink", iface,
		"--", "set", "bridge", bridge, "fail-mode=standalone",
		fmt.Sprintf("other_config:hwaddr=%s", ifaceLink.Attrs().HardwareAddr),
		"--", "--may-exist", "add-port", bridge, iface,
		"--", "set", "port", iface, "other-config:transient=true",
		"--", "set", "Open_vSwitch", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=dataNet:%s", bridge)).CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to create OVS bridge, %v", string(stdout))
		return nil, err
	}
	klog.Infof("Successfully created OVS bridge %q", bridge)

	// Get ip addresses and routes before any real operations.
	addrs, err := netlink.AddrList(ifaceLink, syscall.AF_INET)
	if err != nil {
		return nil, err
	}
	routes, err := netlink.RouteList(ifaceLink, syscall.AF_INET)
	if err != nil {
		return nil, err
	}

	bridgeLink, err := netlink.LinkByName(bridge)
	if err != nil {
		return nil, err
	}

	// save ip addresses to bridge.
	if err = saveIPAddress(ifaceLink, bridgeLink, addrs); err != nil {
		return nil, err
	}

	// save routes to bridge.
	if err = saveRoute(ifaceLink, bridgeLink, routes); err != nil {
		return nil, err
	}

	return bridgeLink, nil
}

// BridgeToNic moves the IP address and routes of internal port of the bridge to
// underlying NIC interface and deletes the OVS bridge.
func BridgeToNic(bridge string) error {
	// Internal port is named same as the bridge
	bridgeLink, err := netlink.LinkByName(bridge)
	if err != nil {
		return err
	}

	// Get ip addresses and routes before any real operations.
	addrs, err := netlink.AddrList(bridgeLink, syscall.AF_INET)
	if err != nil {
		return err
	}
	routes, err := netlink.RouteList(bridgeLink, syscall.AF_INET)
	if err != nil {
		return err
	}

	ifaceLink, err := netlink.LinkByName(GetNicName(bridge))
	if err != nil {
		return err
	}

	// save ip addresses to iface.
	if err = saveIPAddress(bridgeLink, ifaceLink, addrs); err != nil {
		return err
	}

	// save routes to iface.
	if err = saveRoute(bridgeLink, ifaceLink, routes); err != nil {
		return err
	}

	// Now delete the bridge
	stdout, err := exec.Command("ovs-vsctl", "--", "--if-exists", "del-br", bridge).CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to delete OVS bridge,  %v", string(stdout))
		return err
	}
	klog.Infof("Successfully deleted OVS bridge %q", bridge)

	// Now delete the patch port on the integration bridge, if present
	stdout, err = exec.Command("ovs-vsctl", "--", "--if-exists", "del-port", "br-int",
		fmt.Sprintf("k8s-patch-br-int-%s", bridge)).CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to delete patch port on br-int,  %v", string(stdout), err)
		return err
	}

	return nil
}
