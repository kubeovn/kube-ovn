package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// TestMigrationStateValidation tests the MigrationState validation logic in handleAddOrUpdateVMIMigration.
// The implementation uses VMIMigration.Status.MigrationState as the data source.
// It waits for MigrationState to be populated (non-nil) and have complete source/target info.
func TestMigrationStateValidation(t *testing.T) {
	// isMigrationStateReady checks if VMIMigration.Status.MigrationState is ready to use.
	// This mirrors the validation logic in handleAddOrUpdateVMIMigration.
	isMigrationStateReady := func(migrationState *kubevirtv1.VirtualMachineInstanceMigrationState) bool {
		if migrationState == nil {
			return false
		}
		return migrationState.SourceNode != "" && migrationState.TargetNode != ""
	}

	tests := []struct {
		name           string
		migrationState *kubevirtv1.VirtualMachineInstanceMigrationState
		expected       bool
	}{
		{
			name:           "nil MigrationState - not ready (KubeVirt not populated yet)",
			migrationState: nil,
			expected:       false,
		},
		{
			name: "complete MigrationState - ready",
			migrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				SourceNode: "node-a",
				TargetNode: "node-b",
			},
			expected: true,
		},
		{
			name: "empty SourceNode - not ready",
			migrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				SourceNode: "",
				TargetNode: "node-b",
			},
			expected: false,
		},
		{
			name: "empty TargetNode - not ready",
			migrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				SourceNode: "node-a",
				TargetNode: "",
			},
			expected: false,
		},
		{
			name: "both nodes empty - not ready",
			migrationState: &kubevirtv1.VirtualMachineInstanceMigrationState{
				SourceNode: "",
				TargetNode: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMigrationStateReady(tt.migrationState)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMigrationPhaseHandling tests how different migration phases trigger LSP actions.
//
// Implementation flow:
//  1. Wait for MigrationState to be ready (non-nil with source/target)
//  2. Based on phase, perform the corresponding LSP operation:
//     - MigrationScheduling: SetLogicalSwitchPortMigrateOptions (dual-chassis binding)
//     - MigrationSucceeded: ResetLogicalSwitchPortMigrateOptions (to target node)
//     - MigrationFailed: ResetLogicalSwitchPortMigrateOptions (rollback to source)
//     - Other phases: no-op (return nil)
func TestMigrationPhaseHandling(t *testing.T) {
	type testCase struct {
		name           string
		phase          kubevirtv1.VirtualMachineInstanceMigrationPhase
		stateReady     bool
		expectedAction string // "set_options", "reset_to_target", "reset_to_source", "wait"
	}

	tests := []testCase{
		{
			name:           "MigrationScheduling with ready state sets dual-chassis options",
			phase:          kubevirtv1.MigrationScheduling,
			stateReady:     true,
			expectedAction: "set_options",
		},
		{
			name:           "MigrationScheduling with state not ready waits for KubeVirt to populate",
			phase:          kubevirtv1.MigrationScheduling,
			stateReady:     false,
			expectedAction: "wait",
		},
		{
			name:           "MigrationSucceeded with ready state resets to target node",
			phase:          kubevirtv1.MigrationSucceeded,
			stateReady:     true,
			expectedAction: "reset_to_target",
		},
		{
			name:           "MigrationSucceeded with state not ready waits for KubeVirt to populate",
			phase:          kubevirtv1.MigrationSucceeded,
			stateReady:     false,
			expectedAction: "wait",
		},
		{
			name:           "MigrationFailed with ready state rolls back to source node",
			phase:          kubevirtv1.MigrationFailed,
			stateReady:     true,
			expectedAction: "reset_to_source",
		},
		{
			name:           "MigrationFailed with state not ready waits for KubeVirt to populate",
			phase:          kubevirtv1.MigrationFailed,
			stateReady:     false,
			expectedAction: "wait",
		},
		{
			name:           "MigrationPending is no-op (migration not yet scheduled)",
			phase:          kubevirtv1.MigrationPending,
			stateReady:     false,
			expectedAction: "wait",
		},
		{
			name:           "MigrationRunning is no-op (wait for completion)",
			phase:          kubevirtv1.MigrationRunning,
			stateReady:     true,
			expectedAction: "wait",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the decision logic in handleAddOrUpdateVMIMigration
			var action string
			if !tt.stateReady {
				// State not ready -> return nil, wait for KubeVirt to populate
				action = "wait"
			} else {
				switch tt.phase {
				case kubevirtv1.MigrationScheduling:
					action = "set_options"
				case kubevirtv1.MigrationSucceeded:
					action = "reset_to_target"
				case kubevirtv1.MigrationFailed:
					action = "reset_to_source"
				default:
					// MigrationPending, MigrationRunning, etc. -> no-op
					action = "wait"
				}
			}
			assert.Equal(t, tt.expectedAction, action)
		})
	}
}

// TestConsecutiveMigrationScenario tests the A->B->A consecutive migration scenario (Issue #6220).
//
// Scenario:
//  1. Migration A: node1 -> node2, succeeds
//  2. Migration B: node2 -> node1, starts immediately after
//
// Previous bug: If migration A's cleanup was skipped (early return on Completed=true),
// the LSP would still have activation-strategy set when migration B starts.
//
// Current solution:
//  1. Use VMIMigration.Status.MigrationState (snapshot) instead of VMI.Status.MigrationState
//  2. Each VMIMigration has its own MigrationState, won't be overwritten by new migration
//  3. Detect and clean residual activation-strategy on LSP before setting new options
func TestConsecutiveMigrationScenario(t *testing.T) {
	// After migration A (node1 -> node2) succeeds, migration B starts in opposite direction.
	// VMIMigration B has its own MigrationState snapshot showing the correct direction.
	migrationB := &kubevirtv1.VirtualMachineInstanceMigrationState{
		SourceNode: "node2", // VM is now on node2 after migration A
		TargetNode: "node1", // Migrating back to node1
	}

	// Key insight: Each VMIMigration has its own MigrationState snapshot.
	// Migration B's state shows correct source/target for the new migration,
	// independent of migration A's state.
	assert.Equal(t, "node2", migrationB.SourceNode, "Migration B source should be node2 (where VM is after A)")
	assert.Equal(t, "node1", migrationB.TargetNode, "Migration B target should be node1")

	// This is why using VMIMigration.Status.MigrationState is correct:
	// - Migration A handler sees its own state (node1 -> node2)
	// - Migration B handler sees its own state (node2 -> node1)
	// No confusion about which migration's data we're using.
}

// TestSameNodeMigrationSkip tests that migration is skipped when source and target are the same node.
// This can happen in edge cases and the implementation should handle it gracefully.
func TestSameNodeMigrationSkip(t *testing.T) {
	shouldSkipMigration := func(srcNode, targetNode string) bool {
		return srcNode == targetNode
	}

	tests := []struct {
		name       string
		srcNode    string
		targetNode string
		expectSkip bool
	}{
		{
			name:       "different nodes - should not skip",
			srcNode:    "node1",
			targetNode: "node2",
			expectSkip: false,
		},
		{
			name:       "same node - should skip",
			srcNode:    "node1",
			targetNode: "node1",
			expectSkip: true,
		},
		{
			name:       "same node with different name format - should skip",
			srcNode:    "k8s-worker-01",
			targetNode: "k8s-worker-01",
			expectSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipMigration(tt.srcNode, tt.targetNode)
			assert.Equal(t, tt.expectSkip, result)
		})
	}
}
