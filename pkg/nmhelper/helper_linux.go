package nmhelper

import (
	"errors"
	"net"
	"os"

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

func setUnmanaged(iface string) error {
	if err := setDeviceUnmanaged(iface); err != nil {
		klog.Errorf("failed to set %s unmanaged by NetworkManager: %v", iface, err)
		return err
	}
	klog.Infof("set %s unmanaged by NetworkManager", iface)
	return nil
}

// setDeviceUnmanaged looks the device up per request, so the helper survives
// NetworkManager restarts; dbus.SystemBus reuses a live connection.
func setDeviceUnmanaged(iface string) error {
	manager, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return err
	}
	device, err := manager.GetDeviceByIpIface(iface)
	if err != nil {
		return err
	}
	return device.SetPropertyManaged(false)
}
