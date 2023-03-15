package daemon

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Microsoft/hcsshim"
	"github.com/containernetworking/plugins/pkg/hns"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (csh cniServerHandler) configureDpdkNic(podName, podNamespace, provider, netns, containerID, ifName, mac string, mtu int, ip, gateway, ingress, egress, sharedDir, socketName string) error {
	return errors.New("DPDK is not supported on Windows")
}

func (csh cniServerHandler) configureNicWithInternalPort(podName, podNamespace, provider, netns, containerID, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss, jitter string, gwCheckMode int, u2oInterconnectionIP string) (string, error) {
	return ifName, csh.configureNic(podName, podNamespace, provider, netns, containerID, "", ifName, mac, mtu, ip, gateway, isDefaultRoute, detectIPConflict, routes, dnsServer, dnsSuffix, ingress, egress, DeviceID, nicType, latency, limit, loss, jitter, gwCheckMode, u2oInterconnectionIP)
}

func (csh cniServerHandler) configureNic(podName, podNamespace, provider, netns, containerID, vfDriver, ifName, mac string, mtu int, ip, gateway string, isDefaultRoute, detectIPConflict bool, routes []request.Route, dnsServer, dnsSuffix []string, ingress, egress, DeviceID, nicType, latency, limit, loss, jitter string, gwCheckMode int, u2oInterconnectionIP string) error {
	if DeviceID != "" {
		return errors.New("SR-IOV is not supported on Windows")
	}

	hnsNetwork, err := hcsshim.GetHNSNetworkByName(util.HnsNetwork)
	if err != nil {
		klog.Errorf("failed to get HNS network %s: %v", util.HnsNetwork)
		return err
	}
	if hnsNetwork == nil {
		err = fmt.Errorf("HNS network %s does not exist", util.HnsNetwork)
		klog.Error(err)
		return err
	}
	if !strings.EqualFold(hnsNetwork.Type, "Transparent") {
		err = fmt.Errorf(`type of HNS network %s is "%s", while "Transparent" is required`, util.HnsNetwork, hnsNetwork.Type)
		klog.Error(err)
		return err
	}

	ipAddr := util.GetIpWithoutMask(ip)
	sandbox := hns.GetSandboxContainerID(containerID, netns)
	epName := sandbox[:12]
	_, err = hns.AddHnsEndpoint(epName, hnsNetwork.Id, containerID, netns, func() (*hcsshim.HNSEndpoint, error) {
		ipv4, ipv6 := util.SplitStringIP(ipAddr)
		endpoint := &hcsshim.HNSEndpoint{
			Name:           epName,
			VirtualNetwork: hnsNetwork.Id,
			DNSServerList:  strings.Join(dnsServer, ","),
			DNSSuffix:      strings.Join(dnsSuffix, ","),
			MacAddress:     mac,
			IPAddress:      net.ParseIP(ipv4),
			IPv6Address:    net.ParseIP(ipv6),
		}

		endpoint.GatewayAddress, endpoint.GatewayAddressV6 = util.SplitStringIP(gateway)
		for _, s := range strings.Split(ip, ",") {
			_, network, err := net.ParseCIDR(s)
			if err != nil {
				return nil, err
			}
			if ones, bits := network.Mask.Size(); bits == 128 {
				endpoint.IPv6PrefixLength = uint8(ones)
			} else {
				endpoint.PrefixLength = uint8(ones)
			}
		}

		return endpoint, nil
	})
	if err != nil {
		klog.Errorf("failed to add HNS endpoint: %v", err)
		return err
	}

	if containerID != sandbox {
		// pause container, return here
		return nil
	}

	// add OVS port
	exists, err := ovs.PortExists(epName)
	if err != nil {
		klog.Error(err)
		return err
	}
	if exists {
		return nil
	}

	timeout := 5
	adapterName := fmt.Sprintf("vEthernet (%s)", epName)
	for i := 0; i < timeout; i++ {
		adapter, _ := util.GetNetAdapter(adapterName, true)
		if adapter == nil {
			time.Sleep(time.Second)
			continue
		}

		_mtu := uint32(mtu)
		if err = util.SetNetIPInterface(adapter.InterfaceIndex, nil, &_mtu, nil, nil); err != nil {
			klog.Errorf("failed to set MTU of %s to %d: %v", adapterName, mtu, err)
			return err
		}

		ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
		ovs.CleanDuplicatePort(ifaceID, epName)
		output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", epName, "--",
			"set", "interface", epName, "type=internal", "--",
			"set", "interface", epName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
			fmt.Sprintf("external_ids:pod_name=%s", podName),
			fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
			fmt.Sprintf("external_ids:ip=%s", ipAddr))
		if err != nil {
			return fmt.Errorf("failed to add OVS port %s, %v: %q", epName, err, output)
		}

		if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress); err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf(`failed to get network adapter "%s" after %d seconds`, adapterName, timeout)
}

func configureNic(name, ip string, mac net.HardwareAddr, mtu int) error {
	adapter, err := util.GetNetAdapter(name, false)
	if err != nil {
		klog.Errorf("failed to get network adapter %s: %v", name, err)
		return err
	}

	// we need to set mac address before enabling the adapter
	if newMac := mac.String(); !strings.EqualFold(newMac, adapter.MacAddress) {
		if err = util.SetAdapterMac(name, mac.String()); err != nil {
			klog.Error(err)
			return err
		}
	}

	if adapter.InterfaceAdminStatus != 1 {
		if err = util.EnableAdapter(name); err != nil {
			klog.Error(err)
			return err
		}
	}

	interfaces, err := util.GetNetIPInterface(adapter.InterfaceIndex)
	if err != nil {
		klog.Errorf("failed to get NetIPInterface with index %d: %v", adapter.InterfaceIndex, err)
		return err
	}
	addresses, err := util.GetNetIPAddress(adapter.InterfaceIndex)
	if err != nil {
		klog.Errorf("failed to get NetIPAddress with index %d: %v", adapter.InterfaceIndex, err)
		return err
	}

	// set MTU
	for _, iface := range interfaces {
		if uint32(mtu) != iface.NlMtu {
			_mtu := uint32(mtu)
			if err = util.SetNetIPInterface(iface.InterfaceIndex, &iface.AddressFamily, &_mtu, nil, nil); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	addrToAdd := make(map[string]interface{})
	for _, addr := range strings.Split(ip, ",") {
		addrToAdd[addr] = true
	}

	addrToDel := make(map[string]interface{})
	for _, addr := range addresses {
		// handle IPv6 address, e.g. fe80::e053:1757:f000:be40%47
		addr.IPAddress = strings.TrimSuffix(addr.IPAddress, fmt.Sprintf("%%%d", addr.InterfaceIndex))
		ip := net.ParseIP(addr.IPAddress)
		if ip == nil {
			klog.Warningf("found invalid IP address %s on interface %s", addr.IPAddress, name)
			continue
		}
		if ip.IsLinkLocalUnicast() {
			// skip 169.254.0.0/16 and fe80::/10
			continue
		}

		s := fmt.Sprintf("%s/%d", addr.IPAddress, addr.PrefixLength)
		if _, ok := addrToAdd[s]; ok {
			delete(addrToAdd, s)
		} else {
			addrToDel[s] = true
		}
	}

	for addr := range addrToDel {
		if err = util.RemoveNetIPAddress(adapter.InterfaceIndex, addr); err != nil {
			return err
		}
	}
	for addr := range addrToAdd {
		if err = util.NewNetIPAddress(adapter.InterfaceIndex, addr); err != nil {
			return err
		}
	}

	return nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, containerID, netns, deviceID, ifName, nicType string) error {
	epName := hns.ConstructEndpointName(containerID, netns, util.HnsNetwork)[:12]
	// remove ovs port
	output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", epName)
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %s: %v, %q", epName, err, output)
	}

	return hns.RemoveHnsEndpoint(epName, netns, containerID)
}

func generateNicName(containerID, ifname string) (string, string) {
	if ifname == "eth0" {
		return fmt.Sprintf("%s_h", containerID[0:12]), fmt.Sprintf("%s_c", containerID[0:12])
	}
	return fmt.Sprintf("%s_%s_h", containerID[0:12-len(ifname)], ifname), fmt.Sprintf("%s_%s_c", containerID[0:12-len(ifname)], ifname)
}

func waitNetworkReady(nic, ipAddr, gateway string, underlayGateway, verbose bool, maxRetry int) error {
	ips := strings.Split(ipAddr, ",")
	for i, gw := range strings.Split(gateway, ",") {
		src := strings.Split(ips[i], "/")[0]
		if !underlayGateway || util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv6 {
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

	if err = configureNic(util.NodeNic, ip, macAddr, mtu); err != nil {
		return err
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
	// no need to check ovn0 on Windows
}

func (c *Controller) loopOvnExt0Check() {
	// no need to check ovnext0 on Windows
}

func configureMirrorLink(portName string, mtu int) error {
	adapter, err := util.GetNetAdapter(portName, false)
	if err != nil {
		klog.Errorf("failed to get network adapter %s: %v", portName, err)
		return err
	}

	if adapter.InterfaceAdminStatus != 1 {
		if err = util.EnableAdapter(portName); err != nil {
			klog.Error(err)
			return err
		}
	}

	interfaces, err := util.GetNetIPInterface(adapter.InterfaceIndex)
	if err != nil {
		klog.Errorf("failed to get NetIPInterface with index %d: %v", adapter.InterfaceIndex, err)
		return err
	}

	// set MTU
	for _, iface := range interfaces {
		if uint32(mtu) != iface.NlMtu {
			_mtu := uint32(mtu)
			if err = util.SetNetIPInterface(iface.InterfaceIndex, &iface.AddressFamily, &_mtu, nil, nil); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	return nil
}

func configProviderNic(nicName, brName string) (int, error) {
	// nothing to do on Windows
	return 0, nil
}

func removeProviderNic(nicName, brName string) error {
	// nothing to do on Windows
	return nil
}

func turnOffNicTxChecksum(nicName string) error {
	// TODO
	return nil
}

func getShortSharedDir(uid types.UID, volumeName string) string {
	// DPDK is not supported on Windows
	return ""
}
