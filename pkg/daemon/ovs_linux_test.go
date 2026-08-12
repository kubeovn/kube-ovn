package daemon

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestProviderVlanRestoreNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		provider       string
		bridge         string
		nic            string
		vlanInterface  string
		vlanID         int
		wantVlanName   string
		wantParentName string
		wantError      bool
	}{
		{
			name:           "default bridge",
			provider:       "underlay",
			bridge:         "br-underlay",
			nic:            "enp16s0f0",
			vlanID:         500,
			wantVlanName:   "enp16s0f0.500",
			wantParentName: "enp16s0f0",
		},
		{
			name:           "explicit VLAN interface",
			provider:       "underlay",
			bridge:         "br-underlay",
			nic:            "eth0",
			vlanInterface:  "bond0.20",
			vlanID:         20,
			wantVlanName:   "bond0.20",
			wantParentName: "bond0",
		},
		{
			name:          "invalid explicit VLAN interface",
			provider:      "underlay",
			bridge:        "br-underlay",
			nic:           "eth0",
			vlanInterface: "bond0.invalid",
			vlanID:        20,
			wantError:     true,
		},
		{
			name:          "mismatched explicit VLAN interface",
			provider:      "underlay",
			bridge:        "br-underlay",
			nic:           "eth0",
			vlanInterface: "bond0.21",
			vlanID:        20,
			wantError:     true,
		},
		{
			name:           "exchanged link name",
			provider:       "underlay",
			bridge:         "enp16s0f0",
			nic:            "enp16s0f0",
			vlanID:         500,
			wantVlanName:   "enp16s0f0.500",
			wantParentName: "br-underlay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := providerVlanRestoreContext{provider: tt.provider, bridge: tt.bridge, nic: tt.nic}
			vlanName, parentName, err := providerVlanRestoreNames(ctx, tt.vlanInterface, tt.vlanID)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantVlanName, vlanName)
			require.Equal(t, tt.wantParentName, parentName)
		})
	}
}

func TestProviderVlanPortArgsRecordsSourceInterface(t *testing.T) {
	t.Parallel()

	portName, args := providerVlanPortArgs("br-underlay", "bond0.20", 20)
	require.Equal(t, util.VlanInternalPortName("br-underlay", 20), portName)
	require.Contains(t, args, "external_ids:provider-vlan-interface=bond0.20")
}

func TestValidateProviderVlanInterfaceMap(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateProviderVlanInterfaceMap(map[string]int{
		"eth0.20":  20,
		"bond0.30": 30,
	}))
	require.Error(t, validateProviderVlanInterfaceMap(map[string]int{
		"eth0.20":  20,
		"bond0.20": 20,
	}))
}

func TestValidateProviderVlanPortSource(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateProviderVlanPortSource("kv20-l2bvmi7mc4", "bond0.20", providerVlanPortSourceState{}))
	require.Error(t, validateProviderVlanPortSource("kv20-l2bvmi7mc4", "bond0.20", providerVlanPortSourceState{exists: true}))
	require.NoError(t, validateProviderVlanPortSource("br-e-vlan20", "bond0.20", providerVlanPortSourceState{
		exists: true, owned: true, recorded: "[]", candidates: []string{"bond0.20"},
	}))
	require.Error(t, validateProviderVlanPortSource("br-e-vlan20", "eth0.20", providerVlanPortSourceState{
		exists: true, owned: true, candidates: []string{"eth0.20"}, requestedHasState: true,
	}))
	require.Error(t, validateProviderVlanPortSource("br-e-vlan20", "eth0.20", providerVlanPortSourceState{
		exists: true, owned: true, candidates: []string{"bond0.20", "eth0.20"},
	}))
	require.NoError(t, validateProviderVlanPortSource("kv20-l2bvmi7mc4", "bond0.20", providerVlanPortSourceState{
		exists: true, owned: true, recorded: "bond0.20",
	}))
	require.Error(t, validateProviderVlanPortSource("kv20-l2bvmi7mc4", "eth0.20", providerVlanPortSourceState{
		exists: true, owned: true, recorded: "bond0.20",
	}))
}

func TestValidateProviderVlanLink(t *testing.T) {
	t.Parallel()

	parent := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "bond0", Index: 10}}
	valid := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{Name: "bond0.20", ParentIndex: 10},
		VlanId:    20,
	}
	require.NoError(t, validateProviderVlanLink(valid, parent, 20))
	require.Error(t, validateProviderVlanLink(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "bond0.20"}}, parent, 20))

	wrongVlanID := *valid
	wrongVlanID.VlanId = 21
	require.Error(t, validateProviderVlanLink(&wrongVlanID, parent, 20))

	wrongParent := *valid
	wrongParent.ParentIndex = 11
	require.Error(t, validateProviderVlanLink(&wrongParent, parent, 20))
}

func TestRestoreProviderVlanNetworkStateReturnsError(t *testing.T) {
	t.Parallel()

	vlanLink := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "bond0.20", Index: 20}, VlanId: 20}
	addr := netlink.Addr{IPNet: &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)}}
	_, dst, err := net.ParseCIDR("198.51.100.0/24")
	require.NoError(t, err)
	route := netlink.Route{Dst: dst, Scope: netlink.SCOPE_UNIVERSE}

	addrErr := errors.New("replace address")
	err = restoreProviderVlanNetworkState(vlanLink, []netlink.Addr{addr}, []netlink.Route{route},
		func(netlink.Link, *netlink.Addr) error { return addrErr },
		func(*netlink.Route) error {
			t.Fatal("route replacement must not run after address failure")
			return nil
		},
	)
	require.ErrorIs(t, err, addrErr)

	routeErr := errors.New("replace route")
	err = restoreProviderVlanNetworkState(vlanLink, []netlink.Addr{addr}, []netlink.Route{route},
		func(netlink.Link, *netlink.Addr) error { return nil },
		func(*netlink.Route) error { return routeErr },
	)
	require.ErrorIs(t, err, routeErr)
}

func TestTransferProviderVlanAddressesPreservesSourceUntilDestinationReady(t *testing.T) {
	t.Parallel()

	src := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "bond0.20", Index: 20}, VlanId: 20}
	dst := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "kv20-l2bvmi7mc4", Index: 21}}
	addr := netlink.Addr{IPNet: &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)}}
	replaceErr := errors.New("replace address")
	deleted := false

	err := transferProviderVlanAddresses(src, dst, []netlink.Addr{addr},
		func(netlink.Link, *netlink.Addr) error { return replaceErr },
		func(netlink.Link, *netlink.Addr) error {
			deleted = true
			return nil
		},
	)
	require.ErrorIs(t, err, replaceErr)
	require.False(t, deleted, "source address must remain when destination setup fails")

	steps := make([]string, 0, 2)
	require.NoError(t, transferProviderVlanAddresses(src, dst, []netlink.Addr{addr},
		func(netlink.Link, *netlink.Addr) error {
			steps = append(steps, "replace-destination")
			return nil
		},
		func(netlink.Link, *netlink.Addr) error {
			steps = append(steps, "delete-source")
			return nil
		},
	))
	require.Equal(t, []string{"replace-destination", "delete-source"}, steps)

	deleteErr := errors.New("delete source address")
	err = transferProviderVlanAddresses(src, dst, []netlink.Addr{addr},
		func(netlink.Link, *netlink.Addr) error { return nil },
		func(netlink.Link, *netlink.Addr) error { return deleteErr },
	)
	require.ErrorIs(t, err, deleteErr)
}

func TestTransferProviderVlanRoutesReturnsDestinationFailure(t *testing.T) {
	t.Parallel()

	dst := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "kv20-l2bvmi7mc4", Index: 21}}
	_, network, err := net.ParseCIDR("198.51.100.0/24")
	require.NoError(t, err)
	replaceErr := errors.New("replace route")

	err = transferProviderVlanRoutes(dst, []netlink.Route{{Dst: network, Scope: netlink.SCOPE_UNIVERSE}}, func(*netlink.Route) error {
		return replaceErr
	})
	require.ErrorIs(t, err, replaceErr)
}

func TestSelectProviderVlanSourceInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		recorded      string
		candidates    []string
		existing      map[string]bool
		wantInterface string
		wantError     bool
	}{
		{
			name:          "recorded interface survives spec update",
			recorded:      "bond0.20",
			candidates:    []string{"eth0.20"},
			existing:      map[string]bool{"eth0.20": true},
			wantInterface: "bond0.20",
		},
		{
			name:          "unique local explicit interface",
			candidates:    []string{"eth0.20", "bond0.20"},
			existing:      map[string]bool{"bond0.20": true},
			wantInterface: "bond0.20",
		},
		{
			name:       "ambiguous local explicit interfaces",
			candidates: []string{"eth0.20", "bond0.20"},
			existing:   map[string]bool{"eth0.20": true, "bond0.20": true},
			wantError:  true,
		},
		{
			name:       "no local explicit interface",
			candidates: []string{"eth0.20", "bond0.20"},
			existing:   map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectProviderVlanSourceInterface(tt.recorded, tt.candidates, 20, func(name string) bool {
				return tt.existing[name]
			})
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantInterface, got)
		})
	}
}

func TestWaitNetworkReady_IPGatewayMismatch(t *testing.T) {
	tests := []struct {
		name    string
		ipAddr  string
		gateway string
	}{
		{
			name:    "gateway has more elements than ips",
			ipAddr:  "10.0.0.2/24",
			gateway: "10.0.0.1,fd00::1",
		},
		{
			name:    "ips has more elements than gateway",
			ipAddr:  "10.0.0.2/24,fd00::2/64",
			gateway: "10.0.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := waitNetworkReady("eth0", tt.ipAddr, tt.gateway, false, false, 1, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "mismatch")
		})
	}
}

func TestGatewayForCNIIPFamily(t *testing.T) {
	tests := []struct {
		name    string
		ipAddr  string
		gateway string
		want    string
	}{
		{
			name:    "ipv4 only",
			ipAddr:  "10.0.0.2/24",
			gateway: "10.0.0.1,fd00::1",
			want:    "10.0.0.1",
		},
		{
			name:    "ipv6 only",
			ipAddr:  "fd00::2/64",
			gateway: "10.0.0.1,fd00::1",
			want:    "fd00::1",
		},
		{
			name:    "dual stack",
			ipAddr:  "10.0.0.2/24,fd00::2/64",
			gateway: "10.0.0.1,fd00::1",
			want:    "10.0.0.1,fd00::1",
		},
		{
			name:    "single stack gateway",
			ipAddr:  "10.0.0.2/24",
			gateway: "10.0.0.1",
			want:    "10.0.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gatewayForCNIIPFamily(tt.ipAddr, tt.gateway))
		})
	}
}
