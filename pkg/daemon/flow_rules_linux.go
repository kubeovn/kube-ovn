package daemon

import (
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const flowKindUnderlayService = "usvc"

func (c *Controller) AddOrUpdateUnderlaySubnetSvcLocalFlowCache(serviceIP string, port uint16, protocol, dstMac, underlayNic, bridgeName string) error {
	inPort, err := c.getPortID(bridgeName, underlayNic)
	if err != nil {
		return err
	}

	outPort, err := c.getPortID(bridgeName, "patch-localnet.")
	if err != nil {
		return err
	}

	isIPv6 := util.CheckProtocol(serviceIP) == kubeovnv1.ProtocolIPv6
	protoStr := ""
	switch strings.ToUpper(protocol) {
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
		return fmt.Errorf("unsupported protocol %s", protocol)
	}

	cookie := fmt.Sprintf("0x%x", util.UnderlaySvcLocalOpenFlowCookieV4)
	nwDst := "nw_dst"
	if isIPv6 {
		cookie = fmt.Sprintf("0x%x", util.UnderlaySvcLocalOpenFlowCookieV6)
		nwDst = "ipv6_dst"
	}

	flow := fmt.Sprintf("cookie=%s,priority=%d,in_port=%d,%s,%s=%s,tp_dst=%d "+
		"actions=mod_dl_dst:%s,output:%d",
		cookie, util.UnderlaySvcLocalOpenFlowPriority, inPort, protoStr, nwDst, serviceIP, port, dstMac, outPort)

	key := buildFlowKey(flowKindUnderlayService, serviceIP, port, protocol, "")
	c.setFlowCache(c.flowCache, bridgeName, key, []string{flow})

	klog.V(5).Infof("updated underlay flow cache for service %s", key)
	c.requestFlowSync()
	return nil
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

func (c *Controller) getPortID(bridgeName, portName string) (int, error) {
	if c.ovsClient == nil {
		return 0, fmt.Errorf("ovs client not initialized")
	}

	portInfo, err := c.ovsClient.OpenFlow.DumpPort(bridgeName, portName)
	if err != nil {
		return 0, fmt.Errorf("failed to dump port %s on bridge %s: %w", portName, bridgeName, err)
	}
	return portInfo.PortID, nil
}
