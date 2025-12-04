package util

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"github.com/vishvananda/netlink"
)

// DetectVlanInterfaces detects existing VLAN interfaces for a given parent interface
// Returns a list of VLAN IDs found (e.g., [10, 20] for bond0.10, bond0.20)
func DetectVlanInterfaces(parentInterface string) []int {
	vlanIDs := make([]int, 0) // Always non-nil

	links, err := netlink.LinkList()
	if err != nil {
		klog.Errorf("failed to list network interfaces: %v", err)
		return vlanIDs // Always returns a non-nil slice
	}

	for _, link := range links {
		linkName := link.Attrs().Name

		// Check if this is a VLAN interface of our parent interface
		// Pattern: <parent>.<vlan_id> e.g., bond0.100, eth0.200
		if strings.HasPrefix(linkName, parentInterface+".") {
			// Extract VLAN ID from interface name
			parts := strings.Split(linkName, ".")
			if len(parts) == 2 {
				if vlanID, err := strconv.Atoi(parts[1]); err == nil {
					// Verify it's actually a VLAN interface
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

// CheckInterfaceExists checks if a network interface exists
func CheckInterfaceExists(interfaceName string) bool {
	_, err := netlink.LinkByName(interfaceName)
	if err != nil {
		klog.V(3).Infof("interface %s does not exist: %v", interfaceName, err)
		return false
	}
	klog.V(3).Infof("interface %s exists", interfaceName)
	return true
}

// ExtractVlanIDFromInterface extracts VLAN ID from interface name
// For example: "eth0.10" -> 10, "bond0.20" -> 20
func ExtractVlanIDFromInterface(interfaceName string) (int, error) {
	// Split by dot to separate interface and VLAN ID
	parts := strings.Split(interfaceName, ".")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid VLAN interface name format: %s (expected format: interface.vlanid)", interfaceName)
	}

	// Parse VLAN ID
	vlanID, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("failed to parse VLAN ID from interface name %s: %w", interfaceName, err)
	}

	return vlanID, nil
}

// isVlanInternalPort checks if a port name matches the VLAN internal port pattern
// and returns true with the VLAN ID if it matches (e.g., br-eth0-vlan10 -> true, 10)
func IsVlanInternalPort(portName string) (bool, int) {
	// Pattern: br-<interface>-vlan<id>
	if !strings.Contains(portName, "-vlan") {
		return false, 0
	}

	parts := strings.Split(portName, "-vlan")
	if len(parts) != 2 {
		return false, 0
	}

	// Check if it starts with br-
	if !strings.HasPrefix(parts[0], "br-") {
		return false, 0
	}

	// Parse VLAN ID
	vlanID, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, 0
	}

	return true, vlanID
}
