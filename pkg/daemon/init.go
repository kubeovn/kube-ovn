package daemon

import (
	"fmt"
	"net"
	"strings"
	"time"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// InitNodeGateway init ovn0
func InitNodeGateway(config *Configuration) error {
	var portName, ip, cidr, macAddr, gw, ipAddr string
	for {
		nodeName := config.NodeName
		node, err := config.KubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
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
			ipAddr = fmt.Sprintf("%s/%s", ip, strings.Split(cidr, "/")[1])
			break
		}
	}
	mac, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", mac, err)
	}

	if util.CheckProtocol(cidr) == kubeovnv1.ProtocolDual {
		// The format of ip/cidr/gw in dualstack is "10.244.0.0/24,fd00:10:244::/80"
		cidrBlocks := strings.Split(cidr, ",")
		ips := strings.Split(ip, ",")

		v4IP := fmt.Sprintf("%s/%s", ips[0], strings.Split(cidrBlocks[0], "/")[1])
		v6IP := fmt.Sprintf("%s/%s", ips[1], strings.Split(cidrBlocks[1], "/")[1])
		ipAddr = v4IP + "," + v6IP
		if err := configureNodeNic(portName, ipAddr, gw, mac, config.MTU); err != nil {
			return err
		}
	} else {
		return configureNodeNic(portName, ipAddr, gw, mac, config.MTU)
	}

	return nil
}

func InitMirror(config *Configuration) error {
	if config.EnableMirror {
		return configureMirror(config.MirrorNic, config.MTU)
	} else {
		return removeMirror(config.MirrorNic)
	}

}

func InitVlan(config *Configuration) error {

	if util.IsProviderVlan(config.NetworkType, config.DefaultProviderName) {
		//create patch port
		exists, err := providerBridgeExists()
		if err != nil {
			errMsg := fmt.Errorf("check provider bridge exists failed, %v", err)
			klog.Error(errMsg)
			return err
		}

		if !exists {
			//create br-provider
			if err = configProviderPort(config.DefaultProviderName); err != nil {
				errMsg := fmt.Errorf("configure patch port br-provider failed %v", err)
				klog.Error(errMsg)
				return errMsg
			}

			//add a host nic to br-provider
			ifName := config.getInterfaceName()
			if ifName == "" {
				errMsg := fmt.Errorf("failed get host nic to add ovs br-provider")
				klog.Error(errMsg)
				return errMsg
			}

			if err = configProviderNic(ifName); err != nil {
				errMsg := fmt.Errorf("add nic %s to port br-provider failed %v", ifName, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
	}
	return nil
}

//get host nic name
func (config *Configuration) getInterfaceName() string {
	var interfaceName string

	node, err := config.KubeClient.CoreV1().Nodes().Get(config.NodeName, metav1.GetOptions{})
	if err == nil {
		labels := node.GetLabels()
		interfaceName = labels[util.HostInterfaceName]
	}

	if interfaceName != "" {
		return interfaceName
	}

	if config.DefaultInterfaceName != "" {
		return config.DefaultInterfaceName
	}

	if config.Iface != "" {
		return config.Iface
	}

	return ""
}
