//go:build !windows
// +build !windows

package util

import (
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/mdlayher/arp"
	"k8s.io/klog/v2"
)

func ArpResolve(nic, srcIP, dstIP string, timeout time.Duration, maxRetry int) (net.HardwareAddr, int, error) {
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

	var mac net.HardwareAddr
	for ; count < maxRetry; count++ {
		if err = client.SetDeadline(time.Now().Add(timeout)); err != nil {
			continue
		}
		if mac, err = client.Resolve(target); err == nil {
			return mac, count + 1, nil
		}
	}

	return nil, count, fmt.Errorf("resolve MAC address of %s timeout: %v", dstIP, err)
}

func macEqual(a, b net.HardwareAddr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// https://www.ietf.org/rfc/rfc5227.txt
// returns MAC of the host if the ip address is in use
func ArpDetectIPConflict(nic, ip string, mac net.HardwareAddr) (net.HardwareAddr, error) {
	const (
		probeWait        = 1 * time.Second // initial random delay
		probeNum         = 3               // number of probe packets
		probeMin         = 1 * time.Second // minimum delay until repeated probe
		probeMax         = 2 * time.Second // maximum delay until repeated probe
		announceWait     = 2 * time.Second // delay before announcing
		announceNum      = 2               // number of Announcement packets
		announceInterval = 2 * time.Second // time between Announcement packets
	)

	tpa, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IP address %s: %v", ip, err)
	}
	ip = tpa.String()

	spa := netip.AddrFrom4([4]byte{0, 0, 0, 0})
	tha := net.HardwareAddr{0, 0, 0, 0, 0, 0}
	pkt, err := arp.NewPacket(arp.OperationRequest, mac, spa, tha, tpa)
	if err != nil {
		return nil, err
	}

	ifi, err := net.InterfaceByName(nic)
	if err != nil {
		return nil, err
	}

	client, err := arp.Dial(ifi)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	deadline := time.Now()
	durations := make([]time.Duration, probeNum)
	// wait for a random time interval selected uniformly in the range zero to
	// PROBE_WAIT seconds
	durations[0] = time.Duration(rand.Int63n(int64(probeWait)))
	deadline = deadline.Add(durations[0])
	for i := 1; i < probeNum; i++ {
		// send PROBE_NUM probe packets, each of these probe packets spaced
		// randomly and uniformly, PROBE_MIN to PROBE_MAX seconds apart
		durations[i] = probeMin + time.Duration(rand.Int63n(int64(probeMax-probeMin)))
		deadline = deadline.Add(durations[i])
	}

	var readErr error
	var wg sync.WaitGroup
	ch := make(chan net.HardwareAddr, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			if time.Now().After(deadline) {
				break
			}

			if readErr = client.SetReadDeadline(deadline); readErr != nil {
				return
			}

			pkt, _, err := client.Read()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok {
					if netErr, ok := opErr.Err.(net.Error); ok && netErr.Timeout() {
						// read timeout, ignore
						return
					}
				}
				readErr = err
				return
			}

			if pkt.SenderIP.String() == ip {
				ch <- pkt.SenderHardwareAddr
				return
			}

			spa := pkt.SenderIP.As4()
			if pkt.Operation == arp.OperationRequest &&
				net.IP(spa[:]).Equal(net.IPv4zero) &&
				macEqual(pkt.TargetHardwareAddr, tha) &&
				pkt.TargetIP.String() == ip &&
				!macEqual(pkt.SenderHardwareAddr, mac) {
				// received probe from another host
				// treat this as an address conflict
				klog.Infof("received IPv4 address probe for %s from host %s", ip, pkt.SenderHardwareAddr.String())
				ch <- pkt.SenderHardwareAddr
				return
			}
		}
	}()

	dstMac := net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	for i := 0; i < probeNum; i++ {
		time.Sleep(durations[i])

		select {
		case mac := <-ch:
			// the IPv4 address is in use by another host
			return mac, nil
		default:
		}

		if err = client.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			return nil, err
		}
		if err = client.WriteTo(pkt, dstMac); err != nil {
			return nil, err
		}
	}

	wg.Wait()

	if readErr != nil {
		klog.Error(readErr)
		return nil, readErr
	}

	// The address may be used safely. Broadcast ANNOUNCE_NUM ARP
	// Announcements, spaced ANNOUNCE_INTERVAL seconds apart. An ARP
	// Announcement is identical to the ARP Probe described above,
	// except that now the sender and target IP addresses are both
	// set to the host's newly selected IPv4 address.
	if err = AnnounceArpAddress(nic, ip, mac, announceNum, announceInterval); err != nil {
		return nil, err
	}

	return nil, nil
}

func AnnounceArpAddress(nic, ip string, mac net.HardwareAddr, announceNum int, announceInterval time.Duration) error {
	klog.Infof("announce arp address nic %s , ip %s, with mac %v ", nic, ip, mac)
	netInterface, err := net.InterfaceByName(nic)
	if err != nil {
		return err
	}

	client, err := arp.Dial(netInterface)
	if err != nil {
		return err
	}
	defer client.Close()

	tpa, err := netip.ParseAddr(ip)
	if err != nil {
		klog.Errorf("failed to parse IP address %s: %v ", ip, err)
		return err
	}
	tha := net.HardwareAddr{0, 0, 0, 0, 0, 0}
	pkt, err := arp.NewPacket(arp.OperationRequest, mac, tpa, tha, tpa)
	if err != nil {
		return err
	}

	dstMac := net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	for i := 0; i < announceNum; i++ {
		c := time.NewTimer(announceInterval)
		if err = client.SetDeadline(time.Now().Add(announceInterval)); err != nil {
			return err
		}
		if err = client.WriteTo(pkt, dstMac); err != nil {
			return err
		}
		if i == announceNum-1 {
			// the last one, no need to wait
			c.Stop()
		} else {
			<-c.C
		}
	}

	return nil
}
