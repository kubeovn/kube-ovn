package daemon

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// InitOVSBridges initializes OVS bridges
func InitOVSBridges() (map[string]string, error) {
	bridges, err := ovs.Bridges()
	if err != nil {
		return nil, err
	}

	mappings := make(map[string]string)
	for _, brName := range bridges {
		if err = util.SetLinkUp(brName); err != nil {
			klog.Error(err)
			return nil, err
		}

		output, err := ovs.Exec("list-ports", brName)
		if err != nil {
			return nil, fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
		}

		if output != "" {
			for _, port := range strings.Split(output, "\n") {
				ok, err := ovs.ValidatePortVendor(port)
				if err != nil {
					return nil, fmt.Errorf("failed to check vendor of port %s: %v", port, err)
				}
				if ok {
					if _, err = configProviderNic(port, brName); err != nil {
						return nil, err
					}
					mappings[port] = brName
				}
			}
		}
	}

	return mappings, nil
}

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
			klog.Warningf("no ovn0 address for node %s, please check kube-ovn-controller logs", nodeName)
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

func ovsInitProviderNetwork(provider, nic string, exchangeLinkName, macLearningFallback bool) (int, error) {
	// create and configure external bridge
	brName := util.ExternalBridgeName(provider)
	if exchangeLinkName {
		exchanged, err := changeProvideNicName(nic, brName)
		if err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", nic, brName, err)
			return 0, err
		}
		if exchanged {
			nic, brName = brName, nic
		}
	}

	klog.V(3).Infof("configure external bridge %s", brName)
	if err := configExternalBridge(provider, brName, nic, exchangeLinkName, macLearningFallback); err != nil {
		errMsg := fmt.Errorf("failed to create and configure external bridge %s: %v", brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// init provider chassis mac
	if err := initProviderChassisMac(provider); err != nil {
		errMsg := fmt.Errorf("failed to init chassis mac for provider %s, %v", provider, err)
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
	output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
	if err != nil {
		return fmt.Errorf("failed to get ovn-bridge-mappings, %v: %q", err, output)
	}

	var idx int
	var m, brName string
	mappingPrefix := provider + ":"
	brMappings := strings.Split(output, ",")
	for idx, m = range brMappings {
		if strings.HasPrefix(m, mappingPrefix) {
			brName = m[len(mappingPrefix):]
			klog.V(3).Infof("found bridge name for provider %s: %s", provider, brName)
			break
		}
	}

	if idx != len(brMappings) {
		brMappings = append(brMappings[:idx], brMappings[idx+1:]...)
		if len(brMappings) == 0 {
			output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", "ovn-bridge-mappings")
		} else {
			output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-bridge-mappings="+strings.Join(brMappings, ","))
		}
		if err != nil {
			return fmt.Errorf("failed to set ovn-bridge-mappings, %v: %q", err, output)
		}
	}

	if output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-chassis-mac-mappings"); err != nil {
		return fmt.Errorf("failed to get ovn-chassis-mac-mappings, %v: %q", err, output)
	}
	macMappings := strings.Split(output, ",")
	for _, macMap := range macMappings {
		if strings.HasPrefix(macMap, mappingPrefix) {
			macMappings = util.RemoveString(macMappings, macMap)
			break
		}
	}
	if len(macMappings) == 0 {
		output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", "ovn-chassis-mac-mappings")
	} else {
		output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-chassis-mac-mappings="+strings.Join(macMappings, ","))
	}
	if err != nil {
		return fmt.Errorf("failed to set ovn-chassis-mac-mappings, %v: %q", err, output)
	}

	if output, err = ovs.Exec("list-br"); err != nil {
		return fmt.Errorf("failed to list OVS bridge %v: %q", err, output)
	}

	if !util.ContainsString(strings.Split(output, "\n"), brName) {
		klog.V(3).Infof("ovs bridge %s not found", brName)
		return nil
	}

	// get host nic
	if output, err = ovs.Exec("list-ports", brName); err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
	}

	// remove host nic from the external bridge
	if output != "" {
		for _, port := range strings.Split(output, "\n") {
			klog.V(3).Infof("removing ovs port %s from bridge %s", port, brName)
			if err = removeProviderNic(port, brName); err != nil {
				errMsg := fmt.Errorf("failed to remove port %s from external bridge %s: %v", port, brName, err)
				klog.Error(errMsg)
				return errMsg
			}
			klog.V(3).Infof("ovs port %s has been removed from bridge %s", port, brName)
		}
	}

	// remove OVS bridge
	klog.Infof("delete external bridge %s", brName)
	if output, err = ovs.Exec(ovs.IfExists, "del-br", brName); err != nil {
		return fmt.Errorf("failed to remove OVS bridge %s, %v: %q", brName, err, output)
	}
	klog.V(3).Infof("ovs bridge %s has been deleted", brName)

	if br := util.ExternalBridgeName(provider); br != brName {
		if _, err = changeProvideNicName(br, brName); err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", br, brName, err)
			return err
		}
	}

	return nil
}
