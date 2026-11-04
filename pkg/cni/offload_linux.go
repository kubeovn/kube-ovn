package cni

import (
	"fmt"
	"strings"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

// TurnOffNicTxChecksum disables every changeable TX checksum feature in the
// current network namespace.
func TurnOffNicTxChecksum(nicName string) error {
	link, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("find NIC %s: %w", nicName, err)
	}
	features, err := netlink.NetDevFeaturesGet(link.Attrs().Index)
	if err != nil {
		return fmt.Errorf("query TX checksum features of %s: %w", nicName, err)
	}
	changes := txChecksumChanges(features)
	if len(changes) == 0 {
		return nil
	}
	if err := netlink.NetDevFeaturesSet(link.Attrs().Index, changes); err != nil {
		return fmt.Errorf("disable TX checksum on %s: %w", nicName, err)
	}
	return nil
}

func txChecksumChanges(features map[string]netlink.NetDevFeature) map[string]bool {
	changes := make(map[string]bool)
	for name, feature := range features {
		if strings.HasPrefix(name, "tx-checksum-") && feature.Hardware && !feature.NoChange {
			changes[name] = false
		}
	}
	return changes
}

func disableUFO(nicName string) error {
	link, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("find NIC %s: %w", nicName, err)
	}
	features, err := netlink.NetDevFeaturesGet(link.Attrs().Index)
	if err != nil {
		// Preserve the existing best-effort probe for devices without ethtool support.
		klog.Warningf("failed to query offload features of %s, skip disabling UFO: %v", nicName, err)
		return nil
	}
	feature, ok := features["tx-udp-fragmentation"]
	if !ok || !feature.Active {
		return nil
	}
	if err := netlink.NetDevFeaturesSet(link.Attrs().Index, map[string]bool{"tx-udp-fragmentation": false}); err != nil {
		return fmt.Errorf("disable UFO on %s: %w", nicName, err)
	}
	return nil
}
