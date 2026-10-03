package cni

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/k8snetworkplumbingwg/sriovnet"
	sriovutilfs "github.com/k8snetworkplumbingwg/sriovnet/pkg/utils/filesystem"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	goping "github.com/prometheus-community/pro-bing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/net/yusur"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type (
	executionConfig  struct{ EnableArpDetectIPConflict bool }
	executionHandler struct {
		Config    *executionConfig
		ovsClient *ovs.VswitchClient
	}
)

var (
	getNetNSForReleaseVf        = ns.GetNS
	getCurrentNetNSForReleaseVf = ns.GetCurrentNS
)

var pciAddrRegexp = regexp.MustCompile(`\b([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}.\d{1}\S*)`)

const (
	gatewayCheckModeDisabled = iota
	gatewayCheckModePing
	gatewayCheckModeArping
	gatewayCheckModePingNotConcerned
	gatewayCheckModeArpingNotConcerned
)
const gatewayCheckMaxRetry = 200

func (csh executionHandler) checkGatewayReady(_, _ string, gwCheckMode int, intr, ipAddr, gateway string, verbose bool) error {
	if gwCheckMode == gatewayCheckModeArpingNotConcerned || gwCheckMode == gatewayCheckModePingNotConcerned {
		_ = waitNetworkReady(intr, ipAddr, gateway, true, verbose, 1, nil)
		return nil
	}
	return waitNetworkReady(intr, ipAddr, gateway, gwCheckMode == gatewayCheckModeArping, verbose, gatewayCheckMaxRetry, nil)
}

func (csh executionHandler) configureContainerNic(podName, podNamespace, nicName, ifName, ipAddr, gateway string, isDefaultRoute, vmMigration bool, routes []request.Route, macAddr net.HardwareAddr, netns ns.NetNS, mtu, gwCheckMode int, u2oInterconnectionIP string, routedSubnet bool) ([]request.Route, error) {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return nil, fmt.Errorf("can not find container nic %s: %w", nicName, err)
	}

	if err := netlink.LinkSetAlias(containerLink, nicName); err != nil {
		klog.Errorf("failed to set link alias for container nic %s: %v", nicName, err)
		return nil, err
	}

	fd := int(netns.Fd())
	if err = netlink.LinkSetNsFd(containerLink, fd); err != nil {
		return nil, fmt.Errorf("failed to move link to netns: %w", err)
	}

	ipv6DAD := !vmMigration
	detectIPv4Conflict := !vmMigration && csh.Config.EnableArpDetectIPConflict
	var finalRoutes []request.Route
	err = ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		if err = netlink.LinkSetName(containerLink, ifName); err != nil {
			klog.Error(err)
			return err
		}

		if err = csh.configureLink(ifName, ipAddr, macAddr, mtu, detectIPv4Conflict, ipv6DAD, true, false); err != nil {
			klog.Error(err)
			return err
		}

		if ipAddr == "" {
			klog.Infof("configured MAC-only interface %s for pod %s/%s, skipping IP routes and gateway checks", ifName, podNamespace, podName)
			return nil
		}

		containerGw := gateway
		if u2oInterconnectionIP != "" {
			containerGw = u2oInterconnectionIP
		}

		if routedSubnet {
			if err = util.ValidateRoutedAnnotationRoutes(routes, containerGw); err != nil {
				return err
			}
			onLinkRoutes, routeErr := util.GatewayOnLinkRoutes(containerGw, containerLink.Attrs().Index)
			if routeErr != nil {
				return routeErr
			}
			for _, route := range onLinkRoutes {
				if err = netlink.RouteReplace(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_LINK,
					Dst:       route.Dst,
				}); err != nil {
					return fmt.Errorf("failed to configure on-link route to gateway %s: %w", route.Gateway, err)
				}
			}
		}

		if isDefaultRoute {
			for _, gw := range util.SplitTrimmed(containerGw, ",") {
				if err = netlink.RouteReplace(&netlink.Route{
					LinkIndex: containerLink.Attrs().Index,
					Scope:     netlink.SCOPE_UNIVERSE,
					Gw:        net.ParseIP(gw),
				}); err != nil {
					return fmt.Errorf("failed to configure default gateway %s: %w", gw, err)
				}
			}
		}

		for _, r := range routes {
			var dst *net.IPNet
			if r.Destination != "" {
				if _, dst, err = net.ParseCIDR(r.Destination); err != nil {
					return fmt.Errorf("invalid route destination %s: %w", r.Destination, err)
				}
			}

			var gw net.IP
			if r.Gateway != "" {
				if gw = net.ParseIP(r.Gateway); gw == nil {
					return fmt.Errorf("invalid route gateway %s", r.Gateway)
				}
			}

			route := &netlink.Route{
				Dst:       dst,
				Gw:        gw,
				LinkIndex: containerLink.Attrs().Index,
			}
			if err = netlink.RouteReplace(route); err != nil {
				return fmt.Errorf("failed to add route %+v: %w", r, err)
			}
		}

		linkRoutes, err := netlink.RouteList(containerLink, netlink.FAMILY_ALL)
		if err != nil {
			return fmt.Errorf("failed to get routes on interface %s: %w", ifName, err)
		}

		for _, r := range linkRoutes {
			if r.Family != netlink.FAMILY_V4 && r.Family != netlink.FAMILY_V6 {
				continue
			}
			if r.Dst == nil && r.Gw == nil {
				continue
			}
			if r.Dst != nil && r.Dst.IP.IsLinkLocalUnicast() {
				if _, bits := r.Dst.Mask.Size(); bits == net.IPv6len*8 {
					continue
				}
			}

			var route request.Route
			if r.Dst != nil {
				route.Destination = r.Dst.String()
			}
			if r.Gw != nil {
				route.Gateway = r.Gw.String()
			}
			finalRoutes = append(finalRoutes, route)
		}

		if gwCheckMode != gatewayCheckModeDisabled {
			if util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolIPv6 || util.CheckProtocol(ipAddr) == kubeovnv1.ProtocolDual {
				addrsFlags, err := waitIPv6AddressPreferred(ifName, 10, 500*time.Millisecond, ipv6DAD)
				if err != nil {
					klog.Error(err)
					return err
				}
				for addr, flags := range addrsFlags {
					if flags&unix.IFA_F_DADFAILED == 0 {
						klog.Errorf("address %s on interface %s is not ready, flags: 0x%x", addr, ifName, flags)
						continue
					}
					klog.Errorf("IPv6 DAD of address %s on interface %s failed, flags: 0x%x", addr, ifName, flags)
					available, mac, err := util.DuplicateAddressDetection(ifName, addr)
					if err != nil {
						klog.Errorf("failed to perform IPv6 DAD for address %s on interface %s: %v", addr, ifName, err)
						return err
					}
					if !available && mac != nil {
						return fmt.Errorf("IP address %s has already been used by host with MAC %s", addr, mac)
					}
				}
				if len(addrsFlags) != 0 {
					return fmt.Errorf("ip address(es) %s on interface %s are not in preferred state", strings.Join(slices.Collect(maps.Keys(addrsFlags)), ","), ifName)
				}
			}

			if u2oInterconnectionIP != "" {
				if err = csh.checkGatewayReady(podName, podNamespace, gwCheckMode, ifName, ipAddr, u2oInterconnectionIP, true); err != nil {
					klog.Error(err)
					return err
				}
			}
			if err = csh.checkGatewayReady(podName, podNamespace, gwCheckMode, ifName, ipAddr, gateway, true); err != nil {
				klog.Error(err)
				return err
			}
		}

		return nil
	})

	return finalRoutes, err
}

func (csh executionHandler) configureDpdkNic(podName, podNamespace, provider, netns, containerID, ifName, _ string, _ int, ip, _, ingress, egress, ingressBurst, egressBurst, shortSharedDir, socketName, socketConsumption string) error {
	sharedDir := filepath.Join("/var", shortSharedDir)
	hostNicName, _ := generateNicName(containerID, ifName)

	ipStr := util.GetIPWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	if err := csh.ovsClient.CleanDuplicateCNIPort(ifaceID, hostNicName); err != nil {
		return err
	}

	vhostServerPath := path.Join(sharedDir, socketName)
	if socketConsumption == util.ConsumptionKubevirt {
		vhostServerPath = path.Join(sharedDir, ifName)
	}

	if err := csh.ovsClient.AddCNIPort(&vswitch.Interface{
		Name: hostNicName, Type: "dpdkvhostuserclient", Options: map[string]string{"vhost-server-path": vhostServerPath},
		ExternalIDs: map[string]string{"iface-id": ifaceID, "pod_name": podName, "pod_namespace": podNamespace, "ip": ipStr, "pod_netns": netns},
	}); err != nil {
		return fmt.Errorf("add DPDK nic to OVS: %w", err)
	}
	return csh.ovsClient.SetCNIBandwidth(podName, podNamespace, ifaceID, egress, ingress, egressBurst, ingressBurst)
}

func configureHostNic(nicName string) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find host nic %s: %w", nicName, err)
	}

	if hostLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(hostLink); err != nil {
			return fmt.Errorf("can not set host nic %s up: %w", nicName, err)
		}
	}
	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen: %w", nicName, err)
	}

	return nil
}

func (csh executionHandler) configureLink(link, ip string, macAddr net.HardwareAddr, mtu int, detectIPv4Conflict, ipv6DAD, setUfoOff, ipv6LinkLocalOn bool) error {
	nodeLink, err := netlink.LinkByName(link)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("can not find nic %s: %w", link, err)
	}

	if err = netlink.LinkSetHardwareAddr(nodeLink, macAddr); err != nil {
		klog.Error(err)
		return fmt.Errorf("can not set mac address to nic %s: %w", link, err)
	}

	if mtu > 0 {
		if nodeLink.Type() == "openvswitch" {
			err = csh.ovsClient.SetCNIInterfaceMTU(link, mtu)
		} else {
			err = netlink.LinkSetMTU(nodeLink, mtu)
		}
		if err != nil {
			return fmt.Errorf("failed to set nic %s mtu: %w", link, err)
		}
	}

	if nodeLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(nodeLink); err != nil {
			klog.Error(err)
			return fmt.Errorf("can not set node nic %s up: %w", link, err)
		}
	}

	ipDelMap := make(map[string]netlink.Addr)
	ipAddMap := make(map[string]netlink.Addr)
	ipAddrs, err := util.AddrList(nodeLink, unix.AF_UNSPEC)
	if err != nil {
		err = fmt.Errorf("failed to list addresses on link %s: %w", link, err)
		klog.Error(err)
		return err
	}

	isIPv6LinkLocalExist := false
	for _, ipAddr := range ipAddrs {
		if ipAddr.IP.IsLinkLocalUnicast() {
			if util.CheckProtocol(ipAddr.IP.String()) == kubeovnv1.ProtocolIPv6 {
				isIPv6LinkLocalExist = true
			}
			continue
		}
		ipDelMap[ipAddr.IPNet.String()] = ipAddr
	}

	if ip != "" {
		if ipv6LinkLocalOn && !isIPv6LinkLocalExist && (util.CheckProtocol(ip) == kubeovnv1.ProtocolIPv6 || util.CheckProtocol(ip) == kubeovnv1.ProtocolDual) {
			linkLocal, err := macToLinkLocalIPv6(macAddr)
			if err != nil {
				return fmt.Errorf("failed to generate link-local address: %w", err)
			}
			ipAddMap[linkLocal.String()] = netlink.Addr{
				IPNet: &net.IPNet{
					IP:   linkLocal,
					Mask: net.CIDRMask(64, 128),
				},
			}
		}

		for ipStr := range strings.SplitSeq(ip, ",") {
			if _, ok := ipDelMap[ipStr]; ok {
				delete(ipDelMap, ipStr)
				continue
			}

			ipAddr, err := netlink.ParseAddr(ipStr)
			if err != nil {
				return fmt.Errorf("can not parse address %s: %w", ipStr, err)
			}
			ipAddMap[ipStr] = *ipAddr
		}
	}

	for ip, addr := range ipDelMap {
		klog.Infof("delete ip address %s on %s", ip, link)
		if err = netlink.AddrDel(nodeLink, &addr); err != nil {
			klog.Error(err)
			return fmt.Errorf("delete address %s: %w", addr, err)
		}
	}
	for ip, addr := range ipAddMap {
		if addr.IP.To4() != nil {
			if detectIPv4Conflict {
				ip := addr.IP.String()
				mac, err := util.ArpDetectIPConflict(link, ip, macAddr)
				if err != nil {
					err = fmt.Errorf("failed to detect address conflict for %s on link %s: %w", ip, link, err)
					klog.Error(err)
					return err
				}
				if mac != nil {
					return fmt.Errorf("IP address %s has already been used by host with MAC %s", ip, mac)
				}
			} else {
				if err := util.AnnounceArpAddress(link, addr.IP.String(), macAddr, 1, 1*time.Second); err != nil {
					klog.Warningf("failed to broadcast free arp with err %v", err)
				}
			}
		} else if !ipv6DAD {
			addr.Flags |= unix.IFA_F_NODAD
		}

		klog.Infof("add ip address %s to %s", ip, link)
		if err = netlink.AddrAdd(nodeLink, &addr); err != nil {
			klog.Error(err)
			return fmt.Errorf("can not add address %s to nic %s: %w", addr, link, err)
		}
		if addr.IP.To4() == nil && !addr.IP.IsLinkLocalUnicast() {
			if err := util.AnnounceNDPAddress(link, addr.IP.String(), macAddr, 1, time.Second); err != nil {
				klog.Warningf("failed to send unsolicited neighbor advertisement: %v", err)
			}
		}
	}

	if setUfoOff {
		if err := disableUFO(link); err != nil {
			return err
		}
	}

	return nil
}

func (csh executionHandler) configureNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, vmMigration bool, routes []request.Route, _, _ []string, ingress, egress, ingressBurst, egressBurst, deviceID, latency, limit, loss, jitter string, gwCheckMode int, u2oInterconnectionIP, _, encapIP, localnetSubnet string, appendIfName, routedSubnet bool, execution *request.CNIExecutionResult) ([]request.Route, error) {
	var err error
	var hostNicName, containerNicName, pfPci string
	var vfID int
	if deviceID == "" {
		hostNicName, containerNicName, err = setupVethPair(containerID, ifName, mtu)
		if err != nil {
			klog.Errorf("failed to create veth pair %v", err)
			return nil, err
		}
		defer func() {
			if err != nil {
				if err := rollBackVethPair(hostNicName); err != nil {
					klog.Errorf("failed to rollback veth pair %s, %v", hostNicName, err)
					return
				}
			}
		}()
	} else {
		hostNicName, containerNicName, pfPci, vfID, err = setupSriovInterface(containerID, deviceID, vfDriver, ifName, mac)
		if err != nil {
			klog.Errorf("failed to create sriov interfaces %v", err)
			return nil, err
		}
		defer func() {
			if err != nil {
				if link, linkErr := netlink.LinkByName(hostNicName); linkErr == nil {
					if linkErr = netlink.LinkSetUp(link); linkErr != nil {
						klog.Errorf("failed to bring %s back up during rollback: %v", hostNicName, linkErr)
					}
				}
			}
		}()
	}
	if execution != nil {
		execution.HostNicName = hostNicName
		execution.ContainerNicName = containerNicName
	}

	ipStr := util.GetIPWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)

	if appendIfName {
		ifaceID = fmt.Sprintf("%s.%s", ifaceID, ifName)
	}
	if err = csh.ovsClient.CleanDuplicateCNIPort(ifaceID, hostNicName); err != nil {
		return nil, err
	}
	iface := &vswitch.Interface{Name: hostNicName, ExternalIDs: map[string]string{
		"iface-id": ifaceID, "vendor": util.CniTypeName, "pod_name": podName, "pod_namespace": podNamespace, "pod_netns": netns,
	}}
	if ip != "" {
		iface.ExternalIDs["ip"] = ipStr
	}
	if encapIP != "" {
		iface.ExternalIDs["encap-ip"] = encapIP
	}
	if yusur.IsYusurSmartNic(deviceID) {
		iface.Type = "dpdk"
		iface.Options = map[string]string{"dpdk-devargs": fmt.Sprintf("%s,representor=[%d]", pfPci, vfID)}
		iface.MTURequest = new(mtu)
	}
	if err = csh.ovsClient.AddCNIPort(iface); err != nil {
		return nil, fmt.Errorf("add nic to OVS: %w", err)
	}
	defer func() {
		if err != nil {
			if err := csh.rollbackOvsPort(hostNicName); err != nil {
				klog.Errorf("failed to rollback ovs port %s, %v", hostNicName, err)
				return
			}
		}
	}()

	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mac %q: %w", mac, err)
	}

	if deviceID != "" && !yusur.IsYusurSmartNic(deviceID) {
		link, linkErr := netlink.LinkByName(hostNicName)
		if linkErr != nil {
			return nil, fmt.Errorf("failed to get host link %s: %w", hostNicName, linkErr)
		}
		if linkErr = netlink.LinkSetMTU(link, mtu); linkErr != nil {
			return nil, fmt.Errorf("failed to set MTU on %s: %w", hostNicName, linkErr)
		}
	}
	if !yusur.IsYusurSmartNic(deviceID) {
		if err = configureHostNic(hostNicName); err != nil {
			klog.Error(err)
			return nil, err
		}
	}
	if err = csh.ovsClient.SetCNIBandwidth(podName, podNamespace, ifaceID, egress, ingress, egressBurst, ingressBurst); err != nil {
		klog.Error(err)
		return nil, err
	}

	if err = csh.ovsClient.SetCNINetem(podName, podNamespace, ifaceID, latency, limit, loss, jitter); err != nil {
		klog.Error(err)
		return nil, err
	}

	if containerNicName == "" {
		return nil, nil
	}
	isUserspaceDP, err := csh.ovsClient.CNIUserspaceDataPath()
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	if isUserspaceDP {
		if err = TurnOffNicTxChecksum(containerNicName); err != nil {
			klog.Error(err)
			return nil, err
		}
	}

	// wait for the ovs interface to be ready
	var ready bool
	ch := make(chan struct{}, 1)
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)
	wait.Until(func() {
		if time.Now().After(deadline) {
			ch <- struct{}{}
			return
		}
		iface, err := csh.ovsClient.CNIInterface(hostNicName)
		if err != nil {
			klog.Errorf("failed to read OVS interface %s: %v", hostNicName, err)
			return
		}
		if iface != nil && iface.ExternalIDs["ovn-installed"] == "true" {
			klog.Infof("ovs interface %s is ready", hostNicName)
			ch <- struct{}{}
			ready = true
		}
	}, 500*time.Millisecond, ch)
	if !ready {
		err = fmt.Errorf("ovs interface %s is not ready after %s", hostNicName, timeout.String())
		klog.Error(err)
		return nil, err
	}

	if localnetSubnet != "" {
		if err := csh.waitForLocalnetPatchPort(localnetSubnet); err != nil {
			klog.Error(err)
			return nil, err
		}
	}

	podNS, err := ns.GetNS(netns)
	if err != nil {
		err = fmt.Errorf("failed to open netns %q: %w", netns, err)
		klog.Error(err)
		return nil, err
	}
	defer podNS.Close()
	finalRoutes, err := csh.configureContainerNic(podName, podNamespace, containerNicName, ifName, ip, gateway, isDefaultRoute, vmMigration, routes, macAddr, podNS, mtu, gwCheckMode, u2oInterconnectionIP, routedSubnet)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return finalRoutes, nil
}

func deleteDefaultRoutes(family int) error {
	var defaultDst *net.IPNet
	switch family {
	case netlink.FAMILY_V4:
		defaultDst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	case netlink.FAMILY_V6:
		defaultDst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	}
	routes, err := netlink.RouteListFiltered(family, &netlink.Route{Dst: defaultDst}, netlink.RT_FILTER_DST)
	if err != nil {
		return fmt.Errorf("failed to get default routes: %w", err)
	}
	for _, r := range routes {
		klog.Infof("deleting default route %+v", r)
		if err = netlink.RouteDel(&r); err != nil {
			return fmt.Errorf("failed to delete route %+v: %w", r, err)
		}
	}
	return nil
}

func (csh executionHandler) deleteNic(podName, podNamespace, containerID, netns, deviceID, ifName, nicType string) error {
	if err := csh.releaseVf(podName, podNamespace, netns, ifName, nicType, deviceID); err != nil {
		return fmt.Errorf("failed to release VF %s assigned to the Pod %s/%s back to the host network namespace: "+
			"%w", ifName, podName, podNamespace, err)
	}

	var nicName string
	if yusur.IsYusurSmartNic(deviceID) {
		pfPci, err := yusur.GetYusurNicPfPciFromVfPci(deviceID)
		if err != nil {
			return fmt.Errorf("failed to get pf pci %w, %s", err, deviceID)
		}

		pfIndex, err := yusur.GetYusurNicPfIndexByPciAddress(pfPci)
		if err != nil {
			return fmt.Errorf("failed to get pf index %w, %s", err, deviceID)
		}

		vfIndex, err := yusur.GetYusurNicVfIndexByPciAddress(deviceID)
		if err != nil {
			return fmt.Errorf("failed to get vf index %w, %s", err, deviceID)
		}

		nicName = yusur.GetYusurNicVfRepresentor(pfIndex, vfIndex)
	} else {
		hostNicName, _ := generateNicName(containerID, ifName)
		nicName = hostNicName
	}

	if deviceID != "" {
		if link, linkErr := netlink.LinkByName(nicName); linkErr == nil {
			if linkErr = netlink.LinkSetDown(link); linkErr != nil {
				klog.Warningf("failed to set link %s down: %v", nicName, linkErr)
			}
		}
	}

	if err := csh.ovsClient.DeleteCNIPort(nicName); err != nil {
		return fmt.Errorf("delete OVS port: %w", err)
	}
	if err := csh.ovsClient.ClearCNIQoS(podName, podNamespace, ""); err != nil {
		return err
	}

	if deviceID == "" {
		hostLink, err := netlink.LinkByName(nicName)
		if err != nil {
			if _, ok := err.(netlink.LinkNotFoundError); ok {
				return nil
			}
			return fmt.Errorf("find host link %s failed %w", nicName, err)
		}

		hostLinkType := hostLink.Type()

		if hostLinkType == "veth" {
			if err = netlink.LinkDel(hostLink); err != nil {
				return fmt.Errorf("delete host link %s failed %w", hostLink, err)
			}
		}
	} else if pciAddrRegexp.MatchString(deviceID) && !yusur.IsYusurSmartNic(deviceID) {
		vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
		if err != nil {
			klog.Errorf("failed to get vf %s index, %v", deviceID, err)
			return err
		}
		if err = setVfMac(deviceID, vfIndex, "00:00:00:00:00:00"); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func generateNicName(containerID, ifname string) (string, string) {
	if ifname == "eth0" {
		return containerID[0:12] + "_h", containerID[0:12] + "_c"
	}

	if strings.HasPrefix(ifname, "pod") && len(ifname) == 14 {
		ifname = ifname[3 : len(ifname)-4]
		return fmt.Sprintf("%s_%s_h", containerID[0:12-len(ifname)], ifname), fmt.Sprintf("%s_%s_c", containerID[0:12-len(ifname)], ifname)
	}
	return fmt.Sprintf("%s_%s_h", containerID[0:12-len(ifname)], ifname), fmt.Sprintf("%s_%s_c", containerID[0:12-len(ifname)], ifname)
}

// Convert MAC address to EUI-64 and generate link-local IPv6 address
func macToLinkLocalIPv6(mac net.HardwareAddr) (net.IP, error) {
	if len(mac) != 6 {
		return nil, errors.New("invalid MAC address length")
	}

	eui64 := make([]byte, 8)
	copy(eui64[0:3], mac[0:3])
	eui64[3] = 0xff
	eui64[4] = 0xfe
	copy(eui64[5:], mac[3:])

	eui64[0] ^= 0x02

	linkLocalIPv6 := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	copy(linkLocalIPv6[8:], eui64)

	return linkLocalIPv6, nil
}

func (csh executionHandler) releaseVf(podName, podNamespace, podNetns, ifName, nicType, deviceID string) error {
	if nicType != util.OffloadType || deviceID == "" {
		return nil
	}
	podDesc := fmt.Sprintf("for pod %s/%s", podNamespace, podName)
	klog.Infof("Tear down interface %s", podDesc)
	if podNetns == "" {
		klog.Infof("skip releasing VF interface %s: pod network namespace is empty", podDesc)
		return nil
	}
	netns, err := getNetNSForReleaseVf(podNetns)
	if err != nil {
		var nsPathNotExistErr ns.NSPathNotExistErr
		if os.IsNotExist(err) || errors.As(err, &nsPathNotExistErr) {
			klog.Infof("skip releasing VF interface %s: pod network namespace %q no longer exists", podDesc, podNetns)
			return nil
		}
		return fmt.Errorf("failed to get container namespace %s: %w", podDesc, err)
	}
	defer netns.Close()

	hostNS, err := getCurrentNetNSForReleaseVf()
	if err != nil {
		return fmt.Errorf("failed to get host namespace %s: %w", podDesc, err)
	}
	defer hostNS.Close()

	err = netns.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to get container interface %s %s: %w", ifName, podDesc, err)
		}
		if err = netlink.LinkSetDown(link); err != nil {
			return fmt.Errorf("failed to bring down container interface %s %s: %w", ifName, podDesc, err)
		}

		vfName := link.Attrs().Alias
		if err = netlink.LinkSetName(link, vfName); err != nil {
			return fmt.Errorf("failed to rename container interface %s to %s %s: %w",
				ifName, vfName, podDesc, err)
		}

		fd := int(netns.Fd())
		if err = netlink.LinkSetNsFd(link, fd); err != nil {
			return fmt.Errorf("failed to move container interface %s back to host namespace %s: %w",
				ifName, podDesc, err)
		}
		return nil
	})
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to release VF interface %s %s: %w", ifName, podDesc, err)
	}

	return nil
}

func (csh executionHandler) removeDefaultRoute(netns string, ipv4, ipv6 bool) error {
	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %w", netns, err)
	}
	defer podNS.Close()

	return ns.WithNetNSPath(podNS.Path(), func(_ ns.NetNS) error {
		if ipv4 {
			if err := deleteDefaultRoutes(netlink.FAMILY_V4); err != nil {
				return err
			}
		}
		if ipv6 {
			if err := deleteDefaultRoutes(netlink.FAMILY_V6); err != nil {
				return err
			}
		}
		return nil
	})
}

func renameLink(curName, newName string) error {
	link, err := netlink.LinkByName(curName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := netlink.LinkSetDown(link); err != nil {
		klog.Error(err)
		return err
	}
	if err := netlink.LinkSetName(link, newName); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func rollBackVethPair(nicName string) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		klog.Error(err)
		return fmt.Errorf("find host link %s failed %w", nicName, err)
	}

	hostLinkType := hostLink.Type()

	if hostLinkType == "veth" {
		if err = netlink.LinkDel(hostLink); err != nil {
			klog.Error(err)
			return fmt.Errorf("delete host link %s failed %w", hostLink, err)
		}
	}
	klog.Infof("rollback veth success %s", nicName)
	return nil
}

func (csh executionHandler) rollbackOvsPort(hostNicName string) error {
	return csh.ovsClient.DeleteCNIPort(hostNicName)
}

func setVfMac(deviceID string, vfIndex int, mac string) error {
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %w", macAddr, err)
	}

	pfPci, err := sriovnet.GetPfPciFromVfPci(deviceID)
	if err != nil {
		return fmt.Errorf("failed to get pf of device %s %w", deviceID, err)
	}

	netDevs, err := sriovnet.GetNetDevicesFromPci(pfPci)
	if err != nil {
		return fmt.Errorf("failed to get pf of device %s %w", deviceID, err)
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
		return fmt.Errorf("failed to lookup pf %s: %w", pfName, err)
	}
	if err := netlink.LinkSetVfHardwareAddr(pfLink, vfIndex, macAddr); err != nil {
		return fmt.Errorf("can not set mac address to vf nic:%s vf:%d %w", pfName, vfIndex, err)
	}
	return nil
}

// Setup sriov interface in the pod
// https://github.com/ovn-org/ovn-kubernetes/commit/6c96467d0d3e58cab05641293d1c1b75e5914795
func setupSriovInterface(containerID, deviceID, vfDriver, ifName, mac string) (string, string, string, int, error) {
	isVfioPciDriver := false
	if vfDriver == "vfio-pci" {
		matches, err := filepath.Glob(filepath.Join(util.VfioSysDir, "*"))
		if err != nil {
			return "", "", "", -1, fmt.Errorf("failed to check %s 'vfio-pci' driver path, %w", deviceID, err)
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
			return "", "", "", -1, fmt.Errorf("driver of device %s is not 'vfio-pci'", deviceID)
		}
	}

	var vfNetdevice string
	if !isVfioPciDriver {
		vfNetdevices, err := sriovnet.GetNetDevicesFromPci(deviceID)
		if err != nil {
			klog.Errorf("failed to get vf netdevice %s, %v", deviceID, err)
			return "", "", "", -1, err
		}

		if len(vfNetdevices) != 1 {
			return "", "", "", -1, fmt.Errorf("failed to get one netdevice interface per %s", deviceID)
		}
		vfNetdevice = vfNetdevices[0]
	}

	if yusur.IsYusurSmartNic(deviceID) {
		pfPci, err := yusur.GetYusurNicPfPciFromVfPci(deviceID)
		if err != nil {
			return "", "", "", -1, err
		}

		pfIndex, err := yusur.GetYusurNicPfIndexByPciAddress(pfPci)
		if err != nil {
			klog.Errorf("failed to get up %s link device, %v", deviceID, err)
			return "", "", "", -1, err
		}

		vfIndex, err := yusur.GetYusurNicVfIndexByPciAddress(deviceID)
		if err != nil {
			return "", "", "", -1, err
		}

		rep := yusur.GetYusurNicVfRepresentor(pfIndex, vfIndex)

		_, err = netlink.LinkByName(rep)
		if err != nil {
			klog.Infof("vfr not exist %s", rep)
		}

		return rep, vfNetdevice, pfPci, vfIndex, nil
	}

	uplink, err := sriovnet.GetUplinkRepresentor(deviceID)
	if err != nil {
		klog.Errorf("failed to get up %s link device, %v", deviceID, err)
		return "", "", "", -1, err
	}

	vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
	if err != nil {
		klog.Errorf("failed to get vf %s index, %v", deviceID, err)
		return "", "", "", -1, err
	}

	rep, err := sriovnet.GetVfRepresentor(uplink, vfIndex)
	if err != nil {
		klog.Errorf("failed to get vf %d representor, %v", vfIndex, err)
		return "", "", "", -1, err
	}
	oldHostRepName := rep

	hostNicName, _ := generateNicName(containerID, ifName)
	if err = renameLink(oldHostRepName, hostNicName); err != nil {
		return "", "", "", -1, fmt.Errorf("failed to rename %s to %s: %w", oldHostRepName, hostNicName, err)
	}

	if err = setVfMac(deviceID, vfIndex, mac); err != nil {
		return "", "", "", -1, err
	}

	return hostNicName, vfNetdevice, "", -1, nil
}

func setupVethPair(containerID, ifName string, mtu int) (string, string, error) {
	var err error
	hostNicName, containerNicName := generateNicName(containerID, ifName)

	veth := netlink.Veth{Name: hostNicName, PeerName: containerNicName}
	if mtu > 0 {
		veth.MTU = mtu
	}
	if err = netlink.LinkAdd(&veth); err != nil {
		if err := netlink.LinkDel(&veth); err != nil {
			klog.Errorf("failed to delete veth %v", err)
			return "", "", err
		}
		return "", "", fmt.Errorf("failed to create veth for %w", err)
	}
	return hostNicName, containerNicName, nil
}

func (csh executionHandler) waitForLocalnetPatchPort(subnetName string) error {
	patchPort := fmt.Sprintf("patch-localnet.%s-to-br-int", subnetName)
	klog.Infof("waiting for localnet patch port %s to be ready", patchPort)
	deadline := time.Now().Add(30 * time.Second)
	for {
		iface, err := csh.ovsClient.CNIInterface(patchPort)
		if err != nil {
			return fmt.Errorf("read localnet patch port %s: %w", patchPort, err)
		}
		if iface != nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("localnet patch port %s not ready after 30s", patchPort)
		}
		time.Sleep(100 * time.Millisecond)
	}
	klog.Infof("localnet patch port %s is ready", patchPort)
	return nil
}

// return a map of unready IPv6 addresses and their flags
func waitIPv6AddressPreferred(interfaceName string, maxRetry int, retryInterval time.Duration, checkIPv6DAD bool) (map[string]int, error) {
	var retry int
	var ret map[string]int
	for retry < maxRetry {
		link, err := netlink.LinkByName(interfaceName)
		if err != nil {
			klog.Errorf("failed to get link %s: %v", interfaceName, err)
			return nil, err
		}

		addrs, err := util.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			klog.Errorf("failed to get IPv6 addresses on interface %s: %v", interfaceName, err)
			return nil, err
		}

		addrsFlags := make(map[string]int, len(addrs))
		for _, addr := range addrs {
			if addr.IP.To4() != nil {
				continue
			}

			switch {
			case addr.Flags&unix.IFA_F_DEPRECATED != 0 || addr.Flags&unix.IFA_F_TENTATIVE != 0:
				addrsFlags[addr.IP.String()] = addr.Flags
			case addr.Flags&unix.IFA_F_DADFAILED != 0:
				if !checkIPv6DAD {
					continue
				}
				addrsFlags[addr.IP.String()] = addr.Flags
			default:
				klog.V(3).Infof("IPv6 address %s on interface %s is in preferred state", addr.IP.String(), interfaceName)
			}
		}

		if len(addrsFlags) == 0 {
			return nil, nil
		}
		ret = addrsFlags

		retry++
		if retry < maxRetry {
			time.Sleep(retryInterval)
		}
	}

	return ret, nil
}

func waitNetworkReady(nic, ipAddr, gateway string, preferARP, verbose bool, maxRetry int, done chan struct{}) error {
	ips := util.SplitTrimmed(ipAddr, ",")
	gws := util.SplitTrimmed(gateway, ",")
	if len(ips) != len(gws) {
		return fmt.Errorf("the count of ip addresses and gateways mismatch: ips %q, gateways %q", ipAddr, gateway)
	}
	for i, gw := range gws {
		src, _, _ := strings.Cut(ips[i], "/")
		if preferARP && util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
			mac, count, err := util.ArpResolve(nic, gw, time.Second, maxRetry, done)
			if err != nil {
				err = fmt.Errorf("network %s with gateway %s is not ready for interface %s after %d checks: %w", ips[i], gw, nic, count, err)
				klog.Warning(err)
				return err
			}
			if verbose {
				klog.Infof("MAC address of gateway %s is %s", gw, mac.String())
				klog.Infof("network %s with gateway %s is ready for interface %s after %d checks", ips[i], gw, nic, count)
			}
		} else {
			_, err := pingGateway(gw, src, verbose, maxRetry, done)
			if err != nil {
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func pingGateway(gw, src string, verbose bool, maxRetry int, done chan struct{}) (count int, err error) {
	pinger, err := goping.NewPinger(gw)
	if err != nil {
		return 0, fmt.Errorf("failed to init pinger: %w", err)
	}
	pinger.SetPrivileged(true)
	pinger.Count = maxRetry
	pinger.Timeout = time.Duration(maxRetry) * time.Second
	pinger.Interval = time.Second

	pinger.OnRecv = func(_ *goping.Packet) {
		pinger.Stop()
	}

	pinger.OnSend = func(_ *goping.Packet) {
		if pinger.PacketsRecv == 0 && pinger.PacketsSent != 0 && pinger.PacketsSent%3 == 0 {
			klog.Warningf("%s network not ready after %d ping to gateway %s", src, pinger.PacketsSent, gw)
		}
	}

	if done != nil {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()

		finish := make(chan struct{}, 1)
		pinger.OnFinish = func(_ *goping.Statistics) {
			finish <- struct{}{}
		}
		go func() {
			select {
			case <-done:
				pinger.Stop()
			case <-finish:
			}
		}()
	}

	if err = pinger.Run(); err != nil {
		klog.Errorf("failed to run pinger for destination %s: %v", gw, err)
		return 0, err
	}

	if pinger.PacketsRecv == 0 {
		if pinger.PacketsSent < maxRetry {
			return pinger.PacketsSent, fmt.Errorf("gateway check of %s canceled after %d retries", gw, pinger.PacketsSent)
		}
		klog.Warningf("%s network not ready after %d ping to gateway %s", src, pinger.PacketsSent, gw)
		return pinger.PacketsSent, fmt.Errorf("no packets received from gateway %s", gw)
	}

	if verbose {
		klog.Infof("%s network ready after %d ping to gateway %s", src, pinger.PacketsSent, gw)
	}
	return pinger.PacketsSent, nil
}
