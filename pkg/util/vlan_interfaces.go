package util

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

func DetectVlanInterfaces(parentInterface string) []int {
	vlanIDs := make([]int, 0)

	// LinkList is unavoidable here: the netlink library has no LinkListFiltered
	// and the kernel RTM_GETLINK dump does not support filtering by link type or parent.
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

func FindKubeOVNAutoCreatedInterfaces(providerName string) ([]string, error) {
	var createdInterfaces []string

	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	// Use link.Attrs().Alias (parsed from IFLA_IFALIAS by netlink) instead of reading sysfs
	prefix := "kube-ovn:" + providerName
	for _, link := range links {
		if link.Attrs().Alias == prefix {
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
