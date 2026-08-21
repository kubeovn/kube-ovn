package ovs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"
	"k8s.io/utils/keymutex"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vtep"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// VtepClient is a client for the Hardware VTEP OVSDB (hardware_vtep).
type VtepClient struct {
	ovsDbClient
	portKeyMutex keymutex.KeyMutex
}

func vtepPhysicalPortLockKey(physicalSwitch, physicalPort string) string {
	return physicalSwitch + "/" + physicalPort
}

func vtepVLANKey(physicalSwitch, physicalPort string, vlanID int) string {
	return fmt.Sprintf("%s/%s/%d", physicalSwitch, physicalPort, vlanID)
}

// NewVtepClient creates a client connected to the Hardware VTEP database.
func NewVtepClient(addr string, connTimeout, inactivityTimeout, transactTimeout, maxRetry int) (*VtepClient, error) {
	dbModel, err := vtep.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	monitors := []client.MonitorOption{
		client.WithTable(&vtep.LogicalSwitch{}),
		client.WithTable(&vtep.PhysicalPort{}),
		client.WithTable(&vtep.PhysicalSwitch{}),
		client.WithTable(&vtep.Global{}),
	}

	try := 0
	var c client.Client
	for {
		c, err = ovsclient.NewOvsDbClient(
			vtep.DatabaseName,
			addr,
			dbModel,
			monitors,
			connTimeout,
			inactivityTimeout,
		)
		if err != nil {
			klog.Errorf("failed to create VTEP client: %v", err)
		} else {
			break
		}
		if try >= maxRetry {
			return nil, err
		}
		time.Sleep(2 * time.Second)
		try++
	}

	return &VtepClient{
		ovsDbClient: ovsDbClient{
			Client:  c,
			Timeout: time.Duration(transactTimeout) * time.Second,
		},
		portKeyMutex: keymutex.NewHashed(64),
	}, nil
}

// EnsureVtepBinding writes the VTEP Logical_Switch and Physical_Port.vlan_bindings
// for a VtepBinding. Physical_Switch and Physical_Port must already exist.
func (c *VtepClient) EnsureVtepBinding(physicalSwitch, physicalPort, logicalSwitch, bindingName string, vlanID int) error {
	if physicalSwitch == "" || physicalPort == "" || logicalSwitch == "" {
		return errors.New("physicalSwitch, physicalPort and logicalSwitch must be non-empty")
	}
	if vlanID < 0 || vlanID > 4095 {
		return fmt.Errorf("vlanID %d out of range [0,4095]", vlanID)
	}

	ls, err := c.EnsureLogicalSwitch(logicalSwitch, bindingName)
	if err != nil {
		return err
	}

	lockKey := vtepPhysicalPortLockKey(physicalSwitch, physicalPort)
	c.portKeyMutex.LockKey(lockKey)
	defer func() { _ = c.portKeyMutex.UnlockKey(lockKey) }()

	port, err := c.GetPhysicalPort(physicalSwitch, physicalPort, false)
	if err != nil {
		return err
	}
	if port.VLANBindings != nil {
		if existing, ok := port.VLANBindings[vlanID]; ok && existing == ls.UUID {
			return nil
		}
	}

	if err = c.setPhysicalPortVLANBinding(port, vlanID, ls.UUID); err != nil {
		return err
	}
	klog.Infof("set VTEP vlan_bindings[%d] on physical switch %s port %s to logical switch %s",
		vlanID, physicalSwitch, physicalPort, logicalSwitch)
	return nil
}

func (c *VtepClient) setPhysicalPortVLANBinding(port *vtep.PhysicalPort, vlanID int, lsUUID string) error {
	var ops []ovsdb.Operation
	if port.VLANBindings != nil {
		if existing, ok := port.VLANBindings[vlanID]; ok && existing != lsUUID {
			delOps, err := c.Where(port).Mutate(port, model.Mutation{
				Field:   &port.VLANBindings,
				Mutator: ovsdb.MutateOperationDelete,
				Value:   map[int]string{vlanID: existing},
			})
			if err != nil {
				return fmt.Errorf("generate mutate to replace vlan_bindings on port %s: %w", port.Name, err)
			}
			ops = append(ops, delOps...)
		}
	}
	insOps, err := c.Where(port).Mutate(port, model.Mutation{
		Field:   &port.VLANBindings,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   map[int]string{vlanID: lsUUID},
	})
	if err != nil {
		return fmt.Errorf("generate mutate for vlan_bindings on port %s: %w", port.Name, err)
	}
	ops = append(ops, insOps...)
	if err = c.Transact("vtep-vlan-bind", ops); err != nil {
		return fmt.Errorf("set vlan_bindings[%d]=%s on physical port %s: %w", vlanID, lsUUID, port.Name, err)
	}
	return nil
}

// RemoveVtepBinding removes the owned vlan_binding and deletes the Logical_Switch
// when it is still owned by this binding and no longer referenced.
// vlan_bindings entries are cleared only when they still reference this binding's
// Logical_Switch UUID and that Logical_Switch is still owned by bindingName, so a
// replacement CR that reclaimed the same Logical_Switch is never wiped.
func (c *VtepClient) RemoveVtepBinding(physicalSwitch, physicalPort, logicalSwitch, bindingName string, vlanID int) error {
	if physicalSwitch == "" || physicalPort == "" {
		return nil
	}

	ls, err := c.GetLogicalSwitch(logicalSwitch, true)
	if err != nil {
		return err
	}

	lockKey := vtepPhysicalPortLockKey(physicalSwitch, physicalPort)
	c.portKeyMutex.LockKey(lockKey)
	defer func() { _ = c.portKeyMutex.UnlockKey(lockKey) }()

	port, err := c.GetPhysicalPort(physicalSwitch, physicalPort, true)
	if err != nil {
		return err
	}
	if port != nil && port.VLANBindings != nil {
		if ref, ok := port.VLANBindings[vlanID]; ok {
			switch {
			case ls == nil || ref != ls.UUID:
				klog.Infof("skip clearing VTEP vlan_bindings[%d] on %s/%s: mapping does not reference logical switch %s",
					vlanID, physicalSwitch, physicalPort, logicalSwitch)
			case ls.OtherConfig[vtepOtherConfigBindingKey] != "" && ls.OtherConfig[vtepOtherConfigBindingKey] != bindingName:
				klog.Infof("skip clearing VTEP vlan_bindings[%d] on %s/%s: logical switch %s owned by binding %s",
					vlanID, physicalSwitch, physicalPort, logicalSwitch, ls.OtherConfig[vtepOtherConfigBindingKey])
			default:
				ops, err := c.Where(port).Mutate(port, model.Mutation{
					Field:   &port.VLANBindings,
					Mutator: ovsdb.MutateOperationDelete,
					Value:   map[int]string{vlanID: ls.UUID},
				})
				if err != nil {
					return fmt.Errorf("generate mutate to clear vlan_bindings on port %s: %w", physicalPort, err)
				}
				if err = c.Transact("vtep-vlan-unbind", ops); err != nil {
					return fmt.Errorf("clear vlan_bindings[%d] on physical port %s: %w", vlanID, physicalPort, err)
				}
				klog.Infof("cleared VTEP vlan_bindings[%d] on physical switch %s port %s",
					vlanID, physicalSwitch, physicalPort)
			}
		}
	}

	return c.DeleteLogicalSwitchIfOwned(logicalSwitch, bindingName)
}

// GCOrphanedVtepState removes stale Kube-OVN vlan_bindings and unreferenced
// Kube-OVN Logical_Switch rows that are not owned by any live VtepBinding.
func (c *VtepClient) GCOrphanedVtepState(live []VtepLiveBinding) error {
	liveVLAN := make(map[string]string, len(live))
	liveLS := make(map[string]struct{}, len(live))
	for _, binding := range live {
		liveVLAN[vtepVLANKey(binding.PhysicalSwitch, binding.PhysicalPort, binding.VlanID)] = binding.LogicalSwitch
		if binding.LogicalSwitch != "" {
			liveLS[binding.LogicalSwitch] = struct{}{}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var switches []vtep.PhysicalSwitch
	if err := c.List(ctx, &switches); err != nil {
		return fmt.Errorf("list physical switches for VTEP GC: %w", err)
	}
	var ports []vtep.PhysicalPort
	if err := c.List(ctx, &ports); err != nil {
		return fmt.Errorf("list physical ports for VTEP GC: %w", err)
	}
	var logicalSwitches []vtep.LogicalSwitch
	if err := c.List(ctx, &logicalSwitches); err != nil {
		return fmt.Errorf("list logical switches for VTEP GC: %w", err)
	}

	portByUUID := make(map[string]*vtep.PhysicalPort, len(ports))
	for i := range ports {
		portByUUID[ports[i].UUID] = &ports[i]
	}
	lsByUUID := make(map[string]*vtep.LogicalSwitch, len(logicalSwitches))
	for i := range logicalSwitches {
		lsByUUID[logicalSwitches[i].UUID] = &logicalSwitches[i]
	}

	for i := range switches {
		ps := &switches[i]
		for _, portUUID := range ps.Ports {
			port := portByUUID[portUUID]
			if port == nil || port.VLANBindings == nil {
				continue
			}
			lockKey := vtepPhysicalPortLockKey(ps.Name, port.Name)
			c.portKeyMutex.LockKey(lockKey)
			for vlanID, lsUUID := range port.VLANBindings {
				ls := lsByUUID[lsUUID]
				if ls == nil || ls.OtherConfig[vtepOtherConfigVendorKey] != util.CniTypeName {
					continue
				}
				key := vtepVLANKey(ps.Name, port.Name, vlanID)
				if expected, ok := liveVLAN[key]; ok && expected == ls.Name {
					continue
				}
				klog.Infof("gc stale VTEP vlan_bindings[%d] on %s/%s for logical switch %s",
					vlanID, ps.Name, port.Name, ls.Name)
				ops, err := c.Where(port).Mutate(port, model.Mutation{
					Field:   &port.VLANBindings,
					Mutator: ovsdb.MutateOperationDelete,
					Value:   map[int]string{vlanID: lsUUID},
				})
				if err != nil {
					_ = c.portKeyMutex.UnlockKey(lockKey)
					return fmt.Errorf("generate GC mutate for vlan_bindings on port %s: %w", port.Name, err)
				}
				if err = c.Transact("vtep-vlan-gc", ops); err != nil {
					_ = c.portKeyMutex.UnlockKey(lockKey)
					return fmt.Errorf("gc vlan_bindings[%d] on physical port %s: %w", vlanID, port.Name, err)
				}
			}
			_ = c.portKeyMutex.UnlockKey(lockKey)
		}
	}

	return c.gcUnreferencedLogicalSwitches(liveLS)
}

func (c *VtepClient) gcUnreferencedLogicalSwitches(liveLS map[string]struct{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var logicalSwitches []vtep.LogicalSwitch
	if err := c.List(ctx, &logicalSwitches); err != nil {
		return fmt.Errorf("list logical switches while GCing VTEP state: %w", err)
	}
	var ports []vtep.PhysicalPort
	if err := c.List(ctx, &ports); err != nil {
		return fmt.Errorf("list physical ports while GCing VTEP logical switches: %w", err)
	}

	referenced := make(map[string]struct{})
	for _, port := range ports {
		for _, lsUUID := range port.VLANBindings {
			referenced[lsUUID] = struct{}{}
		}
	}

	for i := range logicalSwitches {
		ls := &logicalSwitches[i]
		if ls.OtherConfig[vtepOtherConfigVendorKey] != util.CniTypeName {
			continue
		}
		if _, live := liveLS[ls.Name]; live {
			continue
		}
		if _, ok := referenced[ls.UUID]; ok {
			continue
		}
		klog.Infof("gc unreferenced VTEP logical switch %s", ls.Name)
		ops, err := c.Where(ls).Delete()
		if err != nil {
			return fmt.Errorf("generate delete for VTEP logical switch %s: %w", ls.Name, err)
		}
		if err = c.Transact("vtep-ls-gc", ops); err != nil {
			return fmt.Errorf("gc VTEP logical switch %s: %w", ls.Name, err)
		}
	}
	return nil
}
