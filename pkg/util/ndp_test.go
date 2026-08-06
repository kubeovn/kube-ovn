package util

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/mdlayher/ndp"
	"github.com/stretchr/testify/require"
)

type captureNDPWriter struct {
	packet []byte
	addr   net.Addr
}

func (w *captureNDPWriter) Close() error { return nil }

func (w *captureNDPWriter) SetWriteDeadline(time.Time) error { return nil }

func (w *captureNDPWriter) WriteTo(packet []byte, addr net.Addr) (int, error) {
	w.packet = append([]byte(nil), packet...)
	w.addr = addr
	return len(packet), nil
}

func TestAnnounceNDPAddressSendsUnsolicitedNeighborAdvertisement(t *testing.T) {
	iface, err := net.InterfaceByName("lo")
	require.NoError(t, err)

	writer := new(captureNDPWriter)
	originalListenNDPPacket := listenNDPPacket
	listenNDPPacket = func(actualIface *net.Interface) (ndpPacketWriter, error) {
		require.Equal(t, iface, actualIface)
		return writer, nil
	}
	t.Cleanup(func() { listenNDPPacket = originalListenNDPPacket })

	target := netip.MustParseAddr("fd00:10:96::fa51")
	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0xfa, 0x51}
	require.NoError(t, AnnounceNDPAddress(iface.Name, target.String(), mac, 1, time.Millisecond))
	require.Equal(t, "33:33:00:00:00:01", writer.addr.String())

	packet := gopacket.NewPacket(writer.packet, layers.LayerTypeEthernet, gopacket.Default)
	ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
	require.NotNil(t, ethernetLayer)
	ethernet := ethernetLayer.(*layers.Ethernet)
	require.Equal(t, mac, ethernet.SrcMAC)
	require.Equal(t, net.HardwareAddr{0x33, 0x33, 0x00, 0x00, 0x00, 0x01}, ethernet.DstMAC)

	ipv6Layer := packet.Layer(layers.LayerTypeIPv6)
	require.NotNil(t, ipv6Layer)
	ipv6Packet := ipv6Layer.(*layers.IPv6)
	require.Equal(t, net.IP(target.AsSlice()), ipv6Packet.SrcIP)
	require.Equal(t, net.ParseIP("ff02::1"), ipv6Packet.DstIP)
	require.Equal(t, uint8(255), ipv6Packet.HopLimit)
	require.Equal(t, layers.IPProtocolICMPv6, ipv6Packet.NextHeader)

	message, err := ndp.ParseMessage(writer.packet[14+40:])
	require.NoError(t, err)
	advertisement, ok := message.(*ndp.NeighborAdvertisement)
	require.True(t, ok)
	require.False(t, advertisement.Router)
	require.False(t, advertisement.Solicited)
	require.True(t, advertisement.Override)
	require.Equal(t, target, advertisement.TargetAddress)
	require.Equal(t, []ndp.Option{
		&ndp.LinkLayerAddress{Direction: ndp.Target, Addr: mac},
	}, advertisement.Options)
}
