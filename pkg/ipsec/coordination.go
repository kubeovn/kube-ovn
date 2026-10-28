package ipsec

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

const (
	CoordinationConfigMap = "ovn-ipsec-coordination"
	ReceiptAnnotation     = "kube-ovn.io/ipsec-receipt"
	PreparePhase          = "Prepare"
	ArmPhase              = "Arm"
	EnabledPhase          = "Enabled"
	DisablingPhase        = "Disabling"
	CleanupPhase          = "Cleanup"
	ReleasePhase          = "Release"
	DisabledPhase         = "Disabled"
)

// Coordination freezes node identities independently of Pod replacements.
// Epoch changes between barriers, so a Prepare receipt cannot enable IPsec.
type Coordination struct {
	Version      int               `json:"version"`
	Generation   string            `json:"generation"`
	Epoch        string            `json:"epoch"`
	Phase        string            `json:"phase"`
	DaemonSetUID string            `json:"daemonSetUID"`
	TemplateHash string            `json:"templateHash"`
	TrustHash    string            `json:"trustHash"`
	Targets      map[string]string `json:"targets"`
	NBGlobalUUID string            `json:"nbGlobalUUID,omitempty"`
	SBGlobalUUID string            `json:"sbGlobalUUID,omitempty"`
}

func DecodeCoordination(data []byte) (*Coordination, error) {
	var state Coordination
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Version != 1 || state.Generation == "" || state.Epoch == "" || state.DaemonSetUID == "" || len(state.TemplateHash) != 64 || len(state.TrustHash) != 64 || len(state.Targets) == 0 {
		return nil, errors.New("invalid IPsec coordination state")
	}
	switch state.Phase {
	case PreparePhase, ArmPhase, EnabledPhase, DisablingPhase, CleanupPhase, ReleasePhase, DisabledPhase:
	default:
		return nil, errors.New("invalid IPsec coordination phase")
	}
	if state.Phase == CleanupPhase || state.Phase == ReleasePhase {
		if !ovsdb.IsValidUUID(state.NBGlobalUUID) || !ovsdb.IsValidUUID(state.SBGlobalUUID) {
			return nil, errors.New("IPsec cleanup lacks live NB/SB database identities")
		}
	} else if state.NBGlobalUUID != "" || state.SBGlobalUUID != "" {
		return nil, errors.New("IPsec cleanup evidence does not match the coordination phase")
	}
	for name, uid := range state.Targets {
		if name == "" || uid == "" {
			return nil, errors.New("invalid IPsec coordination target")
		}
	}
	return &state, nil
}

// Receipt contains public claims and a reference to an authenticated CSR,
// never PEM or private/SA keys. Signature verification alone is insufficient:
// callers must also validate the referenced CSR's live bound Pod and Node.
type Receipt struct {
	Generation string    `json:"generation"`
	Epoch      string    `json:"epoch"`
	Phase      string    `json:"phase"`
	NodeName   string    `json:"nodeName"`
	PodUID     string    `json:"podUID"`
	CSRName    string    `json:"csrName"`
	CSRUID     string    `json:"csrUID"`
	Observed   time.Time `json:"observed"`
	Status     Status    `json:"status"`
}

type SignedReceipt struct {
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}

func receiptHash(payload []byte) [32]byte {
	return sha256.Sum256(append([]byte("kube-ovn IPsec coordination receipt v1\x00"), payload...))
}

func signReceipt(receipt Receipt, key *rsa.PrivateKey) ([]byte, error) {
	payload, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	hash := receiptHash(payload)
	signature, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, hash[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		return nil, err
	}
	return json.Marshal(SignedReceipt{Payload: payload, Signature: signature})
}

// VerifyReceipt authenticates the entire claim and rejects replay across
// barriers, replaced identities, trust changes and the two-minute lease.
func VerifyReceipt(data []byte, key *rsa.PublicKey, state *Coordination, nodeName, nodeUID string, now time.Time) (*Receipt, error) {
	if len(data) > 16<<10 || key == nil || state == nil {
		return nil, errors.New("invalid IPsec receipt envelope")
	}
	var signed SignedReceipt
	if err := json.Unmarshal(data, &signed); err != nil {
		return nil, err
	}
	hash := receiptHash(signed.Payload)
	if err := rsa.VerifyPSS(key, crypto.SHA256, hash[:], signed.Signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
		return nil, err
	}
	var receipt Receipt
	if err := json.Unmarshal(signed.Payload, &receipt); err != nil {
		return nil, err
	}
	status := receipt.Status
	if receipt.Generation != state.Generation || receipt.Epoch != state.Epoch || receipt.Phase != state.Phase || receipt.NodeName != nodeName || nodeUID == "" || state.Targets[nodeName] != nodeUID || status.NodeUID != nodeUID || receipt.PodUID == "" || receipt.CSRName == "" || receipt.CSRUID == "" {
		return nil, errors.New("IPsec receipt does not bind the current barrier and node")
	}
	if receipt.Observed.After(now.Add(5*time.Second)) || !receipt.Observed.After(now.Add(-2*time.Minute)) {
		return nil, errors.New("IPsec receipt is stale or from the future")
	}
	if !identityPrepared(status, now) || status.TrustHash != state.TrustHash || state.Phase != PreparePhase && (!status.ConfigurationApplied || !status.RuntimeHealthy || !status.ProtectionArmed) {
		return nil, errors.New("IPsec receipt does not confirm current identity, runtime and protection")
	}
	return &receipt, nil
}
