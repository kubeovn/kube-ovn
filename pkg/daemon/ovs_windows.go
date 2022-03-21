package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Microsoft/hcsshim"
	"github.com/containernetworking/plugins/pkg/hns"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// NetAdapter represents a network adapter on windows
type NetAdapter struct {
	Name                       string
	ElementName                string
	MacAddress                 string
	InterfaceIndex             uint32
	InterfaceAdminStatus       uint32
	InterfaceOperationalStatus uint32
}

// NetIPInterface represents a net IP interface on windows
type NetIPInterface struct {
	InterfaceIndex uint32
	AddressFamily  uint16
	NlMtu          uint32
	Forwarding     uint8
	Dhcp           uint8
}

// NetIPAddress represents a net IP address on windows
type NetIPAddress struct {
	InterfaceIndex uint32
	AddressFamily  uint16
	IPv6Address    string
	IPv4Address    string
	PrefixLength   uint8
}

// NetRoute represents a net route on windows
type NetRoute struct {
	InterfaceIndex    uint32
	AddressFamily     uint16
	DestinationPrefix string
	NextHop           string
}

func (csh cniServerHandler) configureNic(netName, podName, podNamespace, provider, netns, containerID, mac string, mtu int, ip, cidr, gateway string, isDefaultRoute bool, routes []request.Route, ingress, egress, priority string, gwCheckMode int) (string, error) {
	hnsNetwork, err := hcsshim.GetHNSNetworkByName(netName)
	if err != nil {
		klog.Errorf("failed to get HNS network %s: %v", netName)
		return "", err
	}
	if hnsNetwork == nil {
		err = fmt.Errorf("HNS network %s does not exist", netName)
		klog.Error(err)
		return "", err
	}
	if !strings.EqualFold(hnsNetwork.Type, "Transparent") {
		err = fmt.Errorf(`type of HNS network %s is "%s", while "Transparent" is required`, netName, hnsNetwork.Type)
		klog.Error(err)
		return "", err
	}

	// sanbox := hns.GetSandboxContainerID(containerID, netns)
	epName := hns.ConstructEndpointName(containerID, netns, netName)
	hnsEndpoint, err := hns.AddHnsEndpoint(epName, hnsNetwork.Id, containerID, netns, func() (*hcsshim.HNSEndpoint, error) {
		// result.DNS = n.GetDNS()
		// if n.LoopbackDSR {
		// 	n.ApplyLoopbackDSRPolicy(&ipAddr)
		// }
		ipv4, ipv6 := util.SplitStringIP(ip)
		endpoint := &hcsshim.HNSEndpoint{
			Name:           epName,
			VirtualNetwork: hnsNetwork.Id,
			// DNSServerList:  strings.Join(result.DNS.Nameservers, ","),
			// DNSSuffix:      strings.Join(result.DNS.Search, ","),
			MacAddress:  mac,
			IPAddress:   net.ParseIP(ipv4),
			IPv6Address: net.ParseIP(ipv6),
			// Policies:       n.GetHNSEndpointPolicies(),
		}

		endpoint.GatewayAddress, endpoint.GatewayAddressV6 = util.SplitStringIP(gateway)
		for _, s := range strings.Split(cidr, ",") {
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
		return "", err
	}

	// result, err := hns.ConstructHnsResult(hnsNetwork, hnsEndpoint)
	// if err != nil {
	// 	klog.Errorf("failed to construct HNS result: %v", err)
	// 	return err
	// }

	portName := hnsEndpoint.Name
	ifaceID := ovs.PodNameToPortName(podName, podNamespace, provider)
	ovs.CleanDuplicatePort(ifaceID)
	output, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
		"set", "interface", portName, fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:pod_name=%s", podName),
		fmt.Sprintf("external_ids:pod_namespace=%s", podNamespace),
		fmt.Sprintf("external_ids:ip=%s", ip),
		fmt.Sprintf("external_ids:pod_netns=%s", netns))
	if err != nil {
		return "", fmt.Errorf("failed to add OVS port %s, %v: %q", portName, err, output)
	}

	if err = ovs.SetInterfaceBandwidth(podName, podNamespace, ifaceID, egress, ingress, priority); err != nil {
		return "", err
	}

	go func() {
		for i := 0; i < 10; i++ {
			_, err := getNetAdapter(fmt.Sprintf("vEthernet (%s)", portName))
			if err != nil {
				// klog.Error(err)
				time.Sleep(time.Second)
				continue
			}

			klog.Infof("setting interface %s type to internal", portName)
			output, err := ovs.Exec("set", "interface", portName, "type=internal")
			if err != nil {
				klog.Errorf("failed to set type of interface %s to internal, %v: %q", portName, err, output)
			}
			return
		}
	}()

	return portName, nil
}

func (csh cniServerHandler) deleteNic(podName, podNamespace, netName, netns, containerID string) error {
	epName := hns.ConstructEndpointName(containerID, netns, "kube-ovn")
	// Remove ovs port
	output, err := ovs.Exec(ovs.IfExists, "del-port", "br-int", epName)
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %s:  %v, %q", epName, err, output)
	}

	klog.Infof("removing hns endpoint %s", epName)
	return hns.RemoveHnsEndpoint(epName, netns, containerID)
}

func waitNetworkReady(nic, ipAddr, gateway string, underlayGateway, verbose bool) error {
	// TODO
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

	// if err = configureNic(util.NodeNic, ip, macAddr, mtu); err != nil {
	// 	return err
	// }

	return nil
}

// If OVS restart, the ovn0 port will down and prevent host to pod network,
// Restart the kube-ovn-cni when this happens
func (c *Controller) loopOvn0Check() {
	// node, err := c.nodesLister.Get(c.config.NodeName)
	// if err != nil {
	// 	klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
	// 	return
	// }
	// ip := node.Annotations[util.IpAddressAnnotation]
	// gw := node.Annotations[util.GatewayAnnotation]
	// if err := waitNetworkReady(util.NodeNic, ip, gw, false, false); err != nil {
	// 	klog.Fatalf("failed to ping ovn0 gw: %s, %v", gw, err)
	// }
}

func configureMirrorLink(portName string, mtu int) error {
	// mirrorLink, err := netlink.LinkByName(portName)
	// if err != nil {
	// 	return fmt.Errorf("can not find mirror nic %s: %v", portName, err)
	// }

	// if err = netlink.LinkSetMTU(mirrorLink, mtu); err != nil {
	// 	return fmt.Errorf("can not set mirror nic mtu: %v", err)
	// }

	// if mirrorLink.Attrs().OperState != netlink.OperUp {
	// 	if err = netlink.LinkSetUp(mirrorLink); err != nil {
	// 		return fmt.Errorf("can not set mirror nic %s up: %v", portName, err)
	// 	}
	// }

	return nil
}

func configureNic(name, ip string, mac net.HardwareAddr, mtu int) error {
	adapter, err := getNetAdapter(name)
	if err != nil {
		klog.Errorf("failed to get network adapter %s: %v", name, err)
		return err
	}

	if adapter.InterfaceAdminStatus != 1 {
		if err = enableAdapter(name); err != nil {
			klog.Error(err)
			return err
		}
	}

	interfaces, err := getNetIPInterface(adapter.InterfaceIndex)
	if err != nil {
		klog.Errorf("failed to get NetIPInterface with index %d: %v", adapter.InterfaceIndex, err)
		return err
	}
	addresses, err := getNetIPAddress(adapter.InterfaceIndex)
	if err != nil {
		klog.Errorf("failed to get NetIPAddress with index %d: %v", adapter.InterfaceIndex, err)
		return err
	}

	if newMac := mac.String(); !strings.EqualFold(newMac, adapter.MacAddress) {
		if err = setAdapterMac(name, mac.String()); err != nil {
			klog.Error(err)
			return err
		}
	}

	for _, iface := range interfaces {
		if uint32(mtu) != iface.NlMtu {
			_mtu := uint32(mtu)
			if err = setNetIPInterface(iface.InterfaceIndex, &iface.AddressFamily, &_mtu, nil, nil); err != nil {
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
		var s string
		if addr.IPv4Address != "" {
			s = fmt.Sprintf("%s/%d", addr.IPv4Address, addr.PrefixLength)
		} else {
			s = fmt.Sprintf("%s/%d", addr.IPv6Address, addr.PrefixLength)
		}
		if _, ok := addrToAdd[s]; ok {
			delete(addrToAdd, s)
		} else {
			addrToDel[s] = true
		}
	}

	for addr := range addrToDel {
		if err = removeNetIPAddress(adapter.InterfaceIndex, addr); err != nil {
			return err
		}
	}
	for addr := range addrToAdd {
		if err = newNetIPAddress(adapter.InterfaceIndex, addr); err != nil {
			return err
		}
	}

	return nil
}

// Add host nic to external bridge
// Mac address, MTU, IP addresses & routes will be copied/transferred to the external bridge
func configProviderNic(nicName, brName string) (int, error) {
	return 0, nil
}

// Remove host nic from external bridge
// IP addresses & routes will be transferred to the host nic
func removeProviderNic(nicName, brName string) error {
	return nil
}

func bool2PsParam(v bool) string {
	if v {
		return "Enabled"
	}
	return "Disabled"
}

func getNetAdapter(name string) (*NetAdapter, error) {
	output, err := util.Powershell(fmt.Sprintf(`Get-NetAdapter -Name "%s" | ConvertTo-Json`, name))
	if err != nil {
		err2 := fmt.Errorf("failed to get network adapter %s: %v", name, err)
		klog.Error(err2)
		return nil, err2
	}

	adapter := &NetAdapter{}
	if err = json.Unmarshal([]byte(output), adapter); err != nil {
		err2 := fmt.Errorf("failed to parse information of network adapter %s: %v", name, err)
		klog.Error(err2)
		return nil, err2
	}

	adapter.MacAddress = strings.ReplaceAll(adapter.MacAddress, "-", ":")
	return adapter, nil
}

func getNetIPInterface(ifIndex uint32) ([]NetIPInterface, error) {
	output, err := util.Powershell(fmt.Sprintf("Get-NetIPInterface -InterfaceIndex %d | ConvertTo-Json", ifIndex))
	if err != nil {
		err2 := fmt.Errorf("failed to get NetIPInterface with index %d: %v", ifIndex, err)
		klog.Error(err2)
		return nil, err2
	}

	result := make([]NetIPInterface, 0, 2)
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetIPInterface: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	return result, nil
}

func getNetIPAddress(ifIndex uint32) ([]NetIPAddress, error) {
	output, err := util.Powershell(fmt.Sprintf("Get-NetIPAddress -InterfaceIndex %d | ConvertTo-Json", ifIndex))
	if err != nil {
		err2 := fmt.Errorf("failed to get NetIPAddress with index %d: %v", ifIndex, err)
		klog.Error(err2)
		return nil, err2
	}

	if output[0] == '{' {
		output = fmt.Sprintf("[%s]", output)
	}

	result := make([]NetIPAddress, 0, 2)
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetIPAddress: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	return result, nil
}

func getNetRoute(ifIndex uint32) ([]NetRoute, error) {
	output, err := util.Powershell(fmt.Sprintf("Get-NetRoute -InterfaceIndex %d | ConvertTo-Json", ifIndex))
	if err != nil {
		err2 := fmt.Errorf("failed to get NetRoute with index %d: %v", ifIndex, err)
		klog.Error(err2)
		return nil, err2
	}

	result := make([]NetRoute, 0, 2)
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		err2 := fmt.Errorf("failed to parse information of NetRoute: %v", err)
		klog.Error(err2)
		return nil, err2
	}

	return result, nil
}

func setAdapterMac(adapter, mac string) error {
	_, err := util.Powershell(fmt.Sprintf(`Set-NetAdapter -Name "%s" -MacAddress %s -Confirm:$False`, adapter, mac))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set MAC address of network adapter %s: %v", adapter, err)
	}
	return nil
}

func setNetIPInterface(ifIndex uint32, addressFamily *uint16, mtu *uint32, dhcp, forwarding *bool) error {
	parameters := make([]string, 0)
	if addressFamily != nil {
		parameters = append(parameters, fmt.Sprintf("-AddressFamily %d", *addressFamily))
	}
	if mtu != nil {
		parameters = append(parameters, fmt.Sprintf("-NlMtuBytes %d", *mtu))
	}
	if dhcp != nil {
		parameters = append(parameters, fmt.Sprintf("-Dhcp %s", bool2PsParam(*dhcp)))
	}
	if forwarding != nil {
		parameters = append(parameters, fmt.Sprintf("-Forwading %s", bool2PsParam(*forwarding)))
	}

	_, err := util.Powershell(fmt.Sprintf("Set-NetIPInterface -InterfaceIndex %d %s -Confirm:$False", ifIndex, strings.Join(parameters, " ")))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set NetIPInterface with index %d: %v", ifIndex, err)
	}

	return nil
}

func enableAdapter(adapter string) error {
	_, err := util.Powershell(fmt.Sprintf(`Enable-NetAdapter -Name "%s" -Confirm:$False`, adapter))
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to enable network adapter %s: %v", adapter, err)
	}
	return nil
}

func newNetIPAddress(ifIndex uint32, ipAddr string) error {
	fields := strings.Split(ipAddr, "/")
	cmd := fmt.Sprintf("New-NetIPAddress -InterfaceIndex %d -IPAddress %s -PrefixLength %s -PolicyStore ActiveStore -Confirm:$False", ifIndex, fields[0], fields[1])
	_, err := util.Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to add IP address %s to interface with index %d: %v", ipAddr, ifIndex, err)
	}
	return nil
}

func removeNetIPAddress(ifIndex uint32, ipAddr string) error {
	fields := strings.Split(ipAddr, "/")
	cmd := fmt.Sprintf("Remove-NetIPAddress -InterfaceIndex %d -IPAddress %s -PrefixLength %s -Confirm:$False", ifIndex, fields[0], fields[1])
	_, err := util.Powershell(cmd)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to remove IP address %s from interface with index %d: %v", ipAddr, ifIndex, err)
	}
	return nil
}
