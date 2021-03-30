package daemon

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/Mellanox/sriovnet"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"
)

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, ifName, mac, ip, gateway, ingress, egress, vlanID, DeviceID string) error {
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
		fmt.Sprintf("external_ids:ip=%s", ipStr))
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
	if err = ovs.SetInterfaceBandwidth(fmt.Sprintf("%s.%s", podName, podNamespace), ingress, egress); err != nil {
		return err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, macAddr, podNS, csh.Config.MTU); err != nil {
		return err
	}
	return nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, containerID, deviceID, ifName string) error {
	hostNicName, _ := generateNicName(containerID, ifName)
	// Remove ovs port
	output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", hostNicName)
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %v, %q", err, output)
	}

	if err = ovs.ClearPodBandwidth(podName, podNamespace); err != nil {
		return err
	}

	if deviceID == "" {
		hostLink, err := netlink.LinkByName(hostNicName)
		if err != nil {
			// If link already not exists, return quietly
			if _, ok := err.(netlink.LinkNotFoundError); ok {
				return nil
			}
			return fmt.Errorf("find host link %s failed %v", hostNicName, err)
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

func configureContainerNic(nicName, ifName string, ipAddr, gateway string, macAddr net.HardwareAddr, netns ns.NetNS, mtu int) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find container nic %s %v", nicName, err)
	}

	if err = netlink.LinkSetNsFd(containerLink, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to link netns %v", err)
	}

	return ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		if err = netlink.LinkSetName(containerLink, ifName); err != nil {
			return err
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

		if err = configureNic(ifName, ipAddr, macAddr, mtu); err != nil {
			return err
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

		return nil
	})
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

	go loopOvn0Check(gw)
	return nil
}

// If OVS restart, the ovn0 port will down and prevent host to pod network,
// Restart the kube-ovn-cni when this happens
func loopOvn0Check(gw string) {
	for {
		time.Sleep(5 * time.Second)
		link, err := netlink.LinkByName(util.NodeNic)
		if err != nil {
			klog.Fatalf("failed to get ovn0 nic, %v", err)
		}

		if link.Attrs().OperState == netlink.OperDown {
			klog.Fatalf("ovn0 nic is down")
		}

		if output, err := ovn0Check(gw); err != nil {
			klog.Fatalf("failed to ping ovn0 gw %v, %q", gw, output)
		}
	}
}

func ovn0Check(gw string) ([]byte, error) {
	protocol := util.CheckProtocol(gw)
	if protocol == kubeovnv1.ProtocolDual {
		gws := strings.Split(gw, ",")
		output, err := exec.Command("ping", "-w", "10", gws[0]).CombinedOutput()
		klog.Infof("ping v4 gw result is: \n %s", output)
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
	output, err := ovs.Exec(ovs.MayExist, "add-br", "br-provider")
	if err != nil {
		return fmt.Errorf("failed to create bridge br-provider, %v: %q", err, output)
	}
	output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
	if err != nil {
		return fmt.Errorf("failed to get external-ids, %v", err)
	}
	bridgeMappings := fmt.Sprintf("%s:br-provider", providerInterfaceName)
	if output != "" && !util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
		bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
	}

	output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=%s", bridgeMappings))
	if err != nil {
		return fmt.Errorf("failed to set bridg-mappings, %v: %q", err, output)
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
		if l == "br-provider" {
			return true, nil
		}
	}

	return false, nil
}

// Add host nic to br-provider
// A physical Ethernet device that is part of an Open vSwitch bridge should not have an IP address. If one does, then that IP address will not be fully functional.
// More info refer http://docs.openvswitch.org/en/latest/faq/issues
func configProviderNic(nicName string) error {
	_, err := ovs.Exec(ovs.MayExist, "add-port", "br-provider", nicName)
	return err
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
