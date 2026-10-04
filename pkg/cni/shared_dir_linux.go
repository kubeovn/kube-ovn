package cni

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/moby/sys/mountinfo"
	"golang.org/x/sys/unix"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func createShortSharedDirAt(newSharedDir, originSharedDir, socketConsumption string) (err error) {
	mask := syscall.Umask(0)
	defer syscall.Umask(mask)
	if _, err = os.Stat(newSharedDir); err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(newSharedDir, 0o777)
			if err != nil {
				return fmt.Errorf("createShortSharedDir: failed to create dir (%s): %w", newSharedDir, err)
			}
			if strings.Contains(newSharedDir, util.DefaultHostVhostuserBaseDir) {
				if err = unix.Mount(originSharedDir, newSharedDir, "", unix.MS_BIND, ""); err != nil {
					return fmt.Errorf("createShortSharedDir: failed to bind mount: %w", err)
				}
			}
			return nil
		}
		return err
	}
	if socketConsumption != util.ConsumptionKubevirt {
		return fmt.Errorf("createShortSharedDir: shared directory %s already exists", newSharedDir)
	}
	return nil
}

func removeShortSharedDirAt(sharedDir, socketConsumption string) (err error) {
	if _, err = os.Stat(sharedDir); os.IsNotExist(err) {
		return nil
	}
	if socketConsumption == util.ConsumptionKubevirt {
		files, err := os.ReadDir(sharedDir)
		if err != nil {
			return fmt.Errorf("read file from dpdk share dir error: %w", err)
		}
		if len(files) != 0 {
			return nil
		}
	}
	foundMount, err := mountinfo.Mounted(sharedDir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !foundMount) {
		return nil
	}
	if err = unix.Unmount(sharedDir, 0); err != nil {
		return err
	}
	return os.Remove(sharedDir)
}
