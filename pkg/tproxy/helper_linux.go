package tproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ServeNamespaceHelper owns the privileged namespace dial endpoint. The daemon
// keeps its listeners and forwarding without entering the destination netns.
func ServeNamespaceHelper(ovsSocket string) error {
	c, err := newNamespaceOVSClient("unix:" + ovsSocket)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := os.Remove(NamespaceSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: NamespaceSocket, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer listener.Close()
	// The Pod-local directory is mounted only by the daemon and helper.
	// SO_PEERCRED also rejects callers outside the root/nobody service users.
	if err := os.Chmod(NamespaceSocket, 0o666); err != nil {
		return err
	}
	return ServeNamespaceConnections(listener, func(request NamespaceRequest) error {
		ok, err := c.hasPodNetNS(request)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("pod IP %s does not own the requested network namespace", request.PodIP)
		}
		return nil
	})
}

type namespaceOVSClient struct {
	client.Client
}

func newNamespaceOVSClient(addr string) (*namespaceOVSClient, error) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.InterfaceTable: &vswitch.Interface{},
	})
	if err != nil {
		return nil, err
	}
	c, err := client.NewOVSDBClient(dbModel, client.WithEndpoint(addr),
		client.WithReconnect(30*time.Second, backoff.NewConstantBackOff(time.Second)))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		c.Close()
		return nil, err
	}
	// Synchronous selects need no node-wide monitor or interface cache.
	return &namespaceOVSClient{Client: c}, nil
}

func (c *namespaceOVSClient) hasPodNetNS(request NamespaceRequest) (bool, error) {
	ids, err := ovsdb.NewOvsMap(map[string]string{"pod_netns": request.NetNS, "vendor": util.CniTypeName})
	if err != nil {
		return false, err
	}
	op := ovsdb.Operation{
		Op: ovsdb.OperationSelect, Table: vswitch.InterfaceTable, Columns: []string{"external_ids"},
		Where: []ovsdb.Condition{ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, ids)},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, err := c.Transact(ctx, op)
	if err != nil {
		return false, err
	}
	if _, err := ovsdb.CheckOperationResults(results, []ovsdb.Operation{op}); err != nil {
		return false, err
	}
	for _, row := range results[0].Rows {
		values, ok := row["external_ids"].(ovsdb.OvsMap)
		if !ok {
			return false, errors.New("OVS interface has invalid external IDs")
		}
		ifaceID, _ := values.GoMap["iface-id"].(string)
		ips, _ := values.GoMap["ip"].(string)
		if ifaceID != "" && slices.Contains(strings.Split(ips, ","), request.PodIP) {
			return true, nil
		}
	}
	return false, nil
}
