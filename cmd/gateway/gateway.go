package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/gateway"
	"bitbucket.org/mathildetech/kube-ovn/pkg/ovs"
	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
	"github.com/vishvananda/netlink"
	"k8s.io/klog"
	"os"
)

func main() {
	klog.SetOutput(os.Stdout)
	defer klog.Flush()

	config, err := gateway.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}

	bridge, err := util.NicToBridge(config.Interface)
	if err != nil {
		klog.Errorf("failed to move nic to bridge %v", err)
		os.Exit(1)
	}
	klog.Infof("create bridge %v for gw", bridge)

	ovnClient := ovs.NewClient(config.OvnNbHost, config.OvnNbPort, config.OvnSbHost, config.OvnSbPort, "", "", "")

	err = ovnClient.CreateGatewayRouter(config.EdgeRouterName, config.Chassis)
	if err != nil {
		klog.Errorf("failed to crate gateway router %v", err)
		os.Exit(1)
	}

	err = ovnClient.CreateTransitLogicalSwitch(config.TransitSwitchName, config.ClusterRouterName, config.EdgeRouterName, config.ClusterRouterIP, config.EdgeRouterIP)
	if err != nil {
		klog.Errorf("failed to connect edge and cluster by transit %v", err)
		os.Exit(1)
	}

	addrList, err := netlink.AddrList(bridge, netlink.FAMILY_V4)
	if err != nil {
		klog.Errorf("failed to list %s addr %v", bridge, err)
		os.Exit(1)
	}
	if len(addrList) == 0 {
		klog.Errorf("nic %s has no ip address", bridge)
		os.Exit(1)
	}

	err = ovnClient.CreateOutsideLogicalSwitch(config.OutsideSwitchName, config.EdgeRouterName, addrList[0].IPNet.String(), bridge.Attrs().HardwareAddr.String())
	if err != nil {
		klog.Errorf("failed to connect edge to outside %v", err)
		os.Exit(1)
	}
	return
}
