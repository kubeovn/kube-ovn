package ovs

import (
	"fmt"
	"net"
	"strconv"

	ovs "github.com/digitalocean/go-openvswitch/ovs"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func getOfportByPeer(peer string) (int, error) {
	// ovs-vsctl find interface options:peer="<peer>"
	ifaceNames, err := ovsFind("Interface", "name", fmt.Sprintf(`options:peer="%s"`, peer))
	if err != nil {
		return -1, fmt.Errorf("failed to find interface with peer %s: %w", peer, err)
	}
	if len(ifaceNames) == 0 {
		return -1, fmt.Errorf("no interface found with peer %s", peer)
	}
	if len(ifaceNames) > 1 {
		// This should not happen for patch ports
		klog.Warningf("multiple interfaces found with peer %s: %v", peer, ifaceNames)
	}
	ifaceName := ifaceNames[0]

	ofportStr, err := Get("Interface", ifaceName, "ofport", "", false)
	if err != nil {
		return -1, fmt.Errorf("failed to get ofport for interface %s: %w", ifaceName, err)
	}

	return strconv.Atoi(ofportStr)
}

func AddOrUpdateUnderlaySubnetSvcLocalOpenFlow(client *ovs.Client, bridgeName, lbServiceIP, protocol, dstMAC, underlayNic string, lbServicePort uint16) error {
	isIPv6 := util.CheckProtocol(lbServiceIP) == kubeovnv1.ProtocolIPv6
	var inPortID, outPortID int
	var lrpMacAddr net.HardwareAddr
	var err error
	var cookie uint64

	portInfo, err := client.OpenFlow.DumpPort(bridgeName, underlayNic)
	if err != nil {
		klog.Errorf("failed to dump bridge %s port %s: %v", bridgeName, underlayNic, err)
		return err
	}
	inPortID = portInfo.PortID
	klog.V(3).Infof("underlayNic %s's portID is %d", underlayNic, inPortID)

	portInfo, err = client.OpenFlow.DumpPort(bridgeName, "patch-localnet.")
	if err != nil {
		klog.Errorf("failed to dump bridge %s port %s: %v", bridgeName, "patch-localnet.", err)
		return err
	}
	outPortID = portInfo.PortID

	lrpMacAddr, err = net.ParseMAC(dstMAC)
	if err != nil {
		klog.Errorf("failed to parse MAC address %s: %v", dstMAC, err)
		return err
	}

	cookie = util.UnderlaySvcLocalOpenFlowCookieV4
	if isIPv6 {
		cookie = util.UnderlaySvcLocalOpenFlowCookieV6
	}

	var protocolType ovs.Protocol
	switch protocol {
	case string(v1.ProtocolTCP):
		protocolType = ovs.ProtocolTCPv4
		if isIPv6 {
			protocolType = ovs.ProtocolTCPv6
		}
	case string(v1.ProtocolUDP):
		protocolType = ovs.ProtocolUDPv4
		if isIPv6 {
			protocolType = ovs.ProtocolUDPv6
		}
	default:
		return fmt.Errorf("unsupported protocol %s", protocol)
	}

	flow := &ovs.Flow{
		Priority: util.UnderlaySvcLocalOpenFlowPriority,
		Protocol: protocolType,
		InPort:   inPortID,
		Actions:  []ovs.Action{ovs.ModDataLinkDestination(lrpMacAddr), ovs.Output(outPortID)},
		Matches: []ovs.Match{
			ovs.NetworkDestination(lbServiceIP),
			ovs.TransportDestinationMaskedPort(lbServicePort, 0xffff),
		},
		Cookie: cookie,
	}

	klog.Infof("add bridge %s svc local policy openflow rule", bridgeName)
	err = client.OpenFlow.AddFlow(bridgeName, flow)
	if err != nil {
		return err
	}

	return nil
}

func DeleteUnderlaySubnetSvcLocalOpenFlow(client *ovs.Client, bridgeName, lbServiceIP, protocol, underlayNic string, lbServicePort uint16) error {
	isIPv6 := util.CheckProtocol(lbServiceIP) == kubeovnv1.ProtocolIPv6
	var inPortID int
	var cookie uint64

	cookie = util.UnderlaySvcLocalOpenFlowCookieV4
	if isIPv6 {
		cookie = util.UnderlaySvcLocalOpenFlowCookieV6
	}

	var protocolType ovs.Protocol
	switch protocol {
	case string(v1.ProtocolTCP):
		protocolType = ovs.ProtocolTCPv4
		if isIPv6 {
			protocolType = ovs.ProtocolTCPv6
		}
	case string(v1.ProtocolUDP):
		protocolType = ovs.ProtocolUDPv4
		if isIPv6 {
			protocolType = ovs.ProtocolUDPv6
		}
	default:
		return fmt.Errorf("unsupported protocol %s", protocol)
	}

	portInfo, err := client.OpenFlow.DumpPort(bridgeName, underlayNic)
	if err != nil {
		klog.Errorf("failed to dump bridge %s port %s: %v", bridgeName, underlayNic, err)
		return err
	}
	inPortID = portInfo.PortID
	klog.V(3).Infof("underlayNic %s's portID is %d", underlayNic, inPortID)

	match := &ovs.MatchFlow{
		Protocol: protocolType,
		InPort:   inPortID,
		Matches: []ovs.Match{
			ovs.NetworkDestination(lbServiceIP),
			ovs.TransportDestinationMaskedPort(lbServicePort, 0xffff),
		},
		Cookie: cookie,
	}

	oldflows, err := client.OpenFlow.DumpFlowsWithFlowArgs(bridgeName, match)
	if err != nil {
		klog.Errorf("failed to dump flows: %v", err)
		return err
	}

	if len(oldflows) > 0 {
		klog.Infof("remove bridge %s old svc local policy openflow rule", bridgeName)
		err = client.OpenFlow.DelFlows(bridgeName, match)
		if err != nil {
			klog.Errorf("failed to remove old svc local policy openflow rule: %v", err)
			return err
		}
	}
	return nil
}

func AddOrUpdateU2OKeepSrcMac(client *ovs.Client, bridgeName, podIP, podMac, chassisMac, subnetName string) error {
	var localnetPatchPortID int
	var podMacAddr net.HardwareAddr
	var err error
	var matchs []ovs.Match
	var protocolType ovs.Protocol
	isIPv6 := util.CheckProtocol(podIP) == kubeovnv1.ProtocolIPv6

	peer := fmt.Sprintf("patch-br-int-to-localnet.%s", subnetName)
	localnetPatchPortID, err = getOfportByPeer(peer)
	if err != nil {
		klog.Errorf("failed to get ofport for peer %s: %v", peer, err)
		return err
	}

	podMacAddr, err = net.ParseMAC(podMac)
	if err != nil {
		klog.Errorf("failed to parse MAC address %s: %v", podMac, err)
		return err
	}

	if isIPv6 {
		protocolType = ovs.ProtocolIPv6
		matchs = []ovs.Match{
			ovs.IPv6Source(podIP),
			ovs.DataLinkSource(chassisMac),
		}
	} else {
		protocolType = ovs.ProtocolIPv4
		matchs = []ovs.Match{
			ovs.NetworkSource(podIP),
			ovs.DataLinkSource(chassisMac),
		}
	}

	matchFlow := &ovs.MatchFlow{
		InPort:   localnetPatchPortID,
		Protocol: protocolType,
		Matches:  matchs,
	}

	flows, err := client.OpenFlow.DumpFlowsWithFlowArgs(bridgeName, matchFlow)
	if err != nil {
		klog.Errorf("failed to dump flows from bridge %s: %v", bridgeName, err)
		return err
	}

	expectedAction := fmt.Sprintf("mod_dl_src:%s", podMac)
	for _, flow := range flows {
		if flow.Priority != util.U2OKeepSrcMacPriority || flow.InPort != localnetPatchPortID {
			continue
		}

		for _, action := range flow.Actions {
			if actionText, _ := action.MarshalText(); string(actionText) == expectedAction {
				klog.V(3).Infof("flow already exists in bridge %s for in_port=%d, ip_src=%s, dl_src=%s, actions=mod_dl_src:%s",
					bridgeName, localnetPatchPortID, podIP, chassisMac, podMac)
				return nil
			}
		}
	}

	flow := &ovs.Flow{
		Priority: util.U2OKeepSrcMacPriority,
		InPort:   localnetPatchPortID,
		Protocol: protocolType,
		Actions: []ovs.Action{
			ovs.ModDataLinkSource(podMacAddr),
			ovs.Normal(),
		},
		Matches: matchs,
	}

	klog.V(3).Infof("adding flow to bridge %s: in_port=%d, ip_src=%s, dl_src=%s, actions=mod_dl_src:%s,normal",
		bridgeName, localnetPatchPortID, podIP, chassisMac, podMac)

	return client.OpenFlow.AddFlow(bridgeName, flow)
}

func DeleteU2OKeepSrcMac(client *ovs.Client, bridgeName, podIP, chassisMac, subnetName string) error {
	var localnetPatchPortID int
	var err error
	var matchs []ovs.Match
	var protocolType ovs.Protocol
	isIPv6 := util.CheckProtocol(podIP) == kubeovnv1.ProtocolIPv6

	peer := fmt.Sprintf("patch-br-int-to-localnet.%s", subnetName)
	localnetPatchPortID, err = getOfportByPeer(peer)
	if err != nil {
		klog.Errorf("failed to get ofport for peer %s: %v", peer, err)
		return err
	}

	if isIPv6 {
		protocolType = ovs.ProtocolIPv6
		matchs = []ovs.Match{
			ovs.IPv6Source(podIP),
			ovs.DataLinkSource(chassisMac),
		}
	} else {
		protocolType = ovs.ProtocolIPv4
		matchs = []ovs.Match{
			ovs.NetworkSource(podIP),
			ovs.DataLinkSource(chassisMac),
		}
	}

	matchFlow := &ovs.MatchFlow{
		InPort:   localnetPatchPortID,
		Protocol: protocolType,
		Matches:  matchs,
	}

	klog.Infof("deleting flow from bridge %s: in_port=%d, ip_src=%s, dl_src=%s",
		bridgeName, localnetPatchPortID, podIP, chassisMac)

	return client.OpenFlow.DelFlows(bridgeName, matchFlow)
}
