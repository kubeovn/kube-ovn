package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) GetPortGroup(name string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	pg := &ovnnb.PortGroup{Name: name}
	if err := c.ovnNbClient.Get(context.TODO(), pg); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get port group %s: %v", name, err)
	}

	return pg, nil
}

func (c OvnClient) CreatePortGroup(name string, externalIDs map[string]string) error {
	pg, err := c.GetPortGroup(name, true)
	if err != nil {
		return err
	}
	if pg != nil {
		return nil
	}

	pg = &ovnnb.PortGroup{
		Name:        name,
		ExternalIDs: externalIDs,
	}
	ops, err := c.ovnNbClient.Create(pg)
	if err != nil {
		return fmt.Errorf("failed to generate create operations for port group %s: %v", name, err)
	}
	if err = Transact(c.ovnNbClient, "pg-add", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to create port group %s: %v", name, err)
	}

	return nil
}

func (c OvnClient) portGroupPortOp(pgName, portName string, opIsAdd bool) error {
	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		return err
	}

	lsp, err := c.GetLogicalSwitchPort(portName, false)
	if err != nil {
		return err
	}

	portMap := make(map[string]struct{}, len(pg.Ports))
	for _, port := range pg.Ports {
		portMap[port] = struct{}{}
	}
	if _, ok := portMap[lsp.UUID]; ok == opIsAdd {
		return nil
	}

	if opIsAdd {
		pg.Ports = append(pg.Ports, lsp.UUID)
	} else {
		delete(portMap, lsp.UUID)
		pg.Ports = make([]string, 0, len(portMap))
		for port := range portMap {
			pg.Ports = append(pg.Ports, port)
		}
	}

	ops, err := c.ovnNbClient.Where(pg).Update(pg, &pg.Ports)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for port group %s: %v", pgName, err)
	}
	if err = Transact(c.ovnNbClient.Client, "update", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to update ports of port group %s: %v", pgName, err)
	}

	return nil
}

func (c OvnClient) PortGroupAddPort(pgName, portName string) error {
	return c.portGroupPortOp(pgName, portName, true)
}

func (c OvnClient) PortGroupRemovePort(pgName, portName string) error {
	return c.portGroupPortOp(pgName, portName, false)
}
