package daemon

import "sigs.k8s.io/knftables"

const nftGatewayTable = "kube-ovn"

type nftGatewayBackend struct {
	writer  knftables.Interface
	readers map[knftables.Family]knftables.Interface
}

func (b *nftGatewayBackend) renderFull(snapshot gatewayNFTSnapshot) *knftables.Transaction {
	tx := b.writer.NewTransaction()
	for _, family := range snapshot.Families {
		b.renderNFTBaseSchema(tx, family)
	}
	return tx
}

func (*nftGatewayBackend) renderNFTBaseSchema(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, snapshot.Table
	if table == "" {
		table = nftGatewayTable
	}
	tx.Add(&knftables.Table{Family: family, Name: table})
	tx.Add(&knftables.Chain{Family: family, Table: table, Name: "schema-v1"})

	baseChains := []knftables.Chain{
		{
			Name:     "tproxy-prerouting",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     "tproxy-output",
			Type:     knftables.PtrTo(knftables.RouteType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     "filter-input",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.InputHook),
			Priority: knftables.PtrTo(knftables.FilterPriority + "-10"),
		},
		{
			Name:     "filter-forward",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.ForwardHook),
			Priority: knftables.PtrTo(knftables.FilterPriority + "-10"),
		},
		{
			Name:     "filter-output",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.FilterPriority + "-10"),
		},
		{
			Name:     "mangle-postrouting",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     "nat-host-service",
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.SNATPriority + "-10"),
		},
		{
			Name:     "nat-postrouting",
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.SNATPriority + "+10"),
		},
	}
	for i := range baseChains {
		baseChains[i].Family = family
		baseChains[i].Table = table
		tx.Add(&baseChains[i])
	}
}
