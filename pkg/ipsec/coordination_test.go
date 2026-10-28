package ipsec

import (
	"encoding/json/v2"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCoordinationReceiptRejectsForgeryAndReplay(t *testing.T) {
	_, keyPEM, _ := testIdentity(t, "chassis")
	key, err := privateKey(keyPEM)
	require.NoError(t, err)
	now := time.Now()
	state := &Coordination{Version: 1, Generation: "generation", Epoch: "arm-epoch", Phase: ArmPhase, DaemonSetUID: "ds-uid", TemplateHash: digest([]byte("template")), TrustHash: digest([]byte("trust")), Targets: map[string]string{"node": "node-uid"}}
	base := Receipt{Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, NodeName: "node", PodUID: "pod-uid", CSRName: "request", CSRUID: "request-uid", Observed: now, Status: Status{NodeUID: "node-uid", Chassis: "chassis", Generation: "identity", CertificateHash: "certificate", TrustHash: state.TrustHash, ConfigurationApplied: true, RuntimeHealthy: true, ProtectionArmed: true, Expires: now.Add(time.Hour)}}
	data, err := signReceipt(base, key)
	require.NoError(t, err)
	receipt, err := VerifyReceipt(data, &key.PublicKey, state, "node", "node-uid", now)
	require.NoError(t, err)
	require.Equal(t, base.PodUID, receipt.PodUID)
	var envelope SignedReceipt
	require.NoError(t, json.Unmarshal(data, &envelope))
	claim := base
	claim.Status.ProtectionArmed = false
	envelope.Payload, err = json.Marshal(claim)
	require.NoError(t, err)
	forged, err := json.Marshal(envelope)
	require.NoError(t, err)
	_, err = VerifyReceipt(forged, &key.PublicKey, state, "node", "node-uid", now)
	require.Error(t, err, "a mutable Node annotation cannot forge the root owner's proof")
	for _, tc := range []struct {
		name   string
		change func(*Coordination)
	}{
		{"next-generation", func(s *Coordination) { s.Generation = "next" }},
		{"next-barrier", func(s *Coordination) { s.Epoch = "next" }},
		{"prepare-replay", func(s *Coordination) { s.Phase = PreparePhase }},
		{"replaced-node", func(s *Coordination) { s.Targets["node"] = "new-uid" }},
		{"new-trust", func(s *Coordination) { s.TrustHash = digest([]byte("new-trust")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := *state
			s.Targets = maps.Clone(state.Targets)
			tc.change(&s)
			_, err := VerifyReceipt(data, &key.PublicKey, &s, "node", "node-uid", now)
			require.Error(t, err)
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*Receipt)
	}{
		{"stale", func(r *Receipt) { r.Observed = now.Add(-3 * time.Minute) }},
		{"future", func(r *Receipt) { r.Observed = now.Add(time.Minute) }},
		{"missing-guard", func(r *Receipt) { r.Status.ProtectionArmed = false }},
		{"unapplied-config", func(r *Receipt) { r.Status.ConfigurationApplied = false }},
		{"unhealthy-runtime", func(r *Receipt) { r.Status.RuntimeHealthy = false }},
		{"expired-leaf", func(r *Receipt) { r.Status.Expires = now.Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.change(&r)
			data, err := signReceipt(r, key)
			require.NoError(t, err)
			_, err = VerifyReceipt(data, &key.PublicKey, state, "node", "node-uid", now)
			require.Error(t, err)
		})
	}
}

func TestCoordinationCleanupRequiresDatabaseEvidence(t *testing.T) {
	state := Coordination{Version: 1, Generation: "generation", Epoch: "cleanup-epoch", Phase: CleanupPhase, DaemonSetUID: "ds-uid", TemplateHash: digest([]byte("template")), TrustHash: digest([]byte("trust")), Targets: map[string]string{"offline": "offline-uid"}, NBGlobalUUID: "75980000-0000-0000-0000-000000000001", SBGlobalUUID: "75980000-0000-0000-0000-000000000002"}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = DecodeCoordination(data)
	require.NoError(t, err)
	for _, change := range []func(*Coordination){
		func(s *Coordination) { s.NBGlobalUUID = "" },
		func(s *Coordination) { s.SBGlobalUUID = "invalid" },
		func(s *Coordination) { s.Phase = EnabledPhase },
		func(s *Coordination) { s.Phase = DisablingPhase },
	} {
		s := state
		change(&s)
		data, err := json.Marshal(s)
		require.NoError(t, err)
		_, err = DecodeCoordination(data)
		require.Error(t, err, "incomplete or stale cleanup evidence cannot authorize protection removal")
	}
}
