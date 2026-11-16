package ovs

import (
	"fmt"
	"maps"
	"slices"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ReconcileExternalLBLocalPort owns one shared localport per underlay switch.
func (c *OVNNbClient) ReconcileExternalLBLocalPort(subnet, uid string, enabled bool) error {
	name := util.ExternalLBLocalPortName(subnet)
	port, err := c.GetLogicalSwitchPort(name, true)
	if err != nil {
		return err
	}
	if port != nil && (port.ExternalIDs["vendor"] != util.CniTypeName || port.ExternalIDs["role"] != util.ExternalLBPortRole || port.ExternalIDs[LogicalSwitchKey] != subnet) {
		if !enabled {
			return nil
		}
		return fmt.Errorf("logical switch port %s is not owned by external LB integration", name)
	}
	if !enabled {
		if port == nil {
			return nil
		}
		return c.DeleteLogicalSwitchPort(name)
	}
	desired := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.NamedUUID(), Name: name, Type: "localport",
		Addresses: []string{"unknown"},
		Options:   map[string]string{util.ExternalLBARPOption: "true"},
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName, "role": util.ExternalLBPortRole,
			LogicalSwitchKey: subnet, "subnet-uid": uid,
		},
	}
	if port == nil {
		return c.createLSPIfAbsent(subnet, desired)
	}
	if err := c.EnsureLogicalSwitchPortParent(name, subnet); err != nil {
		return err
	}
	if port.Type == desired.Type && slices.Equal(port.Addresses, desired.Addresses) && len(port.PortSecurity) == 0 &&
		maps.Equal(port.Options, desired.Options) && maps.Equal(port.ExternalIDs, desired.ExternalIDs) {
		return nil
	}
	port.Type, port.Addresses, port.PortSecurity = desired.Type, desired.Addresses, nil
	port.Options, port.ExternalIDs = desired.Options, desired.ExternalIDs
	return c.UpdateLogicalSwitchPort(port, &port.Type, &port.Addresses, &port.PortSecurity, &port.Options, &port.ExternalIDs)
}
