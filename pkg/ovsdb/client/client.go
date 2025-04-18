package client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"
)

const (
	NBDB   = "nbdb"
	SBDB   = "sbdb"
	ICNBDB = "icnbdb"
	ICSBDB = "icsbdb"
)

var namedUUIDCounter uint32

func init() {
	buff := make([]byte, 4)
	if _, err := rand.Reader.Read(buff); err != nil {
		panic(err)
	}
	namedUUIDCounter = binary.LittleEndian.Uint32(buff)
}

func NamedUUID() string {
	return fmt.Sprintf("u%010d", atomic.AddUint32(&namedUUIDCounter, 1))
}

// NewOvsDbClient creates a new ovsdb client
func NewOvsDbClient(
	db string,
	addr string,
	dbModel model.ClientDBModel,
	monitors []client.MonitorOption,
	ovsDbConTimeout int,
	ovsDbInactivityTimeout int,
) (client.Client, error) {
	logger := klog.NewKlogr().WithName("libovsdb").WithValues("db", db)
	connectTimeout := time.Duration(ovsDbConTimeout) * time.Second
	inactivityTimeout := time.Duration(ovsDbInactivityTimeout) * time.Second
	options := []client.Option{
		// Reading and parsing the DB after reconnect at scale can (unsurprisingly)
		// take longer than a normal ovsdb operation. Give it a bit more time so
		// we don't time out and enter a reconnect loop. In addition it also enables
		// inactivity check on the ovsdb connection.
		client.WithInactivityCheck(inactivityTimeout, connectTimeout, &backoff.ZeroBackOff{}),

		client.WithLeaderOnly(true),
		client.WithLogger(&logger),
	}
	klog.Infof("connecting to OVN %s server %s", db, addr)
	var ssl bool
	endpoints := strings.SplitSeq(addr, ",")
	for ep := range endpoints {
		if !ssl && strings.HasPrefix(ep, client.SSL) {
			ssl = true
		}
		options = append(options, client.WithEndpoint(ep))
	}
	if ssl {
		cert, err := tls.LoadX509KeyPair("/var/run/tls/cert", "/var/run/tls/key")
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to load x509 cert key pair: %w", err)
		}
		caCert, err := os.ReadFile("/var/run/tls/cacert")
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to read ca cert: %w", err)
		}
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(caCert)
		tlsConfig := &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            certPool,
			InsecureSkipVerify: true, // #nosec G402
		}
		options = append(options, client.WithTLSConfig(tlsConfig))
	}
	c, err := client.NewOVSDBClient(dbModel, options...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err = c.Connect(ctx); err != nil {
		klog.Errorf("failed to connect to OVN NB server %s: %v", addr, err)
		return nil, err
	}

	monitor := c.NewMonitor(monitors...)
	monitor.Method = ovsdb.ConditionalMonitorRPC
	if _, err = c.Monitor(context.TODO(), monitor); err != nil {
		klog.Errorf("failed to monitor database on OVN %s server %s: %v", db, addr, err)
		return nil, err
	}
	return c, nil
}
