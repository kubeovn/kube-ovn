package controllerruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
)

// SubnetOVNMutationIntent is the durable description of the OVN row owned by
// a Subnet mutation. It deliberately starts with the logical switch, which is
// the only row covered by the current resolver. Additional rows must be added
// here before a resolver claims to verify them.
type SubnetOVNMutationIntent struct {
	LogicalSwitchName string `json:"logicalSwitchName"`
	Generation        int64  `json:"generation"`
}

func (i SubnetOVNMutationIntent) valid() error {
	if i.LogicalSwitchName == "" || i.Generation < 0 {
		return ErrSubnetOVNReceiptInvalid
	}
	return nil
}

func (i SubnetOVNMutationIntent) hash() string {
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

func subnetOVNMutationKey(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation) SubnetOVNMutationKey {
	intent := subnetOVNMutationIntentForSnapshot(snapshot)
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
