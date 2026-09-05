package controller

import (
	"context"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// logicalSwitchExists uses the generic table seam when controller wiring has
// supplied it. The legacy client fallback keeps unit fixtures and incremental
// migrations compatible while callers move away from domain-specific helpers.
func (c *Controller) logicalSwitchExists(name string) (bool, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalSwitchExists(name)
	}

	var rows []ovnnb.LogicalSwitch
	err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Filter(
		context.Background(),
		func(row *ovnnb.LogicalSwitch) bool { return row.Name == name },
		&rows,
	)
	if err != nil {
		return false, fmt.Errorf("list logical switch %q: %w", name, err)
	}
	return len(rows) > 0, nil
}
