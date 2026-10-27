package daemon

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const flowKindUnderlayService = "usvc"

func (c *Controller) AddOrUpdateUnderlaySubnetSvcLocalFlowCache(serviceIP string, port uint16, protocol, dstMac, underlayNic, bridgeName, subnetName string) error {
	rule := LbServiceRules{
		IP: serviceIP, Port: port, Protocol: protocol, DstMac: dstMac,
		UnderlayNic: underlayNic, BridgeName: bridgeName, SubnetName: subnetName,
	}
	if _, err := rule.flows(c.getPortID); err != nil {
		if errors.Is(err, errUnderlayLocalnetPatchNotReady) {
			klog.Infof("patch-localnet port for subnet %s not found on bridge %s, retrying underlay service flow for %s:%d", subnetName, bridgeName, serviceIP, port)
		}
		return err
	}

	key := buildFlowKey(flowKindUnderlayService, serviceIP, port, protocol, "")
	c.setFlowCache(c.flowCache, bridgeName, key, rule)

	klog.V(5).Infof("updated underlay flow cache for service %s", key)
	c.requestFlowSync()
	return nil
}

func (rule LbServiceRules) flows(getPortID func(string) (int, error)) ([]string, error) {
	inPort, err := getPortID(rule.UnderlayNic)
	if err != nil {
		return nil, err
	}

	patchPortName := fmt.Sprintf("patch-localnet.%s-to-br-int", rule.SubnetName)
	outPort, err := getPortID(patchPortName)
	if err != nil {
		return nil, fmt.Errorf("install underlay service flow for %s:%d on %s: %w: %w", rule.IP, rule.Port, patchPortName, errUnderlayLocalnetPatchNotReady, err)
	}

	isIPv6 := util.CheckProtocol(rule.IP) == kubeovnv1.ProtocolIPv6
	protoStr := ""
	switch strings.ToUpper(rule.Protocol) {
	case "TCP":
		protoStr = "tcp"
		if isIPv6 {
			protoStr = "tcp6"
		}
	case "UDP":
		protoStr = "udp"
		if isIPv6 {
			protoStr = "udp6"
		}
	default:
		return nil, fmt.Errorf("unsupported protocol %s", rule.Protocol)
	}

	cookie := fmt.Sprintf("0x%x", util.UnderlaySvcLocalOpenFlowCookieV4)
	nwDst := "nw_dst"
	if isIPv6 {
		cookie = fmt.Sprintf("0x%x", util.UnderlaySvcLocalOpenFlowCookieV6)
		nwDst = "ipv6_dst"
	}
	return underlayServiceLocalFlows(cookie, util.UnderlaySvcLocalOpenFlowPriority, inPort, outPort, protoStr, nwDst, rule.IP, rule.DstMac, rule.Port), nil
}

func (c *Controller) deleteUnderlaySubnetSvcLocalFlowCache(bridgeName, serviceIP string, port uint16, protocol string) {
	key := buildFlowKey(flowKindUnderlayService, serviceIP, port, protocol, "")

	c.deleteFlowCache(c.flowCache, bridgeName, key)

	klog.V(5).Infof("deleted underlay flow cache for service %s", key)
	c.requestFlowSync()
}

func buildFlowKey(kind, ip string, port uint16, protocol, extra string) string {
	if extra == "" {
		return fmt.Sprintf("%s-%s-%s-%d", kind, ip, protocol, port)
	}
	return fmt.Sprintf("%s-%s-%s-%d-%s", kind, ip, protocol, port, extra)
}

func (c *Controller) getPortID(portName string) (int, error) {
	ofportStr, err := ovs.Get("Interface", portName, "ofport", "", true)
	if err != nil {
		return 0, fmt.Errorf("failed to get ofport for interface %s: %w", portName, err)
	}

	portID, err := strconv.Atoi(strings.TrimSpace(ofportStr))
	if err != nil {
		return 0, fmt.Errorf("failed to parse ofport %q: %w", ofportStr, err)
	}

	return portID, nil
}
