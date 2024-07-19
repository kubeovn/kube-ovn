//go:build !windows
// +build !windows

package util

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// SetLinkUp sets a link up
func SetLinkUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("failed to get link %s: %w", name, err)
	}
	if err = netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set link %s up: %w", name, err)
	}

	return nil
}
