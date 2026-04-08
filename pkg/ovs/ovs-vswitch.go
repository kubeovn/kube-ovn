package ovs

import (
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// VswitchClient is a client for interacting with the vswitch database
type VswitchClient struct {
	ovsDbClient
}

var vswitchCoreTables = []string{
	vswitch.BridgeTable,
	vswitch.InterfaceTable,
	vswitch.OpenvSwitchTable,
	vswitch.PortTable,
}

// NewVswitchClient creates a new vswitch client
func NewVswitchClient(addr string, connTimeout, transactTimeout int) (*VswitchClient, error) {
	c, err := newCoreVswitchClient(addr, connTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create vswitch client: %w", err)
	}

	return &VswitchClient{
		ovsDbClient: ovsDbClient{
			Client:  c,
			Timeout: time.Duration(transactTimeout) * time.Second,
		},
	}, nil
}

func newCoreVswitchClient(addr string, connTimeout int) (client.Client, error) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client db model: %w", err)
	}

	monitors := make([]client.MonitorOption, 0, len(vswitchCoreTables))
	for _, tableName := range vswitchCoreTables {
		switch tableName {
		case vswitch.BridgeTable:
			monitors = append(monitors, client.WithTable(&vswitch.Bridge{}))
		case vswitch.InterfaceTable:
			monitors = append(monitors, client.WithTable(&vswitch.Interface{}))
		case vswitch.OpenvSwitchTable:
			monitors = append(monitors, client.WithTable(&vswitch.OpenvSwitch{}))
		case vswitch.PortTable:
			monitors = append(monitors, client.WithTable(&vswitch.Port{}))
		}
	}

	return ovsclient.NewOvsDbClient(vswitch.DatabaseName, addr, dbModel, monitors, connTimeout, 0)
}
