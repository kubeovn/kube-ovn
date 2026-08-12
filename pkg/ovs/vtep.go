package ovs

import (
	"errors"
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vtep"
)

// VtepClient is a client for the Hardware VTEP OVSDB (hardware_vtep).
type VtepClient struct {
	ovsDbClient
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

	port, err := c.GetPhysicalPort(physicalSwitch, physicalPort, false)
	if err != nil {
		return err
	}

	if port.VLANBindings == nil {
		port.VLANBindings = make(map[int]string)
	}
	if existing, ok := port.VLANBindings[vlanID]; ok && existing == ls.UUID {
		return nil
	}
	port.VLANBindings[vlanID] = ls.UUID
	ops, err := c.Where(port).Update(port, &port.VLANBindings)
	if err != nil {
		return fmt.Errorf("generate update for vlan_bindings on port %s: %w", physicalPort, err)
	}
	if err = c.Transact("vtep-vlan-bind", ops); err != nil {
		return fmt.Errorf("set vlan_bindings[%d]=%s on physical port %s: %w", vlanID, ls.UUID, physicalPort, err)
	}
	klog.Infof("set VTEP vlan_bindings[%d] on physical switch %s port %s to logical switch %s",
		vlanID, physicalSwitch, physicalPort, logicalSwitch)
	return nil
}

// RemoveVtepBinding removes the owned vlan_binding and deletes the Logical_Switch
// when it is owned by this binding and no longer referenced.
func (c *VtepClient) RemoveVtepBinding(physicalSwitch, physicalPort, logicalSwitch, bindingName string, vlanID int) error {
	if physicalSwitch == "" || physicalPort == "" {
		return nil
	}

	port, err := c.GetPhysicalPort(physicalSwitch, physicalPort, true)
	if err != nil {
		return err
	}
	if port != nil && port.VLANBindings != nil {
		if _, ok := port.VLANBindings[vlanID]; ok {
			delete(port.VLANBindings, vlanID)
			ops, err := c.Where(port).Update(port, &port.VLANBindings)
			if err != nil {
				return fmt.Errorf("generate update to clear vlan_bindings on port %s: %w", physicalPort, err)
			}
			if err = c.Transact("vtep-vlan-unbind", ops); err != nil {
				return fmt.Errorf("clear vlan_bindings[%d] on physical port %s: %w", vlanID, physicalPort, err)
			}
			klog.Infof("cleared VTEP vlan_bindings[%d] on physical switch %s port %s",
				vlanID, physicalSwitch, physicalPort)
		}
	}

	return c.DeleteLogicalSwitchIfOwned(logicalSwitch, bindingName)
}
