package speaker

import (
	"net"
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestAnnounceEIPsFiltersAndDeduplicatesPrefixes(t *testing.T) {
	const (
		routerID     = "192.0.2.10"
		ipv4Neighbor = "192.0.2.1"
		ipv6NextHop  = "2001:db8::10"
		ipv6Neighbor = "2001:db8::1"
	)

	controller := &Controller{config: &Configuration{
		RouterID:              net.ParseIP(routerID),
		NeighborAddresses:     []net.IP{net.ParseIP(ipv4Neighbor)},
		NeighborIPv6Addresses: []net.IP{net.ParseIP(ipv6Neighbor)},
		NeighborLocalAddresses: map[string]net.IP{
			ipv4Neighbor: net.ParseIP(routerID),
			ipv6Neighbor: net.ParseIP(ipv6NextHop),
		},
		BgpServer: newTestBgpServer(t, routerID),
	}}

	eips := []*kubeovnv1.IptablesEIP{
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "true"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10", V6ip: "2001:db8::10"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "true"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.11"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: false},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "false"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.12"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
		},
		{
			Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.13"},
			Status: kubeovnv1.IptablesEIPStatus{Ready: true},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "true"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
		},
	}

	require.NoError(t, controller.announceEIPs(eips))
	ipv4Prefixes := listTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP)
	require.Len(t, ipv4Prefixes, 1)
	require.Len(t, ipv4Prefixes["192.0.2.10/32"], 1)
	require.True(t, net.ParseIP(routerID).Equal(ipv4Prefixes["192.0.2.10/32"][0]))

	ipv6Prefixes := listTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP6)
	require.Len(t, ipv6Prefixes, 1)
	require.Len(t, ipv6Prefixes["2001:db8::10/128"], 1)
	require.True(t, net.ParseIP(ipv6NextHop).Equal(ipv6Prefixes["2001:db8::10/128"][0]))

	require.NoError(t, controller.announceEIPs(nil))
	require.Empty(t, listTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP))
}

func TestSyncEIPRoutesRequiresGatewayName(t *testing.T) {
	t.Setenv(util.EnvGatewayName, "")

	err := (&Controller{}).syncEIPRoutes()
	require.ErrorContains(t, err, "failed to retrieve the name of the gateway")
}
