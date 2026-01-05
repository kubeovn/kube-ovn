package util

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

func DetectVlanInterfaces(parentInterface string) []int {
	vlanIDs := make([]int, 0)

	links, err := netlink.LinkList()
	if err != nil {
		klog.Errorf("failed to list network interfaces: %v", err)
		return vlanIDs
	}

	for _, link := range links {
		linkName := link.Attrs().Name
		if strings.HasPrefix(linkName, parentInterface+".") {
			parts := strings.Split(linkName, ".")
			if len(parts) == 2 {
				if vlanID, err := strconv.Atoi(parts[1]); err == nil {
					if _, isVlan := link.(*netlink.Vlan); isVlan {
						vlanIDs = append(vlanIDs, vlanID)
						klog.V(3).Infof("detected VLAN interface %s with VLAN ID %d", linkName, vlanID)
					}
				}
			}
		}
	}

	klog.Infof("detected %d VLAN interfaces for %s: %v", len(vlanIDs), parentInterface, vlanIDs)
	return vlanIDs
}

func CheckInterfaceExists(interfaceName string) bool {
	_, err := netlink.LinkByName(interfaceName)
	if err != nil {
		klog.V(3).Infof("interface %s does not exist: %v", interfaceName, err)
		return false
	}
	klog.V(3).Infof("interface %s exists", interfaceName)
	return true
}

func ExtractVlanIDFromInterface(interfaceName string) (int, error) {
	parts := strings.Split(interfaceName, ".")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid VLAN interface name format: %s (expected format: interface.vlanid)", interfaceName)
	}

	vlanID, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("failed to parse VLAN ID from interface name %s: %w", interfaceName, err)
	}

	return vlanID, nil
}

func GetInterfaceAlias(interfaceName string) string {
	aliasFile := fmt.Sprintf("/sys/class/net/%s/ifalias", interfaceName)
	data, err := os.ReadFile(aliasFile)
	if err != nil {
		klog.V(3).Infof("Failed to read alias for interface %s: %v", interfaceName, err)
		return ""
	}

	alias := strings.TrimSpace(string(data))
	if alias == "" {
		klog.V(3).Infof("No alias set for interface %s", interfaceName)
		return ""
	}

	klog.V(3).Infof("Interface %s has alias: %s", interfaceName, alias)
	return alias
}

func IsKubeOVNAutoCreatedInterface(interfaceName string) (bool, string) {
	alias := GetInterfaceAlias(interfaceName)
	if providerName, ok := strings.CutPrefix(alias, "kube-ovn:"); ok {
		return true, providerName
	}
	return false, ""
}

func FindKubeOVNAutoCreatedInterfaces(providerName string) ([]string, error) {
	var createdInterfaces []string

	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	for _, link := range links {
		if isKubeOVN, pnName := IsKubeOVNAutoCreatedInterface(link.Attrs().Name); isKubeOVN && pnName == providerName {
			createdInterfaces = append(createdInterfaces, link.Attrs().Name)
		}
	}

	klog.V(3).Infof("Found %d Kube-OVN auto-created interfaces for provider %s: %v", len(createdInterfaces), providerName, createdInterfaces)
	return createdInterfaces, nil
}

func IsVlanInternalPort(portName string) (bool, int) {
	if !strings.Contains(portName, "-vlan") {
		return false, 0
	}

	parts := strings.Split(portName, "-vlan")
	if len(parts) != 2 {
		return false, 0
	}

	if !strings.HasPrefix(parts[0], "br-") {
		return false, 0
	}

	vlanID, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, 0
	}

	return true, vlanID
}
