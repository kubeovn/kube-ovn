//go:build !windows
// +build !windows

package util

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/mdlayher/arp"
)

func Arping(nic, srcIP, dstIP string, timeout time.Duration, maxRetry int) (net.HardwareAddr, int, error) {
	target, err := netip.ParseAddr(dstIP)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse target address %s: %v", dstIP, err)
	}

	var count int
	var ifi *net.Interface
	for ; count < maxRetry; count++ {
		if ifi, err = net.InterfaceByName(nic); err == nil {
			break
		}
		time.Sleep(timeout)
	}
	if err != nil {
		return nil, count, fmt.Errorf("failed to get interface %s: %v", nic, err)
	}

	var client *arp.Client
	for ; count < maxRetry; count++ {
		if client, err = arp.Dial(ifi); err == nil {
			defer client.Close()
			break
		}
		time.Sleep(timeout)
	}
	if err != nil {
		return nil, count, fmt.Errorf("failed to set up ARP client: %v", err)
	}

	for ; count < maxRetry; count++ {
		if err = client.SetDeadline(time.Now().Add(timeout)); err != nil {
			continue
		}
		if mac, err := client.Resolve(target); err == nil {
			return mac, count + 1, nil
		}
	}

	return nil, count, fmt.Errorf("resolve MAC address of %s timeout: %v", dstIP, err)
}
