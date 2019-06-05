package daemon

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"
	"net"
	"os/exec"
)

func (csh cniServerHandler) configureNic(podName, podNamespace, netns, containerID, mac, ip, gateway, ingress, egress string) error {
	var err error
	hostNicName, containerNicName := generateNicName(containerID)
	// Create a veth pair, put one end to container ,the other to ovs port
	// NOTE: DO NOT use ovs internal type interface for container.
	// Kubernetes will detect 'eth0' nic in pod, so the nic name in pod must be 'eth0'.
	// When renaming internal interface to 'eth0', ovs will delete and recreate this interface.
	veth := netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostNicName, MTU: csh.Config.MTU}, PeerName: containerNicName}
	defer func() {
		// Remove veth link in case any error during creating pod network.
		if err != nil {
			netlink.LinkDel(&veth)
		}
	}()
	err = netlink.LinkAdd(&veth)
	if err != nil {
		return fmt.Errorf("failed to crate veth for %s %v", podName, err)
	}

	// Add veth pair host end to ovs port
	output, err := exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName, fmt.Sprintf("external_ids:iface-id=%s.%s", podName, podNamespace)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("add nic to ovs failed %v: %s", err, output)
	}

	// host and container nic must use same mac address, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	err = configureHostNic(hostNicName, macAddr)
	if err != nil {
		return err
	}

	err = ovs.SetPodBandwidth(podName, podNamespace, ingress, egress)
	if err != nil {
		return err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	err = configureContainerNic(containerNicName, ip, gateway, macAddr, podNS)
	if err != nil {
		return err
	}

	return nil
}

func (csh cniServerHandler) deleteNic(netns, podName, podNamespace, containerID string) error {
	hostNicName, _ := generateNicName(containerID)
	// Remove ovs port
	output, err := exec.Command("ovs-vsctl", "--if-exists", "--with-iface", "del-port", "br-int", hostNicName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %v, %s", err, output)
	}
	err = ovs.ClearPodBandwidth(podName, podNamespace)
	if err != nil {
		return err
	}
	hostLink, err := netlink.LinkByName(hostNicName)
	if err != nil {
		// If link already not exists, return quietly
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("find host link %s failed %v", hostNicName, err)
	}
	err = netlink.LinkDel(hostLink)
	if err != nil {
		return fmt.Errorf("delete host link %s failed %v", hostLink, err)
	}

	return nil
}

func generateNicName(containerID string) (string, string) {
	return fmt.Sprintf("%s_h", containerID[0:12]), fmt.Sprintf("%s_c", containerID[0:12])
}

func configureHostNic(nicName string, macAddr net.HardwareAddr) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find host nic %s %v", nicName, err)
	}

	err = netlink.LinkSetHardwareAddr(hostLink, macAddr)
	if err != nil {
		return fmt.Errorf("can not set mac address to host nic %s %v", nicName, err)
	}
	err = netlink.LinkSetUp(hostLink)
	if err != nil {
		return fmt.Errorf("can not set host nic %s up %v", nicName, err)
	}
	return nil
}

func configureContainerNic(nicName, ipAddr, gateway string, macAddr net.HardwareAddr, netns ns.NetNS) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find container nic %s %v", nicName, err)
	}

	err = netlink.LinkSetNsFd(containerLink, int(netns.Fd()))
	if err != nil {
		return fmt.Errorf("failed to link netns %v", err)
	}

	// TODO: use github.com/containernetworking/plugins/pkg/ipam.ConfigureIface to refactor this logical
	return ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		// Container nic name MUST be 'eth0', otherwise kubelet will recreate the pod
		err = netlink.LinkSetName(containerLink, "eth0")
		if err != nil {
			return err
		}
		addr, err := netlink.ParseAddr(ipAddr)
		if err != nil {
			return fmt.Errorf("can not parse %s %v", ipAddr, err)
		}
		err = netlink.AddrAdd(containerLink, addr)
		if err != nil {
			return fmt.Errorf("can not add address to container nic %v", err)
		}

		err = netlink.LinkSetHardwareAddr(containerLink, macAddr)
		if err != nil {
			return fmt.Errorf("can not set mac address to container nic %v", err)
		}
		err = netlink.LinkSetUp(containerLink)
		if err != nil {
			return fmt.Errorf("can not set container nic %s up %v", nicName, err)
		}

		_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
		err = netlink.RouteAdd(&netlink.Route{
			LinkIndex: containerLink.Attrs().Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       defaultNet,
			Gw:        net.ParseIP(gateway),
		})
		if err != nil {
			return fmt.Errorf("config gateway failed %v", err)
		}
		return nil
	})
}

func configureNodeNic(portName, ip, mac, gw string, mtu int) error {
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	raw, err := exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-int", util.NodeNic, "--",
		"set", "interface", util.NodeNic, "type=internal", "--",
		"set", "interface", util.NodeNic, fmt.Sprintf("external_ids:iface-id=%s", portName)).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to configure node nic %s %s", portName, string(raw))
		return fmt.Errorf(string(raw))
	}

	nodeLink, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		return fmt.Errorf("can not find node nic %s %v", portName, err)
	}

	ipAddr, err := netlink.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("can not parse %s %v", ip, err)
	}

	err = netlink.AddrReplace(nodeLink, ipAddr)
	if err != nil {
		return fmt.Errorf("can not add address to node nic %v", err)
	}

	err = netlink.LinkSetHardwareAddr(nodeLink, macAddr)
	if err != nil {
		return fmt.Errorf("can not set mac address to node nic %v", err)
	}

	err = netlink.LinkSetMTU(nodeLink, mtu)
	if err != nil {
		return fmt.Errorf("can not set mtu %v", err)
	}

	if nodeLink.Attrs().OperState != netlink.OperUp {
		err = netlink.LinkSetUp(nodeLink)
		if err != nil {
			return fmt.Errorf("can not set node nic %s up %v", portName, err)
		}
	}

	// ping gw to activate the flow
	output, _ := exec.Command("ping", "-w", "10", gw).CombinedOutput()
	klog.Infof("ping gw result is: \n %s", string(output))
	return nil
}
