package daemon

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"
)

func TestRenderNFTBaseSchema(t *testing.T) {
	fake := knftables.NewFake("", "")
	backend := newNFTGatewayBackendForTest(fake)
	tx := backend.renderFull(gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family: knftables.IPv4Family,
		Table:  "kube-ovn",
	}}})

	require.NoError(t, fake.Check(context.Background(), tx))
	dump := tx.String()
	require.Contains(t, dump, "add table ip kube-ovn")
	require.Contains(t, dump, "add chain ip kube-ovn schema-v1")
	require.Contains(t, dump, "add chain ip kube-ovn nat-host-service { type nat hook postrouting priority 90 ;")
	require.Contains(t, dump, "add chain ip kube-ovn nat-postrouting { type nat hook postrouting priority 110 ;")
	require.Contains(t, dump, "add chain ip kube-ovn tproxy-output { type route hook output priority -150 ;")
}

func TestRenderNFTDualStackSingleTransaction(t *testing.T) {
	fake := knftables.NewFake("", "")
	backend := newNFTGatewayBackendForTest(fake)
	tx := backend.renderFull(gatewayNFTSnapshot{Families: []nftFamilySnapshot{
		{Family: knftables.IPv4Family, Table: "kube-ovn"},
		{Family: knftables.IPv6Family, Table: "kube-ovn"},
	}})

	require.NoError(t, fake.Check(context.Background(), tx))
	dump := tx.String()
	require.Contains(t, dump, "add table ip kube-ovn")
	require.Contains(t, dump, "add table ip6 kube-ovn")
}

func newNFTGatewayBackendForTest(writer knftables.Interface) *nftGatewayBackend {
	return &nftGatewayBackend{writer: writer, readers: map[knftables.Family]knftables.Interface{}}
}
