//go:build !windows
// +build !windows

package daemon

import (
	"fmt"
	"os/exec"

	"k8s.io/klog/v2"
)

func initForOS() error {
	// disable checksum for genev_sys_6081 as default
	cmd := exec.Command("sh", "-c", "ethtool -K genev_sys_6081 tx off")
	if err := cmd.Run(); err != nil {
		err := fmt.Errorf("failed to set checksum off for genev_sys_6081, %v", err)
		// should not affect cni pod running if failed, just record err log
		klog.Error(err)
	}
	return nil
}
