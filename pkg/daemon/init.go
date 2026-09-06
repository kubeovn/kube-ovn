package daemon

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// InitOVSBridges initializes OVS bridges
func InitOVSBridges(providers ...compat.TableProvider) (map[string]string, error) {
	var provider compat.TableProvider
	if len(providers) != 0 {
		provider = providers[0]
	}

	if provider == nil {
		return initOVSBridgesLegacy()
	}

	ctx := context.Background()
	var bridges []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).Filter(ctx, func(bridge *vswitch.Bridge) bool {
		return bridge.ExternalIDs[ovs.ExternalIDVendor] == util.CniTypeName
	}, &bridges); err != nil {
		return nil, fmt.Errorf("list kube-ovn OVS bridges: %w", err)
	}
	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).List(ctx, &ports); err != nil {
		return nil, fmt.Errorf("list OVS ports: %w", err)
	}

	mappings := make(map[string]string)
	for _, bridge := range bridges {
		if err := util.SetLinkUp(bridge.Name); err != nil {
			klog.Error(err)
			return nil, err
		}

		for _, port := range ports {
			if !slices.Contains(bridge.Ports, port.UUID) {
				continue
			}
			if port.ExternalIDs[ovs.ExternalIDVendor] == util.CniTypeName {
				mappings[port.Name] = bridge.Name
			}
		}
	}

	return mappings, nil
}

func initOVSBridgesLegacy() (map[string]string, error) {
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
			return nil, fmt.Errorf("failed to list ports of OVS bridge %s, %w: %q", brName, err, output)
		}
		for port := range strings.SplitSeq(output, "\n") {
			if port == "" {
				continue
			}
			ok, err := ovs.ValidatePortVendor(port)
			if err != nil {
				return nil, fmt.Errorf("failed to check vendor of port %s: %w", port, err)
			}
			if ok {
				mappings[port] = brName
			}
		}
	}
	return mappings, nil
}

var (
	configureNodeGateway         = configureNodeNic
	nodeGatewayInitRetryInterval = 3 * time.Second
)

// InitNodeGateway init ovn0
func InitNodeGateway(config *Configuration) (err error) {
	var portName, ip, joinCIDR, macAddr, gw, ipAddr string
	var node *corev1.Node
	annotationFailures := make(map[nodeFailureKey]struct{})
	defer func() {
		if err != nil {
			if eventErr := recordNodeFailureEventSync(config.KubeClient, node, config.NodeName, addNodeFailedReason, "initializeOvn0", err); eventErr != nil {
				klog.Errorf("failed to record node %s initialization event: %v", config.NodeName, eventErr)
			}
		}
	}()
	for {
		nodeName := config.NodeName
		node, err = config.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get node %s info %v", nodeName, err)
			return err
		}
		var annotationErr error
		if node.Annotations[util.IPAddressAnnotation] == "" {
			annotationErr = fmt.Errorf("no %s address for node %s, please check kube-ovn-controller logs", util.NodeNic, nodeName)
		} else {
			annotationErr = util.ValidatePodNetwork(node.Annotations)
		}
		if annotationErr != nil {
			klog.Errorf("validate node %s address annotation failed, %v", nodeName, annotationErr)
			for existingKey := range annotationFailures {
				if existingKey.nodeName == node.Name && existingKey.nodeUID != node.UID {
					delete(annotationFailures, existingKey)
				}
			}
			failureKey := nodeFailureKey{nodeName: node.Name, nodeUID: node.UID, reason: addNodeFailedReason, stage: "validateOvn0Annotations", message: annotationErr.Error()}
			if _, seen := annotationFailures[failureKey]; !seen {
				if eventErr := recordNodeFailureEventSync(config.KubeClient, node, config.NodeName, addNodeFailedReason, "validateOvn0Annotations", annotationErr); eventErr != nil {
					klog.Errorf("failed to record node %s annotation validation event: %v", config.NodeName, eventErr)
				} else {
					trimNodeFailureSignatures(annotationFailures, failureKey)
					annotationFailures[failureKey] = struct{}{}
				}
			}
			time.Sleep(nodeGatewayInitRetryInterval)
			continue
		}
		macAddr = node.Annotations[util.MacAddressAnnotation]
		ip = node.Annotations[util.IPAddressAnnotation]
		joinCIDR = node.Annotations[util.CidrAnnotation]
		portName = node.Annotations[util.PortNameAnnotation]
		gw = node.Annotations[util.GatewayAnnotation]
		break
	}
	mac, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s: %w", macAddr, err)
	}

	ipAddr, err = util.GetIPAddrWithMask(ip, joinCIDR)
	if err != nil {
		klog.Errorf("failed to get ip %s with mask %s, %v", ip, joinCIDR, err)
		return err
	}
	if config.VswitchTables != nil {
		return configureNodeNicWithProvider(config.KubeClient, config.NodeName, portName, ipAddr, gw, joinCIDR, mac, config.MTU, config.EnableNonPrimaryCNI, config.VswitchTables)
	}
	return configureNodeGateway(config.KubeClient, config.NodeName, portName, ipAddr, gw, joinCIDR, mac, config.MTU, config.EnableNonPrimaryCNI)
}

func InitMirror(config *Configuration) error {
	if config.EnableMirror {
		return configureGlobalMirror(config.MirrorNic, config.MTU, config.VswitchTables)
	}
	return configureEmptyMirror(config.MirrorNic, config.MTU, config.VswitchTables)
}

func (c *Controller) ovsInitProviderNetwork(provider, nic string, trunks []string, exchangeLinkName, macLearningFallback bool, vlanInterfaceMap map[string]int) (int, error) { // create and configure external bridge
	if err := validateProviderVlanInterfaceMap(vlanInterfaceMap); err != nil {
		return 0, err
	}

	brName := util.ExternalBridgeName(provider)
	if exchangeLinkName {
		exchanged, err := c.changeProviderNicName(nic, brName)
		if err != nil {
			klog.Errorf("failed to change provider nic name from %s to %s: %v", nic, brName, err)
			return 0, err
		}
		if exchanged {
			nic, brName = brName, nic
		}
	}

	klog.V(3).Infof("configure external bridge %s", brName)
	if err := c.configExternalBridge(provider, brName, nic, exchangeLinkName, macLearningFallback, vlanInterfaceMap); err != nil {
		errMsg := fmt.Errorf("failed to create and configure external bridge %s: %w", brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// init provider chassis mac
	if err := c.initProviderChassisMac(provider); err != nil {
		errMsg := fmt.Errorf("failed to init chassis mac for provider %s, %w", provider, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// add host nic to the external bridge
	klog.Infof("config provider nic %s on bridge %s", nic, brName)
	mtu, err := c.configProviderNic(nic, brName, trunks)
	if err != nil {
		errMsg := fmt.Errorf("failed to add nic %s to external bridge %s: %w", nic, brName, err)
		klog.Error(errMsg)
		return 0, errMsg
	}

	// add vlan interfaces to the external bridge
	if len(vlanInterfaceMap) > 0 {
		if err = c.configProviderVlanInterfaces(vlanInterfaceMap, brName); err != nil {
			errMsg := fmt.Errorf("failed to add vlan interfaces to external bridge %s: %w", brName, err)
			klog.Error(errMsg)
			return 0, errMsg
		}
	}

	return mtu, nil
}

func (c *Controller) ovsCleanProviderNetwork(provider, nic string, vlanInterfaces []string) error {
	mappings, err := c.config.getOvnMappings("ovn-bridge-mappings")
	if err != nil {
		klog.Error(err)
		return err
	}

	brName := mappings[provider]
	if brName == "" {
		// The mapping may have been cleared before cleanup finished (e.g., daemon restart
		// or race with bridge setup failure). Fall back to the default bridge name to clean
		// up any orphaned bridge and restore the original NIC name.
		brName = util.ExternalBridgeName(provider)
		klog.Infof("no ovn-bridge-mappings entry for provider %s, trying default bridge name %s", provider, brName)
	}

	var bridgeExists bool
	if c.vswitchTables != nil {
		bridgeExists, err = c.vswitchBridgeExists(brName)
	} else {
		output, listErr := ovs.Exec("list-br")
		err = listErr
		if err == nil {
			bridgeExists = slices.Contains(strings.Split(output, "\n"), brName)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to list OVS bridges: %w", err)
	}
	if !bridgeExists {
		klog.V(3).Infof("ovs bridge %s not found", brName)
		// Even if no OVS bridge exists, check if a NIC was renamed to br-<provider>
		// and needs to be restored (e.g., exchangeLinkName was used but bridge setup failed).
		if br := util.ExternalBridgeName(provider); br != brName {
			if _, err = c.changeProviderNicName(br, brName); err != nil {
				klog.Errorf("failed to change provider nic name from %s to %s: %v", br, brName, err)
				return err
			}
		} else if nic != "" {
			// In exchangeLinkName mode, when mapping was never stored (setup failed before
			// addOvnMapping), both br and brName equal ExternalBridgeName(provider). The NIC
			// may have been renamed to br-<provider> and needs to be restored to its original name.
			if _, err = c.changeProviderNicName(br, nic); err != nil {
				klog.Errorf("failed to restore provider nic name from %s to %s: %v", br, nic, err)
				return err
			}
		}
		return nil
	}

	isUserspaceDP, err := ovs.IsUserspaceDataPath(c.vswitchTables)
	if err != nil {
		klog.Error(err)
		return err
	}

	if !isUserspaceDP {
		ctx := providerVlanRestoreContext{provider: provider, bridge: brName, nic: nic, vlanInterfaces: vlanInterfaces}
		if err := c.cleanProviderBridgePorts(ctx); err != nil {
			return err
		}

		// remove OVS bridge
		klog.Infof("delete external bridge %s", brName)
		if c.vswitchTables != nil {
			if err := ovs.DeleteVswitchBridge(context.Background(), c.vswitchTables, brName); err != nil {
				klog.Errorf("failed to remove OVS bridge %s, %v", brName, err)
				return err
			}
		} else {
			output, delErr := ovs.Exec(ovs.IfExists, "del-br", brName)
			if delErr != nil {
				klog.Errorf("failed to remove OVS bridge %s, %v: %q", brName, delErr, output)
				return delErr
			}
		}
		klog.Infof("ovs bridge %s has been deleted", brName)

		if br := util.ExternalBridgeName(provider); br != brName {
			if _, err = c.changeProviderNicName(br, brName); err != nil {
				klog.Errorf("failed to change provider nic name from %s to %s: %v", br, brName, err)
				return err
			}
		}
	}

	if err := c.config.removeOvnMapping("ovn-chassis-mac-mappings", provider); err != nil {
		klog.Error(err)
		return err
	}
	return c.config.removeOvnMapping("ovn-bridge-mappings", provider)
}

func (c *Controller) cleanProviderBridgePorts(ctx providerVlanRestoreContext) error {
	ports, err := c.listVswitchBridgePorts(ctx.bridge)
	if err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s: %w", ctx.bridge, err)
	}
	for _, port := range ports {
		if err := c.cleanProviderBridgePort(port.Name, ctx); err != nil {
			return err
		}
	}
	return nil
}

func providerBridgePorts(output string) []string {
	if output == "" {
		return nil
	}
	return slices.Collect(strings.SplitSeq(output, "\n"))
}

func (c *Controller) cleanupProviderBridgePort(port string, ctx providerVlanRestoreContext, owned bool) error {
	switch providerBridgePortCleanupAction(port, ctx.bridge, owned) {
	case providerBridgePortReject:
		return fmt.Errorf("refusing to remove OVS bridge %s: VLAN-shaped port %s has different vendor", ctx.bridge, port)
	case providerBridgePortRemoveVlan:
		_, vlanID := util.IsVlanInternalPortForBridge(port, ctx.bridge)
		klog.Infof("removing VLAN internal port %s (VLAN %d) from bridge %s", port, vlanID, ctx.bridge)
		if err := c.removeProviderVlanInterface(port, ctx, vlanID); err != nil {
			return fmt.Errorf("failed to remove VLAN internal port %s from external bridge %s: %w", port, ctx.bridge, err)
		}
	case providerBridgePortRemoveNic:
		klog.Infof("removing ovs port %s from bridge %s", port, ctx.bridge)
		if err := c.removeProviderNic(port, ctx.bridge); err != nil {
			return fmt.Errorf("failed to remove port %s from external bridge %s: %w", port, ctx.bridge, err)
		}
	}
	klog.Infof("ovs port %s has been removed from bridge %s", port, ctx.bridge)
	return nil
}

func (c *Controller) cleanProviderBridgePort(port string, ctx providerVlanRestoreContext) error {
	if c.vswitchTables != nil {
		ports, err := c.listVswitchPorts(func(row *vswitch.Port) bool {
			return row.Name == port
		})
		if err != nil {
			return fmt.Errorf("failed to find OVS port %s: %w", port, err)
		}
		if len(ports) == 0 || ports[0].ExternalIDs["ovn-localnet-port"] != "" {
			return nil
		}
		owned, err := c.validateVswitchPortVendor(port)
		if err != nil {
			return fmt.Errorf("failed to check vendor of port %s: %w", port, err)
		}
		return c.cleanupProviderBridgePort(port, ctx, owned)
	}
	output, err := ovs.Exec("--data=bare", "--no-heading", "--columns=_uuid", "find", "port", "name="+port, `external-ids:ovn-localnet-port!=""`)
	if err != nil {
		return fmt.Errorf("failed to find OVS port %s: %w: %q", port, err, output)
	}
	if output != "" {
		return nil
	}
	owned, err := c.validateVswitchPortVendor(port)
	if err != nil {
		return fmt.Errorf("failed to check vendor of port %s: %w", port, err)
	}
	return c.cleanupProviderBridgePort(port, ctx, owned)
}
