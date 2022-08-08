package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalSwitch create logical switch with basic configuration
func (c OvnClient) CreateBareLogicalSwitch(lsName string) error {
	ls := &ovnnb.LogicalSwitch{
		Name:        lsName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	op, err := c.ovnNbClient.Create(ls)
	if err != nil {
		return fmt.Errorf("generate create operations for logical switch %s: %v", lsName, err)
	}

	return c.Transact("ls-add", op)
}

func (c OvnClient) GetLogicalSwitch(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0)
	if err := c.ovnNbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		return ls.Name == name
	}).List(context.TODO(), &lsList); err != nil {
		return nil, fmt.Errorf("list switch switch %q: %v", name, err)
	}

	// not found
	if 0 == len(lsList) {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical switch %q", name)
	}

	if len(lsList) > 1 {
		return nil, fmt.Errorf("more than one logical switch with same name %q", name)
	}

	return &lsList[0], nil
}

func (c OvnClient) LogicalSwitchOp(lsName string, lsp *ovnnb.LogicalSwitchPort, opIsAdd bool) ([]ovsdb.Operation, error) {
	ls, err := c.GetLogicalSwitch(lsName, false)
	if err != nil {
		return nil, err
	}

	mutation := model.Mutation{
		Field: &ls.Ports,
		Value: []string{lsp.UUID},
	}

	if opIsAdd {
		mutation.Mutator = ovsdb.MutateOperationInsert
	} else {
		mutation.Mutator = ovsdb.MutateOperationDelete
	}

	ops, err := c.ovnNbClient.Where(ls).Mutate(ls, mutation)
	if nil != err {
		return nil, fmt.Errorf("generate mutate operations for logical switch %s: %v", lsName, err)
	}

	return ops, nil
}
