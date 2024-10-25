//go:build !windows
// +build !windows

package daemon

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	geneveLinkName = "genev_sys_6081"
	vxlanLinkName  = "vxlan_sys_4789"
)

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
	start := time.Now()
	args := []string{"-K", vxlanLinkName, "tx", "off"}
	output, err := exec.Command("ethtool", args...).CombinedOutput() // #nosec G204
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", "ethtool", strings.Join(args, " "), elapsed)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to turn off nic tx checksum, output %s, err %s", string(output), err.Error())
	}
	return nil
}
