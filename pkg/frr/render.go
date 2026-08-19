package frr

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	outRouteMap   = "KUBE-OVN-OUT"
	noFibRouteMap = "KUBE-OVN-NO-FIB"
	advPrefixList = "KUBE-OVN-ADVERTISE"
	nhRouteMap    = "KUBE-OVN-NH-"
)

type VpcAdvertisement struct {
	VpcName string
	VrfName string
	TableID uint32
	LrpIP   string
}

type Neighbor struct {
	Address  string
	ASN      uint32
	BFD      bool
	Password string
}

type RenderInput struct {
	NodeName        string
	RouterID        string
	LocalASN        uint32
	Neighbors       []Neighbor
	AdvertiseFilter []string
	HoldTime        int64
	KeepaliveTime   int64
	ConnectTime     int64
	EbgpMultiHop    bool
	GracefulRestart bool
	Vpcs            []VpcAdvertisement
	ImportVrfs      []string
}

func BuildRenderInput(conf *kubeovnv1.BgpConf, nodeName, routerID string, vpcs []VpcAdvertisement, importVrfs []string) RenderInput {
	input := RenderInput{
		NodeName:        nodeName,
		RouterID:        routerID,
		LocalASN:        conf.Spec.LocalASN,
		AdvertiseFilter: conf.Spec.AdvertiseFilter,
		EbgpMultiHop:    conf.Spec.EbgpMultiHop,
		GracefulRestart: conf.Spec.GracefulRestart,
		Vpcs:            vpcs,
		ImportVrfs:      importVrfs,
	}
	if conf.Spec.HoldTime.Duration > 0 {
		input.HoldTime = int64(conf.Spec.HoldTime.Seconds())
	}
	if conf.Spec.KeepaliveTime.Duration > 0 {
		input.KeepaliveTime = int64(conf.Spec.KeepaliveTime.Seconds())
	}
	if conf.Spec.ConnectTime.Duration > 0 {
		input.ConnectTime = int64(conf.Spec.ConnectTime.Seconds())
	}
	if input.HoldTime > 0 && input.KeepaliveTime == 0 {
		input.KeepaliveTime = max(input.HoldTime/3, 1)
	}
	if input.KeepaliveTime > 0 && input.HoldTime == 0 {
		input.HoldTime = input.KeepaliveTime * 3
	}
	for _, addr := range conf.Spec.Neighbours {
		input.Neighbors = append(input.Neighbors, Neighbor{
			Address:  addr,
			ASN:      conf.Spec.PeerASN,
			Password: conf.Spec.Password,
		})
	}
	for _, n := range conf.Spec.Peers {
		asn := n.ASN
		if asn == 0 {
			asn = conf.Spec.PeerASN
		}
		input.Neighbors = append(input.Neighbors, Neighbor{
			Address:  n.Address,
			ASN:      asn,
			BFD:      n.BFD,
			Password: conf.Spec.Password,
		})
	}
	return input
}

func ValidateRenderInput(input RenderInput) error {
	if input.RouterID != "" {
		addr, err := netip.ParseAddr(input.RouterID)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("router id %q is not an IPv4 address", input.RouterID)
		}
	}
	for _, n := range input.Neighbors {
		if _, err := netip.ParseAddr(n.Address); err != nil {
			return fmt.Errorf("neighbor address %q is not a valid IP address", n.Address)
		}
		if n.ASN == 0 {
			return fmt.Errorf("neighbor %s has no ASN: set peers[].asn or peerASN", n.Address)
		}
		if strings.ContainsFunc(n.Password, unicode.IsControl) {
			return fmt.Errorf("neighbor %s password contains control characters", n.Address)
		}
	}
	for _, entry := range input.AdvertiseFilter {
		if err := validateFilterEntry(entry); err != nil {
			return err
		}
	}
	for _, vpc := range input.Vpcs {
		if _, err := netip.ParseAddr(vpc.LrpIP); err != nil {
			return fmt.Errorf("vpc %s lrp address %q is not a valid IP address", vpc.VpcName, vpc.LrpIP)
		}
	}
	return nil
}

func validateFilterEntry(entry string) error {
	if strings.ContainsFunc(entry, unicode.IsControl) {
		return fmt.Errorf("advertise filter entry %q contains control characters", entry)
	}
	fields := strings.Fields(entry)
	if len(fields) == 0 || len(fields)%2 == 0 {
		return fmt.Errorf("advertise filter entry %q must be a prefix optionally followed by ge/le bounds", entry)
	}
	if _, err := netip.ParsePrefix(fields[0]); err != nil {
		return fmt.Errorf("advertise filter entry %q: %w", entry, err)
	}
	for i := 1; i < len(fields); i += 2 {
		if fields[i] != "ge" && fields[i] != "le" {
			return fmt.Errorf("advertise filter entry %q: expected ge or le, got %q", entry, fields[i])
		}
		if length, err := strconv.Atoi(fields[i+1]); err != nil || length < 0 || length > 128 {
			return fmt.Errorf("advertise filter entry %q: invalid prefix length %q", entry, fields[i+1])
		}
	}
	return nil
}

func Render(input RenderInput) string {
	vpcs := make([]VpcAdvertisement, len(input.Vpcs))
	copy(vpcs, input.Vpcs)
	sort.Slice(vpcs, func(i, j int) bool { return vpcs[i].VpcName < vpcs[j].VpcName })

	var b strings.Builder
	b.WriteString("frr defaults traditional\n")
	fmt.Fprintf(&b, "hostname %s\n", input.NodeName)
	b.WriteString("!\n")

	if input.LocalASN == 0 {
		fmt.Fprintf(&b, "route-map %s deny 10\nexit\n!\n", noFibRouteMap)
		fmt.Fprintf(&b, "ip protocol bgp route-map %s\n", noFibRouteMap)
		return b.String()
	}

	for _, vpc := range vpcs {
		fmt.Fprintf(&b, "router bgp %d vrf %s\n", input.LocalASN, vpc.VrfName)
		b.WriteString(" address-family ipv4 unicast\n")
		fmt.Fprintf(&b, "  redistribute kernel route-map %s%s\n", nhRouteMap, vpc.VpcName)
		b.WriteString(" exit-address-family\n")
		b.WriteString("exit\n!\n")
	}

	fmt.Fprintf(&b, "router bgp %d\n", input.LocalASN)
	if input.RouterID != "" {
		fmt.Fprintf(&b, " bgp router-id %s\n", input.RouterID)
	}
	if input.GracefulRestart {
		b.WriteString(" bgp graceful-restart\n")
	}
	for _, n := range input.Neighbors {
		fmt.Fprintf(&b, " neighbor %s remote-as %d\n", n.Address, n.ASN)
		if n.Password != "" {
			fmt.Fprintf(&b, " neighbor %s password %s\n", n.Address, n.Password)
		}
		if input.EbgpMultiHop {
			fmt.Fprintf(&b, " neighbor %s ebgp-multihop\n", n.Address)
		}
		if input.HoldTime > 0 && input.KeepaliveTime > 0 {
			fmt.Fprintf(&b, " neighbor %s timers %d %d\n", n.Address, input.KeepaliveTime, input.HoldTime)
		}
		if input.ConnectTime > 0 {
			fmt.Fprintf(&b, " neighbor %s timers connect %d\n", n.Address, input.ConnectTime)
		}
		if n.BFD {
			fmt.Fprintf(&b, " neighbor %s bfd\n", n.Address)
		}
	}
	imports := make([]string, len(input.ImportVrfs))
	copy(imports, input.ImportVrfs)
	sort.Strings(imports)

	b.WriteString(" address-family ipv4 unicast\n")
	for _, n := range input.Neighbors {
		fmt.Fprintf(&b, "  neighbor %s activate\n", n.Address)
		fmt.Fprintf(&b, "  neighbor %s route-map %s out\n", n.Address, outRouteMap)
	}
	for _, vrf := range imports {
		fmt.Fprintf(&b, "  import vrf %s\n", vrf)
	}
	b.WriteString(" exit-address-family\n")
	b.WriteString("!\n")

	for _, vpc := range vpcs {
		fmt.Fprintf(&b, "route-map %s%s permit 10\n", nhRouteMap, vpc.VpcName)
		fmt.Fprintf(&b, " set ip next-hop %s\n", vpc.LrpIP)
		b.WriteString("exit\n!\n")
	}

	if len(input.AdvertiseFilter) > 0 {
		for i, entry := range input.AdvertiseFilter {
			fmt.Fprintf(&b, "ip prefix-list %s seq %d permit %s\n", advPrefixList, (i+1)*5, entry)
		}
		fmt.Fprintf(&b, "route-map %s permit 10\n", outRouteMap)
		fmt.Fprintf(&b, " match ip address prefix-list %s\n", advPrefixList)
		b.WriteString("exit\n!\n")
	} else {
		fmt.Fprintf(&b, "route-map %s permit 10\n", outRouteMap)
		b.WriteString("exit\n!\n")
	}

	fmt.Fprintf(&b, "route-map %s deny 10\n", noFibRouteMap)
	b.WriteString("exit\n!\n")
	fmt.Fprintf(&b, "ip protocol bgp route-map %s\n", noFibRouteMap)

	return b.String()
}
