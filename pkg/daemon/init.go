package daemon

import (
	"github.com/alauda/kube-ovn/pkg/util"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"time"
)

// InitNodeGateway init ovn0
func InitNodeGateway(config *Configuration) error {
	var portName, ipAddr, macAddr, gw string
	for {
		nodeName := config.NodeName
		node, err := config.KubeClient.CoreV1().Nodes().Get(nodeName, v1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get node %s info %v", nodeName, err)
			return err
		}
		macAddr = node.Annotations[util.MacAddressAnnotation]
		ipAddr = node.Annotations[util.IpAddressAnnotation]
		portName = node.Annotations[util.PortNameAnnotation]
		gw = node.Annotations[util.GatewayAnnotation]
		if macAddr == "" || ipAddr == "" || portName == "" || gw == "" {
			klog.Errorf("can not find macAddr, ipAddr, portName and gw, wait 3 seconds...")
			time.Sleep(3 * time.Second)
		} else {
			break
		}
	}
	return configureNodeNic(portName, ipAddr, macAddr, gw)
}
