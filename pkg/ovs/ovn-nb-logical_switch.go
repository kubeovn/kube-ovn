package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) GetLogicalSwitch(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	ls := &ovnnb.LogicalSwitch{Name: name}
	if err := c.ovnNbClient.Get(context.TODO(), ls); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical switch %s: %v", name, err)
	}

	return ls, nil
}

func (c OvnClient) LogicalSwitchExists(name string) (bool, error) {
	ls, err := c.GetLogicalSwitch(name, true)
	return ls != nil, err
}
