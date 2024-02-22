package daemon

import (
	"fmt"
	"os"
	"path"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// NsHandle is a handle to a network namespace. It can be cast directly
// to an int and used as a file descriptor.
type NsHandle int

// GetFromPath gets a handle to a network namespace
// identified by the path
func GetNsFromPath(path string) (NsHandle, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	return NsHandle(fd), nil
}

// GetFromThread gets a handle to the network namespace of a given pid and tid.
func GetNsFromThread(pid, tid int) (NsHandle, error) {
	return GetNsFromPath(fmt.Sprintf("/proc/%d/task/%d/ns/net", pid, tid))
}

// Get gets a handle to the current threads network namespace.
func GetNs() (NsHandle, error) {
	return GetNsFromThread(os.Getpid(), unix.Gettid())
}

// GetFromName gets a handle to a named network namespace such as one
// created by `ip netns add`.
func GetNsFromName(name string) (NsHandle, error) {
	return GetNsFromPath(fmt.Sprintf("/var/run/netns/%s", name))
}

// None gets an empty (closed) NsHandle.
func ClosedNs() NsHandle {
	return NsHandle(-1)
}

// DeleteNamed deletes a named network namespace
// ip netns del
func DeleteNamedNs(name string) error {
	namedPath := path.Join(util.BindMountPath, name)
	if _, err := os.Stat(namedPath); os.IsNotExist(err) {
		// already deleted
		return nil
	}
	err := unix.Unmount(namedPath, unix.MNT_DETACH)
	if err != nil {
		klog.Error(err)
		return err
	}

	return os.Remove(namedPath)
}
