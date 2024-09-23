//go:build !windows
// +build !windows

package daemon

import (
	"fmt"
	"os/exec"

	"k8s.io/klog/v2"
)

<<<<<<< HEAD
=======
const (
	geneveLinkName = "genev_sys_6081"
	vxlanLinkName  = "vxlan_sys_4789"
)

func printCaps() {
	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())
}

>>>>>>> 5a70de9c (allow user to set vxlan_sys_4789 tx off (#4543))
func initForOS() error {
	// disable checksum for genev_sys_6081 as default
	cmd := exec.Command("sh", "-c", "ethtool -K genev_sys_6081 tx off")
	if err := cmd.Run(); err != nil {
		err := fmt.Errorf("failed to set checksum off for genev_sys_6081, %w", err)
		// should not affect cni pod running if failed, just record err log
		klog.Error(err)
	}
	return nil
}

func setVxlanNicTxOff() error {
	if _, err := netlink.LinkByName(vxlanLinkName); err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		klog.Errorf("failed to get link %s: %v", vxlanLinkName, err)
		return err
	}

	// disable checksum for vxlan_sys_4789 as default
	return daemon.TurnOffNicTxChecksum(vxlanLinkName)
}
