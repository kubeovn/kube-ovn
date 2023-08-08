package ovs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
)

var (
	ErrNoAddr   = errors.New("no address")
	ErrNotFound = errors.New("not found")
)

// LegacyClient is the legacy ovn client
type LegacyClient struct {
	OvnTimeout                    int
	OvnSbAddress                  string
	OvnICNbAddress                string
	OvnICSbAddress                string
	ClusterRouter                 string
	ClusterTcpLoadBalancer        string
	ClusterUdpLoadBalancer        string
	ClusterTcpSessionLoadBalancer string
	ClusterUdpSessionLoadBalancer string
	NodeSwitch                    string
	NodeSwitchCIDR                string
	Version                       string
}

type ovnClient struct {
	ovsDbClient
	ClusterRouter  string
	NodeSwitchCIDR string
}

type ovsDbClient struct {
	client.Client
	Timeout time.Duration
}

const (
	OvnNbCtl   = "ovn-nbctl"
	OvnSbCtl   = "ovn-sbctl"
	OVNIcNbCtl = "ovn-ic-nbctl"
	OVNIcSbCtl = "ovn-ic-sbctl"
	OvsVsCtl   = "ovs-vsctl"
	MayExist   = "--may-exist"
	IfExists   = "--if-exists"

	OVSDBWaitTimeout = 0
)

// NewLegacyClient init a legacy ovn client
func NewLegacyClient(timeout int, ovnSbAddr, clusterRouter, clusterTcpLoadBalancer, clusterUdpLoadBalancer, clusterTcpSessionLoadBalancer, clusterUdpSessionLoadBalancer, nodeSwitch, nodeSwitchCIDR string) *LegacyClient {
	return &LegacyClient{
		OvnSbAddress:                  ovnSbAddr,
		OvnTimeout:                    timeout,
		ClusterRouter:                 clusterRouter,
		ClusterTcpLoadBalancer:        clusterTcpLoadBalancer,
		ClusterUdpLoadBalancer:        clusterUdpLoadBalancer,
		ClusterTcpSessionLoadBalancer: clusterTcpSessionLoadBalancer,
		ClusterUdpSessionLoadBalancer: clusterUdpSessionLoadBalancer,
		NodeSwitch:                    nodeSwitch,
		NodeSwitchCIDR:                nodeSwitchCIDR,
	}
}

func NewOvnNbClient(ovnNbAddr string, ovnNbTimeout int, nodeSwitchCIDR string, insecureSkipVerify bool) (*ovnClient, error) {
	nbClient, err := ovsclient.NewNbClient(ovnNbAddr, insecureSkipVerify)
	if err != nil {
		klog.Errorf("failed to create OVN NB client: %v", err)
		return nil, err
	}

	c := &ovnClient{
		ovsDbClient: ovsDbClient{
			Client:  nbClient,
			Timeout: time.Duration(ovnNbTimeout) * time.Second,
		},
		NodeSwitchCIDR: nodeSwitchCIDR,
	}
	return c, nil
}

func NewOvnSbClient(ovnSbAddr string, ovnSbTimeout int, nodeSwitchCIDR string, insecureSkipVerify bool) (*ovnClient, error) {
	sbClient, err := ovsclient.NewSbClient(ovnSbAddr, insecureSkipVerify)
	if err != nil {
		klog.Errorf("failed to create OVN SB client: %v", err)
		return nil, err
	}

	c := &ovnClient{
		ovsDbClient: ovsDbClient{
			Client:  sbClient,
			Timeout: time.Duration(ovnSbTimeout) * time.Second,
		},
		NodeSwitchCIDR: nodeSwitchCIDR,
	}
	return c, nil
}

// TODO: support ic-nb ic-sb client

func ConstructWaitForNameNotExistsOperation(name string, table string) ovsdb.Operation {
	return ConstructWaitForUniqueOperation(table, "name", name)
}

func ConstructWaitForUniqueOperation(table string, column string, value interface{}) ovsdb.Operation {
	timeout := OVSDBWaitTimeout
	return ovsdb.Operation{
		Op:      ovsdb.OperationWait,
		Table:   table,
		Timeout: &timeout,
		Where:   []ovsdb.Condition{{Column: column, Function: ovsdb.ConditionEqual, Value: value}},
		Columns: []string{column},
		Until:   "!=",
		Rows:    []ovsdb.Row{{column: value}},
	}
}

func (c *ovnClient) Transact(method string, operations []ovsdb.Operation) error {
	if len(operations) == 0 {
		klog.V(6).Info("operations should not be empty")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	start := time.Now()
	results, err := c.ovsDbClient.Transact(ctx, operations...)
	elapsed := float64((time.Since(start)) / time.Millisecond)

	var dbType string
	switch c.Schema().Name {
	case "OVN_Northbound":
		dbType = "ovn-nb"
	case "OVN_Southbound":
		dbType = "ovn-sb"
	}

	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues(dbType, method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Errorf("error occurred in transact with %s operations: %+v in %vms", dbType, operations, elapsed)
		return err
	}

	if elapsed > 500 {
		klog.Warningf("%s operations took too long: %+v in %vms", dbType, operations, elapsed)
	}

	errors, err := ovsdb.CheckOperationResults(results, operations)
	if err != nil {
		klog.Errorf("error occurred in transact with operations %+v with operation errors %+v: %v", operations, errors, err)
		return err
	}

	return nil
}

func (c *ovnClient) GetVersion() (string, error) {
	if c.Schema().Version != "" {
		return c.Schema().Version, nil
	} else {
		err := fmt.Errorf("failed to get version")
		klog.Error(err)
		return "", err
	}
}
