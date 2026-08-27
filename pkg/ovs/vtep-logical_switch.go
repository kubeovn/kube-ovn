package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"

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
// When the row already exists and is Kube-OVN-owned, the caller binding claims
// other_config ownership so a terminating CR's cleanup cannot wipe shared state.
func (c *VtepClient) EnsureLogicalSwitch(name, bindingName string) (*vtep.LogicalSwitch, error) {
	ls, err := c.GetLogicalSwitch(name, true)
	if err != nil {
		return nil, err
	}
	if ls != nil {
		if vendor := ls.OtherConfig[vtepOtherConfigVendorKey]; vendor != "" && vendor != util.CniTypeName {
			return nil, fmt.Errorf("VTEP logical switch %s exists but is not owned by Kube-OVN", name)
		}
		if err = c.claimLogicalSwitchOwnership(ls, bindingName); err != nil {
			return nil, err
		}
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

func (c *VtepClient) claimLogicalSwitchOwnership(ls *vtep.LogicalSwitch, bindingName string) error {
	if ls == nil || bindingName == "" {
		return nil
	}
	if ls.OtherConfig != nil &&
		ls.OtherConfig[vtepOtherConfigVendorKey] == util.CniTypeName &&
		ls.OtherConfig[vtepOtherConfigBindingKey] == bindingName {
		return nil
	}
	otherConfig := map[string]string{}
	maps.Copy(otherConfig, ls.OtherConfig)
	otherConfig[vtepOtherConfigVendorKey] = util.CniTypeName
	otherConfig[vtepOtherConfigBindingKey] = bindingName
	ls.OtherConfig = otherConfig
	ops, err := c.Where(ls).Update(ls, &ls.OtherConfig)
	if err != nil {
		return fmt.Errorf("generate update for VTEP logical switch %s ownership: %w", ls.Name, err)
	}
	if err = c.Transact("vtep-ls-claim", ops); err != nil {
		return fmt.Errorf("claim VTEP logical switch %s for binding %s: %w", ls.Name, bindingName, err)
	}
	klog.Infof("claimed VTEP logical switch %s for binding %s", ls.Name, bindingName)
	return nil
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

// DeleteLogicalSwitchIfOwned deletes the Logical_Switch when it is Kube-OVN-owned
// by this binding and not referenced by any Physical_Port.vlan_bindings.
func (c *VtepClient) DeleteLogicalSwitchIfOwned(name, bindingName string) error {
	ls, err := c.GetLogicalSwitch(name, true)
	if err != nil {
		return err
	}
	if ls == nil {
		return nil
	}
	if ls.OtherConfig[vtepOtherConfigVendorKey] != util.CniTypeName {
		klog.Infof("skip deleting VTEP logical switch %s: not owned by Kube-OVN (binding %s)", name, bindingName)
		return nil
	}
	if owner := ls.OtherConfig[vtepOtherConfigBindingKey]; owner != "" && owner != bindingName {
		klog.Infof("skip deleting VTEP logical switch %s: owned by binding %s, not %s", name, owner, bindingName)
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
	klog.Infof("deleted VTEP logical switch %s after cleanup of binding %s", name, bindingName)
	return nil
}
