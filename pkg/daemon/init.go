package daemon

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

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

func waitNetworkdConfiguration(linkIndex int) {
	done := make(chan struct{})
	ch := make(chan netlink.RouteUpdate)
	if err := netlink.RouteSubscribe(ch, done); err != nil {
		klog.Warningf("failed to subscribe route update events: %v", err)
		klog.Info("Waiting 100ms ...")
		time.Sleep(100 * time.Millisecond)
		return
	}

	timer := time.NewTimer(50 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			done <- struct{}{}
			return
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

// InitOVSBridges initializes OVS bridges
func InitOVSBridges() (map[string]string, error) {
	bridges, err := ovs.Bridges()
	if err != nil {
		return nil, err
	}

	mappings := make(map[string]string)
	for _, brName := range bridges {
		if err = util.SetLinkUp(brName); err != nil {
			klog.Error(err)
			return nil, err
		}

		output, err := ovs.Exec("list-ports", brName)
		if err != nil {
			return nil, fmt.Errorf("failed to list ports of OVS bridge %s, %w: %q", brName, err, output)
		}

		if output != "" {
			for port := range strings.SplitSeq(output, "\n") {
				ok, err := ovs.ValidatePortVendor(port)
				if err != nil {
					return nil, fmt.Errorf("failed to check vendor of port %s: %w", port, err)
				}
				if ok {
					mappings[port] = brName
				}
			}
		}
	}

	return mappings, nil
}

// InitNodeGateway init ovn0
func InitNodeGateway(config *Configuration) error {
	var portName, ip, joinCIDR, macAddr, gw, ipAddr string
	for {
		nodeName := config.NodeName
		node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get node %s info %v", nodeName, err)
			return err
		}
		if node.Annotations[util.IPAddressAnnotation] == "" {
			klog.Warningf("no ovn0 address for node %s, please check kube-ovn-controller logs", nodeName)
			time.Sleep(3 * time.Second)
			continue
		}
		if err := util.ValidatePodNetwork(node.Annotations); err != nil {
			klog.Errorf("validate node %s address annotation failed, %v", nodeName, err)
			time.Sleep(3 * time.Second)
			continue
		}
		macAddr = node.Annotations[util.MacAddressAnnotation]
		ip = node.Annotations[util.IPAddressAnnotation]
		joinCIDR = node.Annotations[util.CidrAnnotation]
		portName = node.Annotations[util.PortNameAnnotation]
		gw = node.Annotations[util.GatewayAnnotation]
		break
	}
	mac, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %w", mac, err)
	}

	ipAddr, err = util.GetIPAddrWithMask(ip, joinCIDR)
	if err != nil {
		klog.Errorf("failed to get ip %s with mask %s, %v", ip, joinCIDR, err)
		return err
	}
	return configureNodeNic(config.KubeClient, config.NodeName, portName, ipAddr, gw, joinCIDR, mac, config.MTU)
}

func InitMirror(config *Configuration) error {
	if config.EnableMirror {
		return configureGlobalMirror(config.MirrorNic, config.MTU)
	}
	return configureEmptyMirror(config.MirrorNic, config.MTU)
}

func (c *Controller) ovsInitProviderNetwork(provider, nic string, trunks []string, exchangeLinkName, macLearningFallback bool) (int, error) {
	// create and configure external bridge
	brName := util.ExternalBridgeName(provider)
	if exchangeLinkName {
		exchanged, err := c.changeProvideNicName(nic, brName)
		if err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", nic, brName, err)
			return 0, err
		}
		if exchanged {
			nic, brName = brName, nic
		}
	}

	klog.V(3).Infof("configure external bridge %s", brName)
	if err := c.configExternalBridge(provider, brName, nic, exchangeLinkName, macLearningFallback); err != nil {
		errMsg := fmt.Errorf("failed to create and configure external bridge %s: %w", brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// init provider chassis mac
	if err := initProviderChassisMac(provider); err != nil {
		errMsg := fmt.Errorf("failed to init chassis mac for provider %s, %w", provider, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// add host nic to the external bridge
	klog.Infof("config provider nic %s on bridge %s", nic, brName)
	mtu, err := c.configProviderNic(nic, brName, trunks)
	if err != nil {
		errMsg := fmt.Errorf("failed to add nic %s to external bridge %s: %w", nic, brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	return mtu, nil
}

func (c *Controller) ovsCleanProviderNetwork(provider string) error {
	mappings, err := getOvnMappings("ovn-bridge-mappings")
	if err != nil {
		klog.Error(err)
		return err
	}

	brName := mappings[provider]
	if brName == "" {
		return nil
	}

	output, err := ovs.Exec("list-br")
	if err != nil {
		return fmt.Errorf("failed to list OVS bridges: %w, %q", err, output)
	}

	if !slices.Contains(strings.Split(output, "\n"), brName) {
		klog.V(3).Infof("ovs bridge %s not found", brName)
		return nil
	}

	isUserspaceDP, err := ovs.IsUserspaceDataPath()
	if err != nil {
		klog.Error(err)
		return err
	}

	if !isUserspaceDP {
		// get host nic
		if output, err = ovs.Exec("list-ports", brName); err != nil {
			klog.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
			return err
		}

		// remove host nic from the external bridge
		if output != "" {
			for port := range strings.SplitSeq(output, "\n") {
				// patch port created by ovn-controller has an external ID ovn-localnet-port=localnet.<SUBNET>
				if output, err = ovs.Exec("--data=bare", "--no-heading", "--columns=_uuid", "find", "port", "name="+port, `external-ids:ovn-localnet-port!=""`); err != nil {
					klog.Errorf("failed to find ovs port %s, %v: %q", port, err, output)
					return err
				}
				if output != "" {
					continue
				}
				klog.Infof("removing ovs port %s from bridge %s", port, brName)
				if err = c.removeProviderNic(port, brName); err != nil {
					klog.Errorf("failed to remove port %s from external bridge %s: %v", port, brName, err)
					return err
				}
				klog.Infof("ovs port %s has been removed from bridge %s", port, brName)
			}
		}

		// remove OVS bridge
		klog.Infof("delete external bridge %s", brName)
		if output, err = ovs.Exec(ovs.IfExists, "del-br", brName); err != nil {
			klog.Errorf("failed to remove OVS bridge %s, %v: %q", brName, err, output)
			return err
		}
		klog.Infof("ovs bridge %s has been deleted", brName)

		if br := util.ExternalBridgeName(provider); br != brName {
			if _, err = c.changeProvideNicName(br, brName); err != nil {
				klog.Errorf("failed to change provider nic name from %s to %s: %v", br, brName, err)
				return err
			}
		}
	}

	if err := removeOvnMapping("ovn-chassis-mac-mappings", provider); err != nil {
		klog.Error(err)
		return err
	}
	return removeOvnMapping("ovn-bridge-mappings", provider)
}

func (c *Controller) changeProvideNicName(current, target string) (bool, error) {
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

	if err = c.nmSyncer.SetManaged(current, false); err != nil {
		klog.Errorf("failed to set device %s unmanaged by NetworkManager: %v", current, err)
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

	if slices.Contains(link.Attrs().AltNames, current) {
		if err = netlink.LinkDelAltName(link, current); err != nil {
			klog.Errorf("failed to delete alternative name %s from link %s: %v", current, link.Attrs().Name, err)
			return false, err
		}
	}

	return true, nil
}
