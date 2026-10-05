package kohelper

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenPodIPsecRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host proc directory descriptors are Linux-only")
	}
	proc := t.TempDir()
	process := func(pid, comm, cgroup string) string {
		path := filepath.Join(proc, pid)
		require.NoError(t, os.MkdirAll(filepath.Join(path, "root/etc"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(path, "comm"), []byte(comm+"\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(path, "cgroup"), []byte(cgroup), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(path, "root/etc/ipsec.conf"), []byte(pid), 0o600))
		return path
	}
	process("1", "charon", "0::/host.service")
	process("20", "charon", "0::/kubepods/podother")
	process("21", "starter", "0::/kubepods/podabc-def")
	_, err := OpenPodIPsecRoot(t.Context(), proc, "abc-def")
	require.ErrorContains(t, err, "no live charon")
	path := process("22", "charon", "0::/kubepods.slice/kubepods-burstable-podabc_def.slice/cri-containerd-test.scope")
	root, err := OpenPodIPsecRoot(t.Context(), proc, "abc-def")
	require.NoError(t, err)
	defer root.Close()
	// Replacing the PID's root after discovery must not switch the pinned root.
	require.NoError(t, os.Rename(filepath.Join(path, "root"), filepath.Join(path, "previous")))
	require.NoError(t, os.MkdirAll(filepath.Join(path, "root/etc"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "root/etc/ipsec.conf"), []byte("other pod"), 0o600))
	data, err := os.ReadFile(filepath.Join("/proc/self/fd", strconv.FormatUint(uint64(root.Fd()), 10), "etc/ipsec.conf"))
	require.NoError(t, err)
	require.Equal(t, "22", string(data))
	process("23", "charon", "0::/kubepods/podabc-def")
	_, err = OpenPodIPsecRoot(t.Context(), proc, "abc-def")
	require.ErrorContains(t, err, "multiple charon")
	_, err = OpenPodIPsecRoot(t.Context(), proc, "../other")
	require.ErrorContains(t, err, "pod UID")
}
