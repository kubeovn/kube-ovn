package daemon

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Mellanox/sriovnet"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	goping "github.com/oilbeater/go-ping"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, ifName, mac, ip, gateway, ingress, egress, vlanID, DeviceID, nicType, podNetns string) error {
	var err error
	var hostNicName, containerNicName string
	if DeviceID == "" {
		hostNicName, containerNicName, err = setupVethPair(containerID, ifName, csh.Config.MTU)
		if err != nil {
			klog.Errorf("failed to create veth pair %v", err)
			return err
		}
	} else {
		hostNicName, containerNicName, err = setupSriovInterface(containerID, DeviceID, ifName, csh.Config.MTU)
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
	if err = configureHostNic(hostNicName, vlanID); err != nil {
		return err
	}
	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, ingress, egress); err != nil {
		return err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, macAddr, podNS, csh.Config.MTU, nicType); err != nil {
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
	}
	return nil
}

func generateNicName(containerID, ifname string) (string, string) {
	if ifname == "eth0" {
		return fmt.Sprintf("%s_h", containerID[0:12]), fmt.Sprintf("%s_c", containerID[0:12])
	}
	return fmt.Sprintf("%s_%s_h", containerID[0:12-len(ifname)], ifname), fmt.Sprintf("%s_%s_c", containerID[0:12-len(ifname)], ifname)
}

func configureHostNic(nicName, vlanID string) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find host nic %s %v", nicName, err)
	}

	if hostLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(hostLink); err != nil {
			return fmt.Errorf("can not set host nic %s up %v", nicName, err)
		}
	}
	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen %v", nicName, err)
	}

	if vlanID != "" && vlanID != "0" {
		if err := ovs.SetPortTag(nicName, vlanID); err != nil {
			return fmt.Errorf("failed to add vlan tag, %v", err)
		}
	}

	return nil
}

func configureContainerNic(nicName, ifName string, ipAddr, gateway string, macAddr net.HardwareAddr, netns ns.NetNS, mtu int, nicType string) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find container nic %s %v", nicName, err)
	}

	if err = netlink.LinkSetNsFd(containerLink, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to link netns %v", err)
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
				return fmt.Errorf("failed to get sysctl net.ipv6.conf.all.disable_ipv6 %v", err)
			}
			if value != "0" {
				if _, err = sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6", "0"); err != nil {
					return fmt.Errorf("failed to enable ipv6 on all nic %v", err)
				}
			}
		}

		if nicType == util.InternalType {
			if err = configureNic(nicName, ipAddr, macAddr, mtu); err != nil {
				return err
			}
			if err = addAdditonalNic(ifName); err != nil {
				return err
			}
			if err = configureAdditonalNic(ifName, ipAddr); err != nil {
				return err
			}
		} else {
			if err = configureNic(ifName, ipAddr, macAddr, mtu); err != nil {
				return err
			}
		}

		if ifName != "eth0" {
			// Only eth0 requires the default route and gateway
			return nil
		}

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
				return fmt.Errorf("config v4 gateway failed %v", err)
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
			return fmt.Errorf("config gateway failed %v", err)
		}

		return waitNetworkReady(gateway)
	})
}

func waitNetworkReady(gateway string) error {
	for _, gw := range strings.Split(gateway, ",") {
		pinger, err := goping.NewPinger(gw)
		if err != nil {
			return fmt.Errorf("failed to init pinger, %v", err)
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
		klog.Errorf("failed to configure node nic %s %q", portName, raw)
		return fmt.Errorf(raw)
	}

	if err = configureNic(util.NodeNic, ip, macAddr, mtu); err != nil {
		return err
	}

	hostLink, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		return fmt.Errorf("can not find nic %s %v", util.NodeNic, err)
	}

	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen %v", util.NodeNic, err)
	}
	// Double nat may lead kernel udp checksum error, disable offload to prevent this issue
	// https://github.com/kubernetes/kubernetes/pull/92035
	output, err := exec.Command("ethtool", "-K", "ovn0", "tx", "off").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to disable checksum offload on ovn0, %v %q", err, output)
		return err
	}

	// ping ovn0 gw to activate the flow
	output, err = ovn0Check(gw)
	if err != nil {
		klog.Errorf("failed to init ovn0 check, %v, %q", err, output)
		return err
	}
	klog.Infof("ping gw result is: \n %s", output)

	return nil
}

func (c *Controller) disableTunnelOffload() {
	_, err := netlink.LinkByName("genev_sys_6081")
	if err == nil {
		output, err := exec.Command("ethtool", "-K", "genev_sys_6081", "tx", "off").CombinedOutput()
		if err != nil {
			klog.Errorf("failed to disable checksum offload on genev_sys_6081, %v %q", err, output)
		}
	}
}

// If OVS restart, the ovn0 port will down and prevent host to pod network,
// Restart the kube-ovn-cni when this happens
func (c *Controller) loopOvn0Check() {
	link, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Fatalf("failed to get ovn0 nic, %v", err)
	}

	if link.Attrs().OperState == netlink.OperDown {
		klog.Fatalf("ovn0 nic is down")
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return
	}
	gw := node.Annotations[util.GatewayAnnotation]
	if output, err := ovn0Check(gw); err != nil {
		klog.Fatalf("failed to ping ovn0 gw %v, %q", gw, output)
	}
}

func ovn0Check(gw string) ([]byte, error) {
	protocol := util.CheckProtocol(gw)
	if protocol == kubeovnv1.ProtocolDual {
		gws := strings.Split(gw, ",")
		output, err := exec.Command("ping", "-w", "10", gws[0]).CombinedOutput()
		klog.V(3).Infof("ping v4 gw result is: \n %s", output)
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s, %v", gws[0], err)
			return output, err
		}

		output, err = exec.Command("ping6", "-w", "10", gws[1]).CombinedOutput()
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s, %v", gws[1], err)
			return output, err
		}
		return output, nil
	} else if protocol == kubeovnv1.ProtocolIPv4 {
		output, err := exec.Command("ping", "-w", "10", gw).CombinedOutput()
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s, %v", gw, err)
			return output, err
		}
		return output, nil
	} else {
		output, err := exec.Command("ping6", "-w", "10", gw).CombinedOutput()
		if err != nil {
			klog.Errorf("ovn0 failed to ping gw %s, %v", gw, err)
			return output, err
		}
		return output, nil
	}
}

func configureMirror(portName string, mtu int) error {
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
		"set", "interface", portName, "type=internal", "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		"--id=@mirror0", "get", "port", portName, "--",
		"--id=@m", "create", "mirror", "name=m0", "select_all=true", "output_port=@mirror0", "--",
		"add", "bridge", "br-int", "mirrors", "@m")
	if err != nil {
		klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
		return fmt.Errorf(raw)
	}

	mirrorLink, err := netlink.LinkByName(portName)
	if err != nil {
		return fmt.Errorf("can not find mirror nic %s %v", portName, err)
	}

	if err = netlink.LinkSetMTU(mirrorLink, mtu); err != nil {
		return fmt.Errorf("can not set mirror nic mtu %v", err)
	}

	if mirrorLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(mirrorLink); err != nil {
			return fmt.Errorf("can not set mirror nic %s up %v", portName, err)
		}
	}

	return nil
}

func removeMirror(portName string) error {
	raw, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", portName, "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		ovs.IfExists, "destroy", "mirror", "m0")
	if err != nil {
		klog.Errorf("failed to remove mirror config, %v, %q", err, raw)
		return fmt.Errorf(raw)
	}
	return nil
}

func configureNic(link, ip string, macAddr net.HardwareAddr, mtu int) error {
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

	if err = netlink.LinkSetHardwareAddr(nodeLink, macAddr); err != nil {
		return fmt.Errorf("can not set mac address to nic %s %v", link, err)
	}

	if err = netlink.LinkSetMTU(nodeLink, mtu); err != nil {
		return fmt.Errorf("can not set nic %s mtu %v", link, err)
	}

	if nodeLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(nodeLink); err != nil {
			return fmt.Errorf("can not set node nic %s up %v", link, err)
		}
	}
	return nil
}

func configProviderPort(providerInterfaceName string) error {
	output, err := ovs.Exec(ovs.MayExist, "add-br", util.UnderlayBridge)
	if err != nil {
		return fmt.Errorf("failed to create bridge %s, %v: %q", util.UnderlayBridge, err, output)
	}
	output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
	if err != nil {
		return fmt.Errorf("failed to get external-ids, %v", err)
	}
	bridgeMappings := fmt.Sprintf("%s:%s", providerInterfaceName, util.UnderlayBridge)
	if output != "" && !util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
		bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
	}

	output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=%s", bridgeMappings))
	if err != nil {
		return fmt.Errorf("failed to set bridge-mappings, %v: %q", err, output)
	}

	return nil
}

func providerBridgeExists() (bool, error) {
	output, err := ovs.Exec("list-br")
	if err != nil {
		klog.Errorf("failed to list bridge %v", err)
		return false, err
	}

	lines := strings.Split(output, "\n")
	for _, l := range lines {
		if l == util.UnderlayBridge {
			return true, nil
		}
	}

	return false, nil
}

// Add host nic to br-provider
// MAC, MTU, IP addresses & routes will be copied/transferred to br-provider
func configProviderNic(nicName string) error {
	brName := util.UnderlayBridge
	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("failed to get nic by name %s: %v", nicName, err)
	}
	bridge, err := netlink.LinkByName(brName)
	if err != nil {
		return fmt.Errorf("failed to get bridge by name %s: %v", brName, err)
	}

	sysctlDisableIPv6 := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", brName)
	disableIPv6, err := sysctl.Sysctl(sysctlDisableIPv6)
	if err != nil {
		return fmt.Errorf("failed to get sysctl %s: %v", sysctlDisableIPv6, err)
	}
	if disableIPv6 != "0" {
		if _, err = sysctl.Sysctl(sysctlDisableIPv6, "0"); err != nil {
			return fmt.Errorf("failed to enable ipv6 on OVS bridge %s: %v", brName, err)
		}
	}

	addrs, err := netlink.AddrList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get addresses on nic %s: %v", nicName, err)
	}
	routes, err := netlink.RouteList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get routes on nic %s: %v", nicName, err)
	}

	if _, err = ovs.Exec(ovs.MayExist, "add-port", brName, nicName); err != nil {
		return fmt.Errorf("failed to add %s to OVS birdge %s: %v", nicName, brName, err)
	}

	oldMac := nic.Attrs().HardwareAddr
	newMac, err := net.ParseMAC(util.GenerateMac())
	if err != nil {
		return fmt.Errorf("unexpected error: MAC address generated is invalid")
	}

	for _, addr := range addrs {
		if err = netlink.AddrDel(nic, &addr); err != nil && !errors.Is(err, syscall.ENOENT) {
			return fmt.Errorf("failed to delete address %s on nic %s: %v", addr.String(), nicName, err)
		}

		if addr.Label != "" {
			addr.Label = brName + strings.TrimPrefix(addr.Label, nicName)
		}
		if err = netlink.AddrReplace(bridge, &addr); err != nil && !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("failed to add address %s to OVS bridge %s: %v", addr.String(), brName, err)
		}
	}

	if err = netlink.LinkSetHardwareAddr(nic, newMac); err != nil {
		return fmt.Errorf("failed to set MAC address of nic %s: %v", nicName, err)
	}
	if _, err = ovs.Exec("set", "bridge", brName, fmt.Sprintf(`other-config:hwaddr="%s"`, oldMac.String())); err != nil {
		return fmt.Errorf("failed to set MAC address of OVS bridge %s: %v", brName, err)
	}
	if err = netlink.LinkSetMTU(bridge, nic.Attrs().MTU); err != nil {
		return fmt.Errorf("failed to set MTU of OVS bridge %s: %v", brName, err)
	}
	if err = netlink.LinkSetUp(bridge); err != nil {
		return fmt.Errorf("failed to set OVS bridge %s up: %v", brName, err)
	}

	for _, route := range routes {
		route.LinkIndex = bridge.Attrs().Index
		if err = netlink.RouteReplace(&route); err != nil && !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("failed to add route %s: %v", route.String(), err)
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
	veth := netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostNicName, MTU: mtu}, PeerName: containerNicName}
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
func setupSriovInterface(containerID, deviceID, ifName string, mtu int) (string, string, error) {
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
	vfNetdevice := vfNetdevices[0]

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

func (csh cniServerHandler) configureNicWithInternalPort(podName, podNamespace, provider, netns, containerID, ifName, mac, ip, gateway, ingress, egress, vlanID, DeviceID, nicType, podNetns string) error {
	var err error

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
		return fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}

	// container nic must use same mac address from pod annotation, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, ingress, egress); err != nil {
		return err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, macAddr, podNS, csh.Config.MTU, nicType); err != nil {
		return err
	}
	return nil
}

// https://github.com/antrea-io/antrea/issues/1691
func configureAdditonalNic(link, ip string) error {
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

func addAdditonalNic(ifName string) error {
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
