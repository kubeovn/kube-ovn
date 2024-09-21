//go:build !windows
// +build !windows

package main

import (
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"

	"github.com/kubeovn/kube-ovn/pkg/daemon"
)

const geneveLinkName = "genev_sys_6081"
const vxlanLinkName = "vxlan_sys_4789"

func printCaps() {
	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())
}

func initForOS() error {
	if _, err := netlink.LinkByName(geneveLinkName); err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		klog.Errorf("failed to get link %s: %v", geneveLinkName, err)
		return err
	}

	// disable checksum for genev_sys_6081 as default
	return daemon.TurnOffNicTxChecksum(geneveLinkName)
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
