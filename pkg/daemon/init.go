package daemon

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
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
		return configureGlobalMirror(config.MirrorNic, config.MTU)
	}
	return configureEmptyMirror(config.MirrorNic, config.MTU)
}

func ovsInitProviderNetwork(provider, nic string) (int, error) {
	// create and configure external bridge
	brName := util.ExternalBridgeName(provider)
	if err := configExternalBridge(provider, brName, nic); err != nil {
		errMsg := fmt.Errorf("failed to create and configure external bridge %s: %v", brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// add host nic to the external bridge
	mtu, err := configProviderNic(nic, brName)
	if err != nil {
		errMsg := fmt.Errorf("failed to add nic %s to external bridge %s: %v", nic, brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	return mtu, nil
}

func ovsCleanProviderNetwork(provider string) error {
	output, err := ovs.Exec("list-br")
	if err != nil {
		return fmt.Errorf("failed to list OVS bridge %v: %q", err, output)
	}

	brName := util.ExternalBridgeName(provider)
	if !util.ContainsString(strings.Split(output, "\n"), brName) {
		return nil
	}

	// get host nic
	if output, err = ovs.Exec("list-ports", brName); err != nil {
		return fmt.Errorf("failed to list ports of OVS birdge %s, %v: %q", brName, err, output)
	}

	// remove host nic from the external bridge
	if output != "" {
		for _, nic := range strings.Split(output, "\n") {
			if err = removeProviderNic(nic, brName); err != nil {
				errMsg := fmt.Errorf("failed to remove nic %s from external bridge %s: %v", nic, brName, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
	}

	// remove external bridge
	if err = removeExternalBridge(provider, brName); err != nil {
		errMsg := fmt.Errorf("failed to remove external bridge %s: %v", brName, err)
		klog.Error(errMsg)
		return errMsg
	}

	return nil
}
