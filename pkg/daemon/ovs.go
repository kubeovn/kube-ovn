package daemon

import (
	"fmt"
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	goping "github.com/sparrc/go-ping"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"
	"net"
	"os/exec"
	"strings"
	"time"
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
	if err = netlink.LinkAdd(&veth); err != nil {
		return fmt.Errorf("failed to crate veth for %s %v", podName, err)
	}

	ifaceID := fmt.Sprintf("%s.%s", podName, podNamespace)
	ovs.CleanDuplicatePort(ifaceID)
	// Add veth pair host end to ovs port
	output, err := exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}

	// host and container nic must use same mac address, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}
	if err = configureHostNic(hostNicName, macAddr); err != nil {
		return err
	}
	if err = ovs.SetPodBandwidth(podName, podNamespace, ingress, egress); err != nil {
		return err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ip, gateway, macAddr, podNS, csh.Config.MTU); err != nil {
		return err
	}
	return nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, containerID string) error {
	hostNicName, _ := generateNicName(containerID)
	// Remove ovs port
	output, err := exec.Command("ovs-vsctl", "--if-exists", "--with-iface", "del-port", "br-int", hostNicName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %v, %q", err, output)
	}

	if err = ovs.ClearPodBandwidth(podName, podNamespace); err != nil {
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
	if err = netlink.LinkDel(hostLink); err != nil {
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

	if err = netlink.LinkSetHardwareAddr(hostLink, macAddr); err != nil {
		return fmt.Errorf("can not set mac address to host nic %s %v", nicName, err)
	}
	if hostLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(hostLink); err != nil {
			return fmt.Errorf("can not set host nic %s up %v", nicName, err)
		}
	}
	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen %v", nicName, err)
	}

	return nil
}

func configureContainerNic(nicName, ipAddr, gateway string, macAddr net.HardwareAddr, netns ns.NetNS, mtu int) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find container nic %s %v", nicName, err)
	}

	if err = netlink.LinkSetNsFd(containerLink, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to link netns %v", err)
	}

	// TODO: use github.com/containernetworking/plugins/pkg/ipam.ConfigureIface to refactor this logical
	return ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		// Container nic name MUST be 'eth0', otherwise kubelet will recreate the pod
		if err = netlink.LinkSetName(containerLink, "eth0"); err != nil {
			return err
		}
		if util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolIPv6 {
			// For docker version >=17.x the "none" network will disable ipv6 by default.
			// We have to enable ipv6 here to add v6 address and gateway.
			// See https://github.com/containernetworking/cni/issues/531
			if _, err = sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6", "0"); err != nil {
				return fmt.Errorf("failed to enable ipv6 on all nic %v", err)
			}
		}

		if err = configureNic("eth0", ipAddr, macAddr, mtu); err != nil {
			return err
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
		}

		if err != nil {
			return fmt.Errorf("config gateway failed %v", err)
		}

		return waiteNetworkReady(gateway)
	})
}

func waiteNetworkReady(gateway string) error {
	pinger, err := goping.NewPinger(gateway)
	if err != nil {
		return fmt.Errorf("failed to init pinger, %v", err)
	}
	pinger.SetPrivileged(true)
	pinger.Count = 600
	pinger.Timeout = 600 * time.Second
	pinger.Interval = 1 * time.Second

	success := false
	pinger.OnRecv = func(p *goping.Packet) {
		success = true
		pinger.Stop()
	}
	pinger.Run()

	if !success {
		return fmt.Errorf("network not ready after 600 ping")
	}
	klog.Infof("network ready after %d ping", pinger.PacketsSent)
	return nil
}

func configureNodeNic(portName, ip, gw string, macAddr net.HardwareAddr, mtu int) error {
	raw, err := exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-int", util.NodeNic, "--",
		"set", "interface", util.NodeNic, "type=internal", "--",
		"set", "interface", util.NodeNic, fmt.Sprintf("external_ids:iface-id=%s", portName)).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to configure node nic %s %q", portName, raw)
		return fmt.Errorf(string(raw))
	}

	if err = configureNic(util.NodeNic, ip, macAddr, mtu); err != nil {
		return err
	}

	// ping gw to activate the flow
	var output []byte
	if util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
		output, _ = exec.Command("ping", "-w", "10", gw).CombinedOutput()
	} else {
		output, _ = exec.Command("ping", "-6", "-w", "10", gw).CombinedOutput()
	}

	klog.Infof("ping gw result is: \n %q", output)
	return nil
}

func configureMirror(portName string, mtu int) error {
	raw, err := exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-int", portName, "--",
		"set", "interface", portName, "type=internal", "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		"--id=@mirror0", "get", "port", portName, "--",
		"--id=@m", "create", "mirror", "name=m0", "select_all=true", "output_port=@mirror0", "--",
		"add", "bridge", "br-int", "mirrors", "@m").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
		return fmt.Errorf(string(raw))
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

func configureNic(link, ip string, macAddr net.HardwareAddr, mtu int) error {
	nodeLink, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("can not find nic %s %v", link, err)
	}

	ipAddr, err := netlink.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("can not parse %s %v", ip, err)
	}

	if err = netlink.AddrReplace(nodeLink, ipAddr); err != nil {
		return fmt.Errorf("can not add address to nic %s, %v", link, err)
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
	output, err := exec.Command("ovs-vsctl", "--may-exist", "add-br", "br-provider", "--",
		"set", "open", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=%s:br-provider", providerInterfaceName)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create bridge br-provider, %v: %q", err, output)
	}

	output, err = exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-provider", "provider-int", "--",
		"set", "interface", "provider-int", "type=patch", "options:peer=int-provider").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create patch port provider-int, %v: %q", err, output)
	}

	output, err = exec.Command(
		"ovs-vsctl", "--may-exist", "add-port", "br-int", "int-provider", "--",
		"set", "interface", "int-provider", "type=patch", "options:peer=provider-int").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create patch port int-provider, %v: %q", err, output)
	}

	return nil
}

func providerBridgeExists() (bool, error) {
	output, err := exec.Command("ovs-vsctl", "list-br").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to list bridge %v", err)
		return false, err
	}

	lines := strings.Split(string(output), "\n")
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
	_, err := exec.Command("ovs-vsctl", "--may-exist", "add-port", "br-provider", nicName).CombinedOutput()
	return err
}
