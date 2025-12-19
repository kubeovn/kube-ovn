package main

import (
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"

	"github.com/kubeovn/kube-ovn/pkg/daemon"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func printCaps() {
	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())
}

func initForOS() error {
	if _, err := netlink.LinkByName(util.GeneveNic); err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		klog.Errorf("failed to get link %s: %v", util.GeneveNic, err)
		return err
	}

	// disable checksum for genev_sys_6081 as default
	return daemon.TurnOffNicTxChecksum(util.GeneveNic)
}

func setVxlanNicTxOff() error {
	if _, err := netlink.LinkByName(util.VxlanNic); err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		klog.Errorf("failed to get link %s: %v", util.VxlanNic, err)
		return err
	}

	// disable checksum for vxlan_sys_4789 as default
	return daemon.TurnOffNicTxChecksum(util.VxlanNic)
}
