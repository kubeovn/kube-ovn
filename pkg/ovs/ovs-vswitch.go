package ovs

import (
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// VswitchClient is a client for interacting with the vswitch database
type VswitchClient struct {
	ovsDbClient
}

// NewVswitchClient creates a new vswitch client
func NewVswitchClient(addr string, connTimeout, transactTimeout int) (*VswitchClient, error) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
	})
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to create client db model: %w", err)
	}

	monitors := []compat.MonitorOption{
		compat.WithTable(&vswitch.Bridge{}),
		compat.WithTable(&vswitch.Interface{}),
		compat.WithTable(&vswitch.OpenvSwitch{}),
		compat.WithTable(&vswitch.Port{}),
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
		backend: c,
		Timeout: time.Duration(transactTimeout) * time.Second,
		call:    compat.New(c, time.Duration(transactTimeout)*time.Second, compat.RetryPolicy{Attempts: 2}),
	}, nil
}
