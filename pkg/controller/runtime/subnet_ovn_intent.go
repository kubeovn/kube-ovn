package controllerruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"slices"

	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
)

// SubnetOVNMutationIntent is the durable description of the OVN rows owned by
// a Subnet mutation. The resolver currently verifies the logical switch and,
// when required, the named router patch ports; other side effects need their
// own typed intent fields and readback.
type SubnetOVNMutationIntent struct {
	LogicalSwitchName     string   `json:"logicalSwitchName"`
	Generation            int64    `json:"generation"`
	LogicalRouterName     string   `json:"logicalRouterName,omitempty"`
	LogicalRouterPortName string   `json:"logicalRouterPortName,omitempty"`
	LogicalSwitchPortName string   `json:"logicalSwitchPortName,omitempty"`
	LogicalRouterNetworks []string `json:"logicalRouterNetworks,omitempty"`
	GatewayMAC            string   `json:"gatewayMAC,omitempty"`
	RequirePatchPorts     bool     `json:"requirePatchPorts,omitzero"`
}

func (i SubnetOVNMutationIntent) valid() error {
	if i.LogicalSwitchName == "" || i.Generation < 0 {
		return ErrSubnetOVNReceiptInvalid
	}
	if !i.RequirePatchPorts && (i.LogicalRouterName != "" || i.LogicalRouterPortName != "" ||
		i.LogicalSwitchPortName != "" || len(i.LogicalRouterNetworks) != 0 || i.GatewayMAC != "") {
		return ErrSubnetOVNReceiptInvalid
	}
	if i.RequirePatchPorts && (i.LogicalRouterName == "" || i.LogicalRouterPortName == "" || i.LogicalSwitchPortName == "") {
		return ErrSubnetOVNReceiptInvalid
	}
	return nil
}

func (i SubnetOVNMutationIntent) equal(other SubnetOVNMutationIntent) bool {
	leftNetworks := slices.Sorted(slices.Values(i.LogicalRouterNetworks))
	rightNetworks := slices.Sorted(slices.Values(other.LogicalRouterNetworks))
	return i.LogicalSwitchName == other.LogicalSwitchName && i.Generation == other.Generation &&
		i.LogicalRouterName == other.LogicalRouterName && i.LogicalRouterPortName == other.LogicalRouterPortName &&
		i.LogicalSwitchPortName == other.LogicalSwitchPortName && slices.Equal(leftNetworks, rightNetworks) &&
		i.GatewayMAC == other.GatewayMAC && i.RequirePatchPorts == other.RequirePatchPorts
}

func (i SubnetOVNMutationIntent) isZero() bool {
	return i.LogicalSwitchName == "" && i.Generation == 0 && i.LogicalRouterName == "" &&
		i.LogicalRouterPortName == "" && i.LogicalSwitchPortName == "" && len(i.LogicalRouterNetworks) == 0 &&
		i.GatewayMAC == "" && !i.RequirePatchPorts
}

func (i SubnetOVNMutationIntent) hash() string {
	i.LogicalRouterNetworks = slices.Clone(i.LogicalRouterNetworks)
	slices.Sort(i.LogicalRouterNetworks)
	record, err := json.Marshal(i)
	if err != nil {
		// The intent contains only strings, so this is unreachable unless the
		// JSON implementation changes. Keep key construction total and let
		// validation reject an impossible empty hash.
		return ""
	}
	hash := sha256.Sum256(record)
	return hex.EncodeToString(hash[:16])
}

func subnetOVNMutationIntentForSnapshot(snapshot SubnetWriteSnapshot) SubnetOVNMutationIntent {
	return SubnetOVNMutationIntent{LogicalSwitchName: snapshot.Name, Generation: snapshot.Generation}
}

func subnetOVNMutationIntentFromLegacy(intent legacycontroller.SubnetRuntimeOVNMutationIntent, generation int64) SubnetOVNMutationIntent {
	return SubnetOVNMutationIntent{
		LogicalSwitchName:     intent.LogicalSwitchName,
		Generation:            generation,
		LogicalRouterName:     intent.LogicalRouterName,
		LogicalRouterPortName: intent.LogicalRouterPortName,
		LogicalSwitchPortName: intent.LogicalSwitchPortName,
		LogicalRouterNetworks: slices.Clone(intent.LogicalRouterNetworks),
		GatewayMAC:            intent.GatewayMAC,
		RequirePatchPorts:     intent.RequirePatchPorts,
	}
}

func subnetOVNMutationKey(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation) SubnetOVNMutationKey {
	return subnetOVNMutationKeyForIntent(snapshot, operation, subnetOVNMutationIntentForSnapshot(snapshot))
}

func subnetOVNMutationKeyForIntent(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, intent SubnetOVNMutationIntent) SubnetOVNMutationKey {
	return SubnetOVNMutationKey{
		Name:       snapshot.Name,
		UID:        snapshot.UID,
		Operation:  operation,
		IntentHash: intent.hash(),
	}
}

// legacySubnetOVNMutationKey describes the key written before receipts
// persisted a typed intent. It is accepted only while reading durable state;
// all new mutations use subnetOVNMutationKey above.
func legacySubnetOVNMutationKey(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation) SubnetOVNMutationKey {
	identity := fmt.Sprintf("%s\x00%s\x00%d\x00%s", snapshot.Name, snapshot.UID, snapshot.Generation, operation)
	hash := sha256.Sum256([]byte(identity))
	return SubnetOVNMutationKey{Name: snapshot.Name, UID: snapshot.UID, Operation: operation, IntentHash: hex.EncodeToString(hash[:16])}
}

func isLegacySubnetOVNMutationKey(key SubnetOVNMutationKey, generation int64) bool {
	return key == legacySubnetOVNMutationKey(SubnetWriteSnapshot{Name: key.Name, UID: key.UID, Generation: generation}, key.Operation)
}

func isCoarseSubnetOVNMutationKey(key SubnetOVNMutationKey, generation int64) bool {
	return key == subnetOVNMutationKey(SubnetWriteSnapshot{Name: key.Name, UID: key.UID, Generation: generation}, key.Operation)
}

func isCoarseOrLegacySubnetOVNMutationKey(key SubnetOVNMutationKey, generation int64) bool {
	return isCoarseSubnetOVNMutationKey(key, generation) || isLegacySubnetOVNMutationKey(key, generation)
}
