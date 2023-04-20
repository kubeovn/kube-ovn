package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Mellanox/sriovnet"
	sriovutilfs "github.com/Mellanox/sriovnet/pkg/utils/filesystem"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss, jitter string, gwCheckMode int, u2oInterconnectionIP string) error {
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
		return fmt.Errorf("add nic to ovs failed %v: %q", err, output)
	}

	// lsp and container nic must use same mac address, otherwise ovn will reject these packets by default
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

	if err = ovs.SetNetemQos(podName, podNamespace, ifaceID, latency, limit, loss, jitter); err != nil {
		return err
	}

	if containerNicName == "" {
		return nil
	}
	isUserspaceDP, err := ovs.IsUserspaceDataPath()
	if err != nil {
		return err
	}
	if isUserspaceDP {
		// turn off tx checksum
		if err = turnOffNicTxChecksum(containerNicName); err != nil {
			return err
		}
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	if err = configureContainerNic(containerNicName, ifName, ip, gateway, isDefaultRoute, detectIPConflict, routes, macAddr, podNS, mtu, nicType, gwCheckMode, u2oInterconnectionIP); err != nil {
		return err
	}
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
		return fmt.Errorf("can not find container nic %s: %v", nicName, err)
	}

	// Set link alias to its origin link name for fastpath to recognize and bypass netfilter
	if err := netlink.LinkSetAlias(containerLink, nicName); err != nil {
		klog.Errorf("failed to set link alias for container nic %s: %v", nicName, err)
		return err
	}

	if err = netlink.LinkSetNsFd(containerLink, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to move link to netns: %v", err)
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
			if err = configureNic(nicName, ipAddr, macAddr, mtu, detectIPConflict); err != nil {
				return err
			}
		} else {
			if err = configureNic(ifName, ipAddr, macAddr, mtu, detectIPConflict); err != nil {
				return err
			}
		}

		if isDefaultRoute {
			// Only eth0 requires the default route and gateway
			containerGw := gateway
			if u2oInterconnectionIP != "" {
				containerGw = u2oInterconnectionIP
			}

			for _, gw := range strings.Split(containerGw, ",") {
				if err = netlink.RouteReplace(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Gw:        net.ParseIP(gw),
				}); err != nil {
					return fmt.Errorf("failed to configure default gateway %s: %v", gw, err)
				}
			}
		}

		for _, r := range routes {
			var dst *net.IPNet
			if r.Destination != "" {
				if _, dst, err = net.ParseCIDR(r.Destination); err != nil {
					klog.Errorf("invalid route destination %s: %v", r.Destination, err)
					continue
				}
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
		// ignore error if disableGatewayCheck=true
		if err = waitNetworkReady(intr, ipAddr, gateway, underlayGateway, verbose, 1); err != nil {
			err = nil
		}
	} else {
		err = waitNetworkReady(intr, ipAddr, gateway, underlayGateway, verbose, gatewayCheckMaxRetry)
	}
	return err
}

func waitNetworkReady(nic, ipAddr, gateway string, underlayGateway, verbose bool, maxRetry int) error {
	ips := strings.Split(ipAddr, ",")
	for i, gw := range strings.Split(gateway, ",") {
		src := strings.Split(ips[i], "/")[0]
		if underlayGateway && util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
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
			if err := pingGateway(gw, src, verbose, maxRetry); err != nil {
				return err
			}
		}
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

	if err = configureNic(util.NodeNic, ip, macAddr, mtu, false); err != nil {
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
	klog.Infof("wait ovn0 gw ready")
	if err := waitNetworkReady(util.NodeNic, ip, gw, false, true, gatewayCheckMaxRetry); err != nil {
		klog.Errorf("failed to init ovn0 check: %v", err)
		return err
	}
	return nil
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
	if err := waitNetworkReady(util.NodeNic, ip, gw, false, false, 5); err != nil {
		util.LogFatalAndExit(err, "failed to ping ovn0 gateway %s", gw)
	}
}

func (c *Controller) checkNodeGwNicInNs(nodeExtIp, ip, gw string, gwNS ns.NetNS) error {
	exists, err := ovs.PortExists(util.NodeGwNic)
	if err != nil {
		klog.Error(err)
		return err
	}
	filters := labels.Set{util.OvnEipUsageLabel: util.LrpUsingEip, util.OvnLrpEipEnableBfdLabel: "true"}
	ovnEips, err := c.ovnEipsLister.List(labels.SelectorFromSet(filters))
	if err != nil {
		klog.Errorf("failed to list node external ovn eip, %v", err)
		return err
	}
	if len(ovnEips) == 0 {
		// no lrp eip, no need to check
		return nil
	}
	if exists {
		return ns.WithNetNSPath(gwNS.Path(), func(_ ns.NetNS) error {
			err = waitNetworkReady(util.NodeGwNic, ip, gw, true, true, 3)
			if err == nil {
				cmd := exec.Command("sh", "-c", "bfdd-control status")
				if err := cmd.Run(); err != nil {
					err := fmt.Errorf("failed to get bfdd status, %v", err)
					klog.Error(err)
					return err
				}
				for _, eip := range ovnEips {
					if eip.Status.Ready {
						cmd := exec.Command("sh", "-c", fmt.Sprintf("bfdd-control status remote %s local %s", eip.Spec.V4Ip, nodeExtIp))
						var outb bytes.Buffer
						cmd.Stdout = &outb
						if err := cmd.Run(); err == nil {
							out := string(outb.String())
							klog.V(3).Info(out)
							if strings.Contains(out, "No session") {
								// not exist
								cmd = exec.Command("sh", "-c", fmt.Sprintf("bfdd-control allow %s", eip.Spec.V4Ip))
								if err := cmd.Run(); err != nil {
									err := fmt.Errorf("failed to add lrp %s ip %s into bfd listening list, %v", eip.Name, eip.Status.V4Ip, err)
									klog.Error(err)
									return err
								}
							}
						} else {
							err := fmt.Errorf("faild to check bfd status remote %s local %s", eip.Spec.V4Ip, nodeExtIp)
							klog.Error(err)
							return err
						}

					}
				}
			}
			return err
		})
	} else {
		err := fmt.Errorf("node external gw not ready")
		klog.Error(err)
		return err
	}
}

func configureNodeGwNic(portName, ip, gw string, macAddr net.HardwareAddr, mtu int, gwNS ns.NetNS) error {
	ipStr := util.GetIpWithoutMask(ip)
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", util.NodeGwNic, "--",
		"set", "interface", util.NodeGwNic, "type=internal", "--",
		"set", "interface", util.NodeGwNic, fmt.Sprintf("external_ids:iface-id=%s", portName),
		fmt.Sprintf("external_ids:ip=%s", ipStr),
		fmt.Sprintf("external_ids:pod_netns=%s", util.NodeGwNsPath))
	if err != nil {
		klog.Errorf("failed to configure node external nic %s: %v, %q", portName, err, output)
		return fmt.Errorf(output)
	}
	gwLink, err := netlink.LinkByName(util.NodeGwNic)
	if err == nil {
		if err = netlink.LinkSetNsFd(gwLink, int(gwNS.Fd())); err != nil {
			klog.Errorf("failed to move link into netns: %v", err)
			return err
		}
	} else {
		klog.V(3).Infof("node external nic %q already in ns %s", util.NodeGwNic, util.NodeGwNsPath)
	}
	return ns.WithNetNSPath(gwNS.Path(), func(_ ns.NetNS) error {
		if util.CheckProtocol(ip) == kubeovnv1.ProtocolDual || util.CheckProtocol(ip) == kubeovnv1.ProtocolIPv6 {
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

		if err = configureNic(util.NodeGwNic, ip, macAddr, mtu, true); err != nil {
			klog.Errorf("failed to congigure node gw nic %s, %v", util.NodeGwNic, err)
			return err
		}

		if err = configureLoNic(); err != nil {
			klog.Errorf("failed to configure nic %s, %v", util.LoNic, err)
			return err
		}
		gwLink, err = netlink.LinkByName(util.NodeGwNic)
		if err != nil {
			klog.Errorf("failed to get link %q, %v", util.NodeGwNic, err)
			return err
		}
		switch util.CheckProtocol(ip) {
		case kubeovnv1.ProtocolIPv4:
			_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
			err = netlink.RouteReplace(&netlink.Route{
				LinkIndex: gwLink.Attrs().Index,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       defaultNet,
				Gw:        net.ParseIP(gw),
			})
		case kubeovnv1.ProtocolIPv6:
			_, defaultNet, _ := net.ParseCIDR("::/0")
			err = netlink.RouteReplace(&netlink.Route{
				LinkIndex: gwLink.Attrs().Index,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       defaultNet,
				Gw:        net.ParseIP(gw),
			})
		case kubeovnv1.ProtocolDual:
			gws := strings.Split(gw, ",")
			_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
			err = netlink.RouteReplace(&netlink.Route{
				LinkIndex: gwLink.Attrs().Index,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       defaultNet,
				Gw:        net.ParseIP(gws[0]),
			})
			if err != nil {
				return fmt.Errorf("config v4 gateway failed: %v", err)
			}

			_, defaultNet, _ = net.ParseCIDR("::/0")
			err = netlink.RouteReplace(&netlink.Route{
				LinkIndex: gwLink.Attrs().Index,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       defaultNet,
				Gw:        net.ParseIP(gws[1]),
			})
		}
		if err != nil {
			return fmt.Errorf("failed to configure gateway: %v", err)
		}
		cmd := exec.Command("sh", "-c", "/usr/local/bin/bfdd-beacon --listen=0.0.0.0")
		if err := cmd.Run(); err != nil {
			err := fmt.Errorf("failed to get start bfd listen, %v", err)
			klog.Error(err)
			return err
		}
		return waitNetworkReady(util.NodeGwNic, ip, gw, true, true, 3)
	})
}

func removeNodeGwNic() error {
	if _, err := ovs.Exec(ovs.IfExists, "del-port", "br-int", util.NodeGwNic); err != nil {
		return fmt.Errorf("failed to remove ecmp external port %s from OVS bridge %s: %v", "br-int", util.NodeGwNic, err)
	}
	klog.Infof("removed node external gw nic %q", util.NodeGwNic)
	return nil
}

func removeNodeGwNs() error {
	if err := DeleteNamedNs(util.NodeGwNs); err != nil {
		return fmt.Errorf("failed to remove node external gw ns %s: %v", util.NodeGwNs, err)
	}
	klog.Infof("node external gw ns %s removed", util.NodeGwNs)
	return nil
}

// If OVS restart, the ovnext0 port will down and prevent host to pod network,
// Restart the kube-ovn-cni when this happens
func (c *Controller) loopOvnExt0Check() {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return
	}

	portName := node.Name
	cachedEip, err := c.ovnEipsLister.Get(portName)
	needClean := false
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if val, ok := node.Labels[util.NodeExtGwLabel]; !ok || (val == "false") {
				// not gw node or already clean
				return
			}
			needClean = true
		} else {
			klog.Errorf("failed to get ecmp gateway ovn eip, %v", err)
			return
		}
	}

	if needClean {
		if err := removeNodeGwNic(); err != nil {
			klog.Error(err)
			return
		}
		if err := removeNodeGwNs(); err != nil {
			klog.Error(err)
			return
		}
		if err = c.patchNodeExternalGwLabel(node.Name, false); err != nil {
			klog.Errorf("failed to patch label on node %s, %v", node, err)
			return
		}
		return
	}

	if cachedEip.Status.V4Ip == "" {
		klog.Errorf("ecmp gateway ovn eip still has no ip")
		return
	}
	ips := util.GetStringIP(cachedEip.Status.V4Ip, cachedEip.Status.V6Ip)
	cachedSubnet, err := c.subnetsLister.Get(cachedEip.Spec.ExternalSubnet)
	if err != nil {
		klog.Errorf("failed to get external subnet %s, %v", cachedEip.Spec.ExternalSubnet, err)
		return
	}
	gw := cachedSubnet.Spec.Gateway
	mac, err := net.ParseMAC(cachedEip.Status.MacAddress)
	if err != nil {
		klog.Errorf("failed to parse mac %s, %v", cachedEip.Status.MacAddress, err)
		return
	}
	gwNS, err := ns.GetNS(util.NodeGwNsPath)
	if err != nil {
		// ns not exist, create node external gw ns
		cmd := exec.Command("sh", "-c", fmt.Sprintf("/usr/sbin/ip netns add %s", util.NodeGwNs))
		if err := cmd.Run(); err != nil {
			err := fmt.Errorf("failed to get create gw ns %s, %v", util.NodeGwNs, err)
			klog.Error(err)
			return
		}
		if gwNS, err = ns.GetNS(util.NodeGwNsPath); err != nil {
			err := fmt.Errorf("failed to get node gw ns %s, %v", util.NodeGwNs, err)
			klog.Error(err)
			return
		}
	}
	nodeExtIp := cachedEip.Spec.V4Ip
	ipAddr := util.GetIpAddrWithMask(ips, cachedSubnet.Spec.CIDRBlock)
	if err := c.checkNodeGwNicInNs(nodeExtIp, ipAddr, gw, gwNS); err == nil {
		// add all lrp ip in bfd listening list
		return
	}
	klog.Infof("setup nic ovnext0 ip %s, mac %v, mtu %d", ipAddr, mac, c.config.MTU)
	if err := configureNodeGwNic(portName, ipAddr, gw, mac, c.config.MTU, gwNS); err != nil {
		klog.Errorf("failed to setup ovnext0, %v", err)
		return
	}
	if err = c.patchNodeExternalGwLabel(portName, true); err != nil {
		klog.Errorf("failed to patch label on node %s, %v", node, err)
		return
	}
	if err = c.patchOvnEipStatus(portName, true); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", portName, err)
		return
	}
}

func (c *Controller) patchOvnEipStatus(key string, ready bool) error {
	cachedOvnEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		klog.Errorf("failed to get cached ovn eip '%s', %v", key, err)
		return err
	}
	ovnEip := cachedOvnEip.DeepCopy()
	changed := false
	if ovnEip.Status.Ready != ready {
		ovnEip.Status.Ready = ready
		changed = true
	}
	if changed {
		bytes, err := ovnEip.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal ovn eip status '%s', %v", key, err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), ovnEip.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch status for ovn eip '%s', %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchNodeExternalGwLabel(key string, enabled bool) error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return err
	}

	if enabled {
		node.Labels[util.NodeExtGwLabel] = "true"
	} else {
		node.Labels[util.NodeExtGwLabel] = "false"
	}

	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	op := "replace"
	if len(node.Labels) == 0 {
		op = "add"
	}

	raw, _ := json.Marshal(node.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	if _, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), key, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}
	return nil
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

func configureNic(link, ip string, macAddr net.HardwareAddr, mtu int, detectIPConflict bool) error {
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

	return nil
}

func configureLoNic() error {
	loLink, err := netlink.LinkByName(util.LoNic)
	if err != nil {
		err := fmt.Errorf("can not find nic %s, %v", util.LoNic, err)
		klog.Error(err)
		return err
	}

	if loLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(loLink); err != nil {
			err := fmt.Errorf("failed to set up nic %s, %v", util.LoNic, err)
			klog.Error(err)
			return err
		}
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

	// set link unmanaged by NetworkManager
	if err = nmSetManaged(nicName, false); err != nil {
		klog.Errorf("failed set device %s unmanaged by NetworkManager: %v", nicName, err)
		return 0, err
	}

	for _, addr := range addrs {
		if addr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}

		if err = netlink.AddrDel(nic, &addr); err != nil {
			errMsg := fmt.Errorf("failed to delete address %q on nic %s: %v", addr.String(), nicName, err)
			klog.Error(errMsg)
			return 0, errMsg
		}
		klog.Infof("address %q has been removed from link %s", addr.String(), nicName)

		addr.Label = ""
		if err = netlink.AddrReplace(bridge, &addr); err != nil {
			return 0, fmt.Errorf("failed to replace address %q on OVS bridge %s: %v", addr.String(), brName, err)
		}
		klog.Infof("address %q has been added/replaced to link %s", addr.String(), brName)
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
					return 0, fmt.Errorf("failed to add/replace route %s: %v", route.String(), err)
				}
				klog.Infof("route %q has been added/replaced to link %s", route.String(), brName)
			}
		}
	}

	if _, err = ovs.Exec(ovs.MayExist, "add-port", brName, nicName,
		"--", "set", "port", nicName, "external_ids:vendor="+util.CniTypeName); err != nil {
		return 0, fmt.Errorf("failed to add %s to OVS bridge %s: %v", nicName, brName, err)
	}
	klog.V(3).Infof("ovs port %s has been added to bridge %s", nicName, brName)

	if err = netlink.LinkSetUp(nic); err != nil {
		return 0, fmt.Errorf("failed to set link %s up: %v", nicName, err)
	}

	return nic.Attrs().MTU, nil
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
func removeProviderNic(nicName, brName string) error {
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
		klog.Error("failed to set link %s up: %v", nicName, err)
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

	// 7. set MAC address to VF
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

func (csh cniServerHandler) configureNicWithInternalPort(podName, podNamespace, provider, netns, containerID, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss, jitter string, gwCheckMode int, u2oInterconnectionIP string) (string, error) {
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

	if err = ovs.SetNetemQos(podName, podNamespace, ifaceID, latency, limit, loss, jitter); err != nil {
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
