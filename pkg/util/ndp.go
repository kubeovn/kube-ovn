//go:build !windows
// +build !windows

package util

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/mdlayher/ndp"
	"k8s.io/klog/v2"
)

// DuplicateAddressDetection performs Duplicate Address Detection (DAD) for the given IP address on the specified interface.
// It sends a Neighbor Solicitation message and waits for a Neighbor Advertisement response.
// If a Neighbor Advertisement is received with the same target address, it indicates that the address is already in use.
// If no response is received within the timeout period, it indicates that the address is available for use.
// Returns true if the address is available, false if it is already in use, and an error if any occurred during the process.
// Note: This function is designed to work with IPv6 addresses only.
// Returns:
// - true if the address is available for use
// - false if the address is already in use
// - net.HardwareAddr of the conflicting address if available
// - error if any occurred during the process
func DuplicateAddressDetection(iface, ip string) (bool, net.HardwareAddr, error) {
	target, err := netip.ParseAddr(ip)
	if err != nil {
		err = fmt.Errorf("failed to parse ip address %q: %w", ip, err)
		klog.Error(err)
		return false, nil, err
	}

	if !target.Is6() {
		err = fmt.Errorf("ip address %q is not ipv6", ip)
		klog.Error(err)
		return false, nil, err
	}

	snm, err := ndp.SolicitedNodeMulticast(target)
	if err != nil {
		err = fmt.Errorf("failed to get solicited node multicast address for %q: %w", target.String(), err)
		klog.Error(err)
		return false, nil, err
	}

	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		err = fmt.Errorf("failed to get interface %q: %w", iface, err)
		klog.Error(err)
		return false, nil, err
	}

	conn, _, err := ndp.Listen(ifi, ndp.LinkLocal)
	if err != nil {
		err = fmt.Errorf("failed to listen on interface %q: %w", iface, err)
		klog.Error(err)
		return false, nil, err
	}
	defer conn.Close()

	msg := &ndp.NeighborSolicitation{
		TargetAddress: target,
		Options: []ndp.Option{
			&ndp.LinkLayerAddress{
				Direction: ndp.Source,
				Addr:      ifi.HardwareAddr,
			},
		},
	}

	if err = conn.WriteTo(msg, nil, snm); err != nil {
		err = fmt.Errorf("failed to send neighbor solicitation message: %w", err)
		klog.Error(err)
		return false, nil, err
	}

	if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		err = fmt.Errorf("failed to set read deadline: %w", err)
		klog.Error(err)
		return false, nil, err
	}

	for {
		msg, _, _, err := conn.ReadFrom()
		if err == nil {
			na, ok := msg.(*ndp.NeighborAdvertisement)
			if !ok || na.TargetAddress != target {
				continue
			}
			for _, opt := range na.Options {
				if opt, ok := opt.(*ndp.LinkLayerAddress); ok && opt.Direction == ndp.Target {
					return false, opt.Addr, nil
				}
			}
			return false, nil, nil
		}
		if e, ok := err.(net.Error); ok && e.Timeout() {
			// No response received, address is available
			return true, nil, nil
		}
		err = fmt.Errorf("failed to read from connection: %w", err)
		klog.Error(err)
		return false, nil, err
	}
}
