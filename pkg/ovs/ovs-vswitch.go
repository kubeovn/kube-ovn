package ovs

import (
	"fmt"
	"path"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

const defaultVswitchEndpoint = "unix:/var/run/openvswitch/db.sock"

// VswitchClient is a client for interacting with the vswitch database
type VswitchClient struct {
	*table.Database
}

var (
	_ Vswitch              = (*VswitchClient)(nil)
	_ table.TableProvider = (*VswitchClient)(nil)
)

// NewVswitchClient creates a new vswitch client
func NewVswitchClient(addr string, connTimeout, transactTimeout int) (*VswitchClient, error) {
	addr = normalizeVswitchEndpoint(addr)

	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
		vswitch.QoSTable:         &vswitch.QoS{},
		vswitch.QueueTable:       &vswitch.Queue{},
	})
	if err != nil {
		return nil, logWrap(err, wrapErr("failed to create client db model: %w"))
	}

	monitors := []table.MonitorOption{
		table.WithTable(&vswitch.Bridge{}),
		table.WithTable(&vswitch.Interface{}),
		table.WithTable(&vswitch.OpenvSwitch{}),
		table.WithTable(&vswitch.Port{}),
		table.WithTable(&vswitch.QoS{}),
		table.WithTable(&vswitch.Queue{}),
	}
	c, err := ovsclient.NewOvsDbClient(
		vswitch.DatabaseName,
		addr,
		dbModel,
		monitors,
		connTimeout,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create vswitch client: %w", err)
	}

	return &VswitchClient{
		Database: table.NewDatabase(c, time.Duration(transactTimeout)*time.Second, table.RetryPolicy{},
			table.WithDatabaseName("vswitchd"), table.WithTransactionObserver(ovsTransactionObserver{})),
	}, nil
}

func normalizeVswitchEndpoint(endpoint string) string {
	if endpoint == "" {
		return defaultVswitchEndpoint
	}
	if path.IsAbs(endpoint) {
		return "unix:" + endpoint
	}
	return endpoint
}
