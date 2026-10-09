package kohelper

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

const netnsTestUID = "12345678-1234-1234-1234-123456789abc"

func TestResolvePodNetns(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host proc namespace resolution is Linux-only")
	}
	root := t.TempDir()
	process := func(pid, cgroup, namespace string) {
		path := filepath.Join(root, pid)
		require.NoError(t, os.MkdirAll(filepath.Join(path, "ns"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(path, "cgroup"), []byte(cgroup), 0o600))
		if namespace != "" {
			require.NoError(t, os.Symlink(namespace, filepath.Join(path, "ns/net")))
		}
	}
	process("10", "0::/kubepods/pod"+netnsTestUID+"-foreign/container", "net:[9]")
	process("11", "0::/kubepods/pod"+netnsTestUID+"/container", "")
	process("12", "0::/kubepods/pod"+netnsTestUID+"/container", "net:[42]")
	process("13", "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod12345678_1234_1234_1234_123456789abc.slice/cri-containerd.scope", "net:[42]")
	path, err := ResolvePodNetns(t.Context(), root, netnsTestUID)
	require.NoError(t, err)
	require.Equal(t, "/proc/12/ns/net", path)
	_, err = ResolvePodNetns(t.Context(), root, "foreign-uid")
	require.ErrorContains(t, err, "no live host process")
	_, err = ResolvePodNetns(t.Context(), root, "")
	require.Error(t, err)
	process("14", "0::/kubepods/pod"+netnsTestUID+"/container", "net:[43]")
	_, err = ResolvePodNetns(t.Context(), root, netnsTestUID)
	require.ErrorContains(t, err, "multiple network namespaces")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = ResolvePodNetns(ctx, root, netnsTestUID)
	require.ErrorIs(t, err, context.Canceled)
}

func TestResolvePodNetnsReportsUnexpectedProcessErrors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host proc namespace resolution is Linux-only")
	}
	for _, operation := range []string{"cgroup", "ns/net"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			process := filepath.Join(root, "10")
			require.NoError(t, os.MkdirAll(filepath.Join(process, "ns"), 0o700))
			failure := syscall.EISDIR
			if operation == "cgroup" {
				require.NoError(t, os.Mkdir(filepath.Join(process, "cgroup"), 0o700))
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(process, "cgroup"), []byte("0::/kubepods/pod"+netnsTestUID), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(process, "ns/net"), nil, 0o600))
				failure = syscall.EINVAL
			}
			_, err := ResolvePodNetns(t.Context(), root, netnsTestUID)
			require.ErrorIs(t, err, failure)
			require.ErrorContains(t, err, filepath.Join(process, operation))
			// An unreadable unrelated process must not hide a usable match.
			valid := filepath.Join(root, "11")
			require.NoError(t, os.MkdirAll(filepath.Join(valid, "ns"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(valid, "cgroup"), []byte("0::/kubepods/pod"+netnsTestUID), 0o600))
			require.NoError(t, os.Symlink("net:[42]", filepath.Join(valid, "ns/net")))
			path, err := ResolvePodNetns(t.Context(), root, netnsTestUID)
			require.NoError(t, err)
			require.Equal(t, "/proc/11/ns/net", path)
		})
	}
}
