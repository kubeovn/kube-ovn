package framework

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubevirt/fake"
)

func TestWaitForStartedScheduledMigration(t *testing.T) {
	migration := MakeVMIMigration("migration", "vm")
	migration.Namespace = "default"
	migration.Status.Phase = v1.MigrationScheduled
	client := fake.NewSimpleClientset(migration)
	migrationClient := &VMIMigrationClient{
		VirtualMachineInstanceMigrationInterface: client.KubevirtV1().VirtualMachineInstanceMigrations(migration.Namespace),
	}

	require.NoError(t, migrationClient.WaitForStarted(migration.Name, 100*time.Millisecond),
		"a scheduled migration must be recognized before it can finish between polls")
}

func TestMigrationUnstartedAndTerminalPhases(t *testing.T) {
	for _, phase := range []v1.VirtualMachineInstanceMigrationPhase{
		v1.MigrationPhaseUnset, v1.MigrationPending, v1.MigrationSucceeded, v1.MigrationFailed,
	} {
		t.Run(string(phase), func(t *testing.T) {
			require.False(t, isStartedMigrationPhase(phase))
		})
	}
}
