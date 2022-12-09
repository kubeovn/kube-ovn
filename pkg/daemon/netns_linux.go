package daemon

import (
	"fmt"
	"os"

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
