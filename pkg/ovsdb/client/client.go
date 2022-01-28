package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ovn-org/libovsdb/client"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

var namedUUIDCounter = rand.Uint32()

func NamedUUID() string {
	return fmt.Sprintf("u%010d", atomic.AddUint32(&namedUUIDCounter, 1))
}

// NewNbClient creates a new OVN NB client
func NewNbClient(addr string, timeout int) (client.Client, error) {
	dbModel, err := ovnnb.FullDatabaseModel()
	if err != nil {
		return nil, err
	}

	options := []client.Option{
		client.WithReconnect(time.Duration(timeout)*time.Second, &backoff.ZeroBackOff{}),
		client.WithLeaderOnly(true),
	}

	var ssl bool
	for _, ep := range strings.Split(addr, ",") {
		if !ssl && strings.HasPrefix(ep, client.SSL) {
			ssl = true
		}
		options = append(options, client.WithEndpoint(ep))
	}

	if ssl {
		cert, err := tls.LoadX509KeyPair("/var/run/tls/cert", "/var/run/tls/key")
		if err != nil {
			return nil, fmt.Errorf("failed to load x509 cert key pair: %v", err)
		}
		caCert, err := os.ReadFile("/var/run/tls/cacert")
		if err != nil {
			return nil, fmt.Errorf("failed to read ca cert: %v", err)
		}

		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(caCert)
		// #nosec
		tlsConfig := &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            certPool,
			InsecureSkipVerify: true,
		}

		options = append(options, client.WithTLSConfig(tlsConfig))
	}

	c, err := client.NewOVSDBClient(dbModel, options...)
	if err != nil {
		return nil, err
	}

	if err = c.Connect(context.TODO()); err != nil {
		klog.Errorf("failed to connect to OVN NB server %s: %v", addr, err)
		return nil, err
	}

	monitorOpts := []client.MonitorOption{
		client.WithTable(&ovnnb.ACL{}),
		client.WithTable(&ovnnb.AddressSet{}),
		client.WithTable(&ovnnb.LoadBalancer{}),
		client.WithTable(&ovnnb.LogicalRouter{}),
		client.WithTable(&ovnnb.LogicalRouterPolicy{}),
		client.WithTable(&ovnnb.LogicalRouterPort{}),
		client.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		client.WithTable(&ovnnb.LogicalSwitch{}),
		client.WithTable(&ovnnb.LogicalSwitchPort{}),
		client.WithTable(&ovnnb.PortGroup{}),
		client.WithTable(&ovnnb.QoS{}),
	}
	if _, err = c.Monitor(context.TODO(), c.NewMonitor(monitorOpts...)); err != nil {
		klog.Errorf("failed to monitor database on OVN NB server %s: %v", addr, err)
		return nil, err
	}

	return c, nil
}
