package daemon

import (
	"context"
	"fmt"
	"net"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// InitNodeGateway init ovn0
func InitNodeGateway(config *Configuration) error {
	var portName, ip, cidr, macAddr, gw, ipAddr string
	for {
		nodeName := config.NodeName
		node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get node %s info %v", nodeName, err)
			return err
		}
		if node.Annotations[util.IpAddressAnnotation] == "" {
			klog.Errorf("no ovn0 address for node %s, please check kube-ovn-controller logs", nodeName)
			time.Sleep(3 * time.Second)
			continue
		}
		if err := util.ValidatePodNetwork(node.Annotations); err != nil {
			klog.Errorf("validate node %s address annotation failed, %v", nodeName, err)
			time.Sleep(3 * time.Second)
			continue
		} else {
			macAddr = node.Annotations[util.MacAddressAnnotation]
			ip = node.Annotations[util.IpAddressAnnotation]
			cidr = node.Annotations[util.CidrAnnotation]
			portName = node.Annotations[util.PortNameAnnotation]
			gw = node.Annotations[util.GatewayAnnotation]
			break
		}
	}
	mac, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", mac, err)
	}

	ipAddr = util.GetIpAddrWithMask(ip, cidr)
	return configureNodeNic(portName, ipAddr, gw, mac, config.MTU)
}

func InitMirror(config *Configuration) error {
	if config.EnableMirror {
		return configureMirror(config.MirrorNic, config.MTU)
	} else {
		return removeMirror(config.MirrorNic)
	}

}

func InitVlan(config *Configuration) error {
	// TODO: move to flag validation
	if config.DefaultProviderName == "" {
		panic("provider should not be empty")
	}

	ifName := config.getInterfaceName()
	if ifName == "" {
		errMsg := fmt.Errorf("failed get host nic to add ovs %s", util.UnderlayBridge)
		klog.Error(errMsg)
		return errMsg
	}

	// create and configure external bridge
	brName := util.UnderlayBridge
	if err := configExternalBridge(config.DefaultProviderName, brName); err != nil {
		errMsg := fmt.Errorf("failed to create and configure external bridge %s: %v", brName, err)
		klog.Error(errMsg)
		return errMsg
	}

	// add host nic to the external bridge
	if err := configProviderNic(ifName, brName); err != nil {
		errMsg := fmt.Errorf("failed to add nic %s to external bridge %s: %v", ifName, brName, err)
		klog.Error(errMsg)
		return errMsg
	}

	return nil
}

//get host nic name
func (config *Configuration) getInterfaceName() (ifName string) {
	defer func() {
		if ifName == "" {
			return
		}
		iface, err := findInterface(ifName)
		if err != nil {
			klog.Errorf("failed to find iface %s, %v", ifName, err)
			ifName = ""
			return
		}
		ifName = iface.Name
	}()
	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err == nil {
		if interfaceName := node.GetLabels()[util.HostInterfaceName]; interfaceName != "" {
			return interfaceName
		}
	}

	if config.DefaultInterfaceName != "" {
		return config.DefaultInterfaceName
	}

	if config.Iface != "" {
		return config.Iface
	}

	return ""
}
