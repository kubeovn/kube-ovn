package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	godbus "github.com/godbus/dbus/v5"
	"github.com/kubeovn/gonetworkmanager/v3"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/nmhelper"
)

type fakeNMDevice struct {
	gonetworkmanager.Device
	err     error
	managed []bool
}

func (d *fakeNMDevice) SetPropertyManaged(managed bool) error {
	d.managed = append(d.managed, managed)
	return d.err
}

func TestSetUnmanagedDirect(t *testing.T) {
	// NetworkManager returns this value, not a pointer, through godbus.
	denied := godbus.Error{Name: "org.freedesktop.NetworkManager.PermissionDenied", Body: []any{"Not authorized to perform this operation"}}
	for name, err := range map[string]error{"denied": denied, "wrapped denied": fmt.Errorf("set property: %w", denied)} {
		t.Run(name, func(t *testing.T) {
			device := &fakeNMDevice{err: err}
			got := setUnmanagedFunc(false, "")(device, "enp2s0")
			require.Equal(t, []bool{false}, device.managed)
			require.ErrorContains(t, got, "NetworkManager denied setting enp2s0 unmanaged")
			require.ErrorContains(t, got, "ENABLE_NM_HELPER in install.sh, func.ENABLE_NM_HELPER or features.enableNmHelper in the charts")
			require.ErrorContains(t, got, "or mark enp2s0 unmanaged on the host")
			var dbusErr godbus.Error
			require.ErrorAs(t, got, &dbusErr)
			require.Equal(t, denied.Name, dbusErr.Name)
		})
	}
	for name, err := range map[string]error{
		"success":     nil,
		"other D-Bus": godbus.Error{Name: "org.freedesktop.NetworkManager.UnknownDevice", Body: []any{"No device found"}},
		"plain error": errors.New("connection closed"),
	} {
		t.Run(name, func(t *testing.T) {
			device := &fakeNMDevice{err: err}
			require.Equal(t, err, setUnmanagedFunc(false, "")(device, "enp2s0"))
			require.Equal(t, []bool{false}, device.managed)
		})
	}
}

func TestSetUnmanagedThroughHelper(t *testing.T) {
	if uid := os.Geteuid(); uid != 0 && uid != 65534 {
		t.Skip("NetworkManager helper accepts only root or nobody")
	}
	dir, err := os.MkdirTemp("", "nm-helper-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	socket := filepath.Join(dir, "nm-helper.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
	require.NoError(t, err)
	calls := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		done <- nmhelper.Serve(listener, func(iface string) error {
			calls <- iface
			if iface == "missing0" {
				return errors.New("no device found")
			}
			return nil
		})
	}()
	t.Cleanup(func() { _ = listener.Close(); require.ErrorIs(t, <-done, net.ErrClosed) })

	device := &fakeNMDevice{}
	setUnmanaged := setUnmanagedFunc(true, socket)
	require.NoError(t, setUnmanaged(device, "enp2s0"))
	require.Equal(t, "enp2s0", <-calls)
	err = setUnmanaged(device, "missing0")
	require.EqualError(t, err, `failed to set missing0 unmanaged: NetworkManager helper replied "no device found"`)
	require.Equal(t, "missing0", <-calls)
	require.Empty(t, device.managed, "the helper path must not write through the daemon's D-Bus connection")
}

// No silent fallback to the direct write when the helper is unreachable; runs as any UID.
func TestSetUnmanagedHelperUnreachable(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "nm-helper.sock")
	device := &fakeNMDevice{}
	require.ErrorContains(t, setUnmanagedFunc(true, socket)(device, "enp2s0"), "failed to set enp2s0 unmanaged: dial unixpacket "+socket)
	require.Empty(t, device.managed)
}
