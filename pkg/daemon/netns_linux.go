package daemon

import (
	"fmt"
	"os"

	"github.com/containernetworking/plugins/pkg/ns"
	"golang.org/x/sys/unix"
)

// this file is copied from https://github.com/containerd/containerd/blob/main/pkg/netns/netns_linux.go

// getCurrentThreadNetNSPath copied from pkg/ns
func getCurrentThreadNetNSPath() string {
	// /proc/self/ns/net returns the namespace of the main thread, not
	// of whatever thread this goroutine is running on.  Make sure we
	// use the thread's net namespace since the thread is switching around
	return fmt.Sprintf("/proc/%d/task/%d/ns/net", os.Getpid(), unix.Gettid())
}

// newNetNSInCurrentMountNamespace must be called from a locked OS thread. It
// is used by the node gateway helper after entering the host mount namespace;
// moving the operation to another goroutine could inherit the wrong mount
// namespace.
func newNetNSInCurrentMountNamespace(path string) (err error) {
	// A previous process may have left the mount point as an ordinary file.
	// Reuse that fixed, trusted path instead of requiring the parent directory
	// to be writable by the helper or failing forever on O_EXCL.
	mountPointFd, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o660)
	created := err == nil
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("network namespace mount point %s is not a regular file", path)
		}
		mountPointFd, err = os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("open existing network namespace mount point %s: %w", path, err)
		}
	}
	if err := mountPointFd.Close(); err != nil {
		if created {
			_ = os.Remove(path)
		}
		return err
	}

	defer func() {
		// Ensure the mount point is cleaned up on errors
		if err != nil && created {
			os.RemoveAll(path)
		}
	}()

	var origNS ns.NetNS
	if origNS, err = ns.GetNS(getCurrentThreadNetNSPath()); err != nil {
		return err
	}
	defer origNS.Close()

	if err = unix.Unshare(unix.CLONE_NEWNET); err != nil {
		return err
	}
	// Put this thread back to the original namespace before it can be reused.
	defer func() { _ = origNS.Set() }()

	// Bind mounting the namespace keeps it alive after this thread exits.
	if err = unix.Mount(getCurrentThreadNetNSPath(), path, "none", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to bind mount ns at %s: %w", path, err)
	}
	return nil
}
