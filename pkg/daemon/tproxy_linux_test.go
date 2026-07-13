package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func TestTproxyRulesToCleanup(t *testing.T) {
	t.Parallel()

	const (
		table = 10001
		mask  = uint32(0x90003)
	)

	rule := func(t int, m *uint32) netlink.Rule {
		return netlink.Rule{Table: t, Mask: m}
	}
	maskPtr := func(v uint32) *uint32 { return &v }

	tests := []struct {
		name       string
		curRules   []netlink.Rule
		wantDelLen int
		wantFound  bool
	}{
		{
			name:      "no existing rules",
			curRules:  nil,
			wantFound: false,
		},
		{
			name:      "single matching rule kept",
			curRules:  []netlink.Rule{rule(table, maskPtr(mask))},
			wantFound: true,
		},
		{
			name:       "stale rule wrong table deleted",
			curRules:   []netlink.Rule{rule(20002, maskPtr(mask))},
			wantDelLen: 1,
			wantFound:  false,
		},
		{
			name:       "stale rule wrong mask deleted",
			curRules:   []netlink.Rule{rule(table, maskPtr(0x12345))},
			wantDelLen: 1,
			wantFound:  false,
		},
		{
			name:       "stale rule nil mask deleted",
			curRules:   []netlink.Rule{rule(table, nil)},
			wantDelLen: 1,
			wantFound:  false,
		},
		{
			name:       "correct rule first then stale rule still cleaned up",
			curRules:   []netlink.Rule{rule(table, maskPtr(mask)), rule(20002, maskPtr(mask))},
			wantDelLen: 1,
			wantFound:  true,
		},
		{
			name:       "stale rule first then correct rule",
			curRules:   []netlink.Rule{rule(20002, maskPtr(mask)), rule(table, maskPtr(mask))},
			wantDelLen: 1,
			wantFound:  true,
		},
		{
			name:       "duplicate correct rule deleted keeping one",
			curRules:   []netlink.Rule{rule(table, maskPtr(mask)), rule(table, maskPtr(mask))},
			wantDelLen: 1,
			wantFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			toDel, found := tproxyRulesToCleanup(tt.curRules, mask, table)
			require.Equal(t, tt.wantFound, found)
			require.Len(t, toDel, tt.wantDelLen)
		})
	}
}
