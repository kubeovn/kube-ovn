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
const timeout = 3 * time.Second

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
func NewOvsDbClient(db, addr string, dbModel model.ClientDBModel, monitors []client.MonitorOption) (client.Client, error) {
	logger := klog.NewKlogr().WithName("libovsdb").WithValues("db", db)
	options := []client.Option{
		client.WithReconnect(timeout, &backoff.ConstantBackOff{Interval: time.Second}),
		client.WithLeaderOnly(true),
		client.WithLogger(&logger),
	}
	klog.Infof("connecting to OVN %s server %s", db, addr)
	var ssl bool
	endpoints := strings.Split(addr, ",")
	for _, ep := range endpoints {
		if !ssl && strings.HasPrefix(ep, client.SSL) {
			ssl = true
		}
		options = append(options, client.WithEndpoint(ep))
	}
	if ssl {
		cert, err := tls.LoadX509KeyPair("/var/run/tls/cert", "/var/run/tls/key")
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to load x509 cert key pair: %v", err)
		}
		caCert, err := os.ReadFile("/var/run/tls/cacert")
		if err != nil {
			klog.Error(err)
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
		klog.Error(err)
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(endpoints)+1)*timeout)
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
