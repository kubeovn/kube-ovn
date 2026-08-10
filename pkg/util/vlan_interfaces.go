package util

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const linuxInterfaceNameMaxLength = 15

var vlanInternalPortHashEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// VlanInternalPortName returns a deterministic OVS internal interface name that
// fits Linux's IFNAMSIZ limit while preserving existing compatible names.
func VlanInternalPortName(bridgeName string, vlanID int) string {
	name := fmt.Sprintf("%s-vlan%d", bridgeName, vlanID)
	if len(name) <= linuxInterfaceNameMaxLength {
		return name
	}

	prefix := fmt.Sprintf("kv%d-", vlanID)
	digest := sha256.Sum256([]byte(bridgeName))
	hash := vlanInternalPortHashEncoding.EncodeToString(digest[:])
	return prefix + hash[:linuxInterfaceNameMaxLength-len(prefix)]
}

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
	if matched, vlanID := isCompactVlanInternalPort(portName); matched {
		return true, vlanID
	}

	separatorIndex := strings.LastIndex(portName, "-vlan")
	if separatorIndex == -1 || !strings.HasPrefix(portName[:separatorIndex], "br-") {
		return false, 0
	}
	return parseVlanID(portName[separatorIndex+len("-vlan"):])
}

// IsVlanInternalPortForBridge reports whether portName is a compact or legacy
// VLAN internal port created for bridgeName.
func IsVlanInternalPortForBridge(portName, bridgeName string) (bool, int) {
	if matched, vlanID := isCompactVlanInternalPort(portName); matched {
		if portName == VlanInternalPortName(bridgeName, vlanID) {
			return true, vlanID
		}
		return false, 0
	}

	vlanIDText, found := strings.CutPrefix(portName, bridgeName+"-vlan")
	if !found {
		return false, 0
	}
	return parseVlanID(vlanIDText)
}

func isCompactVlanInternalPort(portName string) (bool, int) {
	if len(portName) == linuxInterfaceNameMaxLength && strings.HasPrefix(portName, "kv") {
		vlanIDText, hash, found := strings.Cut(portName[2:], "-")
		if !found || !isLowerBase32(hash) {
			return false, 0
		}
		return parseVlanID(vlanIDText)
	}
	return false, 0
}

func parseVlanID(value string) (bool, int) {
	vlanID, err := strconv.Atoi(value)
	if err != nil || vlanID < 0 || vlanID > 4095 {
		return false, 0
	}
	return true, vlanID
}

func isLowerBase32(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '2' || char > '7') {
			return false
		}
	}
	return true
}
