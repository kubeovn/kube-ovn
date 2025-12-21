package util

import (
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/mdlayher/ndp"
	"github.com/mdlayher/netx/eui64"
	"github.com/mdlayher/packet"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const (
	// maximum number of multicast solicitations to send during DAD
	dadMaxMulticastSolicit = 3
	// retransmission timer for DAD
	dadRetransTimer = time.Second
)

var ipv6LLAPrefix = netip.MustParseAddr("fe80::").AsSlice()

var icmpv6NAFilter []bpf.RawInstruction

func init() {
	instructions := []bpf.Instruction{
		// length must be at least 86 bytes
		bpf.LoadExtension{Num: bpf.ExtLen},
		bpf.JumpIf{Cond: bpf.JumpGreaterOrEqual, Val: 14 + 40 + 32, SkipFalse: 8},
		// L3 protocol must be IPv6
		bpf.LoadExtension{Num: bpf.ExtProto},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.ETH_P_IPV6, SkipFalse: 6},
		// IPv6 L4 protocol must be ICMPv6
		bpf.LoadAbsolute{Off: 14 + 6, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.IPPROTO_ICMPV6, SkipFalse: 4},
		// ICMPv6 type must be Neighbor Advertisement
		bpf.LoadAbsolute{Off: 14 + 40, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: uint32(ipv6.ICMPTypeNeighborAdvertisement), SkipFalse: 2},
		// return this packet
		bpf.LoadExtension{Num: bpf.ExtLen},
		bpf.RetA{},
		// skip this packet
		bpf.RetConstant{},
	}

	for _, instruction := range instructions {
		ri, err := instruction.Assemble()
		if err != nil {
			panic(err)
		}
		icmpv6NAFilter = append(icmpv6NAFilter, ri)
	}
}

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

	addresses, err := ifi.Addrs()
	if err != nil {
		err = fmt.Errorf("failed to get addresses for interface %q: %w", iface, err)
		klog.Error(err)
		return false, nil, err
	}

	var srcIP net.IP
	if target.IsLinkLocalUnicast() {
		srcIP = netip.IPv6Unspecified().AsSlice()
	} else {
		// use link local address
		for _, addr := range addresses {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				klog.Warningf("failed to parse address %q: %v", addr.String(), err)
				continue
			}
			if ip.To4() == nil && ip.IsLinkLocalUnicast() {
				srcIP = ip
				break
			}
		}
		if srcIP == nil {
			if srcIP, err = eui64.ParseMAC(ipv6LLAPrefix, ifi.HardwareAddr); err != nil {
				err = fmt.Errorf("failed to generate EUI-64 format link-local address for interface %q: %w", iface, err)
				klog.Error(err)
				return false, nil, err
			}
		}
	}

	conn, err := packet.Listen(ifi, packet.Raw, unix.ETH_P_IPV6, &packet.Config{Filter: icmpv6NAFilter})
	if err != nil {
		return false, nil, fmt.Errorf("failed to listen on interface %s: %w", ifi.Name, err)
	}
	defer conn.Close()

	ns := &ndp.NeighborSolicitation{
		TargetAddress: target,
		Options: []ndp.Option{
			&ndp.LinkLayerAddress{
				Direction: ndp.Source,
				Addr:      ifi.HardwareAddr,
			},
		},
	}
	msg, err := ndp.MarshalMessageChecksum(ns, netip.MustParseAddr(srcIP.String()), snm)
	if err != nil {
		err = fmt.Errorf("failed to marshal neighbor solicitation message: %w", err)
		klog.Error(err)
		return false, nil, err
	}

	dstMac := net.HardwareAddr{0x33, 0x33, 0, 0, 0, 0}
	copy(dstMac[2:], snm.AsSlice()[12:])
	sb := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	err = gopacket.SerializeLayers(sb, opts,
		&layers.Ethernet{
			SrcMAC:       ifi.HardwareAddr,
			DstMAC:       dstMac,
			EthernetType: layers.EthernetTypeIPv6,
		},
		&layers.IPv6{
			Version:    6,
			SrcIP:      srcIP,
			DstIP:      snm.AsSlice(),
			HopLimit:   0xff,
			NextHeader: layers.IPProtocolICMPv6,
		},
		gopacket.Payload(msg),
	)
	if err != nil {
		err = fmt.Errorf("failed to serialize packet: %w", err)
		klog.Error(err)
		return false, nil, err
	}

	if err = conn.SetReadDeadline(time.Now().Add(dadRetransTimer * dadMaxMulticastSolicit)); err != nil {
		err = fmt.Errorf("failed to set read deadline: %w", err)
		klog.Error(err)
		return false, nil, err
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 1)
	macChan := make(chan net.HardwareAddr, 1)

	wg.Go(func() {
		buf := make([]byte, ifi.MTU+14)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err == nil {
				msg, err := ndp.ParseMessage(buf[14+40 : n])
				if err != nil {
					klog.Warningf("failed to parse NA message: %v", err)
					continue
				}
				na, ok := msg.(*ndp.NeighborAdvertisement)
				if !ok || na.TargetAddress != target {
					continue
				}
				for _, opt := range na.Options {
					if opt, ok := opt.(*ndp.LinkLayerAddress); ok && opt.Direction == ndp.Target {
						macChan <- opt.Addr
						return
					}
				}
				// unexpected NA without link-layer address option
				macChan <- nil
			}
			if e, ok := err.(net.Error); ok && e.Timeout() {
				// No response received, address is available
				return
			}
			klog.Error(err)
			errChan <- fmt.Errorf("failed to read from connection: %w", err)
			return
		}
	})

LOOP:
	for i := range dadMaxMulticastSolicit {
		timer := time.NewTimer(dadRetransTimer)
		if err = conn.SetWriteDeadline(time.Now().Add(dadRetransTimer)); err != nil {
			err = fmt.Errorf("failed to set write deadline: %w", err)
			klog.Error(err)
			return false, nil, err
		}
		if _, err = conn.WriteTo(sb.Bytes(), &packet.Addr{HardwareAddr: ifi.HardwareAddr}); err != nil {
			err = fmt.Errorf("failed to send neighbor solicitation message: %w", err)
			klog.Error(err)
			return false, nil, err
		}

		select {
		case <-timer.C:
			if i == dadMaxMulticastSolicit-1 {
				// last attempt, wait for response
				break LOOP
			}
		case mac := <-macChan:
			macChan <- mac
			break LOOP
		case err = <-errChan:
			return false, nil, err
		}
	}

	wg.Wait()

	select {
	case mac := <-macChan:
		return false, mac, nil
	default:
		return true, nil, nil
	}
}
