package daemon

import (
	"context"
	"fmt"
	"net"
	"slices"
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
		if node.Annotations[util.IPAddressAnnotation] == "" {
			klog.Warningf("no ovn0 address for node %s, please check kube-ovn-controller logs", nodeName)
			time.Sleep(3 * time.Second)
			continue
		}
		if err := util.ValidatePodNetwork(node.Annotations); err != nil {
			klog.Errorf("validate node %s address annotation failed, %v", nodeName, err)
			time.Sleep(3 * time.Second)
			continue
		}
		macAddr = node.Annotations[util.MacAddressAnnotation]
		ip = node.Annotations[util.IPAddressAnnotation]
		cidr = node.Annotations[util.CidrAnnotation]
		portName = node.Annotations[util.PortNameAnnotation]
		gw = node.Annotations[util.GatewayAnnotation]
		break
	}
	mac, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", mac, err)
	}

	ipAddr, err = util.GetIPAddrWithMask(ip, cidr)
	if err != nil {
		klog.Errorf("failed to get ip %s with mask %s, %v", ip, cidr, err)
		return err
	}
	return configureNodeNic(portName, ipAddr, gw, mac, config.MTU)
}

func InitMirror(config *Configuration) error {
	if config.EnableMirror {
		return configureGlobalMirror(config.MirrorNic, config.MTU)
	}
	return configureEmptyMirror(config.MirrorNic, config.MTU)
}

func (c *Controller) ovsInitProviderNetwork(provider, nic string, exchangeLinkName, macLearningFallback bool) (int, error) {
	// create and configure external bridge
	brName := util.ExternalBridgeName(provider)
	if exchangeLinkName {
		exchanged, err := c.changeProvideNicName(nic, brName)
		if err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", nic, brName, err)
			return 0, err
		}
		if exchanged {
			nic, brName = brName, nic
		}
	}

	klog.V(3).Infof("configure external bridge %s", brName)
	if err := c.configExternalBridge(provider, brName, nic, exchangeLinkName, macLearningFallback); err != nil {
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
	klog.Infof("config provider nic %s on bridge %s", nic, brName)
	mtu, err := c.configProviderNic(nic, brName)
	if err != nil {
		errMsg := fmt.Errorf("failed to add nic %s to external bridge %s: %v", nic, brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	return mtu, nil
}

func (c *Controller) ovsCleanProviderNetwork(provider string) error {
	mappings, err := getOvnMappings("ovn-bridge-mappings")
	if err != nil {
		klog.Error(err)
		return err
	}

	brName := mappings[provider]
	if brName == "" {
		return nil
	}

	output, err := ovs.Exec("list-br")
	if err != nil {
		return fmt.Errorf("failed to list OVS bridges: %v, %q", err, output)
	}

	if !slices.Contains(strings.Split(output, "\n"), brName) {
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
			// patch port created by ovn-controller has an external ID ovn-localnet-port=localnet.<SUBNET>
			if output, err = ovs.Exec("--data=bare", "--no-heading", "--columns=_uuid", "find", "port", "name="+port, `external-ids:ovn-localnet-port!=""`); err != nil {
				return fmt.Errorf("failed to find ovs port %s, %v: %q", port, err, output)
			}
			if output != "" {
				continue
			}
			klog.V(3).Infof("removing ovs port %s from bridge %s", port, brName)
			if err = c.removeProviderNic(port, brName); err != nil {
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
		if _, err = c.changeProvideNicName(br, brName); err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", br, brName, err)
			return err
		}
	}

	if err := removeOvnMapping("ovn-chassis-mac-mappings", provider); err != nil {
		klog.Error(err)
		return err
	}
	return removeOvnMapping("ovn-bridge-mappings", provider)
}
