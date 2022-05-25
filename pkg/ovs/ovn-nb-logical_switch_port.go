package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c OvnClient) GetLogicalSwitchPort(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	lsp := &ovnnb.LogicalSwitchPort{Name: name}
	if err := c.ovnNbClient.Get(context.TODO(), lsp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical switch port %s: %v", name, err)
	}

	return lsp, nil
}

func (c OvnClient) ListPodLogicalSwitchPorts(key string) ([]ovnnb.LogicalSwitchPort, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0, 1)
	if err := c.ovnNbClient.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		return len(lsp.ExternalIDs) != 0 && lsp.ExternalIDs["pod"] == key
	}).List(context.TODO(), &lspList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch ports of Pod %s: %v", key, err)
	}

	return lspList, nil
}

func (c OvnClient) ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0)
	if err := c.ovnNbClient.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
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
	}).List(context.TODO(), &lspList); err != nil {
		klog.Errorf("failed to list logical switch ports: %v", err)
		return nil, err
	}

	return lspList, nil
}

func (c OvnClient) LogicalSwitchPortExists(name string) (bool, error) {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	return lsp != nil, err
}
