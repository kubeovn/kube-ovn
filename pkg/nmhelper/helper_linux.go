package nmhelper

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/kubeovn/gonetworkmanager/v3"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

// ServeHelper owns the only NetworkManager write of the node daemon. It runs as
// UID 0 because NetworkManager authorizes this write without polkit only for root.
func ServeHelper() error {
	// The image installs this binary unreadable (0111), so exec already makes
	// it non-dumpable; this also covers readable copies.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := os.Remove(Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: Socket, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer listener.Close()
	// The Pod-local directory is mounted only by the daemon and helper. The
	// helper's GID 65534 owns the socket; SO_PEERCRED also checks the caller.
	if err := os.Chmod(Socket, 0o660); err != nil {
		return err
	}
	return Serve(listener, setUnmanaged)
}

// requestTimeout is below the client's 10s deadline, so a stalled bus frees the
// Serve worker and the client still gets the error.
var requestTimeout = 5 * time.Second

func setUnmanaged(iface string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := setDeviceUnmanaged(ctx, iface); err != nil {
		klog.Errorf("failed to set %s unmanaged by NetworkManager: %v", iface, err)
		return err
	}
	klog.Infof("set %s unmanaged by NetworkManager", iface)
	return nil
}

// setDeviceUnmanaged connects and looks the device up per request, so the
// helper survives NetworkManager and D-Bus restarts. It skips gonetworkmanager
// and dbus.SystemBus, which cannot be bounded: ctx closes this connection,
// which also ends Auth and Hello.
func setDeviceUnmanaged(ctx context.Context, iface string) (err error) {
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = ctx.Err() // the closed connection fails the call with a socket error
		}
	}()
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer conn.Close()
	var device dbus.ObjectPath
	if err = conn.Object(gonetworkmanager.NetworkManagerInterface, gonetworkmanager.NetworkManagerObjectPath).
		CallWithContext(ctx, gonetworkmanager.NetworkManagerGetDeviceByIpIface, 0, iface).Store(&device); err != nil {
		return err
	}
	return conn.Object(gonetworkmanager.NetworkManagerInterface, device).
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0, gonetworkmanager.DeviceInterface, "Managed", dbus.MakeVariant(false)).Err
}
