package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/client"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vtep"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	vtepOtherConfigVendorKey  = "vendor"
	vtepOtherConfigBindingKey = VtepBindingKey
)

// EnsureLogicalSwitch creates the Hardware VTEP Logical_Switch if missing.
func (c *VtepClient) EnsureLogicalSwitch(name, bindingName string) (*vtep.LogicalSwitch, error) {
	ls, err := c.GetLogicalSwitch(name, true)
	if err != nil {
		return nil, err
	}
	if ls != nil {
		return ls, nil
	}

	ls = &vtep.LogicalSwitch{
		UUID: ovsclient.NamedUUID(),
		Name: name,
		OtherConfig: map[string]string{
			vtepOtherConfigVendorKey:  util.CniTypeName,
			vtepOtherConfigBindingKey: bindingName,
		},
	}
	ops, err := c.Create(ls)
	if err != nil {
		return nil, fmt.Errorf("generate operations for creating VTEP logical switch %s: %w", name, err)
	}
	if err = c.Transact("vtep-ls-add", ops); err != nil {
		return nil, fmt.Errorf("create VTEP logical switch %s: %w", name, err)
	}
	klog.Infof("created VTEP logical switch %s for binding %s", name, bindingName)

	ls, err = c.GetLogicalSwitch(name, false)
	if err != nil {
		return nil, err
	}
	return ls, nil
}

// GetLogicalSwitch returns a Hardware VTEP Logical_Switch by name.
func (c *VtepClient) GetLogicalSwitch(name string, ignoreNotFound bool) (*vtep.LogicalSwitch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	if name == "" {
		return nil, errors.New("logical switch name is empty")
	}

	var lsList []vtep.LogicalSwitch
	if err := c.WhereCache(func(ls *vtep.LogicalSwitch) bool {
		return ls.Name == name
	}).List(ctx, &lsList); err != nil {
		return nil, fmt.Errorf("list VTEP logical switch %s: %w", name, err)
	}
	if len(lsList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("VTEP logical switch %s not found: %w", name, client.ErrNotFound)
	}
	if len(lsList) > 1 {
		return nil, fmt.Errorf("more than one VTEP logical switch with name %s", name)
	}
	return &lsList[0], nil
}

// DeleteLogicalSwitchIfOwned deletes the Logical_Switch when owned by the binding
// and not referenced by any Physical_Port.vlan_bindings.
func (c *VtepClient) DeleteLogicalSwitchIfOwned(name, bindingName string) error {
	ls, err := c.GetLogicalSwitch(name, true)
	if err != nil {
		return err
	}
	if ls == nil {
		return nil
	}
	if ls.OtherConfig[vtepOtherConfigVendorKey] != util.CniTypeName ||
		ls.OtherConfig[vtepOtherConfigBindingKey] != bindingName {
		klog.Infof("skip deleting VTEP logical switch %s: not owned by binding %s", name, bindingName)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	var ports []vtep.PhysicalPort
	if err = c.List(ctx, &ports); err != nil {
		return fmt.Errorf("list physical ports while deleting VTEP logical switch %s: %w", name, err)
	}
	for _, port := range ports {
		for _, ref := range port.VLANBindings {
			if ref == ls.UUID {
				klog.Infof("skip deleting VTEP logical switch %s: still referenced by physical port %s", name, port.Name)
				return nil
			}
		}
	}

	ops, err := c.Where(ls).Delete()
	if err != nil {
		return fmt.Errorf("generate delete for VTEP logical switch %s: %w", name, err)
	}
	if err = c.Transact("vtep-ls-del", ops); err != nil {
		return fmt.Errorf("delete VTEP logical switch %s: %w", name, err)
	}
	klog.Infof("deleted VTEP logical switch %s owned by binding %s", name, bindingName)
	return nil
}
