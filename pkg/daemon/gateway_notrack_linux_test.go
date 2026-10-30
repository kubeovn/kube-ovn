package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/kubeovn/felix/ipsets"
	"github.com/kubeovn/go-iptables/iptables"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/knftables"
)

// Injecting Ethernet frames across a veth exercises PREROUTING without first
// traversing the sender's OUTPUT conntrack hook. Run only in an isolated netns.
func TestTunnelNoTrackPreservesServiceDNAT(t *testing.T) {
	if os.Getenv("KUBE_OVN_TEST_NETFILTER") != "true" {
		t.Skip("requires isolated Linux netfilter environment")
	}
	for modeIndex, mode := range []string{"nftables", "iptables-nft", "iptables-legacy"} {
		for _, family := range []knftables.Family{knftables.IPv4Family, knftables.IPv6Family} {
			for _, port := range []int{6081, 4789} {
				t.Run(fmt.Sprintf("%s/%s/%d", mode, family, port), func(t *testing.T) {
					testTunnelServiceDNAT(t, mode, family, port, 40000+modeIndex*100)
				})
			}
		}
	}
}

func testTunnelServiceDNAT(t *testing.T, mode string, family knftables.Family, port, sourcePort int) {
	peer, vip, local := "198.18.0.2", "198.18.0.10", "198.18.0.20"
	prefix, hostPrefix := "/24", "/32"
	if family == knftables.IPv6Family {
		peer, vip, local = "fd00:198:18::2", "fd00:198:18::10", "fd00:198:18::20"
		prefix, hostPrefix = "/64", "/128"
	}
	txLink := &netlink.Veth{Name: "notrack-tx", PeerName: "notrack-rx"}
	require.NoError(t, netlink.LinkAdd(txLink))
	t.Cleanup(func() { require.NoError(t, netlink.LinkDel(txLink)) })
	rxLink, err := netlink.LinkByName("notrack-rx")
	require.NoError(t, err)
	for _, cidr := range []string{vip + prefix, local + hostPrefix} {
		address, err := netlink.ParseAddr(cidr)
		require.NoError(t, err)
		address.Flags = unix.IFA_F_NODAD
		require.NoError(t, netlink.AddrAdd(rxLink, address))
	}
	require.NoError(t, netlink.LinkSetUp(rxLink))
	require.NoError(t, netlink.LinkSetUp(txLink))
	txIface, err := netlink.LinkByName("notrack-tx")
	require.NoError(t, err)
	installTunnelRulesForPacketTest(t, mode, family, port, peer, local)

	writer, err := knftables.New(family, "notrack-observe", knftables.EmulateDestroy)
	require.NoError(t, err)
	tx := writer.NewTransaction()
	tx.Add(&knftables.Table{})
	tx.Add(&knftables.Chain{Name: "nat-pre", Type: knftables.PtrTo(knftables.NATType), Hook: knftables.PtrTo(knftables.PreroutingHook), Priority: knftables.PtrTo(knftables.DNATPriority)})
	tx.Add(&knftables.Rule{Chain: "nat-pre", Rule: fmt.Sprintf("udp dport %d dnat to %s", port, net.JoinHostPort(vip, "9999"))})
	tx.Add(&knftables.Chain{Name: "input", Type: knftables.PtrTo(knftables.FilterType), Hook: knftables.PtrTo(knftables.InputHook), Priority: knftables.PtrTo(knftables.FilterPriority)})
	tx.Add(&knftables.Counter{Name: "untracked"})
	tx.Add(&knftables.Rule{Chain: "input", Rule: fmt.Sprintf("udp dport %d ct state untracked counter name untracked", port)})
	t.Cleanup(func() {
		cleanup := writer.NewTransaction()
		cleanup.Destroy(&knftables.Table{})
		require.NoError(t, writer.Run(context.Background(), cleanup))
	})
	require.NoError(t, writer.Run(context.Background(), tx))

	for i, scenario := range []struct {
		name, destination string
		listenPort        int
	}{{"service", vip, 9999}, {"tunnel", local, port}} {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(scenario.destination), Port: scenario.listenPort})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		injectTunnelTestPacket(t, txIface, rxLink, peer, scenario.destination, sourcePort+i, port, scenario.name)
		buffer := make([]byte, 64)
		n, _, err := conn.ReadFromUDP(buffer)
		require.NoError(t, err, scenario.name+" packet was not delivered to expected port")
		require.Equal(t, scenario.name, string(buffer[:n]))
	}
	counters, err := writer.ListCounters(context.Background())
	require.NoError(t, err)
	require.Len(t, counters, 1)
	require.EqualValues(t, 1, *counters[0].Packets, "only the tunnel packet should be untracked")
}

func installTunnelRulesForPacketTest(t *testing.T, mode string, family knftables.Family, port int, peer, local string) {
	if mode == "nftables" {
		writer, err := knftables.New("", "", knftables.EmulateDestroy)
		require.NoError(t, err)
		overlay := "IPv6"
		if family == knftables.IPv6Family {
			overlay = "IPv4"
		}
		desired, err := buildNFTGatewaySnapshot(nftSnapshotInput{Protocol: overlay, TunnelPort: port, TunnelPeerIPs: []string{peer}, LocalEncapIPs: []string{local}})
		require.NoError(t, err)
		snapshot := nftFamilySnapshotForTest(t, desired, family)
		require.True(t, snapshot.TunnelOnly)
		snapshot.Table = "notrack-production"
		backend := &nftGatewayBackend{writer: writer}
		t.Cleanup(func() {
			tx := writer.NewTransaction()
			tx.Destroy(&knftables.Table{Family: family, Name: snapshot.Table})
			require.NoError(t, writer.Run(context.Background(), tx))
		})
		require.NoError(t, writer.Run(context.Background(), backend.renderFull(gatewayNFTSnapshot{Families: []nftFamilySnapshot{snapshot}})))
		return
	}
	protocol, setFamily, outer, overlay, prefix := iptables.ProtocolIPv4, ipsets.IPFamilyV4, "IPv4", "IPv6", "ovn40"
	if family == knftables.IPv6Family {
		protocol, setFamily, outer, overlay, prefix = iptables.ProtocolIPv6, ipsets.IPFamilyV6, "IPv6", "IPv4", "ovn60"
	}
	ipt, err := iptables.NewWithProtocolAndMode(protocol, mode[len("iptables-"):])
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(&corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: peer}}}}))
	networkType := "geneve"
	if port == 4789 {
		networkType = "vxlan"
	}
	c := &Controller{protocol: overlay, config: &Configuration{NodeName: "self", NetworkType: networkType, DefaultEncapIP: local}, nodesLister: listerv1.NewNodeLister(indexer), iptables: map[string]*iptables.IPTables{outer: ipt}, ipsets: map[string]*ipsets.IPSets{outer: ipsets.NewIPSets(ipsets.NewIPVersionConfig(setFamily, IPSetPrefix, nil, nil))}}
	peerSet, localSet := prefix+OtherNodeSet, prefix+LocalEncapSet
	t.Cleanup(func() {
		for _, name := range []string{peerSet, localSet} {
			out, err := exec.Command("ipset", "destroy", name).CombinedOutput()
			require.NoError(t, err, string(out))
		}
	})
	require.NoError(t, c.setTunnelIPSets(outer))
	_, rules := generateTunnelNoTrackRules(0, peerSet, localSet)
	t.Cleanup(func() {
		for _, rule := range rules {
			bestEffortDeleteIptablesRule(ipt, rule)
		}
	})
	c.reconcileTunnelIptables()
}

func injectTunnelTestPacket(t *testing.T, tx, rx netlink.Link, source, destination string, sourcePort, port int, payload string) {
	eth := &layers.Ethernet{SrcMAC: tx.Attrs().HardwareAddr, DstMAC: rx.Attrs().HardwareAddr, EthernetType: layers.EthernetTypeIPv4}
	udp := &layers.UDP{SrcPort: layers.UDPPort(sourcePort), DstPort: layers.UDPPort(port)}
	var network gopacket.SerializableLayer
	if net.ParseIP(source).To4() != nil {
		ip := &layers.IPv4{Version: 4, TTL: 64, SrcIP: net.ParseIP(source), DstIP: net.ParseIP(destination), Protocol: layers.IPProtocolUDP}
		require.NoError(t, udp.SetNetworkLayerForChecksum(ip))
		network = ip
	} else {
		eth.EthernetType = layers.EthernetTypeIPv6
		ip := &layers.IPv6{Version: 6, HopLimit: 64, SrcIP: net.ParseIP(source), DstIP: net.ParseIP(destination), NextHeader: layers.IPProtocolUDP}
		require.NoError(t, udp.SetNetworkLayerForChecksum(ip))
		network = ip
	}
	buffer := gopacket.NewSerializeBuffer()
	require.NoError(t, gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, network, udp, gopacket.Payload(payload)))
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(unix.ETH_P_ALL<<8))
	require.NoError(t, err)
	defer func() { require.NoError(t, unix.Close(fd)) }()
	require.NoError(t, unix.Sendto(fd, buffer.Bytes(), 0, &unix.SockaddrLinklayer{Ifindex: tx.Attrs().Index}), "send UDP/"+strconv.Itoa(port))
}
