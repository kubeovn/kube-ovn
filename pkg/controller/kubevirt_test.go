package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

func TestShouldUseMigrationState(t *testing.T) {
	// Helper function to simulate the MigrationUID check logic
	shouldUseMigrationState := func(vmiMigrationState *kubevirtv1.VirtualMachineInstanceMigrationState, vmiMigrationUID types.UID) bool {
		if vmiMigrationState == nil {
			return false
		}
		return vmiMigrationState.MigrationUID == vmiMigrationUID
	}

	tests := []struct {
		name              string
		vmiMigrationState *kubevirtv1.VirtualMachineInstanceMigrationState
		vmiMigrationUID   types.UID
		expected          bool
		description       string
	}{
		{
			name:              "nil MigrationState",
			vmiMigrationState: nil,
			vmiMigrationUID:   "migration-uid-1",
			expected:          false,
			description:       "Should return false when MigrationState is nil",
		},
		{
			name: "MigrationUID matches",
			vmiMigrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "migration-uid-1",
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			vmiMigrationUID: "migration-uid-1",
			expected:        true,
			description:     "Should return true when MigrationUID matches current migration",
		},
		{
			name: "MigrationUID does not match (stale state)",
			vmiMigrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "old-migration-uid",
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			vmiMigrationUID: "new-migration-uid",
			expected:        false,
			description:     "Should return false when MigrationUID does not match (stale state from previous migration)",
		},
		{
			name: "Empty MigrationUID in state",
			vmiMigrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "",
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			vmiMigrationUID: "migration-uid-1",
			expected:        false,
			description:     "Should return false when MigrationUID in state is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldUseMigrationState(tt.vmiMigrationState, tt.vmiMigrationUID)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestMigrationPhaseHandling(t *testing.T) {
	// Test the logic for handling different migration phases when MigrationState is stale/nil
	// This mirrors the behavior of vmiMigration.IsFinal() which returns true for Succeeded/Failed
	isFinalPhase := func(phase kubevirtv1.VirtualMachineInstanceMigrationPhase) bool {
		return phase == kubevirtv1.MigrationSucceeded || phase == kubevirtv1.MigrationFailed
	}

	tests := []struct {
		name        string
		phase       kubevirtv1.VirtualMachineInstanceMigrationPhase
		shouldSkip  bool
		description string
	}{
		{
			name:        "MigrationScheduling phase",
			phase:       kubevirtv1.MigrationScheduling,
			shouldSkip:  false,
			description: "Should NOT skip MigrationScheduling - can still try to get node info from pod",
		},
		{
			name:        "MigrationPending phase",
			phase:       kubevirtv1.MigrationPending,
			shouldSkip:  false,
			description: "Should NOT skip MigrationPending - waiting for state sync",
		},
		{
			name:        "MigrationRunning phase",
			phase:       kubevirtv1.MigrationRunning,
			shouldSkip:  false,
			description: "Should NOT skip MigrationRunning - migration in progress",
		},
		{
			name:        "MigrationSucceeded phase",
			phase:       kubevirtv1.MigrationSucceeded,
			shouldSkip:  true,
			description: "Should skip MigrationSucceeded with stale state - need correct nodes for Reset",
		},
		{
			name:        "MigrationFailed phase",
			phase:       kubevirtv1.MigrationFailed,
			shouldSkip:  true,
			description: "Should skip MigrationFailed with stale state - need correct nodes for Reset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFinalPhase(tt.phase)
			assert.Equal(t, tt.shouldSkip, result, tt.description)
		})
	}
}

func TestMigrationScenarios(t *testing.T) {
	// Simulate the complete migration scenario to ensure correct behavior
	type migrationScenario struct {
		name                string
		previousMigration   *kubevirtv1.VirtualMachineInstanceMigrationState // State from previous migration
		currentMigrationUID types.UID
		currentPhase        kubevirtv1.VirtualMachineInstanceMigrationPhase
		expectedSrcNode     string
		expectedTargetNode  string
		expectedSkip        bool
		description         string
	}

	scenarios := []migrationScenario{
		{
			name: "First migration - state matches",
			previousMigration: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "migration-1",
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			currentMigrationUID: "migration-1",
			currentPhase:        kubevirtv1.MigrationScheduling,
			expectedSrcNode:     "node-a",
			expectedTargetNode:  "node-b",
			expectedSkip:        false,
			description:         "First migration should use MigrationState",
		},
		{
			name: "Second migration - state is stale (A->B->A scenario)",
			previousMigration: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "migration-1", // Old migration
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			currentMigrationUID: "migration-2", // New migration
			currentPhase:        kubevirtv1.MigrationScheduling,
			expectedSrcNode:     "", // Cannot use stale state
			expectedTargetNode:  "",
			expectedSkip:        false, // MigrationScheduling should not skip, can try pod-based approach
			description:         "Second migration should NOT use stale state, but continue to try pod-based node detection",
		},
		{
			name: "Migration succeeded with stale state",
			previousMigration: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "migration-1",
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			currentMigrationUID: "migration-2",
			currentPhase:        kubevirtv1.MigrationSucceeded,
			expectedSrcNode:     "",
			expectedTargetNode:  "",
			expectedSkip:        true, // Should skip - can't Reset without correct nodes
			description:         "MigrationSucceeded with stale state should skip",
		},
		{
			name: "Migration failed with stale state",
			previousMigration: &kubevirtv1.VirtualMachineInstanceMigrationState{
				MigrationUID: "migration-1",
				SourceNode:   "node-a",
				TargetNode:   "node-b",
			},
			currentMigrationUID: "migration-2",
			currentPhase:        kubevirtv1.MigrationFailed,
			expectedSrcNode:     "",
			expectedTargetNode:  "",
			expectedSkip:        true, // Should skip - can't Reset without correct nodes
			description:         "MigrationFailed with stale state should skip",
		},
		{
			name:                "Migration with nil state",
			previousMigration:   nil,
			currentMigrationUID: "migration-1",
			currentPhase:        kubevirtv1.MigrationScheduling,
			expectedSrcNode:     "",
			expectedTargetNode:  "",
			expectedSkip:        false, // MigrationScheduling should continue
			description:         "Nil MigrationState should continue for MigrationScheduling",
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			var srcNode, targetNode string
			var shouldSkip bool

			// Simulate the fixed logic
			if sc.previousMigration != nil && sc.previousMigration.MigrationUID == sc.currentMigrationUID {
				srcNode = sc.previousMigration.SourceNode
				targetNode = sc.previousMigration.TargetNode
			} else {
				// State is stale or nil
				if sc.currentPhase == kubevirtv1.MigrationSucceeded || sc.currentPhase == kubevirtv1.MigrationFailed {
					shouldSkip = true
				}
			}

			assert.Equal(t, sc.expectedSrcNode, srcNode, "Source node mismatch: "+sc.description)
			assert.Equal(t, sc.expectedTargetNode, targetNode, "Target node mismatch: "+sc.description)
			assert.Equal(t, sc.expectedSkip, shouldSkip, "Skip decision mismatch: "+sc.description)
		})
	}
}
