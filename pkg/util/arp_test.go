package util

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestMacEqual(t *testing.T) {
	tests := []struct {
		name     string
		mac1     net.HardwareAddr
		mac2     net.HardwareAddr
		expected bool
	}{
		{
			name:     "equal",
			mac1:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			mac2:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			expected: true,
		},
		{
			name:     "not_equal",
			mac1:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			mac2:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x07},
			expected: false,
		},
		{
			name:     "different_lengths",
			mac1:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			mac2:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05},
			expected: false,
		},
		{
			name:     "empty_macs",
			mac1:     nil,
			mac2:     nil,
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := macEqual(test.mac1, test.mac2)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestArpResolve(t *testing.T) {
	// get the default route gw and nic
	routes, err := netlink.RouteList(nil, unix.AF_UNSPEC)
	if errors.Is(err, netlink.ErrNotImplemented) {
		return // skip if not implemented
	}
	if err != nil {
		t.Fatalf("failed to get routes: %v", err)
	}
	var defaultGW string
	var nicIndex int
	for _, r := range routes {
		if r.Dst != nil && r.Dst.IP.String() == "0.0.0.0" {
			defaultGW = r.Gw.String()
			nicIndex = r.LinkIndex
		}
	}
	if defaultGW == "" {
		t.Fatalf("failed to get default gateway")
	}
	if nicIndex == 0 {
		t.Fatalf("failed to get nic")
	}

	link, err := netlink.LinkByIndex(nicIndex)
	if err != nil {
		t.Fatalf("failed to get link: %v", err)
	}
	maxRetry := 3
	done := make(chan struct{})
	linkName := link.Attrs().Name
	mac, count, err := ArpResolve(linkName, defaultGW, time.Second, maxRetry, done)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skipf("ARP request operation not permitted: try %d, link name %s, default gw %s", count, linkName, defaultGW)
			return
		}
		t.Errorf("Error resolving ARP: %v: try %d, link name %s, default gw %s", err, count, linkName, defaultGW)
	}
	if mac == nil {
		t.Errorf("ARP resolved MAC address is nil: try %d, link name %s, default gw %s", count, linkName, defaultGW)
	}
	// should failed
	defaultGW = "xx.xx.xx.xx"
	mac, count, err = ArpResolve(linkName, defaultGW, time.Second, maxRetry, done)
	if err == nil {
		t.Errorf("Expect error, but got nil: try %d, link name %s, default gw %s", count, linkName, defaultGW)
	}
	if mac != nil {
		t.Errorf("Expect nil MAC address, but got %v: try %d, link name %s, default gw %s", mac, count, linkName, defaultGW)
	}
}
