package ovs

import (
	"github.com/digitalocean/go-openvswitch/ovs"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func AddOrUpdateU2OFilterOpenFlow(client *ovs.Client, bridgeName, gatewayIP, u2oIP, underlayNic string) error {
	isIPv6 := false
	if util.CheckProtocol(gatewayIP) == kubeovnv1.ProtocolIPv6 {
		isIPv6 = true
	}
	var match *ovs.MatchFlow
	var flow *ovs.Flow
	var inPortID int

	portInfo, err := client.OpenFlow.DumpPort(bridgeName, underlayNic)
	if err != nil {
		klog.Errorf("failed to dump bridge %s port %s: %v", bridgeName, underlayNic, err)
		return err
	}
	inPortID = portInfo.PortID
	klog.V(3).Infof(" underlayNic %s's portID is %d", underlayNic, inPortID)

	if isIPv6 {
		match = &ovs.MatchFlow{
			Protocol: ovs.ProtocolICMPv6,
			InPort:   inPortID,
			Matches: []ovs.Match{
				ovs.ICMP6Type(135),
				ovs.NeighborDiscoveryTarget(u2oIP),
			},
			Cookie: util.U2OFilterOpenFlowCookieV6,
		}
		// ovs-ofctl add-flow {underlay bridge} "cookie=0x1001,table=0,priority=10000,in_port=1,icmp6,icmp_type=135,nd_target={u2oIP},actions=drop"
		flow = &ovs.Flow{
			Priority: util.U2OFilterOpenFlowPriority,
			Protocol: ovs.ProtocolICMPv6,
			InPort:   inPortID,
			Matches: []ovs.Match{
				ovs.ICMP6Type(135),
				ovs.NeighborDiscoveryTarget(u2oIP),
			},
			Cookie:  util.U2OFilterOpenFlowCookieV6,
			Actions: []ovs.Action{ovs.Drop()},
		}
	} else {
		match = &ovs.MatchFlow{
			Protocol: ovs.ProtocolARP,
			InPort:   inPortID,
			Matches: []ovs.Match{
				ovs.ARPSourceProtocolAddress(gatewayIP),
				ovs.ARPTargetProtocolAddress(u2oIP),
				ovs.ARPOperation(1),
			},
			Cookie: util.U2OFilterOpenFlowCookieV4,
		}
		// ovs-ofctl add-flow {underlay bridge} "cookie=0x1000,table=0,priority=10000,in_port=1,arp,arp_spa={gatewayIP},arp_tpa={u2oIP},arp_op=1,actions=drop"
		flow = &ovs.Flow{
			Priority: util.U2OFilterOpenFlowPriority,
			Protocol: ovs.ProtocolARP,
			InPort:   inPortID,
			Matches: []ovs.Match{
				ovs.ARPSourceProtocolAddress(gatewayIP),
				ovs.ARPTargetProtocolAddress(u2oIP),
				ovs.ARPOperation(1), // ARP Request
			},
			Cookie:  util.U2OFilterOpenFlowCookieV4,
			Actions: []ovs.Action{ovs.Drop()},
		}
	}

	flows, err := client.OpenFlow.DumpFlowsWithFlowArgs(bridgeName, match)
	if err != nil {
		klog.Errorf("failed to dump flows: %v", err)
		return err
	}

	// check if the target flow already exist, if exist return
	if len(flows) > 0 {
		return nil
	}

	// check if any gc flow exist, if exist remove it
	if err := delU2OFilterOpenFlow(client, bridgeName, isIPv6); err != nil {
		return err
	}

	klog.Infof("add bridge %s u2o filter openflow rule", bridgeName)
	err = client.OpenFlow.AddFlow(bridgeName, flow)
	if err != nil {
		return err
	}

	return nil
}

func DeleteAllU2OFilterOpenFlow(client *ovs.Client, bridgeName, protocol string) error {
	if protocol == kubeovnv1.ProtocolIPv4 || protocol == kubeovnv1.ProtocolDual {
		if err := delU2OFilterOpenFlow(client, bridgeName, false); err != nil {
			return err
		}
	}
	if protocol == kubeovnv1.ProtocolIPv6 || protocol == kubeovnv1.ProtocolDual {
		if err := delU2OFilterOpenFlow(client, bridgeName, true); err != nil {
			return err
		}
	}
	return nil
}

func delU2OFilterOpenFlow(client *ovs.Client, bridgeName string, isV6 bool) error {
	cookie := util.U2OFilterOpenFlowCookieV4
	if isV6 {
		cookie = util.U2OFilterOpenFlowCookieV6
	}

	match := &ovs.MatchFlow{
		Cookie: uint64(cookie),
	}

	oldflows, err := client.OpenFlow.DumpFlowsWithFlowArgs(bridgeName, match)
	if err != nil {
		klog.Errorf("failed to dump flows: %v", err)
		return err
	}

	if len(oldflows) > 0 {
		klog.Infof("remove bridge %s old u2o filter openflow rule", bridgeName)
		err = client.OpenFlow.DelFlows(bridgeName, match)
		if err != nil {
			klog.Errorf("failed to remove old u2o filter openflow rule: %v", err)
			return err
		}
	}
	return nil
}
