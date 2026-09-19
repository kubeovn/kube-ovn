package ovs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunDirOfComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component string
		runDir    string
		wantErr   bool
	}{
		{name: "ovsdb-server", component: OvsdbServer, runDir: ovsRunDir},
		{name: "ovs-vswitchd", component: OvsVswitchd, runDir: ovsRunDir},
		{name: "ovn-controller", component: OvnController, runDir: ovnRunDir},
		{name: "ovn-northd", component: OvnNorthd, runDir: ovnRunDir},
		{name: "unknown component", component: "foo", wantErr: true},
		{name: "empty component", component: "", wantErr: true},
		{name: "ovn nb database", component: "ovnnb_db", wantErr: true},
		{name: "ovn sb database", component: "ovnsb_db", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runDir, err := runDirOfComponent(tt.component)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.runDir, runDir)
		})
	}
}

func TestCtlSocketPathIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		socket  string
		wantErr string
	}{
		// the pid is taken from the file content as is, so a pid written by a daemon
		// running in another pid namespace or as another user is honored
		{name: "plain pid", content: "12345", socket: "ovs-vswitchd.12345.ctl"},
		{name: "trailing newline", content: "12345\n", socket: "ovs-vswitchd.12345.ctl"},
		{name: "surrounding whitespace", content: " 12345 \n", socket: "ovs-vswitchd.12345.ctl"},
		{name: "empty", content: "", wantErr: "is empty or contains only whitespace"},
		{name: "whitespace only", content: "\n", wantErr: "is empty or contains only whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			pidFile := filepath.Join(runDir, OvsVswitchd+".pid")
			require.NoError(t, os.WriteFile(pidFile, []byte(tt.content), 0o600))

			socket, err := ctlSocketPathIn(runDir, OvsVswitchd)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, filepath.Join(runDir, tt.socket), socket)
		})
	}
}

func TestCtlSocketPathInMissingPidFile(t *testing.T) {
	t.Parallel()

	_, err := ctlSocketPathIn(t.TempDir(), OvsVswitchd)
	require.ErrorContains(t, err, "failed to read pid file")
}

func TestCtlSocketPathErrors(t *testing.T) {
	t.Parallel()

	_, err := ctlSocketPath("foo")
	require.ErrorContains(t, err, `unknown component "foo"`)

	_, err = ctlSocketPath("ovnnb_db")
	require.ErrorContains(t, err, `unknown component "ovnnb_db"`)
}

func TestOvnDatabaseControlUnknownDB(t *testing.T) {
	t.Parallel()

	_, err := OvnDatabaseControl("foo", "cluster/status")
	require.ErrorContains(t, err, `unknown db "foo"`)
}

func TestAppctlUnknownTarget(t *testing.T) {
	t.Parallel()

	_, err := Appctl("foo", "version")
	require.ErrorContains(t, err, `unknown component "foo"`)
}
