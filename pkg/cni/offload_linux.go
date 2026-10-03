package cni

import (
	"fmt"
	"strings"

	"github.com/safchain/ethtool"
	"k8s.io/klog/v2"
)

// TurnOffNicTxChecksum uses SIOCETHTOOL in the current network namespace.
// ethtool's "tx" alias covers every tx-checksum-* kernel feature.
func TurnOffNicTxChecksum(nicName string) error {
	handle, err := ethtool.NewEthtool()
	if err != nil {
		return fmt.Errorf("open ethtool socket: %w", err)
	}
	defer handle.Close()
	features, err := handle.FeaturesWithState(nicName)
	if err != nil {
		return fmt.Errorf("query TX checksum features of %s: %w", nicName, err)
	}
	changes := txChecksumChanges(features)
	if len(changes) == 0 {
		return nil
	}
	if err := handle.Change(nicName, changes); err != nil {
		return fmt.Errorf("disable TX checksum on %s: %w", nicName, err)
	}
	return nil
}

func txChecksumChanges(features map[string]ethtool.FeatureState) map[string]bool {
	changes := make(map[string]bool)
	for name, feature := range features {
		if strings.HasPrefix(name, "tx-checksum-") && feature.Available && !feature.NeverChanged {
			changes[name] = false
		}
	}
	return changes
}

func disableUFO(nicName string) error {
	handle, err := ethtool.NewEthtool()
	if err != nil {
		return fmt.Errorf("open ethtool socket: %w", err)
	}
	defer handle.Close()
	features, err := handle.FeaturesWithState(nicName)
	if err != nil {
		// Preserve the existing best-effort probe for devices without ethtool support.
		klog.Warningf("failed to query offload features of %s, skip disabling UFO: %v", nicName, err)
		return nil
	}
	feature, ok := features["tx-udp-fragmentation"]
	if !ok || !feature.Active {
		return nil
	}
	if err := handle.Change(nicName, map[string]bool{"tx-udp-fragmentation": false}); err != nil {
		return fmt.Errorf("disable UFO on %s: %w", nicName, err)
	}
	return nil
}
