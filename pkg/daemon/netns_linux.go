package daemon

import (
	"fmt"
	"os"
	"path"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"golang.org/x/sys/unix"
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

// New creates a new network namespace, sets it as current and returns
// a handle to it.
func newNs() (ns NsHandle, err error) {
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		return -1, err
	}
	return GetNs()
}

// NewNamed creates a new named network namespace, sets it as current,
// and returns a handle to it
func NewNamedNs(name string) (NsHandle, error) {
	if _, err := os.Stat(util.BindMountPath); os.IsNotExist(err) {
		err = os.MkdirAll(util.BindMountPath, 0755)
		if err != nil {
			return ClosedNs(), err
		}
	}

	newNs, err := newNs()
	if err != nil {
		return ClosedNs(), err
	}

	namedPath := path.Join(util.BindMountPath, name)
	f, err := os.OpenFile(namedPath, os.O_CREATE|os.O_EXCL, 0444)
	if err != nil {
		return ClosedNs(), err
	}
	f.Close()

	nsPath := fmt.Sprintf("/proc/%d/task/%d/ns/net", os.Getpid(), unix.Gettid())
	err = unix.Mount(nsPath, namedPath, "bind", unix.MS_BIND, "")
	if err != nil {
		return ClosedNs(), err
	}

	return newNs, nil
}

// DeleteNamed deletes a named network namespace
// ip netns del

func DeleteNamedNs(name string) error {
	namedPath := path.Join(util.BindMountPath, name)

	err := unix.Unmount(namedPath, unix.MNT_DETACH)
	if err != nil {
		return err
	}

	return os.Remove(namedPath)
}
