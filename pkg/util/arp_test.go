package util

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
		if r.Dst == nil || r.Dst.IP.String() == "0.0.0.0" {
			defaultGW = r.Gw.String()
			nicIndex = r.LinkIndex
		}
	}
	if defaultGW == "" {
		t.Fatalf("failed to get default gateway")
		return
	}
	if nicIndex == 0 {
		t.Fatalf("failed to get nic")
		return
	}

	link, err := netlink.LinkByIndex(nicIndex)
	if err != nil {
		t.Fatalf("failed to get link: %v", err)
		return
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
	// invalid link name
	linkName = "invalid"
	mac, count, err = ArpResolve(linkName, defaultGW, time.Second, maxRetry, done)
	if err == nil {
		t.Errorf("Expect error, but got nil: try %d, link name %s, default gw %s", count, linkName, defaultGW)
	}
	if mac != nil {
		t.Errorf("Expect nil MAC address, but got %v: try %d, link name %s, default gw %s", mac, count, linkName, defaultGW)
	}
	linkName = link.Attrs().Name
	// invalid gw
	defaultGW = "x.x.x.x"
	mac, count, err = ArpResolve(linkName, defaultGW, time.Second, maxRetry, done)
	if err == nil {
		t.Errorf("Expect error, but got nil: try %d, link name %s, default gw %s", count, linkName, defaultGW)
	}
	if mac != nil {
		t.Errorf("Expect nil MAC address, but got %v: try %d, link name %s, default gw %s", mac, count, linkName, defaultGW)
	}
	// unreachable gw
	defaultGW = "123.45.67.8"
	mac, count, err = ArpResolve(linkName, defaultGW, time.Second, maxRetry, done)
	if err == nil {
		t.Errorf("Expect error, but got nil: try %d, link name %s, default gw %s", count, linkName, defaultGW)
	}
	if mac != nil {
		t.Errorf("Expect nil MAC address, but got %v: try %d, link name %s, default gw %s", mac, count, linkName, defaultGW)
	}
}

func TestDetectIPConflict(t *testing.T) {
	// get the default route gw and nic
	routes, err := netlink.RouteList(nil, unix.AF_UNSPEC)
	if errors.Is(err, netlink.ErrNotImplemented) {
		return // skip if not implemented
	}
	if err != nil {
		t.Fatalf("failed to get routes: %v", err)
	}
	var validIP, invalidIP string
	var nicIndex int
	var inMac, outMac net.HardwareAddr
	for _, r := range routes {
		if r.Dst == nil || r.Dst.IP.String() == "0.0.0.0" {
			nicIndex = r.LinkIndex
		}
	}

	// failed to get nic
	if nicIndex == 0 {
		return
	}

	link, err := netlink.LinkByIndex(nicIndex)
	if err != nil {
		t.Fatalf("failed to get link: %v", err)
		return
	}

	addrs, err := netlink.AddrList(link, unix.AF_INET)
	if err != nil {
		t.Fatalf("Failed to get addresses: %v", err)
		return
	}

	if len(addrs) > 0 {
		validIP = addrs[0].IP.String()
	} else {
		return
	}

	linkName := link.Attrs().Name
	inMac = link.Attrs().HardwareAddr
	outMac, err = ArpDetectIPConflict(linkName, validIP, inMac)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP request operation not permitted")
			return
		}
		t.Errorf("Error resolving ARP: %v", err)
	}
	require.Nil(t, err)
	require.Nil(t, outMac)
	// invalid ip
	invalidIP = "x.x.x.x"
	outMac, err = ArpDetectIPConflict(linkName, invalidIP, inMac)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP request operation not permitted")
			return
		}
	}
	require.NotNil(t, err)
	require.Nil(t, outMac)
	// invalid nil nic
	linkName = ""
	outMac, err = ArpDetectIPConflict(linkName, validIP, inMac)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP request operation not permitted")
			return
		}
	}
	require.NotNil(t, err)
	require.Nil(t, outMac)

	// invalid mac
	outMac, err = ArpDetectIPConflict(linkName, validIP, nil)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP request operation not permitted")
			return
		}
	}
	require.NotNil(t, err)
	require.Nil(t, outMac)
}

func TestAnnounceArpAddress(t *testing.T) {
	// get the default route gw and nic
	routes, err := netlink.RouteList(nil, unix.AF_UNSPEC)
	if errors.Is(err, netlink.ErrNotImplemented) {
		return // skip if not implemented
	}
	if err != nil {
		t.Fatalf("failed to get routes: %v", err)
	}
	var validIP, invalidIP string
	var nicIndex int
	var inMac net.HardwareAddr
	for _, r := range routes {
		if r.Dst == nil || r.Dst.IP.String() == "0.0.0.0" {
			nicIndex = r.LinkIndex
		}
	}
	if nicIndex == 0 {
		t.Fatalf("failed to get nic")
		return
	}

	link, err := netlink.LinkByIndex(nicIndex)
	if err != nil {
		t.Fatalf("failed to get link: %v", err)
		return
	}

	addrs, err := netlink.AddrList(link, unix.AF_INET)
	if err != nil {
		t.Fatalf("Failed to get addresses: %v", err)
		return
	}

	if len(addrs) > 0 {
		validIP = addrs[0].IP.String()
	} else {
		return
	}

	maxRetry := 1
	linkName := link.Attrs().Name
	inMac = link.Attrs().HardwareAddr
	err = AnnounceArpAddress(linkName, validIP, inMac, maxRetry, time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP announce operation not permitted")
			return
		}
	}
	require.Nil(t, err)
	// invalid link name
	linkName = "invalid"
	err = AnnounceArpAddress(linkName, validIP, inMac, maxRetry, time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP announce operation not permitted")
			return
		}
	}
	require.NotNil(t, err)
	linkName = link.Attrs().Name
	// invalid ip
	invalidIP = "x.x.x.x"
	err = AnnounceArpAddress(linkName, invalidIP, inMac, maxRetry, time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP announce operation not permitted")
			return
		}
	}
	require.NotNil(t, err)
	// invalid mac
	err = AnnounceArpAddress(linkName, validIP, nil, maxRetry, time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") {
			t.Skip("ARP announce operation not permitted")
			return
		}
	}
	require.NotNil(t, err)
}
