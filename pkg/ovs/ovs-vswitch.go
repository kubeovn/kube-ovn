package ovs

import (
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// VswitchClient is a client for interacting with the vswitch database
type VswitchClient struct {
	ovsDbClient
}

// NewVswitchClient creates a new vswitch client
func NewVswitchClient(addr string, connTimeout, transactTimeout int) (*VswitchClient, error) {
	dbModel, err := vswitch.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	monitors := []client.MonitorOption{
		client.WithTable(&vswitch.Bridge{}),
		client.WithTable(&vswitch.Interface{}),
		client.WithTable(&vswitch.OpenvSwitch{}),
		client.WithTable(&vswitch.Port{}),
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
		ovsDbClient: ovsDbClient{
			Client:  c,
			Timeout: time.Duration(transactTimeout) * time.Second,
		},
	}, nil
}
