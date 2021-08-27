package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Mellanox/sriovnet"
	sriovutilfs "github.com/Mellanox/sriovnet/pkg/utils/filesystem"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	goping "github.com/oilbeater/go-ping"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, routes []request.Route, ingress, egress, DeviceID, nicType, podNetns string, checkGw bool) error {
	var err error
	var hostNicName, containerNicName string
	if DeviceID == "" {
		hostNicName, containerNicName, err = setupVethPair(containerID, ifName, mtu)
		if err != nil {
			klog.Errorf("failed to create veth pair %v", err)
			return err
		}
	} else {
		hostNicName, containerNicName, err = setupSriovInterface(containerID, DeviceID, vfDriver, ifName, mtu, mac)
		if err != nil {
			klog.Errorf("failed to create sriov interfaces %v", err)
			return err
		}
	}

	ipStr := util.GetIpWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID)
	// Add veth pair host end to ovs port
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:pod_name=%s", podName),
		fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
		fmt.Sprintf("external_ids:ip=%s", ipStr),
		fmt.Sprintf("external_ids:pod_netns=%s", podNetns))
	if err != nil {
		return fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}

	// host and container nic must use same mac address, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}
	if err = configureHostNic(hostNicName); err != nil {
		return err
	}
	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
		return err
	}

	if containerNicName == "" {
		return nil
	}
	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, routes, macAddr, podNS, mtu, nicType, checkGw); err != nil {
		return err
	}
	return nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, containerID, deviceID, ifName, nicType string) error {
	var nicName string
	hostNicName, containerNicName := generateNicName(containerID, ifName)

	if nicType == util.InternalType {
		nicName = containerNicName
	} else {
		nicName = hostNicName
	}

	// Remove ovs port
	output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", nicName)
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %v, %q", err, output)
	}

	if err = ovs.ClearPodBandwidth(podName, podNamespace); err != nil {
		return err
	}

	if deviceID == "" {
		hostLink, err := netlink.LinkByName(nicName)
		if err != nil {
			// If link already not exists, return quietly
			if _, ok := err.(netlink.LinkNotFoundError); ok {
				return nil
			}
			return fmt.Errorf("find host link %s failed %v", nicName, err)
		}
		if err = netlink.LinkDel(hostLink); err != nil {
			return fmt.Errorf("delete host link %s failed %v", hostLink, err)
		}
	} else {
		// Ret VF index from PCI
		vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
		if err != nil {
			klog.Errorf("failed to get vf %s index, %v", deviceID, err)
			return err
		}
		if err = setVfMac(deviceID, vfIndex, "00:00:00:00:00:00"); err != nil {
			return err
		}
	}
	return nil
}

func generateNicName(containerID, ifname string) (string, string) {
	if ifname == "eth0" {
		return fmt.Sprintf("%s_h", containerID[0:12]), fmt.Sprintf("%s_c", containerID[0:12])
	}
	return fmt.Sprintf("%s_%s_h", containerID[0:12-len(ifname)], ifname), fmt.Sprintf("%s_%s_c", containerID[0:12-len(ifname)], ifname)
}

func configureHostNic(nicName string) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find host nic %s: %v", nicName, err)
	}

	if hostLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(hostLink); err != nil {
			return fmt.Errorf("can not set host nic %s up: %v", nicName, err)
		}
	}
	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen: %v", nicName, err)
	}

	return nil
}

func configureContainerNic(nicName, ifName string, ipAddr, gateway string, routes []request.Route, macAddr net.HardwareAddr, netns ns.NetNS, mtu int, nicType string, checkGw bool) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find container nic %s: %v", nicName, err)
	}

	if err = netlink.LinkSetNsFd(containerLink, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to link netns: %v", err)
	}

	return ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {

		if nicType != util.InternalType {
			if err = netlink.LinkSetName(containerLink, ifName); err != nil {
				return err
			}
		}

		if util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolDual || util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolIPv6 {
			// For docker version >=17.x the "none" network will disable ipv6 by default.
			// We have to enable ipv6 here to add v6 address and gateway.
			// See https://github.com/containernetworking/cni/issues/531
			value, err := sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6")
			if err != nil {
				return fmt.Errorf("failed to get sysctl net.ipv6.conf.all.disable_ipv6: %v", err)
			}
			if value != "0" {
				if _, err = sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6", "0"); err != nil {
					return fmt.Errorf("failed to enable ipv6 on all nic: %v", err)
				}
			}
		}

		if nicType == util.InternalType {
			if err = addAdditionalNic(ifName); err != nil {
				return err
			}
			if err = configureAdditionalNic(ifName, ipAddr); err != nil {
				return err
			}
			if err = configureNic(nicName, ipAddr, macAddr, mtu); err != nil {
				return err
			}
		} else {
			if err = configureNic(ifName, ipAddr, macAddr, mtu); err != nil {
				return err
			}
		}

		if ifName == "eth0" {
			// Only eth0 requires the default route and gateway
			switch util.CheckProtocol(ipAddr) {
			case kubeovnv1.ProtocolIPv4:
				_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
				err = netlink.RouteAdd(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(gateway),
				})
			case kubeovnv1.ProtocolIPv6:
				_, defaultNet, _ := net.ParseCIDR("::/0")
				err = netlink.RouteAdd(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(gateway),
				})
			case kubeovnv1.ProtocolDual:
				gws := strings.Split(gateway, ",")
				_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
				err = netlink.RouteAdd(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(gws[0]),
				})
				if err != nil {
					return fmt.Errorf("config v4 gateway failed: %v", err)
				}

				_, defaultNet, _ = net.ParseCIDR("::/0")
				err = netlink.RouteAdd(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(gws[1]),
				})
			}

			if err != nil {
				return fmt.Errorf("failed to configure gateway: %v", err)
			}
		}

		for _, r := range routes {
			_, dst, err := net.ParseCIDR(r.Destination)
			if err != nil {
				klog.Errorf("invalid route destination %s: %v", r.Destination, err)
				continue
			}

			var gw net.IP
			if r.Gateway != "" {
				if gw = net.ParseIP(r.Gateway); gw == nil {
					klog.Errorf("invalid route gateway %s", r.Gateway)
					continue
				}
			}

			route := &netlink.Route{
				Dst:       dst,
				Gw:        gw,
				LinkIndex: containerLink.Attrs().Index,
			}
			if err = netlink.RouteReplace(route); err != nil {
				klog.Errorf("failed to add route %+v: %v", r, err)
			}
		}

		if checkGw {
			return waitNetworkReady(gateway)
		}

		return nil
	})
}

func waitNetworkReady(gateway string) error {
	for _, gw := range strings.Split(gateway, ",") {
		pinger, err := goping.NewPinger(gw)
		if err != nil {
			return fmt.Errorf("failed to init pinger: %v", err)
		}
		pinger.SetPrivileged(true)
		// CNITimeoutSec = 220, cannot exceed
		count := 200
		pinger.Count = count
		pinger.Timeout = time.Duration(count) * time.Second
		pinger.Interval = 1 * time.Second

		success := false
		pinger.OnRecv = func(p *goping.Packet) {
			success = true
			pinger.Stop()
		}
		pinger.Run()

		cniConnectivityResult.WithLabelValues(nodeName).Add(float64(pinger.PacketsSent))
		if !success {
			return fmt.Errorf("network not ready after %d ping", count)
		}
		klog.Infof("network ready after %d ping, gw %v", pinger.PacketsSent, gw)
	}
	return nil
}

func configureNodeNic(portName, ip, gw string, macAddr net.HardwareAddr, mtu int) error {
	ipStr := util.GetIpWithoutMask(ip)
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", util.NodeNic, "--",
		"set", "interface", util.NodeNic, "type=internal", "--",
		"set", "interface", util.NodeNic, fmt.Sprintf("external_ids:iface-id=%s", portName),
		fmt.Sprintf("external_ids:ip=%s", ipStr))
	if err != nil {
		klog.Errorf("failed to configure node nic %s: %v, %q", portName, err, raw)
		return fmt.Errorf(raw)
	}

	if err = configureNic(util.NodeNic, ip, macAddr, mtu); err != nil {
		return err
	}

	hostLink, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		return fmt.Errorf("can not find nic %s: %v", util.NodeNic, err)
	}

	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen: %v", util.NodeNic, err)
	}

	// ping ovn0 gw to activate the flow
	output, err := ovn0Check(gw)
	if err != nil {
		klog.Errorf("failed to init ovn0 check: %v, %q", err, output)
		return err
	}
	klog.Infof("ping gw result is: \n %s", output)

	return nil
}

// If OVS restart, the ovn0 port will down and prevent host to pod network,
// Restart the kube-ovn-cni when this happens
func (c *Controller) loopOvn0Check() {
	link, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Fatalf("failed to get ovn0 nic: %v", err)
	}

	if link.Attrs().OperState == netlink.OperDown {
		klog.Fatalf("ovn0 nic is down")
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return
	}
	gw := node.Annotations[util.GatewayAnnotation]
	if output, err := ovn0Check(gw); err != nil {
		klog.Fatalf("failed to ping ovn0 gw: %v, %q", gw, output)
	}
}

func ovn0Check(gw string) ([]byte, error) {
	protocol := util.CheckProtocol(gw)
	if protocol == kubeovnv1.ProtocolDual {
		gws := strings.Split(gw, ",")
		output, err := exec.Command("ping", "-w", "10", gws[0]).CombinedOutput()
		klog.V(3).Infof("ping v4 gw result is: \n %s", output)
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s: %v", gws[0], err)
			return output, err
		}

		output, err = exec.Command("ping6", "-w", "10", gws[1]).CombinedOutput()
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s: %v", gws[1], err)
			return output, err
		}
		return output, nil
	} else if protocol == kubeovnv1.ProtocolIPv4 {
		output, err := exec.Command("ping", "-w", "10", gw).CombinedOutput()
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s: %v", gw, err)
			return output, err
		}
		return output, nil
	} else {
		output, err := exec.Command("ping6", "-w", "10", gw).CombinedOutput()
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s: %v", gw, err)
			return output, err
		}
		return output, nil
	}
}

func configureGlobalMirror(portName string, mtu int) error {
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
		"set", "interface", portName, "type=internal", "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		"--id=@mirror0", "get", "port", portName, "--",
		"--id=@m", "create", "mirror", fmt.Sprintf("name=%s", util.MirrorDefaultName), "select_all=true", "output_port=@mirror0", "--",
		"add", "bridge", "br-int", "mirrors", "@m")
	if err != nil {
		klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
		return fmt.Errorf(raw)
	}
	return configureMirrorLink(portName, mtu)
}

func configureEmptyMirror(portName string, mtu int) error {
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
		"set", "interface", portName, "type=internal", "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		"--id=@mirror0", "get", "port", portName, "--",
		"--id=@m", "create", "mirror", fmt.Sprintf("name=%s", util.MirrorDefaultName), "output_port=@mirror0", "--",
		"add", "bridge", "br-int", "mirrors", "@m")
	if err != nil {
		klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
		return fmt.Errorf(raw)
	}
	return configureMirrorLink(portName, mtu)
}

func configureMirrorLink(portName string, mtu int) error {
	mirrorLink, err := netlink.LinkByName(portName)
	if err != nil {
		return fmt.Errorf("can not find mirror nic %s: %v", portName, err)
	}

	if err = netlink.LinkSetMTU(mirrorLink, mtu); err != nil {
		return fmt.Errorf("can not set mirror nic mtu: %v", err)
	}

	if mirrorLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(mirrorLink); err != nil {
			return fmt.Errorf("can not set mirror nic %s up: %v", portName, err)
		}
	}

	return nil
}

func configureNic(link, ip string, macAddr net.HardwareAddr, mtu int) error {
	nodeLink, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("can not find nic %s: %v", link, err)
	}

	ipDelMap := make(map[string]netlink.Addr)
	ipAddMap := make(map[string]netlink.Addr)
	ipAddrs, err := netlink.AddrList(nodeLink, 0x0)
	if err != nil {
		return fmt.Errorf("can not get addr %s: %v", nodeLink, err)
	}
	for _, ipAddr := range ipAddrs {
		if strings.HasPrefix(ipAddr.IP.String(), "fe80::") {
			continue
		}
		ipDelMap[ipAddr.IP.String()+"/"+ipAddr.Mask.String()] = ipAddr
	}

	for _, ipStr := range strings.Split(ip, ",") {
		// Do not reassign same address for link
		if _, ok := ipDelMap[ipStr]; ok {
			delete(ipDelMap, ipStr)
			continue
		}

		ipAddr, err := netlink.ParseAddr(ipStr)
		if err != nil {
			return fmt.Errorf("can not parse address %s: %v", ipStr, err)
		}
		ipAddMap[ipStr] = *ipAddr
	}

	for _, addr := range ipDelMap {
		ipDel := addr
		if err = netlink.AddrDel(nodeLink, &ipDel); err != nil {
			return fmt.Errorf("delete address %s: %v", addr, err)
		}
	}
	for _, addr := range ipAddMap {
		ipAdd := addr
		if err = netlink.AddrAdd(nodeLink, &ipAdd); err != nil {
			return fmt.Errorf("can not add address %v to nic %s: %v", addr, link, err)
		}
	}

	if err = netlink.LinkSetHardwareAddr(nodeLink, macAddr); err != nil {
		return fmt.Errorf("can not set mac address to nic %s: %v", link, err)
	}

	if mtu > 0 {
		if err = netlink.LinkSetMTU(nodeLink, mtu); err != nil {
			return fmt.Errorf("can not set nic %s mtu: %v", link, err)
		}
	}

	if nodeLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(nodeLink); err != nil {
			return fmt.Errorf("can not set node nic %s up: %v", link, err)
		}
	}
	return nil
}

func configExternalBridge(provider, bridge, nic string) error {
	output, err := ovs.Exec(ovs.MayExist, "add-br", bridge,
		"--", "set", "bridge", bridge, "external_ids:vendor="+util.CniTypeName)
	if err != nil {
		return fmt.Errorf("failed to create OVS bridge %s, %v: %q", bridge, err, output)
	}
	if output, err = ovs.Exec("list-ports", bridge); err != nil {
		return fmt.Errorf("failed to list ports of OVS birdge %s, %v: %q", bridge, err, output)
	}
	if output != "" {
		for _, port := range strings.Split(output, "\n") {
			if port != nic {
				ok, err := ovs.ValidatePortVendor(port)
				if err != nil {
					return fmt.Errorf("failed to check vendor of port %s: %v", port, err)
				}
				if ok {
					if err = removeProviderNic(port, bridge); err != nil {
						return fmt.Errorf("failed to remove port %s from OVS birdge %s: %v", port, bridge, err)
					}
				}
			}
		}
	}

	if output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings"); err != nil {
		return fmt.Errorf("failed to get ovn-bridge-mappings, %v", err)
	}

	bridgeMappings := fmt.Sprintf("%s:%s", provider, bridge)
	if util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
		return nil
	}
	if output != "" {
		bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
	}
	if output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-bridge-mappings="+bridgeMappings); err != nil {
		return fmt.Errorf("failed to set ovn-bridge-mappings, %v: %q", err, output)
	}

	return nil
}

// Add host nic to external bridge
// Mac address, MTU, IP addresses & routes will be copied/transferred to the external bridge
func configProviderNic(nicName, brName string) (int, error) {
	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		return 0, fmt.Errorf("failed to get nic by name %s: %v", nicName, err)
	}
	bridge, err := netlink.LinkByName(brName)
	if err != nil {
		return 0, fmt.Errorf("failed to get bridge by name %s: %v", brName, err)
	}

	sysctlDisableIPv6 := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", brName)
	disableIPv6, err := sysctl.Sysctl(sysctlDisableIPv6)
	if err != nil {
		return 0, fmt.Errorf("failed to get sysctl %s: %v", sysctlDisableIPv6, err)
	}
	if disableIPv6 != "0" {
		if _, err = sysctl.Sysctl(sysctlDisableIPv6, "0"); err != nil {
			return 0, fmt.Errorf("failed to enable ipv6 on OVS bridge %s: %v", brName, err)
		}
	}

	addrs, err := netlink.AddrList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses on nic %s: %v", nicName, err)
	}
	routes, err := netlink.RouteList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get routes on nic %s: %v", nicName, err)
	}

	if _, err = ovs.Exec(ovs.MayExist, "add-port", brName, nicName,
		"--", "set", "port", nicName, "external_ids:vendor="+util.CniTypeName); err != nil {
		return 0, fmt.Errorf("failed to add %s to OVS birdge %s: %v", nicName, brName, err)
	}

	for _, addr := range addrs {
		if addr.IP.To4() == nil && addr.IP.IsLinkLocalUnicast() {
			if prefix, _ := addr.Mask.Size(); prefix == 64 {
				// skip link local IPv6 address
				continue
			}
		}

		if err = netlink.AddrDel(nic, &addr); err != nil && !errors.Is(err, syscall.ENOENT) {
			return 0, fmt.Errorf("failed to delete address %s on nic %s: %v", addr.String(), nicName, err)
		}

		if addr.Label != "" {
			addr.Label = brName + strings.TrimPrefix(addr.Label, nicName)
		}
		if err = netlink.AddrReplace(bridge, &addr); err != nil {
			return 0, fmt.Errorf("failed to replace address %s on OVS bridge %s: %v", addr.String(), brName, err)
		}
	}

	if _, err = ovs.Exec("set", "bridge", brName, fmt.Sprintf(`other-config:hwaddr="%s"`, nic.Attrs().HardwareAddr.String())); err != nil {
		return 0, fmt.Errorf("failed to set MAC address of OVS bridge %s: %v", brName, err)
	}
	if err = netlink.LinkSetMTU(bridge, nic.Attrs().MTU); err != nil {
		return 0, fmt.Errorf("failed to set MTU of OVS bridge %s: %v", brName, err)
	}
	if err = netlink.LinkSetUp(bridge); err != nil {
		return 0, fmt.Errorf("failed to set OVS bridge %s up: %v", brName, err)
	}

	scopeOrders := [...]netlink.Scope{
		netlink.SCOPE_HOST,
		netlink.SCOPE_LINK,
		netlink.SCOPE_SITE,
		netlink.SCOPE_UNIVERSE,
	}
	for _, scope := range scopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.String() == "fe80::/64" {
				continue
			}
			if route.Scope == scope {
				route.LinkIndex = bridge.Attrs().Index
				if err = netlink.RouteReplace(&route); err != nil {
					return 0, fmt.Errorf("failed to add/replace route %s: %v", route.String(), err)
				}
			}
		}
	}

	if err = netlink.LinkSetUp(nic); err != nil {
		return 0, fmt.Errorf("failed to set link %s up: %v", nicName, err)
	}

	return nic.Attrs().MTU, nil
}

// Remove host nic from external bridge
// IP addresses & routes will be transferred to the host nic
func removeProviderNic(nicName, brName string) error {
	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("failed to get nic by name %s: %v", nicName, err)
	}
	bridge, err := netlink.LinkByName(brName)
	if err != nil {
		return fmt.Errorf("failed to get bridge by name %s: %v", brName, err)
	}

	addrs, err := netlink.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get addresses on bridge %s: %v", brName, err)
	}
	routes, err := netlink.RouteList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get routes on bridge %s: %v", brName, err)
	}

	if _, err = ovs.Exec(ovs.IfExists, "del-port", brName, nicName); err != nil {
		return fmt.Errorf("failed to remove %s from OVS birdge %s: %v", nicName, brName, err)
	}

	for _, addr := range addrs {
		if addr.IP.To4() == nil && addr.IP.IsLinkLocalUnicast() {
			if prefix, _ := addr.Mask.Size(); prefix == 64 {
				// skip link local IPv6 address
				continue
			}
		}

		if err = netlink.AddrDel(bridge, &addr); err != nil && !errors.Is(err, syscall.ENOENT) {
			return fmt.Errorf("failed to delete address %s on OVS bridge %s: %v", addr.String(), brName, err)
		}

		if addr.Label != "" {
			addr.Label = nicName + strings.TrimPrefix(addr.Label, brName)
		}
		if err = netlink.AddrReplace(nic, &addr); err != nil {
			return fmt.Errorf("failed to replace address %s on nic %s: %v", addr.String(), nicName, err)
		}
	}

	if err = netlink.LinkSetDown(bridge); err != nil {
		return fmt.Errorf("failed to set OVS bridge %s down: %v", brName, err)
	}

	scopeOrders := [...]netlink.Scope{
		netlink.SCOPE_HOST,
		netlink.SCOPE_LINK,
		netlink.SCOPE_SITE,
		netlink.SCOPE_UNIVERSE,
	}
	for _, scope := range scopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.String() == "fe80::/64" {
				continue
			}
			if route.Scope == scope {
				route.LinkIndex = nic.Attrs().Index
				if err = netlink.RouteReplace(&route); err != nil {
					return fmt.Errorf("failed to add/replace route %s: %v", route.String(), err)
				}
			}
		}
	}

	return nil
}

func setupVethPair(containerID, ifName string, mtu int) (string, string, error) {
	var err error
	hostNicName, containerNicName := generateNicName(containerID, ifName)
	// Create a veth pair, put one end to container ,the other to ovs port
	// NOTE: DO NOT use ovs internal type interface for container.
	// Kubernetes will detect 'eth0' nic in pod, so the nic name in pod must be 'eth0'.
	// When renaming internal interface to 'eth0', ovs will delete and recreate this interface.
	veth := netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostNicName}, PeerName: containerNicName}
	if mtu > 0 {
		veth.MTU = mtu
	}
	if err = netlink.LinkAdd(&veth); err != nil {
		if err := netlink.LinkDel(&veth); err != nil {
			klog.Errorf("failed to delete veth %v", err)
			return "", "", err
		}
		return "", "", fmt.Errorf("failed to crate veth for %v", err)
	}
	return hostNicName, containerNicName, nil
}

// Setup sriov interface in the pod
// https://github.com/ovn-org/ovn-kubernetes/commit/6c96467d0d3e58cab05641293d1c1b75e5914795
func setupSriovInterface(containerID, deviceID, vfDriver, ifName string, mtu int, mac string) (string, string, error) {
	var isVfioPciDriver = false
	if vfDriver == "vfio-pci" {
		matches, err := filepath.Glob(filepath.Join(util.VfioSysDir, "*"))
		if err != nil {
			return "", "", fmt.Errorf("failed to check %s 'vfio-pci' driver path, %v", deviceID, err)
		}

		for _, match := range matches {
			tmp, err := os.Readlink(match)
			if err != nil {
				continue
			}
			if strings.Contains(tmp, deviceID) {
				isVfioPciDriver = true
				break
			}
		}

		if !isVfioPciDriver {
			return "", "", fmt.Errorf("driver of device %s is not 'vfio-pci'", deviceID)
		}
	}

	var vfNetdevice string
	if !isVfioPciDriver {
		// 1. get VF netdevice from PCI
		vfNetdevices, err := sriovnet.GetNetDevicesFromPci(deviceID)
		if err != nil {
			klog.Errorf("failed to get vf netdevice %s, %v", deviceID, err)
			return "", "", err
		}

		// Make sure we have 1 netdevice per pci address
		if len(vfNetdevices) != 1 {
			return "", "", fmt.Errorf("failed to get one netdevice interface per %s", deviceID)
		}
		vfNetdevice = vfNetdevices[0]
	}

	// 2. get Uplink netdevice
	uplink, err := sriovnet.GetUplinkRepresentor(deviceID)
	if err != nil {
		klog.Errorf("failed to get up %s link device, %v", deviceID, err)
		return "", "", err
	}

	// 3. get VF index from PCI
	vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
	if err != nil {
		klog.Errorf("failed to get vf %s index, %v", deviceID, err)
		return "", "", err
	}

	// 4. lookup representor
	rep, err := sriovnet.GetVfRepresentor(uplink, vfIndex)
	if err != nil {
		klog.Errorf("failed to get vf %d representor, %v", vfIndex, err)
		return "", "", err
	}
	oldHostRepName := rep

	// 5. rename the host VF representor
	hostNicName, _ := generateNicName(containerID, ifName)
	if err = renameLink(oldHostRepName, hostNicName); err != nil {
		return "", "", fmt.Errorf("failed to rename %s to %s: %v", oldHostRepName, hostNicName, err)
	}

	link, err := netlink.LinkByName(hostNicName)
	if err != nil {
		return "", "", err
	}

	// 6. set MTU on VF representor
	if err = netlink.LinkSetMTU(link, mtu); err != nil {
		return "", "", fmt.Errorf("failed to set MTU on %s: %v", hostNicName, err)
	}

	if isVfioPciDriver {
		// 7. set MAC address to VF
		if err := setVfMac(deviceID, vfIndex, mac); err != nil {
			return "", "", err
		}
	}
	return hostNicName, vfNetdevice, nil
}

func renameLink(curName, newName string) error {
	link, err := netlink.LinkByName(curName)
	if err != nil {
		return err
	}

	if err := netlink.LinkSetDown(link); err != nil {
		return err
	}
	if err := netlink.LinkSetName(link, newName); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}

	return nil
}

func (csh cniServerHandler) configureNicWithInternalPort(podName, podNamespace, provider, netns, containerID, ifName, mac string, mtu int, ip, gateway string, routes []request.Route, ingress, egress, DeviceID, nicType, podNetns string, checkGw bool) (string, error) {
	_, containerNicName := generateNicName(containerID, ifName)
	ipStr := util.GetIpWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID)

	// Add container iface to ovs port as internal port
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", containerNicName, "--",
		"set", "interface", containerNicName, "type=internal", "--",
		"set", "interface", containerNicName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:pod_name=%s", podName),
		fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
		fmt.Sprintf("external_ids:ip=%s", ipStr),
		fmt.Sprintf("external_ids:pod_netns=%s", podNetns))
	if err != nil {
		return containerNicName, fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}

	// container nic must use same mac address from pod annotation, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return containerNicName, fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
		return containerNicName, err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return containerNicName, fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, routes, macAddr, podNS, mtu, nicType, checkGw); err != nil {
		return containerNicName, err
	}
	return containerNicName, nil
}

// https://github.com/antrea-io/antrea/issues/1691
func configureAdditionalNic(link, ip string) error {
	nodeLink, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("can not find nic %s %v", link, err)
	}

	ipDelMap := make(map[string]netlink.Addr)
	ipAddMap := make(map[string]netlink.Addr)
	ipAddrs, err := netlink.AddrList(nodeLink, 0x0)
	if err != nil {
		return fmt.Errorf("can not get addr %s %v", nodeLink, err)
	}
	for _, ipAddr := range ipAddrs {
		if strings.HasPrefix(ipAddr.IP.String(), "fe80::") {
			continue
		}
		ipDelMap[ipAddr.IP.String()+"/"+ipAddr.Mask.String()] = ipAddr
	}

	for _, ipStr := range strings.Split(ip, ",") {
		// Do not reassign same address for link
		if _, ok := ipDelMap[ipStr]; ok {
			delete(ipDelMap, ipStr)
			continue
		}

		ipAddr, err := netlink.ParseAddr(ipStr)
		if err != nil {
			return fmt.Errorf("can not parse %s %v", ipStr, err)
		}
		ipAddMap[ipStr] = *ipAddr
	}

	for _, addr := range ipDelMap {
		ipDel := addr
		if err = netlink.AddrDel(nodeLink, &ipDel); err != nil {
			return fmt.Errorf("delete address %s %v", addr, err)
		}
	}
	for _, addr := range ipAddMap {
		ipAdd := addr
		if err = netlink.AddrAdd(nodeLink, &ipAdd); err != nil {
			return fmt.Errorf("can not add address %v to nic %s, %v", addr, link, err)
		}
	}

	return nil
}

func addAdditionalNic(ifName string) error {
	dummy := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Name: ifName,
		},
	}

	if err := netlink.LinkAdd(dummy); err != nil {
		if err := netlink.LinkDel(dummy); err != nil {
			klog.Errorf("failed to delete static iface %v, err %v", ifName, err)
			return err
		}
		return fmt.Errorf("failed to crate static iface %v, err %v", ifName, err)
	}
	return nil
}

func setVfMac(deviceID string, vfIndex int, mac string) error {
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	pfPci, err := sriovnet.GetPfPciFromVfPci(deviceID)
	if err != nil {
		return fmt.Errorf("failed to get pf of device %s %v", deviceID, err)
	}

	netDevs, err := sriovnet.GetNetDevicesFromPci(pfPci)
	if err != nil {
		return fmt.Errorf("failed to get pf of device %s %v", deviceID, err)
	}

	// get real pf
	var pfName string
	for _, dev := range netDevs {
		devicePortNameFile := filepath.Join(util.NetSysDir, dev, "phys_port_name")
		physPortName, err := sriovutilfs.Fs.ReadFile(devicePortNameFile)
		if err != nil {
			return err
		}

		if !strings.Contains(strings.TrimSpace(string(physPortName)), "vf") {
			pfName = dev
			break
		}
	}
	if pfName == "" {
		return fmt.Errorf("the PF device was not found in the device list, %v", netDevs)
	}

	pfLink, err := netlink.LinkByName(pfName)
	if err != nil {
		return fmt.Errorf("failed to lookup pf %s: %v", pfName, err)
	}
	if err := netlink.LinkSetVfHardwareAddr(pfLink, vfIndex, macAddr); err != nil {
		return fmt.Errorf("can not set mac address to vf nic:%s vf:%d %v", pfName, vfIndex, err)
	}
	return nil
}
