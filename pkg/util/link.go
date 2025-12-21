package util

import (
	"fmt"

	"k8s.io/klog/v2"

	"github.com/vishvananda/netlink"
)

// SetLinkUp sets a link up
func SetLinkUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get link %s: %w", name, err)
	}
	if err = netlink.LinkSetUp(link); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set link %s up: %w", name, err)
	}

	return nil
}
