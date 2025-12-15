package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/pkg/netns"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/k8snetworkplumbingwg/sriovnet"
	sriovutilfs "github.com/k8snetworkplumbingwg/sriovnet/pkg/utils/filesystem"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/net/yusur"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var pciAddrRegexp = regexp.MustCompile(`\b([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}.\d{1}\S*)`)

func (csh cniServerHandler) configureDpdkNic(podName, podNamespace, provider, netns, containerID, ifName, _ string, _ int, ip, _, ingress, egress, shortSharedDir, socketName, socketConsumption string) error {
	sharedDir := filepath.Join("/var", shortSharedDir)
	hostNicName, _ := generateNicName(containerID, ifName)

	ipStr := util.GetIPWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID, hostNicName)

	vhostServerPath := path.Join(sharedDir, socketName)
	if socketConsumption == util.ConsumptionKubevirt {
		vhostServerPath = path.Join(sharedDir, ifName)
	}

	// Add vhostuser host end to ovs port
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", hostNicName, "--",
		"set", "interface", hostNicName,
		"type=dpdkvhostuserclient",
		"options:vhost-server-path="+vhostServerPath,
		"external_ids:iface-id="+ifaceID,
		"external_ids:pod_name="+podName,
		"external_ids:pod_namespace="+podNamespace,
		"external_ids:ip="+ipStr,
		"external_ids:pod_netns="+netns)
	if err != nil {
		return fmt.Errorf("add nic to ovs failed %w: %q", err, output)
	}
	return ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress)
}

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, vmMigration bool, routes []request.Route, _, _ []string, ingress, egress, deviceID, latency, limit, loss, jitter string, gwCheckMode int, u2oInterconnectionIP, oldPodName, encapIP string) ([]request.Route, error) {
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
		hostNicName, containerNicName, pfPci, vfID, err = setupSriovInterface(containerID, deviceID, vfDriver, ifName, mtu, mac)
		if err != nil {
			klog.Errorf("failed to create sriov interfaces %v", err)
			return nil, err
		}
	}

	ipStr := util.GetIPWithoutMask(ip)
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID, hostNicName)
	if yusur.IsYusurSmartNic(deviceID) {
		klog.Infof("add Yusur smartnic vfr %s to ovs", hostNicName)
		// Add yusur ovs port
		args := []string{
			ovs.MayExist, "add-port", "br-int", hostNicName, "--",
			"set", "interface", hostNicName, "type=dpdk",
			fmt.Sprintf("options:dpdk-devargs=%s,representor=[%d]", pfPci, vfID),
			fmt.Sprintf("mtu_request=%d", mtu),
			"external_ids:iface-id=" + ifaceID,
			"external_ids:vendor=" + util.CniTypeName,
			"external_ids:pod_name=" + podName,
			"external_ids:pod_namespace=" + podNamespace,
			"external_ids:ip=" + ipStr,
			"external_ids:pod_netns=" + netns,
		}
		if encapIP != "" {
			args = append(args, "external_ids:encap-ip="+encapIP)
		}
		output, err := ovs.Exec(args...)
		if err != nil {
			return nil, fmt.Errorf("add nic to ovs failed %w: %q", err, output)
		}
	} else {
		// Add veth pair host end to ovs port
		args := []string{
			ovs.MayExist, "add-port", "br-int", hostNicName, "--",
			"set", "interface", hostNicName, "external_ids:iface-id=" + ifaceID,
			"external_ids:vendor=" + util.CniTypeName,
			"external_ids:pod_name=" + podName,
			"external_ids:pod_namespace=" + podNamespace,
			"external_ids:ip=" + ipStr,
			"external_ids:pod_netns=" + netns,
		}
		if encapIP != "" {
			args = append(args, "external_ids:encap-ip="+encapIP)
		}
		output, err := ovs.Exec(args...)
		if err != nil {
			return nil, fmt.Errorf("add nic to ovs failed %w: %q", err, output)
		}
	}
	defer func() {
		if err != nil {
			if err := csh.rollbackOvsPort(hostNicName); err != nil {
				klog.Errorf("failed to rollback ovs port %s, %v", hostNicName, err)
				return
			}
		}
	}()

	// add hostNicName and containerNicName into pod annotations
	if deviceID != "" {
		var podNameNew string
		if podName != oldPodName {
			podNameNew = oldPodName
		} else {
			podNameNew = podName
		}
		patch := util.KVPatch{
			fmt.Sprintf(util.VfRepresentorNameTemplate, provider): hostNicName,
			fmt.Sprintf(util.VfNameTemplate, provider):            containerNicName,
			fmt.Sprintf(util.PodNicAnnotationTemplate, provider):  util.SriovNicType,
		}
		if err = util.PatchAnnotations(csh.Config.KubeClient.CoreV1().Pods(podNamespace), podNameNew, patch); err != nil {
			klog.Errorf("failed to patch pod %s/%s: %v", podNamespace, podNameNew, err)
			return nil, err
		}
	}

	// lsp and container nic must use same mac address, otherwise ovn will reject these packets by default
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mac %s %w", macAddr, err)
	}
	if !yusur.IsYusurSmartNic(deviceID) {
		if err = configureHostNic(hostNicName); err != nil {
			klog.Error(err)
			return nil, err
		}
	}
	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
		klog.Error(err)
		return nil, err
	}

	if err = ovs.SetNetemQos(podName, podNamespace, ifaceID, latency, limit, loss, jitter); err != nil {
		klog.Error(err)
		return nil, err
	}

	if containerNicName == "" {
		return nil, nil
	}
	isUserspaceDP, err := ovs.IsUserspaceDataPath()
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	if isUserspaceDP {
		// turn off tx checksum
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
		output, err := ovs.Exec(ovs.IfExists, "get", "interface", hostNicName, "external-ids:ovn-installed")
		if err != nil {
			klog.Errorf("failed to get ovn-installed for ovs port %s: %v, %q", hostNicName, err, output)
			return
		}
		if strings.Trim(strings.TrimSpace(output), `"`) == "true" {
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

	podNS, err := ns.GetNS(netns)
	if err != nil {
		err = fmt.Errorf("failed to open netns %q: %w", netns, err)
		klog.Error(err)
		return nil, err
	}
	finalRoutes, err := csh.configureContainerNic(podName, podNamespace, containerNicName, ifName, ip, gateway, isDefaultRoute, vmMigration, routes, macAddr, podNS, mtu, gwCheckMode, u2oInterconnectionIP)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return finalRoutes, nil
}

func (csh cniServerHandler) releaseVf(podName, podNamespace, podNetns, ifName, nicType, deviceID string) error {
	// Only for SRIOV case, we'd need to move the VF from container namespace back to the host namespace
	if nicType != util.OffloadType || deviceID == "" {
		return nil
	}
	podDesc := fmt.Sprintf("for pod %s/%s", podNamespace, podName)
	klog.Infof("Tear down interface %s", podDesc)
	netns, err := ns.GetNS(podNetns)
	if err != nil {
		return fmt.Errorf("failed to get container namespace %s: %w", podDesc, err)
	}
	defer netns.Close()

	hostNS, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("failed to get host namespace %s: %w", podDesc, err)
	}
	defer hostNS.Close()

	err = netns.Do(func(_ ns.NetNS) error {
		// container side interface deletion
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to get container interface %s %s: %w", ifName, podDesc, err)
		}
		if err = netlink.LinkSetDown(link); err != nil {
			return fmt.Errorf("failed to bring down container interface %s %s: %w", ifName, podDesc, err)
		}
		// rename VF device back to its original name in the host namespace:
		vfName := link.Attrs().Alias
		if err = netlink.LinkSetName(link, vfName); err != nil {
			return fmt.Errorf("failed to rename container interface %s to %s %s: %w",
				ifName, vfName, podDesc, err)
		}
		// move VF device to host netns
		fd := int(netns.Fd()) // #nosec G115
		if err = netlink.LinkSetNsFd(link, fd); err != nil {
			return fmt.Errorf("failed to move container interface %s back to host namespace %s: %w",
				ifName, podDesc, err)
		}
		return nil
	})
	if err != nil {
		klog.Error(err)
	}

	return nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, containerID, netns, deviceID, ifName, nicType string) error {
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
	// Remove ovs port
	output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", nicName)
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %w, %q", err, output)
	}

	if err = ovs.ClearPodBandwidth(podName, podNamespace, ""); err != nil {
		klog.Error(err)
		return err
	}
	if err = ovs.ClearHtbQosQueue(podName, podNamespace, ""); err != nil {
		klog.Error(err)
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
			return fmt.Errorf("find host link %s failed %w", nicName, err)
		}

		hostLinkType := hostLink.Type()
		// Sometimes no deviceID input for vf nic, avoid delete vf nic.
		if hostLinkType == "veth" {
			if err = netlink.LinkDel(hostLink); err != nil {
				return fmt.Errorf("delete host link %s failed %w", hostLink, err)
			}
		}
	} else if pciAddrRegexp.MatchString(deviceID) && !yusur.IsYusurSmartNic(deviceID) {
		// Ret VF index from PCI
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

func (csh cniServerHandler) rollbackOvsPort(hostNicName string) (err error) {
	output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", hostNicName)
	if err != nil {
		klog.Warningf("failed to delete down ovs port %v, %q", err, output)
	}
	klog.Infof("rollback ovs port success %s", hostNicName)
	return err
}

func generateNicName(containerID, ifname string) (string, string) {
	if ifname == "eth0" {
		return containerID[0:12] + "_h", containerID[0:12] + "_c"
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

func (csh cniServerHandler) configureContainerNic(podName, podNamespace, nicName, ifName, ipAddr, gateway string, isDefaultRoute, vmMigration bool, routes []request.Route, macAddr net.HardwareAddr, netns ns.NetNS, mtu, gwCheckMode int, u2oInterconnectionIP string) ([]request.Route, error) {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return nil, fmt.Errorf("can not find container nic %s: %w", nicName, err)
	}

	// Set link alias to its origin link name for fastpath to recognize and bypass netfilter
	if err := netlink.LinkSetAlias(containerLink, nicName); err != nil {
		klog.Errorf("failed to set link alias for container nic %s: %v", nicName, err)
		return nil, err
	}

	fd := int(netns.Fd()) // #nosec G115
	if err = netlink.LinkSetNsFd(containerLink, fd); err != nil {
		return nil, fmt.Errorf("failed to move link to netns: %w", err)
	}

	// do not perform ipv4/ipv6 duplicate address detection during VM live migration
	ipv6DAD := !vmMigration
	detectIPv4Conflict := !vmMigration && csh.Config.EnableArpDetectIPConflict
	var finalRoutes []request.Route
	err = ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		if err = netlink.LinkSetName(containerLink, ifName); err != nil {
			klog.Error(err)
			return err
		}

		if err = configureNic(ifName, ipAddr, macAddr, mtu, detectIPv4Conflict, ipv6DAD, true, false); err != nil {
			klog.Error(err)
			return err
		}

		if isDefaultRoute {
			// Only eth0 requires the default route and gateway
			containerGw := gateway
			if u2oInterconnectionIP != "" {
				containerGw = u2oInterconnectionIP
			}

			for gw := range strings.SplitSeq(containerGw, ",") {
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
					// skip fe80::/10
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

func (csh cniServerHandler) checkGatewayReady(podName, podNamespace string, gwCheckMode int, intr, ipAddr, gateway string, verbose bool) error {
	if gwCheckMode == gatewayCheckModeArpingNotConcerned || gwCheckMode == gatewayCheckModePingNotConcerned {
		// ignore error if disableGatewayCheck=true
		_ = waitNetworkReady(intr, ipAddr, gateway, verbose, 1, nil)
		return nil
	}

	done := make(chan struct{}, 1)
	go func() {
		interval := 5 * time.Second
		timer := time.NewTimer(interval)
		for {
			select {
			case <-done:
				return
			case <-timer.C:
			}

			pod, err := csh.KubeClient.CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
			if err != nil {
				if !k8serrors.IsNotFound(err) {
					klog.Errorf("failed to get pod %s/%s: %v", podNamespace, podName, err)
					continue
				}
				pod = nil
			}
			if pod == nil || !pod.DeletionTimestamp.IsZero() {
				// TODO: check pod UID
				select {
				case <-done:
				case done <- struct{}{}:
				}
				return
			}
			timer.Reset(interval)
		}
	}()

	return waitNetworkReady(intr, ipAddr, gateway, verbose, gatewayCheckMaxRetry, done)
}

func waitNetworkReady(nic, ipAddr, gateway string, verbose bool, maxRetry int, done chan struct{}) error {
	ips := strings.Split(ipAddr, ",")
	for i, gw := range strings.Split(gateway, ",") {
		src := strings.Split(ips[i], "/")[0]
		if util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
			mac, count, err := util.ArpResolve(nic, gw, time.Second, maxRetry, done)
			cniConnectivityResult.WithLabelValues(nodeName).Add(float64(count))
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

func configureNodeNic(cs kubernetes.Interface, nodeName, portName, ip, gw, joinCIDR string, macAddr net.HardwareAddr, mtu int) error {
	ipStr := util.GetIPWithoutMask(ip)
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", util.NodeNic, "--",
		"set", "interface", util.NodeNic, "type=internal", "--",
		"set", "interface", util.NodeNic, "external_ids:iface-id="+portName,
		"external_ids:ip="+ipStr)
	if err != nil {
		klog.Errorf("failed to configure node nic %s: %v, %q", portName, err, raw)
		return errors.New(raw)
	}

	if err = configureNic(util.NodeNic, ip, macAddr, mtu, false, false, false, true); err != nil {
		klog.Error(err)
		return err
	}

	hostLink, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		return fmt.Errorf("can not find nic %s: %w", util.NodeNic, err)
	}

	actualMac := hostLink.Attrs().HardwareAddr
	if actualMac.String() != macAddr.String() {
		macAddr = actualMac
		err := fmt.Errorf("MAC address mismatch on %s: expected %s, actual %s", util.NodeNic, macAddr.String(), actualMac.String())
		klog.Error(err)
		return err
	}
	klog.Infof("MAC address %s successfully set on %s", macAddr.String(), util.NodeNic)

	if err = netlink.LinkSetTxQLen(hostLink, 1000); err != nil {
		return fmt.Errorf("can not set host nic %s qlen: %w", util.NodeNic, err)
	}

	// check and add default route for ovn0 in case of can not add automatically
	nodeNicRoutes, err := getNicExistRoutes(hostLink, gw)
	if err != nil {
		klog.Error(err)
		return err
	}

	var toAdd []netlink.Route
	for c := range strings.SplitSeq(joinCIDR, ",") {
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
				for ip := range strings.SplitSeq(ipStr, ",") {
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
	klog.Infof("wait %s gw ready", util.NodeNic)
	status := corev1.ConditionFalse
	reason := "JoinSubnetGatewayReachable"
	message := fmt.Sprintf("ping check to gateway ip %s succeeded", gw)
	if err = waitNetworkReady(util.NodeNic, ip, gw, true, gatewayCheckMaxRetry, nil); err != nil {
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
	ip := node.Annotations[util.IPAddressAnnotation]
	gw := node.Annotations[util.GatewayAnnotation]
	status := corev1.ConditionFalse
	reason := "JoinSubnetGatewayReachable"
	message := fmt.Sprintf("ping check to gateway ip %s succeeded", gw)
	if err = waitNetworkReady(util.NodeNic, ip, gw, false, 5, nil); err != nil {
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

// This method checks the status of the tunnel interface,
// If the interface is found to be down, it attempts to bring it up
func (c *Controller) loopTunnelCheck() {
	tunnelType := c.config.NetworkType
	var tunnelNic string
	switch tunnelType {
	case "vxlan":
		tunnelNic = util.VxlanNic
	case "geneve":
		tunnelNic = util.GeneveNic
	case "stt":
		// TODO: tunnelNic = "stt tunnel nic name"
		return
	default:
		return
	}

	link, err := netlink.LinkByName(tunnelNic)
	if err != nil || link == nil {
		return
	}

	if link.Attrs().OperState == netlink.OperDown {
		klog.Errorf("nic: %s is down, attempting to bring it up", tunnelNic)
		if err := netlink.LinkSetUp(link); err != nil {
			klog.Errorf("fail to bring up nic: %s, %v", tunnelNic, err)
		}
	}
}

func (c *Controller) checkNodeGwNicInNs(nodeExtIP, ip, gw string, gwNS ns.NetNS) error {
	exists, err := ovs.PortExists(util.NodeGwNic)
	if err != nil {
		klog.Error(err)
		return err
	}
	filters := labels.Set{util.OvnEipTypeLabel: util.OvnEipTypeLRP}
	ovnEips, err := c.ovnEipsLister.List(labels.SelectorFromSet(filters))
	if err != nil {
		klog.Errorf("failed to list ovn eip, %v", err)
		return err
	}
	if len(ovnEips) == 0 {
		klog.Errorf("failed to get type %s ovn eip, %v", util.OvnEipTypeLRP, err)
		// node ext gw eip need lrp eip to establish bfd session
		return nil
	}
	if exists {
		return ns.WithNetNSPath(gwNS.Path(), func(_ ns.NetNS) error {
			err = waitNetworkReady(util.NodeGwNic, ip, gw, true, 3, nil)
			if err == nil {
				if output, err := exec.Command("bfdd-control", "status").CombinedOutput(); err != nil {
					err := fmt.Errorf("failed to get bfdd status, %w, %s", err, output)
					klog.Error(err)
					return err
				}
				for _, eip := range ovnEips {
					if eip.Status.Ready {
						// #nosec G204
						cmd := exec.Command("bfdd-control", "status", "remote", eip.Spec.V4Ip, "local", nodeExtIP)
						var outb bytes.Buffer
						cmd.Stdout = &outb
						if err := cmd.Run(); err == nil {
							out := outb.String()
							klog.V(3).Info(out)
							if strings.Contains(out, "No session") {
								// not exist
								cmd = exec.Command("bfdd-control", "allow", eip.Spec.V4Ip) // #nosec G204
								if err := cmd.Run(); err != nil {
									err := fmt.Errorf("failed to add lrp %s ip %s into bfd listening list, %w", eip.Name, eip.Status.V4Ip, err)
									klog.Error(err)
									return err
								}
							}
						} else {
							err := fmt.Errorf("faild to check bfd status remote %s local %s", eip.Spec.V4Ip, nodeExtIP)
							klog.Error(err)
							return err
						}
					}
				}
			}
			return err
		})
	}

	err = errors.New("node external gw not ready")
	klog.Error(err)
	return err
}

func configureNodeGwNic(portName, ip, gw string, macAddr net.HardwareAddr, mtu int, gwNS ns.NetNS) error {
	ipStr := util.GetIPWithoutMask(ip)
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", util.NodeGwNic, "--",
		"set", "interface", util.NodeGwNic, "type=internal", "--",
		"set", "interface", util.NodeGwNic, "external_ids:iface-id="+portName,
		"external_ids:ip="+ipStr,
		"external_ids:pod_netns="+util.NodeGwNsPath)
	if err != nil {
		klog.Errorf("failed to configure node external nic %s: %v, %q", portName, err, output)
		return errors.New(output)
	}
	gwLink, err := netlink.LinkByName(util.NodeGwNic)
	if err == nil {
		fd := int(gwNS.Fd()) // #nosec G115
		if err = netlink.LinkSetNsFd(gwLink, fd); err != nil {
			klog.Errorf("failed to move link into netns: %v", err)
			return err
		}
	} else {
		klog.V(3).Infof("node external nic %q already in ns %s", util.NodeGwNic, util.NodeGwNsPath)
	}
	return ns.WithNetNSPath(gwNS.Path(), func(_ ns.NetNS) error {
		if err = configureNic(util.NodeGwNic, ip, macAddr, mtu, true, true, false, false); err != nil {
			klog.Errorf("failed to configure node gw nic %s, %v", util.NodeGwNic, err)
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
				return fmt.Errorf("config v4 gateway failed: %w", err)
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
			return fmt.Errorf("failed to configure gateway: %w", err)
		}
		cmd := exec.Command("bfdd-beacon", "--listen=0.0.0.0")
		if err := cmd.Run(); err != nil {
			err := fmt.Errorf("failed to get start bfd listen, %w", err)
			klog.Error(err)
			return err
		}
		return waitNetworkReady(util.NodeGwNic, ip, gw, true, 3, nil)
	})
}

func removeNodeGwNic() error {
	if _, err := ovs.Exec(ovs.IfExists, "del-port", "br-int", util.NodeGwNic); err != nil {
		return fmt.Errorf("failed to remove ecmp external port %s from OVS bridge %s: %w", "br-int", util.NodeGwNic, err)
	}
	klog.Infof("removed node external gw nic %q", util.NodeGwNic)
	return nil
}

func removeNodeGwNs() error {
	ns := netns.LoadNetNS(util.NodeGwNsPath)
	ok, err := ns.Closed()
	if err != nil {
		return fmt.Errorf("failed to remove node external gw ns %s: %w", util.NodeGwNs, err)
	}
	if !ok {
		if err = ns.Remove(); err != nil {
			return fmt.Errorf("failed to remove node external gw ns %s: %w", util.NodeGwNs, err)
		}
	}
	klog.Infof("node external gw ns %s removed", util.NodeGwNs)
	return nil
}

func (c *Controller) loopOvnExt0Check() {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return
	}

	portName := node.Name
	needClean := false
	cachedEip, err := c.ovnEipsLister.Get(portName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			val, ok := node.Labels[util.NodeExtGwLabel]
			if !ok {
				// not gw node before
				return
			}
			if val == "false" {
				// already clean
				return
			}
			if val == "true" {
				needClean = true
			}
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
		if err = c.patchNodeExternalGwLabel(false); err != nil {
			klog.Errorf("failed to patch labels of node %s: %v", node.Name, err)
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
		if _, ok := err.(ns.NSPathNotExistErr); !ok {
			klog.Errorf("failed to get netns from path %s: %v", util.NodeGwNsPath, err)
			return
		}
		if err = newNetNS(util.NodeGwNsPath); err != nil {
			klog.Error(fmt.Errorf("failed to create gw ns %s: %w", util.NodeGwNs, err))
			return
		}
		if gwNS, err = ns.GetNS(util.NodeGwNsPath); err != nil {
			klog.Errorf("failed to get netns from path %s: %v", util.NodeGwNsPath, err)
			return
		}
	}
	nodeExtIP := cachedEip.Spec.V4Ip
	ipAddr, err := util.GetIPAddrWithMask(ips, cachedSubnet.Spec.CIDRBlock)
	if err != nil {
		klog.Errorf("failed to get ip addr with mask %s, %v", ips, err)
		return
	}
	if err := c.checkNodeGwNicInNs(nodeExtIP, ipAddr, gw, gwNS); err == nil {
		// add all lrp ip in bfd listening list
		return
	}
	klog.Infof("setup nic ovnext0 ip %s, mac %v, mtu %d", ipAddr, mac, c.config.MTU)
	if err := configureNodeGwNic(portName, ipAddr, gw, mac, c.config.MTU, gwNS); err != nil {
		klog.Errorf("failed to setup ovnext0, %v", err)
		return
	}
	if err = c.patchNodeExternalGwLabel(true); err != nil {
		klog.Errorf("failed to patch labels of node %s: %v", node.Name, err)
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

func (c *Controller) patchNodeExternalGwLabel(enabled bool) error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return err
	}

	patch := util.KVPatch{util.NodeExtGwLabel: strconv.FormatBool(enabled)}
	if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
		klog.Errorf("failed to patch labels of node %s: %v", node.Name, err)
		return err
	}

	return nil
}

func configureMirrorLink(portName string, _ int) error {
	mirrorLink, err := netlink.LinkByName(portName)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("can not find mirror nic %s: %w", portName, err)
	}

	if mirrorLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(mirrorLink); err != nil {
			klog.Error(err)
			return fmt.Errorf("can not set mirror nic %s up: %w", portName, err)
		}
	}

	return nil
}

// Convert MAC address to EUI-64 and generate link-local IPv6 address
func macToLinkLocalIPv6(mac net.HardwareAddr) (net.IP, error) {
	if len(mac) != 6 {
		return nil, errors.New("invalid MAC address length")
	}

	// Create EUI-64 format
	eui64 := make([]byte, 8)
	copy(eui64[0:3], mac[0:3]) // Copy the first 3 bytes
	eui64[3] = 0xff            // Insert ff
	eui64[4] = 0xfe            // Insert fe
	copy(eui64[5:], mac[3:])   // Copy the last 3 bytes

	// Flip the 7th bit of the first byte
	eui64[0] ^= 0x02

	// Prepend the link-local prefix
	linkLocalIPv6 := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	copy(linkLocalIPv6[8:], eui64)

	return linkLocalIPv6, nil
}

func configureNic(link, ip string, macAddr net.HardwareAddr, mtu int, detectIPv4Conflict, ipv6DAD, setUfoOff, ipv6LinkLocalOn bool) error {
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
			_, err = ovs.Exec("set", "interface", link, fmt.Sprintf(`mtu_request=%d`, mtu))
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
	ipAddrs, err := netlink.AddrList(nodeLink, unix.AF_UNSPEC)
	if err != nil {
		err = fmt.Errorf("failed to list addresses on link %s: %w", link, err)
		klog.Error(err)
		return err
	}

	isIPv6LinkLocalExist := false
	for _, ipAddr := range ipAddrs {
		if ipAddr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			if util.CheckProtocol(ipAddr.IP.String()) == kubeovnv1.ProtocolIPv6 {
				isIPv6LinkLocalExist = true
			}
			continue
		}
		ipDelMap[ipAddr.IPNet.String()] = ipAddr
	}

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
		// Do not reassign same address for link
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
				// when detectIPConflict is true, free arp is already broadcast in the step of announcement
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

func configureLoNic() error {
	loLink, err := netlink.LinkByName(util.LoNic)
	if err != nil {
		err := fmt.Errorf("can not find nic %s, %w", util.LoNic, err)
		klog.Error(err)
		return err
	}

	if loLink.Attrs().OperState != netlink.OperUp {
		if err = netlink.LinkSetUp(loLink); err != nil {
			err := fmt.Errorf("failed to set up nic %s, %w", util.LoNic, err)
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (c *Controller) transferAddrsAndRoutes(nicName, brName string, delNonExistent bool) (int, error) {
	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		return 0, fmt.Errorf("failed to get nic by name %s: %w", nicName, err)
	}
	bridge, err := netlink.LinkByName(brName)
	if err != nil {
		return 0, fmt.Errorf("failed to get bridge by name %s: %w", brName, err)
	}

	addrs, err := netlink.AddrList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses on nic %s: %w", nicName, err)
	}
	routes, err := netlink.RouteList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get routes on nic %s: %w", nicName, err)
	}

	brAddrs, err := netlink.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses on OVS bridge %s: %w", brName, err)
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
			errMsg := fmt.Errorf("failed to delete address %q on nic %s: %w", addr.String(), nicName, err)
			klog.Error(errMsg)
			return 0, errMsg
		}
		klog.Infof("address %q has been removed from link %s", addr.String(), nicName)

		addr.Label = ""
		addr.PreferedLft, addr.ValidLft = 0, 0
		if err = netlink.AddrReplace(bridge, &addr); err != nil {
			return 0, fmt.Errorf("failed to replace address %q on OVS bridge %s: %w", addr.String(), brName, err)
		}
		klog.Infof("address %q has been added/replaced to link %s", addr.String(), brName)
	}

	if count != 0 {
		for _, addr := range delAddrs {
			if err = netlink.AddrDel(bridge, &addr); err != nil {
				errMsg := fmt.Errorf("failed to delete address %q on OVS bridge %s: %w", addr.String(), brName, err)
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
			return 0, fmt.Errorf("failed to set MAC address of OVS bridge %s: %w", brName, err)
		}
	}

	if err = netlink.LinkSetUp(bridge); err != nil {
		return 0, fmt.Errorf("failed to set OVS bridge %s up: %w", brName, err)
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
					return 0, fmt.Errorf("failed to add/replace route %s to OVS bridge %s: %w", route.String(), brName, err)
				}
				klog.Infof("route %q has been added/replaced to OVS bridge %s", route.String(), brName)
			}
		}
	}

	brRoutes, err := netlink.RouteList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return 0, fmt.Errorf("failed to get routes on OVS bridge %s: %w", brName, err)
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
					return 0, fmt.Errorf("failed to delete route %s from OVS bridge %s: %w", route.String(), brName, err)
				}
				klog.Infof("route %q has been deleted from OVS bridge %s", route.String(), brName)
			}
		}
	}

	if err = netlink.LinkSetUp(nic); err != nil {
		return 0, fmt.Errorf("failed to set link %s up: %w", nicName, err)
	}

	return nic.Attrs().MTU, nil
}

// Add host nic to external bridge
// Mac address, MTU, IP addresses & routes will be copied/transferred to the external bridge
func (c *Controller) configProviderNic(nicName, brName string, trunks []string) (int, error) {
	isUserspaceDP, err := ovs.IsUserspaceDataPath()
	if err != nil {
		klog.Error(err)
		return 0, err
	}

	var mtu int
	if !isUserspaceDP {
		// Check if bridge exists before attempting to add port
		output, err := ovs.Exec("list-br")
		if err != nil {
			klog.Errorf("failed to list OVS bridges: %v, %q", err, output)
			return 0, err
		}
		if !slices.Contains(strings.Split(output, "\n"), brName) {
			err := fmt.Errorf("bridge %s does not exist", brName)
			klog.Error(err)
			return 0, err
		}

		mtu, err = c.transferAddrsAndRoutes(nicName, brName, false)
		if err != nil {
			klog.Errorf("failed to transfer addresses and routes from %s to %s: %v", nicName, brName, err)
			return 0, err
		}

		if _, err = ovs.Exec(ovs.MayExist, "add-port", brName, nicName,
			"--", "set", "port", nicName, "trunks="+strings.Join(trunks, ","), "external_ids:vendor="+util.CniTypeName); err != nil {
			klog.Errorf("failed to add %s to OVS bridge %s: %v", nicName, brName, err)
			return 0, err
		}
		klog.V(3).Infof("ovs port %s has been added to bridge %s", nicName, brName)
	} else {
		mtu = c.config.MTU
	}

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
		return fmt.Errorf("failed to get nic by name %s: %w", nicName, err)
	}
	bridge, err := netlink.LinkByName(brName)
	if err != nil {
		return fmt.Errorf("failed to get bridge by name %s: %w", brName, err)
	}

	addrs, err := netlink.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get addresses on bridge %s: %w", brName, err)
	}
	routes, err := netlink.RouteList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get routes on bridge %s: %w", brName, err)
	}

	if _, err = ovs.Exec(ovs.IfExists, "del-port", brName, nicName); err != nil {
		return fmt.Errorf("failed to remove %s from OVS bridge %s: %w", nicName, brName, err)
	}
	klog.V(3).Infof("ovs port %s has been removed from bridge %s", nicName, brName)

	for _, addr := range addrs {
		if addr.IP.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}

		if err = netlink.AddrDel(bridge, &addr); err != nil {
			errMsg := fmt.Errorf("failed to delete address %q on OVS bridge %s: %w", addr.String(), brName, err)
			klog.Error(errMsg)
			return errMsg
		}
		klog.Infof("address %q has been deleted from link %s", addr.String(), brName)

		addr.Label = ""
		if err = netlink.AddrReplace(nic, &addr); err != nil {
			return fmt.Errorf("failed to replace address %q on nic %s: %w", addr.String(), nicName, err)
		}
		klog.Infof("address %q has been added/replaced to link %s", addr.String(), nicName)
	}

	if err = netlink.LinkSetUp(nic); err != nil {
		klog.Errorf("failed to set link %s up: %v", nicName, err)
		return err
	}

	for _, scope := range routeScopeOrders {
		for _, route := range routes {
			if route.Gw == nil && route.Dst != nil && route.Dst.IP.IsLinkLocalUnicast() {
				// skip 169.254.0.0/16 and fe80::/10
				continue
			}
			if route.Scope == scope {
				route.LinkIndex = nic.Attrs().Index
				if err = netlink.RouteReplace(&route); err != nil {
					return fmt.Errorf("failed to add/replace route %s: %w", route.String(), err)
				}
				klog.Infof("route %q has been added/replaced to link %s", route.String(), nicName)
			}
		}
	}

	if err = netlink.LinkSetDown(bridge); err != nil {
		return fmt.Errorf("failed to set OVS bridge %s down: %w", brName, err)
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
		return "", "", fmt.Errorf("failed to create veth for %w", err)
	}
	return hostNicName, containerNicName, nil
}

// Setup sriov interface in the pod
// https://github.com/ovn-org/ovn-kubernetes/commit/6c96467d0d3e58cab05641293d1c1b75e5914795
func setupSriovInterface(containerID, deviceID, vfDriver, ifName string, mtu int, mac string) (string, string, string, int, error) {
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
		// 1. get VF netdevice from PCI
		vfNetdevices, err := sriovnet.GetNetDevicesFromPci(deviceID)
		if err != nil {
			klog.Errorf("failed to get vf netdevice %s, %v", deviceID, err)
			return "", "", "", -1, err
		}

		// Make sure we have 1 netdevice per pci address
		if len(vfNetdevices) != 1 {
			return "", "", "", -1, fmt.Errorf("failed to get one netdevice interface per %s", deviceID)
		}
		vfNetdevice = vfNetdevices[0]
	}

	if yusur.IsYusurSmartNic(deviceID) {
		// 2. get PF PCI
		pfPci, err := yusur.GetYusurNicPfPciFromVfPci(deviceID)
		if err != nil {
			return "", "", "", -1, err
		}

		// 3. get PF index from Pci
		pfIndex, err := yusur.GetYusurNicPfIndexByPciAddress(pfPci)
		if err != nil {
			klog.Errorf("failed to get up %s link device, %v", deviceID, err)
			return "", "", "", -1, err
		}

		// 4. get VF index from PCI
		vfIndex, err := yusur.GetYusurNicVfIndexByPciAddress(deviceID)
		if err != nil {
			return "", "", "", -1, err
		}

		// 5. get vf representor
		rep := yusur.GetYusurNicVfRepresentor(pfIndex, vfIndex)

		_, err = netlink.LinkByName(rep)
		if err != nil {
			klog.Infof("vfr not exist %s", rep)
		}

		return rep, vfNetdevice, pfPci, vfIndex, nil
	}

	// 2. get Uplink netdevice
	uplink, err := sriovnet.GetUplinkRepresentor(deviceID)
	if err != nil {
		klog.Errorf("failed to get up %s link device, %v", deviceID, err)
		return "", "", "", -1, err
	}

	// 3. get VF index from PCI
	vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
	if err != nil {
		klog.Errorf("failed to get vf %s index, %v", deviceID, err)
		return "", "", "", -1, err
	}

	// 4. lookup representor
	rep, err := sriovnet.GetVfRepresentor(uplink, vfIndex)
	if err != nil {
		klog.Errorf("failed to get vf %d representor, %v", vfIndex, err)
		return "", "", "", -1, err
	}
	oldHostRepName := rep

	// 5. rename the host VF representor
	hostNicName, _ := generateNicName(containerID, ifName)
	if err = renameLink(oldHostRepName, hostNicName); err != nil {
		return "", "", "", -1, fmt.Errorf("failed to rename %s to %s: %w", oldHostRepName, hostNicName, err)
	}

	link, err := netlink.LinkByName(hostNicName)
	if err != nil {
		return "", "", "", -1, err
	}

	// 6. set MTU on VF representor
	if err = netlink.LinkSetMTU(link, mtu); err != nil {
		return "", "", "", -1, fmt.Errorf("failed to set MTU on %s: %w", hostNicName, err)
	}

	// 7. set MAC address to VF
	if err = setVfMac(deviceID, vfIndex, mac); err != nil {
		return "", "", "", -1, err
	}

	return hostNicName, vfNetdevice, "", -1, nil
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
	return netlink.LinkSetUp(link)
}

func (csh cniServerHandler) removeDefaultRoute(netns string, ipv4, ipv6 bool) error {
	podNS, err := ns.GetNS(netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %w", netns, err)
	}

	return ns.WithNetNSPath(podNS.Path(), func(_ ns.NetNS) error {
		routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
		if err != nil {
			return fmt.Errorf("failed to get all routes: %w", err)
		}

		for _, r := range routes {
			if r.Dst != nil {
				if ones, _ := r.Dst.Mask.Size(); ones != 0 {
					continue
				}
			}
			if ipv4 && r.Family == netlink.FAMILY_V4 {
				klog.Infof("deleting default ipv4 route %+v", r)
				if err = netlink.RouteDel(&r); err != nil {
					return fmt.Errorf("failed to delete route %+v: %w", r, err)
				}
				continue
			}
			if ipv6 && r.Family == netlink.FAMILY_V6 {
				klog.Infof("deleting default ipv6 route %+v", r)
				if err = netlink.RouteDel(&r); err != nil {
					return fmt.Errorf("failed to delete route %+v: %w", r, err)
				}
			}
		}
		return nil
	})
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

func TurnOffNicTxChecksum(nicName string) error {
	start := time.Now()
	args := []string{"-K", nicName, "tx", "off"}
	output, err := exec.Command("ethtool", args...).CombinedOutput() // #nosec G204
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", "ethtool", strings.Join(args, " "), elapsed)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to turn off nic tx checksum, output %s, err %s", string(output), err.Error())
	}
	return nil
}

func getShortSharedDir(uid types.UID, volumeName string) string {
	return filepath.Clean(filepath.Join(util.DefaultHostVhostuserBaseDir, string(uid), volumeName))
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

func rollBackVethPair(nicName string) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		// if link already not exists, return quietly
		// e.g. Internal port had been deleted by Remove ovs port previously
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		klog.Error(err)
		return fmt.Errorf("find host link %s failed %w", nicName, err)
	}

	hostLinkType := hostLink.Type()
	// sometimes no deviceID input for vf nic, avoid delete vf nic.
	if hostLinkType == "veth" {
		if err = netlink.LinkDel(hostLink); err != nil {
			klog.Error(err)
			return fmt.Errorf("delete host link %s failed %w", hostLink, err)
		}
	}
	klog.Infof("rollback veth success %s", nicName)
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

		addrs, err := netlink.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			klog.Errorf("failed to get IPv6 addresses on interface %s: %v", interfaceName, err)
			return nil, err
		}

		addrsFlags := make(map[string]int, len(addrs))
		for _, addr := range addrs {
			// skip ipv4 addresses
			if addr.IP.To4() != nil {
				continue
			}
			// Check if the address is in a bad state
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

func (c *Controller) createVlanSubinterfaces(vlanInterfaces []string, baseInterface, providerName string) error {
	if baseInterface == "" {
		return errors.New("base interface is empty")
	}
	if !util.CheckInterfaceExists(baseInterface) {
		return fmt.Errorf("base interface %s does not exist", baseInterface)
	}

	for _, vlanIfName := range vlanInterfaces {
		klog.V(3).Infof("Processing VLAN interface creation for %s", vlanIfName)

		parts := strings.SplitN(vlanIfName, ".", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid VLAN interface name format: %s (expected <interface>.<vlanid>)", vlanIfName)
		}
		parentIf := parts[0]
		if parentIf != baseInterface {
			return fmt.Errorf("vlan interface %s uses parent %s, which does not match default interface %s", vlanIfName, parentIf, baseInterface)
		}

		vlanID, err := util.ExtractVlanIDFromInterface(vlanIfName)
		if err != nil {
			return fmt.Errorf("failed to extract VLAN ID from interface name %s: %w", vlanIfName, err)
		}

		if util.CheckInterfaceExists(vlanIfName) {
			klog.Infof("VLAN interface %s already exists, skipping creation", vlanIfName)
			continue
		}

		klog.Infof("Creating VLAN interface %s (ID: %d) on %s", vlanIfName, vlanID, baseInterface)
		output, err := exec.Command("ip", "link", "add", "link", baseInterface, "name", vlanIfName, "type", "vlan", "id", strconv.Itoa(vlanID)).CombinedOutput()
		if err != nil {
			klog.Errorf("Failed to create VLAN interface %s: %v, output: %s", vlanIfName, err, string(output))
			return fmt.Errorf("failed to create VLAN interface %s: %w", vlanIfName, err)
		}

		if err := util.SetLinkUp(vlanIfName); err != nil {
			klog.Errorf("Failed to set VLAN interface %s up: %v", vlanIfName, err)
			if _, delErr := exec.Command("ip", "link", "delete", vlanIfName).CombinedOutput(); delErr != nil {
				klog.Errorf("Failed to clean up VLAN interface %s: %v", vlanIfName, delErr)
			}
			return fmt.Errorf("failed to set VLAN interface %s up: %w", vlanIfName, err)
		}

		alias := fmt.Sprintf("kube-ovn:%s", providerName)
		if output, err := exec.Command("ip", "link", "set", vlanIfName, "alias", alias).CombinedOutput(); err != nil {
			klog.Errorf("Failed to set alias for interface %s: %v, output: %s", vlanIfName, err, string(output))
			if _, delErr := exec.Command("ip", "link", "delete", vlanIfName).CombinedOutput(); delErr != nil {
				klog.Errorf("Failed to clean up VLAN interface %s after alias set failure: %v", vlanIfName, delErr)
			}
			return fmt.Errorf("failed to set alias for interface %s: %w", vlanIfName, err)
		}
		klog.V(3).Infof("Set alias %s for VLAN interface %s", alias, vlanIfName)

		klog.Infof("Successfully created VLAN interface %s (ID: %d) with alias %s", vlanIfName, vlanID, alias)
	}

	return nil
}

func (c *Controller) cleanupAutoCreatedVlanInterfaces(providerName string) error {
	createdInterfaces, err := util.FindKubeOVNAutoCreatedInterfaces(providerName)
	if err != nil {
		return fmt.Errorf("failed to find auto-created interfaces for provider %s: %w", providerName, err)
	}

	if len(createdInterfaces) == 0 {
		klog.V(3).Infof("No auto-created VLAN interfaces found for provider %s", providerName)
		return nil
	}

	klog.Infof("Found %d auto-created VLAN interfaces to clean up for provider %s: %v", len(createdInterfaces), providerName, createdInterfaces)

	// Delete each auto-created interface
	for _, ifaceName := range createdInterfaces {
		klog.Infof("Cleaning up auto-created VLAN interface %s", ifaceName)
		output, err := exec.Command("ip", "link", "delete", ifaceName).CombinedOutput()
		if err != nil {
			klog.Warningf("Failed to delete auto-created VLAN interface %s: %v, output: %s", ifaceName, err, string(output))
			// Continue with other interfaces even if deletion fails
		} else {
			klog.Infof("Successfully deleted auto-created VLAN interface %s", ifaceName)
		}
	}

	return nil
}
