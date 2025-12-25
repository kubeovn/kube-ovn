package ovs

import (
	"fmt"
	"net"
	"slices"

	"github.com/digitalocean/go-openvswitch/ovs"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

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
	case string(corev1.ProtocolTCP):
		protocolType = ovs.ProtocolTCPv4
		if isIPv6 {
			protocolType = ovs.ProtocolTCPv6
		}
	case string(corev1.ProtocolUDP):
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
	case string(corev1.ProtocolTCP):
		protocolType = ovs.ProtocolTCPv4
		if isIPv6 {
			protocolType = ovs.ProtocolTCPv6
		}
	case string(corev1.ProtocolUDP):
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

// ClearU2OFlows clears obsolete U2O flows
func ClearU2OFlows(client *ovs.Client) error {
	bridges, err := Bridges()
	if err != nil {
		klog.Errorf("failed to get ovs bridges: %v", err)
		return err
	}

	for bridge := range slices.Values(bridges) {
		flows, err := client.OpenFlow.DumpFlows(bridge)
		if err != nil {
			klog.Errorf("failed to dump flows on bridge %s: %v", bridge, err)
			return err
		}

		for flow := range slices.Values(flows) {
			if flow.Priority != util.U2OKeepSrcMacPriority {
				continue
			}

			klog.Infof("deleting obsolete U2O keep src mac flow from bridge %s: %+v", bridge, flow)
			if err = client.OpenFlow.DelFlows(bridge, &ovs.MatchFlow{
				Protocol: flow.Protocol,
				InPort:   flow.InPort,
				Matches:  flow.Matches,
				Table:    flow.Table,
				Cookie:   flow.Cookie,
			}); err != nil {
				klog.Errorf("failed to delete obsolete U2O keep src mac flow from bridge %s: %v", bridge, err)
				return err
			}
		}
	}

	return nil
}
