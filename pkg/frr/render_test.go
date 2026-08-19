package frr

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestRenderEmpty(t *testing.T) {
	config := Render(RenderInput{NodeName: "node1"})
	if !strings.Contains(config, "hostname node1") {
		t.Errorf("expected hostname line, got:\n%s", config)
	}
	if strings.Contains(config, "router bgp") {
		t.Errorf("expected no bgp instance, got:\n%s", config)
	}
	if !strings.Contains(config, "ip protocol bgp route-map KUBE-OVN-NO-FIB") {
		t.Errorf("expected no-fib guard, got:\n%s", config)
	}
}

func TestRenderFull(t *testing.T) {
	input := RenderInput{
		NodeName: "node1",
		RouterID: "172.19.0.2",
		LocalASN: 65002,
		Neighbors: []Neighbor{
			{Address: "172.19.0.4", ASN: 65001, BFD: true},
			{Address: "172.19.0.5", ASN: 65003, Password: "secret"},
		},
		AdvertiseFilter: []string{"91.246.31.0/24 ge 32 le 32"},
		HoldTime:        30,
		KeepaliveTime:   10,
		Vpcs: []VpcAdvertisement{
			{VpcName: "vpc-b", VrfName: "ovnvrf1002", TableID: 1002, LrpIP: "172.19.0.24"},
			{VpcName: "vpc-a", VrfName: "ovnvrf1001", TableID: 1001, LrpIP: "172.19.0.21"},
		},
		ImportVrfs: []string{"ovnvrf1002", "ovnvrf1001"},
	}
	config := Render(input)

	for _, want := range []string{
		"router bgp 65002 vrf ovnvrf1001",
		"router bgp 65002 vrf ovnvrf1002",
		"  redistribute kernel route-map KUBE-OVN-NH-vpc-a",
		"  redistribute kernel route-map KUBE-OVN-NH-vpc-b",
		"router bgp 65002",
		" bgp router-id 172.19.0.2",
		" neighbor 172.19.0.4 remote-as 65001",
		" neighbor 172.19.0.4 bfd",
		" neighbor 172.19.0.5 remote-as 65003",
		" neighbor 172.19.0.5 password secret",
		" neighbor 172.19.0.4 timers 10 30",
		"  neighbor 172.19.0.4 activate",
		"  neighbor 172.19.0.4 route-map KUBE-OVN-OUT out",
		"  import vrf ovnvrf1001",
		"  import vrf ovnvrf1002",
		"route-map KUBE-OVN-NH-vpc-a permit 10",
		" set ip next-hop 172.19.0.21",
		"ip prefix-list KUBE-OVN-ADVERTISE seq 5 permit 91.246.31.0/24 ge 32 le 32",
		" match ip address prefix-list KUBE-OVN-ADVERTISE",
		"route-map KUBE-OVN-NO-FIB deny 10",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("expected %q in config:\n%s", want, config)
		}
	}

	posA := strings.Index(config, "router bgp 65002 vrf ovnvrf1001")
	posB := strings.Index(config, "router bgp 65002 vrf ovnvrf1002")
	if posA > posB {
		t.Errorf("expected vpc-a before vpc-b:\n%s", config)
	}
	if posMain := strings.Index(config, "router bgp 65002\n"); posMain < posB {
		t.Errorf("expected vrf instances before the default instance:\n%s", config)
	}
}

func TestRenderDeterministic(t *testing.T) {
	input := RenderInput{
		NodeName: "node1",
		RouterID: "10.0.0.1",
		LocalASN: 65002,
		Vpcs: []VpcAdvertisement{
			{VpcName: "z", TableID: 3, LrpIP: "10.0.0.3"},
			{VpcName: "a", TableID: 1, LrpIP: "10.0.0.1"},
			{VpcName: "m", TableID: 2, LrpIP: "10.0.0.2"},
		},
	}
	first := Render(input)
	for range 10 {
		if got := Render(input); got != first {
			t.Fatal("render output is not deterministic")
		}
	}
}

func TestRenderNoFilterPermitsAll(t *testing.T) {
	config := Render(RenderInput{
		NodeName:  "node1",
		RouterID:  "10.0.0.1",
		LocalASN:  65002,
		Neighbors: []Neighbor{{Address: "10.0.0.9", ASN: 65001}},
	})
	if strings.Contains(config, "ip prefix-list") {
		t.Errorf("expected no prefix-list, got:\n%s", config)
	}
	if !strings.Contains(config, "route-map KUBE-OVN-OUT permit 10") {
		t.Errorf("expected permit-all out route-map, got:\n%s", config)
	}
	if strings.Contains(config, "match ip address prefix-list") {
		t.Errorf("expected no match clause, got:\n%s", config)
	}
}

func TestBuildRenderInput(t *testing.T) {
	conf := &kubeovnv1.BgpConf{
		Spec: kubeovnv1.BgpConfSpec{
			LocalASN:   65002,
			PeerASN:    65001,
			Neighbours: []string{"10.0.0.9"},
			Peers: []kubeovnv1.BgpPeer{
				{Address: "10.0.0.10", BFD: true},
				{Address: "10.0.0.11", ASN: 65500},
			},
			HoldTime:        metav1.Duration{Duration: 30e9},
			AdvertiseFilter: []string{"192.0.2.0/24 le 32"},
		},
	}
	input := BuildRenderInput(conf, "node1", "10.0.0.1", nil, nil)

	if len(input.Neighbors) != 3 {
		t.Fatalf("expected 3 neighbors, got %d", len(input.Neighbors))
	}
	if input.Neighbors[0].Address != "10.0.0.9" || input.Neighbors[0].ASN != 65001 {
		t.Errorf("legacy neighbour not mapped: %+v", input.Neighbors[0])
	}
	if input.Neighbors[1].Address != "10.0.0.10" || input.Neighbors[1].ASN != 65001 || !input.Neighbors[1].BFD {
		t.Errorf("structured neighbor not mapped: %+v", input.Neighbors[1])
	}
	if input.Neighbors[2].ASN != 65500 {
		t.Errorf("per-neighbor asn not honored: %+v", input.Neighbors[2])
	}
	if input.HoldTime != 30 {
		t.Errorf("hold time not mapped: %d", input.HoldTime)
	}
	if input.KeepaliveTime != 10 {
		t.Errorf("keepalive time not derived from hold time: %d", input.KeepaliveTime)
	}
}

func TestValidateRenderInput(t *testing.T) {
	valid := RenderInput{
		RouterID:        "10.0.0.1",
		LocalASN:        65002,
		Neighbors:       []Neighbor{{Address: "10.0.0.9", ASN: 65001, Password: "secret"}},
		AdvertiseFilter: []string{"192.0.2.0/24 ge 32 le 32"},
		Vpcs:            []VpcAdvertisement{{VpcName: "vpc-a", LrpIP: "10.0.0.21"}},
	}
	if err := ValidateRenderInput(valid); err != nil {
		t.Errorf("expected valid input to pass, got %v", err)
	}

	cases := map[string]func(*RenderInput){
		"router id not ipv4":    func(in *RenderInput) { in.RouterID = "fd00::1" },
		"router id garbage":     func(in *RenderInput) { in.RouterID = "not-an-ip" },
		"neighbor not an ip":    func(in *RenderInput) { in.Neighbors[0].Address = "10.0.0.9\nrouter bgp 65000" },
		"neighbor asn zero":     func(in *RenderInput) { in.Neighbors[0].ASN = 0 },
		"password newline":      func(in *RenderInput) { in.Neighbors[0].Password = "secret\nrouter bgp 65000 vrf x" },
		"filter incomplete":     func(in *RenderInput) { in.AdvertiseFilter[0] = "192.0.2.0/24 ge" },
		"filter not a prefix":   func(in *RenderInput) { in.AdvertiseFilter[0] = "bogus ge 32" },
		"filter bad keyword":    func(in *RenderInput) { in.AdvertiseFilter[0] = "192.0.2.0/24 gt 32" },
		"filter injected line":  func(in *RenderInput) { in.AdvertiseFilter[0] = "192.0.2.0/24\nge 32" },
		"filter bad length":     func(in *RenderInput) { in.AdvertiseFilter[0] = "192.0.2.0/24 ge 300" },
		"lrp address missing":   func(in *RenderInput) { in.Vpcs[0].LrpIP = "" },
		"lrp address not an ip": func(in *RenderInput) { in.Vpcs[0].LrpIP = "bogus" },
	}
	for name, mutate := range cases {
		in := valid
		in.Neighbors = []Neighbor{valid.Neighbors[0]}
		in.AdvertiseFilter = []string{valid.AdvertiseFilter[0]}
		in.Vpcs = []VpcAdvertisement{valid.Vpcs[0]}
		mutate(&in)
		if err := ValidateRenderInput(in); err == nil {
			t.Errorf("case %q: expected a validation error", name)
		}
	}
}
