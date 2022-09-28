package daemon

import (
	"context"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/Wifx/gonetworkmanager"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var routeScopeOrders = [...]netlink.Scope{
	netlink.SCOPE_HOST,
	netlink.SCOPE_LINK,
	netlink.SCOPE_SITE,
	netlink.SCOPE_UNIVERSE,
}

// InitOVSBridges initializes OVS bridges
func InitOVSBridges() (map[string]string, error) {
	bridges, err := ovs.Bridges()
	if err != nil {
		return nil, err
	}

	mappings := make(map[string]string)
	for _, brName := range bridges {
		bridge, err := netlink.LinkByName(brName)
		if err != nil {
			return nil, fmt.Errorf("failed to get bridge by name %s: %v", brName, err)
		}
		if err = netlink.LinkSetUp(bridge); err != nil {
			return nil, fmt.Errorf("failed to set OVS bridge %s up: %v", brName, err)
		}

		output, err := ovs.Exec("list-ports", brName)
		if err != nil {
			return nil, fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
		}

		if output != "" {
			for _, port := range strings.Split(output, "\n") {
				ok, err := ovs.ValidatePortVendor(port)
				if err != nil {
					return nil, fmt.Errorf("failed to check vendor of port %s: %v", port, err)
				}
				if ok {
					if _, err = configProviderNic(port, brName); err != nil {
						return nil, err
					}
					mappings[port] = brName
				}
			}
		}
	}

	return mappings, nil
}

// InitNodeGateway init ovn0
func InitNodeGateway(config *Configuration) error {
	var portName, ip, cidr, macAddr, gw, ipAddr string
	for {
		nodeName := config.NodeName
		node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get node %s info %v", nodeName, err)
			return err
		}
		if node.Annotations[util.IpAddressAnnotation] == "" {
			klog.Warningf("no ovn0 address for node %s, please check kube-ovn-controller logs", nodeName)
			time.Sleep(3 * time.Second)
			continue
		}
		if err := util.ValidatePodNetwork(node.Annotations); err != nil {
			klog.Errorf("validate node %s address annotation failed, %v", nodeName, err)
			time.Sleep(3 * time.Second)
			continue
		} else {
			macAddr = node.Annotations[util.MacAddressAnnotation]
			ip = node.Annotations[util.IpAddressAnnotation]
			cidr = node.Annotations[util.CidrAnnotation]
			portName = node.Annotations[util.PortNameAnnotation]
			gw = node.Annotations[util.GatewayAnnotation]
			break
		}
	}
	mac, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", mac, err)
	}

	ipAddr = util.GetIpAddrWithMask(ip, cidr)
	return configureNodeNic(portName, ipAddr, gw, mac, config.MTU)
}

func InitMirror(config *Configuration) error {
	if config.EnableMirror {
		return configureGlobalMirror(config.MirrorNic, config.MTU)
	}
	return configureEmptyMirror(config.MirrorNic, config.MTU)
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
		klog.Infof("%s is an openvswitch interface, skip", current)
		return true, nil
	}

	// set link unmanaged by NetworkManager to avoid getting new IP by DHCP
	if err = nmSetManaged(current, false); err != nil {
		klog.Errorf("failed set device %s to unmanaged by NetworkManager: %v", current, err)
		return false, err
	}

	klog.Infof("change nic name from %s to %s", current, target)
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

	for _, addr := range addresses {
		if addr.IP.IsLinkLocalUnicast() {
			continue
		}
		if err = netlink.AddrReplace(link, &addr); err != nil {
			klog.Errorf("failed to replace address %s: %v", addr.String(), err)
			return false, err
		}
	}

	for _, scope := range routeScopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.IP.IsLinkLocalUnicast() {
				continue
			}
			if route.Scope == scope {
				if err = netlink.RouteReplace(&route); err != nil && err != syscall.EEXIST {
					klog.Errorf("failed to replace route %s: %v", route.String(), err)
					return false, err
				}
			}
		}
	}

	return true, nil
}

func ovsInitProviderNetwork(provider, nic string, exchangeLinkName bool) (int, error) {
	// create and configure external bridge
	brName := util.ExternalBridgeName(provider)
	if exchangeLinkName {
		exchanged, err := changeProvideNicName(nic, brName)
		if err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", nic, brName, err)
			return 0, err
		}
		if exchanged {
			nic, brName = brName, nic
		}
	}

	if err := configExternalBridge(provider, brName, nic, exchangeLinkName); err != nil {
		errMsg := fmt.Errorf("failed to create and configure external bridge %s: %v", brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// init provider chassis mac
	if err := initProviderChassisMac(provider); err != nil {
		errMsg := fmt.Errorf("failed to init chassis mac for provider %s, %v", provider, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// add host nic to the external bridge
	mtu, err := configProviderNic(nic, brName)
	if err != nil {
		errMsg := fmt.Errorf("failed to add nic %s to external bridge %s: %v", nic, brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	return mtu, nil
}

func ovsCleanProviderNetwork(provider string) error {
	output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
	if err != nil {
		return fmt.Errorf("failed to get ovn-bridge-mappings, %v: %q", err, output)
	}

	var idx int
	var m, brName string
	mappingPrefix := provider + ":"
	brMappings := strings.Split(output, ",")
	for idx, m = range brMappings {
		if strings.HasPrefix(m, mappingPrefix) {
			brName = m[len(mappingPrefix):]
			break
		}
	}

	if output, err = ovs.Exec("list-br"); err != nil {
		return fmt.Errorf("failed to list OVS bridge %v: %q", err, output)
	}

	if !util.ContainsString(strings.Split(output, "\n"), brName) {
		return nil
	}

	// get host nic
	if output, err = ovs.Exec("list-ports", brName); err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
	}

	// remove host nic from the external bridge
	if output != "" {
		for _, port := range strings.Split(output, "\n") {
			if err = removeProviderNic(port, brName); err != nil {
				errMsg := fmt.Errorf("failed to remove port %s from external bridge %s: %v", port, brName, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
	}

	if idx != len(brMappings) {
		brMappings = append(brMappings[:idx], brMappings[idx+1:]...)
		if len(brMappings) == 0 {
			output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", "ovn-bridge-mappings")
		} else {
			output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-bridge-mappings="+strings.Join(brMappings, ","))
		}
		if err != nil {
			return fmt.Errorf("failed to set ovn-bridge-mappings, %v: %q", err, output)
		}
	}

	if output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-chassis-mac-mappings"); err != nil {
		return fmt.Errorf("failed to get ovn-chassis-mac-mappings, %v: %q", err, output)
	}
	macMappings := strings.Split(output, ",")
	for _, macMap := range macMappings {
		if strings.HasPrefix(macMap, mappingPrefix) {
			macMappings = util.RemoveString(macMappings, macMap)
			break
		}
	}
	if len(macMappings) == 0 {
		output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", "ovn-chassis-mac-mappings")
	} else {
		output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-chassis-mac-mappings="+strings.Join(macMappings, ","))
	}
	if err != nil {
		return fmt.Errorf("failed to set ovn-chassis-mac-mappings, %v: %q", err, output)
	}

	// get host nic
	if output, err = ovs.Exec("list-ports", brName); err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
	}

	// remove host nic from the external bridge
	if output != "" {
		for _, port := range strings.Split(output, "\n") {
			if err = removeProviderNic(port, brName); err != nil {
				errMsg := fmt.Errorf("failed to remove port %s from external bridge %s: %v", port, brName, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
	}

	// remove OVS bridge
	if output, err = ovs.Exec(ovs.IfExists, "del-br", brName); err != nil {
		return fmt.Errorf("failed to remove OVS bridge %s, %v: %q", brName, err, output)
	}

	if br := util.ExternalBridgeName(provider); br != brName {
		if _, err = changeProvideNicName(br, brName); err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", br, brName, err)
			return err
		}
	}

	return nil
}
