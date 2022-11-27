package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c OvnClient) GetLogicalRouterPort(name string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrp := &ovnnb.LogicalRouterPort{Name: name}
	if err := c.ovnNbClient.Get(ctx, lrp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical router port %s: %v", name, err)
	}

	return lrp, nil
}

func (c OvnClient) AddLogicalRouterPort(lr, name, mac, networks string) error {
	router, err := c.GetLogicalRouter(lr, false)
	if err != nil {
		return err
	}

	if mac == "" {
		mac = util.GenerateMac()
	}

	var ops []ovsdb.Operation

	lrp := &ovnnb.LogicalRouterPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        name,
		MAC:         mac,
		Networks:    strings.Split(networks, ","),
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	// ensure there is no port in the same name, before we create it in the transaction
	waitOp := ConstructWaitForNameNotExistsOperation(name, "Logical_Router_Port")
	ops = append(ops, waitOp)

	createOps, err := c.ovnNbClient.Create(lrp)
	if err != nil {
		return err
	}
	ops = append(ops, createOps...)

	mutationOps, err := c.ovnNbClient.
		Where(router).
		Mutate(router,
			model.Mutation{
				Field:   &router.Ports,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   []string{lrp.UUID},
			},
		)
	if err != nil {
		return err
	}
	ops = append(ops, mutationOps...)

	if err := Transact(c.ovnNbClient, "lrp-add", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to create logical router port %s: %v", name, err)
	}
	return nil
}

func (c OvnClient) LogicalRouterPortExists(name string) (bool, error) {
	lrp, err := c.GetLogicalRouterPort(name, true)
	return lrp != nil, err
}
