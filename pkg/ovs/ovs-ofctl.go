package ovs

import (
	"github.com/digitalocean/go-openvswitch/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"k8s.io/klog/v2"
)

func AddOrUpdateU2OFilterOpenFlow(client *ovs.Client, bridgeName, gatewayIP, u2oIP string) error {

	match := &ovs.MatchFlow{
		Protocol: ovs.ProtocolARP,
		InPort:   1,
		Matches: []ovs.Match{
			ovs.ARPSourceProtocolAddress(gatewayIP),
			ovs.ARPTargetProtocolAddress(u2oIP),
			ovs.ARPOperation(1), // ARP Request
		},
		Cookie: util.U2OFilterOpenFlowCookie,
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

	// check if old flow exist, if exist remove it
	if err := DelU2OFilterOpenFlow(client, bridgeName); err != nil {
		return err
	}

	// ovs-ofctl add-flow {underlay bridge } "table=0,priority=10000,in_port=1,arp,arp_spa={gatewayIP},arp_tpa={u2oIP},arp_op=1,actions=drop"
	flow := &ovs.Flow{
		Priority: util.U2OFilterOpenFlowPriority,
		Protocol: ovs.ProtocolARP,
		InPort:   1,
		Matches: []ovs.Match{
			ovs.ARPSourceProtocolAddress(gatewayIP),
			ovs.ARPTargetProtocolAddress(u2oIP),
			ovs.ARPOperation(1), // ARP Request
		},
		Cookie:  util.U2OFilterOpenFlowCookie,
		Actions: []ovs.Action{ovs.Drop()},
	}

	err = client.OpenFlow.AddFlow(bridgeName, flow)
	if err != nil {
		return err
	}

	return nil
}

func DelU2OFilterOpenFlow(client *ovs.Client, bridgeName string) error {

	// check if old flow exist, if exist remove it
	match := &ovs.MatchFlow{
		Protocol: ovs.ProtocolARP,
		InPort:   1,
		Matches: []ovs.Match{
			ovs.ARPOperation(1), // ARP Request
		},
		Cookie: util.U2OFilterOpenFlowCookie,
	}

	oldflows, err := client.OpenFlow.DumpFlowsWithFlowArgs(bridgeName, match)
	if err != nil {
		klog.Errorf("failed to dump flows: %v", err)
		return err
	}

	if len(oldflows) > 0 {
		klog.Infof("remove old u2o filter openflow rule")
		err = client.OpenFlow.DelFlows(bridgeName, match)
		if err != nil {
			klog.Errorf("failed to remove old u2o filter openflow rule: %v", err)
			return err
		}
	}
	return nil
}
