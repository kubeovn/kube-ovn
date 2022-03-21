package daemon

import (
	"fmt"
	"strings"

	"github.com/vishvananda/netlink"
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
		bridge, err := netlink.LinkByName(brName)
		if err != nil {
			return nil, fmt.Errorf("failed to get bridge by name %s: %v", brName, err)
		}
		if err = netlink.LinkSetUp(bridge); err != nil {
			return nil, fmt.Errorf("failed to set OVS bridge %s up: %v", brName, err)
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

func ovsInitProviderNetwork(provider, nic string) (int, error) {
	// create and configure external bridge
	brName := util.ExternalBridgeName(provider)
	if err := configExternalBridge(provider, brName, nic); err != nil {
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
	output, err := ovs.Exec("list-br")
	if err != nil {
		return fmt.Errorf("failed to list OVS bridge %v: %q", err, output)
	}

	brName := util.ExternalBridgeName(provider)
	if !util.ContainsString(strings.Split(output, "\n"), brName) {
		return nil
	}

	if output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings"); err != nil {
		return fmt.Errorf("failed to get ovn-bridge-mappings, %v: %q", err, output)
	}

	mappings := strings.Split(output, ",")
	brMap := fmt.Sprintf("%s:%s", provider, brName)

	var idx int
	for idx = range mappings {
		if mappings[idx] == brMap {
			break
		}
	}
	if idx != len(mappings) {
		mappings = append(mappings[:idx], mappings[idx+1:]...)
		if len(mappings) == 0 {
			output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", "ovn-bridge-mappings")
		} else {
			output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-bridge-mappings="+strings.Join(mappings, ","))
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
		if len(macMap) == len(provider)+18 && strings.HasPrefix(macMap, provider) {
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

	// get host nic
	if output, err = ovs.Exec("list-ports", brName); err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", brName, err, output)
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

	// remove OVS bridge
	if output, err = ovs.Exec(ovs.IfExists, "del-br", brName); err != nil {
		return fmt.Errorf("failed to remove OVS bridge %s, %v: %q", brName, err, output)
	}

	return nil
}
