package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c OvnClient) GetLogicalSwitchPort(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lsp := &ovnnb.LogicalSwitchPort{Name: name}
	if err := c.ovnNbClient.Get(ctx, lsp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical switch port %s: %v", name, err)
	}

	return lsp, nil
}

func (c OvnClient) ListPodLogicalSwitchPorts(key string) ([]ovnnb.LogicalSwitchPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(lsp *ovnnb.LogicalSwitchPort) bool {
		return len(lsp.ExternalIDs) != 0 && lsp.ExternalIDs["pod"] == key
	})
	if err != nil {
		return nil, err
	}

	var lspList []ovnnb.LogicalSwitchPort
	if err = api.List(context.TODO(), &lspList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch ports of Pod %s: %v", key, err)
	}

	return lspList, nil
}

func (c OvnClient) ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(lsp *ovnnb.LogicalSwitchPort) bool {
		if lsp.Type != "" {
			return false
		}
		if needVendorFilter && (len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}
		if len(lsp.ExternalIDs) < len(externalIDs) {
			return false
		}
		if len(lsp.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				if lsp.ExternalIDs[k] != v {
					return false
				}
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	var lspList []ovnnb.LogicalSwitchPort
	if err = api.List(context.TODO(), &lspList); err != nil {
		klog.Errorf("failed to list logical switch ports: %v", err)
		return nil, err
	}

	return lspList, nil
}

func (c OvnClient) LogicalSwitchPortExists(name string) (bool, error) {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	return lsp != nil, err
}

func (c OvnClient) AddSwitchRouterPort(ls, lr string) error {
	klog.Infof("add %s to %s", ls, lr)
	lsTolr := fmt.Sprintf("%s-%s", ls, lr)
	lrTols := fmt.Sprintf("%s-%s", lr, ls)

	logicalSwitch, err := c.GetLogicalSwitch(ls, false)
	if err != nil {
		return err
	}

	var ops []ovsdb.Operation

	lsp := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lsTolr,
		Type:        "router",
		Addresses:   []string{"router"},
		Options:     map[string]string{"router-port": lrTols},
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	// ensure there is no port in the same name, before we create it in the transaction
	waitOp := ConstructWaitForNameNotExistsOperation(lsTolr, "Logical_Switch_Port")
	ops = append(ops, waitOp)

	createOps, err := c.ovnNbClient.Create(lsp)
	if err != nil {
		return err
	}
	ops = append(ops, createOps...)

	mutationOps, err := c.ovnNbClient.
		Where(logicalSwitch).
		Mutate(logicalSwitch,
			model.Mutation{
				Field:   &logicalSwitch.Ports,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   []string{lsp.UUID},
			},
		)
	if err != nil {
		return err
	}
	ops = append(ops, mutationOps...)

	if err := Transact(c.ovnNbClient, "lsp-add", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to create logical switch port %s: %v", lsTolr, err)
	}
	return nil
}
