package util

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestSetLinkUp(t *testing.T) {
	// 1. should fail
	linkName := "abc"
	err := SetLinkUp(linkName)
	require.Error(t, err)

	// 2. should succeed
	// get the default route gw and nic
	routes, err := netlink.RouteList(nil, unix.AF_UNSPEC)
	if errors.Is(err, netlink.ErrNotImplemented) {
		return // skip if not implemented
	}
	if err != nil {
		t.Fatalf("failed to get routes: %v", err)
	}
	var nicIndex int
	for _, r := range routes {
		if r.Dst != nil && r.Dst.IP.String() == "0.0.0.0" {
			nicIndex = r.LinkIndex
		}
	}
	if nicIndex == 0 {
		t.Fatalf("failed to get nic")
	}

	link, err := netlink.LinkByIndex(nicIndex)
	if err != nil {
		t.Fatalf("failed to get link: %v", err)
	}
	linkName = link.Attrs().Name
	err = SetLinkUp(linkName)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP request operation not permitted")
			return
		}
		t.Errorf("Error resolving ARP: %v", err)
	}
	require.NoError(t, err)
}
