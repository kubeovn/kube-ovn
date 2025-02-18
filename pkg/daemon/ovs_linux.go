package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Mellanox/sriovnet"
	sriovutilfs "github.com/Mellanox/sriovnet/pkg/utils/filesystem"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var pciAddrRegexp = regexp.MustCompile(`\b([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}.\d{1}\S*)`)

func (csh cniServerHandler) configureDpdkNic(podName, podNamespace, provider, netns, containerID, ifName, mac string, mtu int, ip, gateway, ingress, egress, shortSharedDir, socketName string) error {
	sharedDir := filepath.Join("/var", shortSharedDir)
	hostNicName, _ := generateNicName(containerID, ifName)

	ipStr := util.GetIpWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID, hostNicName)
	// Add veth pair host end to ovs port
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName,
		"type=dpdkvhostuserclient",
		fmt.Sprintf("options:vhost-server-path=%s", path.Join(sharedDir, socketName)),
		fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:pod_name=%s", podName),
		fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
		fmt.Sprintf("external_ids:ip=%s", ipStr),
		fmt.Sprintf("external_ids:pod_netns=%s", netns))
	if err != nil {
		return fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}
	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
		return err
	}
	return nil
}

func getCurrentVfCount(pfNetdevName string) (int, error) {
	devDirName := filepath.Join(util.NetSysDir, pfNetdevName, "device", "sriov_numvfs")
	value, err := os.ReadFile(devDirName) // #nosec G304
	if err != nil {
		klog.Errorf("read file %s error: %v", devDirName, err)
		return 0, nil
	}

	vfNum := strings.TrimSuffix(string(value), "\n")
	klog.Infof("get current vf number is %s for %s", vfNum, pfNetdevName)
	return strconv.Atoi(vfNum)
}

func getNetDeviceNameFromPci(deviceID string) string {
	netDevs, err := sriovnet.GetNetDevicesFromPci(deviceID)
	if err != nil {
		return ""
	}

	var netName string
	if len(netDevs) > 0 {
		netName = netDevs[0]
	}
	klog.Infof("get net device name is %s", netName)
	return netName
}

func getRepIndex(vfDeviceID, DpdkDevID string) (int, int, error) {
	repIndex, vfNums := 0, 0
	// get vf index
	vfIndex, err := sriovnet.GetVfIndexByPciAddress(vfDeviceID)
	if err != nil {
		klog.Errorf("failed to get vf %s index, %v", vfDeviceID, err)
		return repIndex, vfNums, err
	}

	// get vf nums for two pf
	pfDeviceID, err := sriovnet.GetPfPciFromVfPci(vfDeviceID)
	if err != nil {
		return repIndex, vfNums, fmt.Errorf("failed to get pf of device %s %v", vfDeviceID, err)
	}
	pf0Name := getNetDeviceNameFromPci(pfDeviceID[:len(pfDeviceID)-1] + "0")
	pf0VfCount, err := getCurrentVfCount(pf0Name)
	if err != nil {
		klog.Errorf("failed to get vf number for %s, %v", pf0Name, err)
		return repIndex, vfNums, err
	}
	pf1Name := getNetDeviceNameFromPci(pfDeviceID[:len(pfDeviceID)-1] + "1")
	pf1VfCount, err := getCurrentVfCount(pf1Name)
	if err != nil {
		klog.Errorf("failed to get vf number for %s, %v", pf1Name, err)
		return repIndex, vfNums, err
	}
	vfNums = pf0VfCount + pf1VfCount

	klog.Infof("get vf device id is %s, index is %d, pf device is %s, pf0 vf num is %d, pf1 vf num is %d",
		vfDeviceID, vfIndex, pfDeviceID, pf0VfCount, pf1VfCount)

	if pfDeviceID[len(pfDeviceID)-1:] == "1" {
		repIndex = vfIndex + pf0VfCount
	} else {
		repIndex = vfIndex
	}

	return repIndex, vfNums, nil
}

func getQueueDesc(vfNums int) (string, string) {
	RxQueueDesc := ovs.GetOtherConfig("rx-queue-desc")
	TxQueueDesc := ovs.GetOtherConfig("tx-queue-desc")

	if RxQueueDesc == "" {
		if vfNums <= 512 {
			RxQueueDesc = "1024"
		} else {
			RxQueueDesc = "512"
		}
	}

	if TxQueueDesc == "" {
		if vfNums <= 128 {
			TxQueueDesc = "1024"
		} else if vfNums <= 256 {
			TxQueueDesc = "512"
		} else {
			TxQueueDesc = "256"
		}
	}

	return RxQueueDesc, TxQueueDesc
}

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss string, gwCheckMode int, u2oInterconnectionIP string) error {
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
	ovs.CleanDuplicatePort(ifaceID, hostNicName)
	// Add veth pair host end to ovs port
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName),
		fmt.Sprintf("external_ids:pod_name=%s", podName),
		fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
		fmt.Sprintf("external_ids:ip=%s", ipStr),
		fmt.Sprintf("external_ids:pod_netns=%s", netns))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}

	// lsp and container nic must use same mac address, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}
	if err = configureHostNic(hostNicName); err != nil {
		klog.Error(err)
		return err
	}
	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
		klog.Error(err)
		return err
	}

	if err = ovs.SetNetemQos(podName, podNamespace, ifaceID, latency, limit, loss); err != nil {
		klog.Error(err)
		return err
	}

	if containerNicName == "" {
		return nil
	}
	isUserspaceDP, err := ovs.IsUserspaceDataPath()
	if err != nil {
		klog.Error(err)
		return err
	}
	if isUserspaceDP {
		// turn off tx checksum
		if err = turnOffNicTxChecksum(containerNicName); err != nil {
			klog.Error(err)
			return err
		}
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, isDefaultRoute, detectIPConflict, routes, macAddr, podNS, mtu, nicType, gwCheckMode, u2oInterconnectionIP); err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func (csh cniServerHandler) configureYunsiliconNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss string, gwCheckMode int, u2oInterconnectionIP string) error {
	var err error
	var hostNicName, containerNicName string

	hostNicName, containerNicName, err = setupYunsiliconInterface(containerID, DeviceID, ifName, mac)
	if err != nil {
		klog.Errorf("failed to create sriov interfaces %v", err)
		return err
	}

	ipStr := util.GetIpWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	var cmdArg []string
	cmdArg = append(cmdArg, ovs.MayExist, "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName)
	cmdArg = append(cmdArg, fmt.Sprintf("external_ids:iface-id=%s", ifaceID))
	cmdArg = append(cmdArg, fmt.Sprintf("external_ids:pod_name=%s", podName))
	cmdArg = append(cmdArg, fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace))
	cmdArg = append(cmdArg, fmt.Sprintf("external_ids:ip=%s", ipStr))
	cmdArg = append(cmdArg, fmt.Sprintf("external_ids:pod_netns=%s", netns))

	ovs.DeleteDuplicatePort(DeviceID)

	DpdkDevID := ovs.GetOtherConfig("dpdk-dev")
	if DpdkDevID == "" {
		return fmt.Errorf("failed to get DPDK_DEV from ovs dpdk file")
	}
	repIndex, vfNums, err := getRepIndex(DeviceID, DpdkDevID)
	if err != nil {
		return err
	}
	RxQueueDesc, TxQueueDesc := getQueueDesc(vfNums)
	cmdArg = append(cmdArg, "type=dpdk")
	cmdArg = append(cmdArg, fmt.Sprintf("options:dpdk-devargs=%s,representor=[%d]", DpdkDevID, repIndex))
	cmdArg = append(cmdArg, fmt.Sprintf("options:n_rxq_desc=%s", RxQueueDesc))
	cmdArg = append(cmdArg, fmt.Sprintf("options:n_txq_desc=%s", TxQueueDesc))
	cmdArg = append(cmdArg, fmt.Sprintf("mtu_request=%d", mtu))
	cmdArg = append(cmdArg, fmt.Sprintf("external_ids:pci_bdf=%s", strings.Replace(DeviceID, ":", "", -1)))

	klog.Infof("ovs-vsctl add-port br-int %s type=dpdk options:dpdk-devargs=%s,representor=[%d] external_ids:iface-id=%s,ip=%s,pod_name=%s,pod_netns=%s",
		hostNicName, DpdkDevID, repIndex, ifaceID, ipStr, podName, netns)

	// Add vf host end to ovs port
	output, err := ovs.Exec(cmdArg...)
	if err != nil {
		return fmt.Errorf("add vf_rep nic to ovs failed %v: %q", err, output)
	}

	// lsp and container nic must use same mac address, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	klog.Infof("set bw on ifaceID %s", ifaceID)
	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("set qos on ifaceID=%s, containerNicName=%s", ifaceID, containerNicName)
	if err = ovs.SetNetemQos(podName, podNamespace, ifaceID, latency, limit, loss); err != nil {
		klog.Error(err)
		return err
	}

	if containerNicName == "" {
		return nil
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}

	if err = configureContainerNic(containerNicName, ifName, ip, gateway, isDefaultRoute, detectIPConflict, routes, macAddr, podNS, mtu, nicType, gwCheckMode, u2oInterconnectionIP); err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("configureNic %s ok", containerNicName)
	return nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, containerID, netns, deviceID, ifName, nicType string) error {
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

	if err = ovs.ClearPodBandwidth(podName, podNamespace, ""); err != nil {
		return err
	}
	if err = ovs.ClearHtbQosQueue(podName, podNamespace, ""); err != nil {
		return err
	}

	if deviceID == "" {
		hostLink, err := netlink.LinkByName(nicName)
		if err != nil {
			// If link already not exists, return quietly
			// E.g. Internal port had been deleted by Remove ovs port previously
			if _, ok := err.(netlink.LinkNotFoundError); ok {
				return nil
			}
			return fmt.Errorf("find host link %s failed %v", nicName, err)
		}

		hostLinkType := hostLink.Type()
		// Sometimes no deviceID input for vf nic, avoid delete vf nic.
		if hostLinkType == "veth" {
			if err = netlink.LinkDel(hostLink); err != nil {
				return fmt.Errorf("delete host link %s failed %v", hostLink, err)
			}
		}
	} else if pciAddrRegexp.MatchString(deviceID) {
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
	// The nic name is 14 length and have prefix pod in the Kubevirt v1.0.0
	if strings.HasPrefix(ifname, "pod") && len(ifname) == 14 {
		ifname = ifname[3 : len(ifname)-4]
		return fmt.Sprintf("%s_%s_h", containerID[0:12-len(ifname)], ifname), fmt.Sprintf("%s_%s_c", containerID[0:12-len(ifname)], ifname)
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

func configureContainerNic(nicName, ifName string, ipAddr, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, macAddr net.HardwareAddr, netns ns.NetNS, mtu int, nicType string, gwCheckMode int, u2oInterconnectionIP string) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("can not find container nic %s: %v", nicName, err)
	}

	// Set link alias to its origin link name for fastpath to recognize and bypass netfilter
	if err := netlink.LinkSetAlias(containerLink, nicName); err != nil {
		klog.Errorf("failed to set link alias for container nic %s: %v", nicName, err)
		return err
	}

	if err = netlink.LinkSetNsFd(containerLink, int(netns.Fd())); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to move link to netns: %v", err)
	}

	return ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		if nicType != util.InternalType {
			if err = netlink.LinkSetName(containerLink, ifName); err != nil {
				klog.Error(err)
				return err
			}
		}

		if util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolDual || util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolIPv6 {
			// For docker version >=17.x the "none" network will disable ipv6 by default.
			// We have to enable ipv6 here to add v6 address and gateway.
			// See https://github.com/containernetworking/cni/issues/531
			value, err := sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6")
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("failed to get sysctl net.ipv6.conf.all.disable_ipv6: %v", err)
			}
			if value != "0" {
				if _, err = sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6", "0"); err != nil {
					klog.Error(err)
					return fmt.Errorf("failed to enable ipv6 on all nic: %v", err)
				}
			}
		}

		if nicType == util.InternalType {
			if err = addAdditionalNic(ifName); err != nil {
				klog.Error(err)
				return err
			}
			if err = configureAdditionalNic(ifName, ipAddr); err != nil {
				klog.Error(err)
				return err
			}
			if err = configureNic(nicName, ipAddr, macAddr, mtu, detectIPConflict, false); err != nil {
				klog.Error(err)
				return err
			}
		} else {
			if err = configureNic(ifName, ipAddr, macAddr, mtu, detectIPConflict, true); err != nil {
				klog.Error(err)
				return err
			}
		}

		if isDefaultRoute {
			// Only eth0 requires the default route and gateway
			containerGw := gateway
			if u2oInterconnectionIP != "" {
				containerGw = u2oInterconnectionIP
			}

			switch util.CheckProtocol(ipAddr) {
			case kubeovnv1.ProtocolIPv4:
				_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
				err = netlink.RouteReplace(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(containerGw),
				})
			case kubeovnv1.ProtocolIPv6:
				_, defaultNet, _ := net.ParseCIDR("::/0")
				err = netlink.RouteReplace(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(containerGw),
				})
			case kubeovnv1.ProtocolDual:
				gws := strings.Split(containerGw, ",")
				_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
				err = netlink.RouteReplace(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Dst:       defaultNet,
					Gw:        net.ParseIP(gws[0]),
				})
				if err != nil {
					return fmt.Errorf("config v4 gateway failed: %v", err)
				}

				_, defaultNet, _ = net.ParseCIDR("::/0")
				err = netlink.RouteReplace(&netlink.Route{
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

		if gwCheckMode != gatewayModeDisabled {
			var (
				underlayGateway = gwCheckMode == gatewayCheckModeArping || gwCheckMode == gatewayCheckModeArpingNotConcerned
				interfaceName   = nicName
			)

			if nicType != util.InternalType {
				interfaceName = ifName
			}

			if u2oInterconnectionIP != "" {
				if err := checkGatewayReady(gwCheckMode, interfaceName, ipAddr, u2oInterconnectionIP, false, true); err != nil {
					klog.Error(err)
					return err
				}
			}
			return checkGatewayReady(gwCheckMode, interfaceName, ipAddr, gateway, underlayGateway, true)
		}

		return nil
	})
}

func checkGatewayReady(gwCheckMode int, intr, ipAddr, gateway string, underlayGateway, verbose bool) error {
	var err error

	if gwCheckMode == gatewayCheckModeArpingNotConcerned || gwCheckMode == gatewayCheckModePingNotConcerned {
		// ignore error while disableGatewayCheck is true
		if err = waitNetworkReady(intr, ipAddr, gateway, underlayGateway, verbose, 1); err != nil {
			klog.Warningf("network %s with gateway %s is not ready for interface %s: %v", ipAddr, gateway, intr, err)
			err = nil
		}
	} else {
		err = waitNetworkReady(intr, ipAddr, gateway, underlayGateway, verbose, gatewayCheckMaxRetry)
	}
	if err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func waitNetworkReady(nic, ipAddr, gateway string, underlayGateway, verbose bool, maxRetry int) error {
	ips := strings.Split(ipAddr, ",")
	for i, gw := range strings.Split(gateway, ",") {
		src := strings.Split(ips[i], "/")[0]
		if underlayGateway && util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
			// v4 underlay gateway check use arping
			mac, count, err := util.ArpResolve(nic, src, gw, time.Second, maxRetry)
			cniConnectivityResult.WithLabelValues(nodeName).Add(float64(count))
			if err != nil {
				err = fmt.Errorf("network %s with gateway %s is not ready for interface %s after %d checks: %v", ips[i], gw, nic, count, err)
				klog.Warning(err)
				return err
			}
			if verbose {
				klog.Infof("MAC addresses of gateway %s is %s", gw, mac.String())
				klog.Infof("network %s with gateway %s is ready for interface %s after %d checks", ips[i], gw, nic, count)
			}
		} else {
			// v6 or vpc gateway check use ping
			if err := pingGateway(gw, src, verbose, maxRetry); err != nil {
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func configureNodeNic(cs kubernetes.Interface, nodeName, portName, ip, gw, joinCIDR string, macAddr net.HardwareAddr, mtu int) error {
	ipStr := util.GetIpWithoutMask(ip)
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", util.NodeNic, "--",
		"set", "interface", util.NodeNic, "type=internal", "--",
		"set", "interface", util.NodeNic, fmt.Sprintf("external_ids:iface-id=%s", portName),
		fmt.Sprintf("external_ids:ip=%s", ipStr))
	if err != nil {
		klog.Errorf("failed to configure node nic %s: %v, %q", portName, err, raw)
		return fmt.Errorf(raw)
	}

	if err = configureNic(util.NodeNic, ip, macAddr, mtu, false, false); err != nil {
		return err
	}

	hostLink, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		return fmt.Errorf("can not find nic %s: %v", util.NodeNic, err)
	}

	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen: %v", util.NodeNic, err)
	}

	// check and add default route for ovn0 in case of can not add automatically
	nodeNicRoutes, err := getNicExistRoutes(hostLink, gw)
	if err != nil {
		klog.Error(err)
		return err
	}

	var toAdd []netlink.Route
	for _, c := range strings.Split(joinCIDR, ",") {
		found := false
		for _, r := range nodeNicRoutes {
			if r.Dst.String() == c {
				found = true
				break
			}
		}
		if !found {
			protocol := util.CheckProtocol(c)
			var src net.IP
			var priority int
			if protocol == kubeovnv1.ProtocolIPv4 {
				for _, ip := range strings.Split(ipStr, ",") {
					if util.CheckProtocol(ip) == protocol {
						src = net.ParseIP(ip)
						break
					}
				}
			} else {
				priority = 256
			}
			_, cidr, _ := net.ParseCIDR(c)
			toAdd = append(toAdd, netlink.Route{
				Dst:      cidr,
				Src:      src,
				Protocol: netlink.RouteProtocol(unix.RTPROT_KERNEL),
				Scope:    netlink.SCOPE_LINK,
				Priority: priority,
			})
		}
	}
	if len(toAdd) > 0 {
		klog.Infof("routes to be added on nic %s: %v", util.NodeNic, toAdd)
	}

	for _, r := range toAdd {
		r.LinkIndex = hostLink.Attrs().Index
		klog.Infof("adding route %q on %s", r.String(), hostLink.Attrs().Name)
		if err = netlink.RouteReplace(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
			klog.Errorf("failed to replace route %v: %v", r, err)
		}
	}

	// ping ovn0 gw to activate the flow
	klog.Info("wait ovn0 gw ready")
	status := corev1.ConditionFalse
	reason := "JoinSubnetGatewayReachable"
	message := fmt.Sprintf("ping check to gateway ip %s succeeded", gw)
	if err = waitNetworkReady(util.NodeNic, ip, gw, false, true, gatewayCheckMaxRetry); err != nil {
		klog.Errorf("failed to init ovn0 check: %v", err)
		status = corev1.ConditionTrue
		reason = "JoinSubnetGatewayUnreachable"
		message = fmt.Sprintf("ping check to gateway ip %s failed", gw)
	}
	if err := util.SetNodeNetworkUnavailableCondition(cs, nodeName, status, reason, message); err != nil {
		klog.Errorf("failed to set node network unavailable condition: %v", err)
	}

	return err
}

// If OVS restart, the ovn0 port will down and prevent host to pod network,
// Restart the kube-ovn-cni when this happens
func (c *Controller) loopOvn0Check() {
	link, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		util.LogFatalAndExit(err, "failed to get ovn0 nic")
	}

	if link.Attrs().OperState == netlink.OperDown {
		util.LogFatalAndExit(err, "ovn0 nic is down")
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return
	}
	ip := node.Annotations[util.IpAddressAnnotation]
	gw := node.Annotations[util.GatewayAnnotation]
	status := corev1.ConditionFalse
	reason := "JoinSubnetGatewayReachable"
	message := fmt.Sprintf("ping check to gateway ip %s succeeded", gw)
	if err = waitNetworkReady(util.NodeNic, ip, gw, false, false, 5); err != nil {
		klog.Errorf("failed to init ovn0 check: %v", err)
		status = corev1.ConditionTrue
		reason = "JoinSubnetGatewayUnreachable"
		message = fmt.Sprintf("ping check to gateway ip %s failed", gw)
	}

	var alreadySet bool
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeNetworkUnavailable && condition.Status == corev1.ConditionTrue &&
			condition.Reason == reason && condition.Message == message {
			alreadySet = true
			break
		}
	}
	if !alreadySet {
		if err := util.SetNodeNetworkUnavailableCondition(c.config.KubeClient, c.config.NodeName, status, reason, message); err != nil {
			klog.Errorf("failed to set node network unavailable condition: %v", err)
		}
	}

	if err != nil {
		util.LogFatalAndExit(err, "failed to ping ovn0 gateway %s", gw)
	}
}

func configureMirrorLink(portName string, mtu int) error {
	mirrorLink, err := netlink.LinkByName(portName)
	if err != nil {
		return fmt.Errorf("can not find mirror nic %s: %v", portName, err)
	}

	if mirrorLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(mirrorLink); err != nil {
			return fmt.Errorf("can not set mirror nic %s up: %v", portName, err)
		}
	}

	return nil
}

func configureNic(link, ip string, macAddr net.HardwareAddr, mtu int, detectIPConflict, setUfoOff bool) error {
	nodeLink, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("can not find nic %s: %v", link, err)
	}

	if err = netlink.LinkSetHardwareAddr(nodeLink, macAddr); err != nil {
		return fmt.Errorf("can not set mac address to nic %s: %v", link, err)
	}

	if mtu > 0 {
		if nodeLink.Type() == "openvswitch" {
			_, err = ovs.Exec("set", "interface", link, fmt.Sprintf(`mtu_request=%d`, mtu))
		} else {
			err = netlink.LinkSetMTU(nodeLink, mtu)
		}
		if err != nil {
			return fmt.Errorf("failed to set nic %s mtu: %v", link, err)
		}
	}

	if nodeLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(nodeLink); err != nil {
			return fmt.Errorf("can not set node nic %s up: %v", link, err)
		}
	}

	ipDelMap := make(map[string]netlink.Addr)
	ipAddMap := make(map[string]netlink.Addr)
	ipAddrs, err := netlink.AddrList(nodeLink, unix.AF_UNSPEC)
	if err != nil {
		return fmt.Errorf("can not get addr %s: %v", nodeLink, err)
	}
	for _, ipAddr := range ipAddrs {
		if ipAddr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}
		ipDelMap[ipAddr.IPNet.String()] = ipAddr
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

	for ip, addr := range ipDelMap {
		klog.Infof("delete ip address %s on %s", ip, link)
		if err = netlink.AddrDel(nodeLink, &addr); err != nil {
			return fmt.Errorf("delete address %s: %v", addr, err)
		}
	}
	for ip, addr := range ipAddMap {
		if detectIPConflict && addr.IP.To4() != nil {
			ip := addr.IP.String()
			mac, err := util.ArpDetectIPConflict(link, ip, macAddr)
			if err != nil {
				err = fmt.Errorf("failed to detect address conflict for %s on link %s: %v", ip, link, err)
				klog.Error(err)
				return err
			}
			if mac != nil {
				return fmt.Errorf("IP address %s has already been used by host with MAC %s", ip, mac)
			}
		}
		if addr.IP.To4() != nil && !detectIPConflict {
			// when detectIPConflict is true, free arp is already broadcast in the step of announcement
			if err := util.AnnounceArpAddress(link, addr.IP.String(), macAddr, 1, 1*time.Second); err != nil {
				klog.Warningf("failed to broadcast free arp with err %v ", err)
			}
		}

		klog.Infof("add ip address %s to %s", ip, link)
		if err = netlink.AddrAdd(nodeLink, &addr); err != nil {
			return fmt.Errorf("can not add address %v to nic %s: %v", addr, link, err)
		}
	}

	if setUfoOff {
		cmd := fmt.Sprintf("if ethtool -k %s | grep -q ^udp-fragmentation-offload; then ethtool -K %s ufo off; fi", link, link)
		if output, err := exec.Command("sh", "-xc", cmd).CombinedOutput(); err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to disable udp-fragmentation-offload feature of device %s to off: %w, %s", link, err, output)
		}
	}

	return nil
}

func (c *Controller) transferAddrsAndRoutes(nicName, brName string, delNonExistent bool) (int, error) {
	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		return 0, fmt.Errorf("failed to get nic by name %s: %v", nicName, err)
	}
	bridge, err := netlink.LinkByName(brName)
	if err != nil {
		return 0, fmt.Errorf("failed to get bridge by name %s: %v", brName, err)
	}

	addrs, err := netlink.AddrList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses on nic %s: %v", nicName, err)
	}
	routes, err := netlink.RouteList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get routes on nic %s: %v", nicName, err)
	}

	brAddrs, err := netlink.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses on OVS bridge %s: %v", brName, err)
	}

	var delAddrs []netlink.Addr
	if delNonExistent {
		for _, addr := range brAddrs {
			if addr.IP.IsLinkLocalUnicast() {
				// skip 169.254.0.0/16 and fe80::/10
				continue
			}

			var found bool
			for _, v := range addrs {
				if v.Equal(addr) {
					found = true
					break
				}
			}
			if !found {
				delAddrs = append(delAddrs, addr)
			}
		}
	}

	// set link unmanaged by NetworkManager
	if err = c.nmSyncer.SetManaged(nicName, false); err != nil {
		klog.Errorf("failed to set device %s unmanaged by NetworkManager: %v", nicName, err)
		return 0, err
	}
	if err = c.nmSyncer.AddDevice(nicName, brName); err != nil {
		klog.Errorf("failed to monitor NetworkManager event for device %s: %v", nicName, err)
		return 0, err
	}

	var count int
	for _, addr := range addrs {
		if addr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}
		count++

		if err = netlink.AddrDel(nic, &addr); err != nil {
			errMsg := fmt.Errorf("failed to delete address %q on nic %s: %v", addr.String(), nicName, err)
			klog.Error(errMsg)
			return 0, errMsg
		}
		klog.Infof("address %q has been removed from link %s", addr.String(), nicName)

		addr.Label = ""
		addr.PreferedLft, addr.ValidLft = 0, 0
		if err = netlink.AddrReplace(bridge, &addr); err != nil {
			return 0, fmt.Errorf("failed to replace address %q on OVS bridge %s: %v", addr.String(), brName, err)
		}
		klog.Infof("address %q has been added/replaced to link %s", addr.String(), brName)
	}

	if count != 0 {
		for _, addr := range delAddrs {
			if err = netlink.AddrDel(bridge, &addr); err != nil {
				errMsg := fmt.Errorf("failed to delete address %q on OVS bridge %s: %v", addr.String(), brName, err)
				klog.Error(errMsg)
				return 0, errMsg
			}
			klog.Infof("address %q has been removed from OVS bridge %s", addr.String(), brName)
		}
	}

	// keep mac address the same with the provider nic,
	// unless the provider nic is a bond in mode 6, or a vlan interface of a bond in mode 6
	albBond, err := linkIsAlbBond(nic)
	if err != nil {
		return 0, err
	}
	if !albBond {
		if _, err = ovs.Exec("set", "bridge", brName, fmt.Sprintf(`other-config:hwaddr="%s"`, nic.Attrs().HardwareAddr.String())); err != nil {
			return 0, fmt.Errorf("failed to set MAC address of OVS bridge %s: %v", brName, err)
		}
	}

	if err = netlink.LinkSetUp(bridge); err != nil {
		return 0, fmt.Errorf("failed to set OVS bridge %s up: %v", brName, err)
	}

	for _, scope := range routeScopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.IP.IsLinkLocalUnicast() {
				// skip 169.254.0.0/16 and fe80::/10
				continue
			}
			if route.Scope == scope {
				route.LinkIndex = bridge.Attrs().Index
				if err = netlink.RouteReplace(&route); err != nil {
					return 0, fmt.Errorf("failed to add/replace route %s to OVS bridge %s: %v", route.String(), brName, err)
				}
				klog.Infof("route %q has been added/replaced to OVS bridge %s", route.String(), brName)
			}
		}
	}

	brRoutes, err := netlink.RouteList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get routes on OVS bridge %s: %v", brName, err)
	}

	var delRoutes []netlink.Route
	if delNonExistent && count != 0 {
		for _, route := range brRoutes {
			if route.Gw == nil && route.Dst != nil && route.Dst.IP.IsLinkLocalUnicast() {
				// skip 169.254.0.0/16 and fe80::/10
				continue
			}

			var found bool
			for _, v := range routes {
				v.LinkIndex = route.LinkIndex
				v.ILinkIndex = route.ILinkIndex
				if v.Equal(route) {
					found = true
					break
				}
			}
			if !found {
				delRoutes = append(delRoutes, route)
			}
		}
	}

	for i := len(routeScopeOrders) - 1; i >= 0; i-- {
		for _, route := range delRoutes {
			if route.Scope == routeScopeOrders[i] {
				if err = netlink.RouteDel(&route); err != nil {
					return 0, fmt.Errorf("failed to delete route %s from OVS bridge %s: %v", route.String(), brName, err)
				}
				klog.Infof("route %q has been deleted from OVS bridge %s", route.String(), brName)
			}
		}
	}

	if err = netlink.LinkSetUp(nic); err != nil {
		return 0, fmt.Errorf("failed to set link %s up: %v", nicName, err)
	}

	return nic.Attrs().MTU, nil
}

// Add host nic to external bridge
// Mac address, MTU, IP addresses & routes will be copied/transferred to the external bridge
func (c *Controller) configProviderNic(nicName, brName string, trunks []string) (int, error) {
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

	mtu, err := c.transferAddrsAndRoutes(nicName, brName, false)
	if err != nil {
		return 0, fmt.Errorf("failed to transfer addresess and routes from %s to %s: %v", nicName, brName, err)
	}

	if _, err = ovs.Exec(ovs.MayExist, "add-port", brName, nicName,
		"--", "set", "port", nicName, "trunks="+strings.Join(trunks, ","), "external_ids:vendor="+util.CniTypeName); err != nil {
		return 0, fmt.Errorf("failed to add %s to OVS bridge %s: %v", nicName, brName, err)
	}
	klog.V(3).Infof("ovs port %s has been added to bridge %s", nicName, brName)

	return mtu, nil
}

func linkIsAlbBond(link netlink.Link) (bool, error) {
	check := func(link netlink.Link) bool {
		bond, ok := link.(*netlink.Bond)
		return ok && bond.Mode == netlink.BOND_MODE_BALANCE_ALB
	}

	if check(link) {
		return true, nil
	}

	vlan, ok := link.(*netlink.Vlan)
	if !ok {
		return false, nil
	}
	parent, err := netlink.LinkByIndex(vlan.ParentIndex)
	if err != nil {
		klog.Errorf("failed to get link by index %d: %v", vlan.ParentIndex, err)
		return false, err
	}

	return check(parent), nil
}

// Remove host nic from external bridge
// IP addresses & routes will be transferred to the host nic
func (c *Controller) removeProviderNic(nicName, brName string) error {
	c.nmSyncer.RemoveDevice(nicName)

	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			klog.Warningf("failed to get nic by name %s: %v", nicName, err)
			return nil
		}
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
		return fmt.Errorf("failed to remove %s from OVS bridge %s: %v", nicName, brName, err)
	}
	klog.V(3).Infof("ovs port %s has been removed from bridge %s", nicName, brName)

	for _, addr := range addrs {
		if addr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}

		if err = netlink.AddrDel(bridge, &addr); err != nil {
			errMsg := fmt.Errorf("failed to delete address %q on OVS bridge %s: %v", addr.String(), brName, err)
			klog.Error(errMsg)
			return errMsg
		}
		klog.Infof("address %q has been deleted from link %s", addr.String(), brName)

		addr.Label = ""
		if err = netlink.AddrReplace(nic, &addr); err != nil {
			return fmt.Errorf("failed to replace address %q on nic %s: %v", addr.String(), nicName, err)
		}
		klog.Infof("address %q has been added/replaced to link %s", addr.String(), nicName)
	}

	if err = netlink.LinkSetUp(nic); err != nil {
		klog.Errorf("failed to set link %s up: %v", nicName, err)
		return err
	}

	scopeOrders := [...]netlink.Scope{
		netlink.SCOPE_HOST,
		netlink.SCOPE_LINK,
		netlink.SCOPE_SITE,
		netlink.SCOPE_UNIVERSE,
	}
	for _, scope := range scopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.IP.IsLinkLocalUnicast() {
				// skip 169.254.0.0/16 and fe80::/10
				continue
			}
			if route.Scope == scope {
				route.LinkIndex = nic.Attrs().Index
				if err = netlink.RouteReplace(&route); err != nil {
					return fmt.Errorf("failed to add/replace route %s: %v", route.String(), err)
				}
				klog.Infof("route %q has been added/replaced to link %s", route.String(), nicName)
			}
		}
	}

	if err = netlink.LinkSetDown(bridge); err != nil {
		return fmt.Errorf("failed to set OVS bridge %s down: %v", brName, err)
	}
	klog.V(3).Infof("link %s has been set down", brName)

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
		return "", "", fmt.Errorf("failed to create veth for %v", err)
	}
	return hostNicName, containerNicName, nil
}

// Setup sriov interface in the pod
// https://github.com/ovn-org/ovn-kubernetes/commit/6c96467d0d3e58cab05641293d1c1b75e5914795
func setupSriovInterface(containerID, deviceID, vfDriver, ifName string, mtu int, mac string) (string, string, error) {
	isVfioPciDriver := false
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

	// 7. set MAC address to VF
	if err = setVfMac(deviceID, vfIndex, mac); err != nil {
		return "", "", err
	}

	return hostNicName, vfNetdevice, nil
}

func setupYunsiliconInterface(containerID, deviceID, ifName string, mac string) (string, string, error) {
	var vfNetdevice string
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

	// 2. get VF index from PCI
	vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
	if err != nil {
		klog.Errorf("failed to get vf %s index, %v", deviceID, err)
		return "", "", err
	}

	// 3. rename the host VF representor
	hostNicName, _ := generateNicName(containerID, ifName)

	// 4. set MAC address to VF
	if err = setVfMac(deviceID, vfIndex, mac); err != nil {
		return "", "", err
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

func (csh cniServerHandler) configureNicWithInternalPort(podName, podNamespace, provider, netns, containerID, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss string, gwCheckMode int, u2oInterconnectionIP string) (string, error) {
	_, containerNicName := generateNicName(containerID, ifName)
	ipStr := util.GetIpWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID, containerNicName)

	// Add container iface to ovs port as internal port
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", containerNicName, "--",
		"set", "interface", containerNicName, "type=internal", "--",
		"set", "interface", containerNicName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName),
		fmt.Sprintf("external_ids:pod_name=%s", podName),
		fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
		fmt.Sprintf("external_ids:ip=%s", ipStr),
		fmt.Sprintf("external_ids:pod_netns=%s", netns))
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

	if err = ovs.SetNetemQos(podName, podNamespace, ifaceID, latency, limit, loss); err != nil {
		return containerNicName, err
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return containerNicName, fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, isDefaultRoute, detectIPConflict, routes, macAddr, podNS, mtu, nicType, gwCheckMode, u2oInterconnectionIP); err != nil {
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
		if ipAddr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}
		ipDelMap[ipAddr.IPNet.String()] = ipAddr
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
		if err = netlink.AddrDel(nodeLink, &addr); err != nil {
			return fmt.Errorf("delete address %s %v", addr, err)
		}
	}
	for _, addr := range ipAddMap {
		if err = netlink.AddrAdd(nodeLink, &addr); err != nil {
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
		return fmt.Errorf("failed to create static iface %v, err %v", ifName, err)
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
			continue
		}

		if !strings.Contains(strings.TrimSpace(string(physPortName)), "vf") {
			pfName = dev
			break
		}
	}
	if pfName == "" && len(netDevs) > 0 {
		pfName = netDevs[0]
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

func turnOffNicTxChecksum(nicName string) (err error) {
	start := time.Now()
	args := []string{"-K", nicName, "tx", "off"}
	output, err := exec.Command("ethtool", args...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", "ethtool", strings.Join(args, " "), elapsed)
	if err != nil {
		return fmt.Errorf("failed to turn off nic tx checksum, output %s, err %s", string(output), err.Error())
	}
	return nil
}

func getShortSharedDir(uid types.UID, volumeName string) string {
	return filepath.Join(util.DefaultHostVhostuserBaseDir, string(uid), volumeName)
}

func linkExists(name string) (bool, error) {
	if _, err := netlink.LinkByName(name); err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
