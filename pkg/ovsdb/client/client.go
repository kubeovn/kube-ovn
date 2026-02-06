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

	"github.com/cenkalti/backoff/v5"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
)

const (
	NBDB   = "nbdb"
	SBDB   = "sbdb"
	ICNBDB = "icnbdb"
	ICSBDB = "icsbdb"
)

var namedUUIDCounter uint32

var logger logr.Logger

func init() {
	buff := make([]byte, 4)
	if _, err := rand.Reader.Read(buff); err != nil {
		panic(err)
	}
	namedUUIDCounter = binary.LittleEndian.Uint32(buff)

	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(zapcore.Level(-3))
	zc.EncoderConfig.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		if l < zapcore.InfoLevel {
			l = zapcore.InfoLevel
		}
		enc.AppendString(l.String())
	}
	z, err := zc.Build()
	if err != nil {
		panic(err)
	}
	logger = zapr.NewLogger(z).WithName("libovsdb")
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
	klog.Infof("creating ovsdb client for %s database at %s", db, addr)

	var ssl bool
	var options []client.Option
	for ep := range strings.SplitSeq(addr, ",") {
		if !ssl && strings.HasPrefix(ep, client.SSL) {
			ssl = true
		}
		options = append(options, client.WithEndpoint(ep))
	}
	var backOff backoff.BackOff
	if len(options) > 1 {
		// multiple endpoints, use zero backoff to try next endpoint immediately
		backOff = &backoff.ZeroBackOff{}
	} else {
		// single endpoint, use exponential backoff for reconnects
		bo := backoff.NewExponentialBackOff()
		bo.InitialInterval = 1 * time.Second
		bo.MaxInterval = 10 * time.Second
		backOff = bo
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

	dbLogger := logger.WithValues("db", db)
	connectTimeout := time.Duration(ovsDbConTimeout) * time.Second
	inactivityTimeout := time.Duration(ovsDbInactivityTimeout) * time.Second
	options = append(options, client.WithLeaderOnly(true), client.WithLogger(&dbLogger))
	if inactivityTimeout > 0 {
		// Reading and parsing the DB after reconnect at scale can (unsurprisingly)
		// take longer than a normal ovsdb operation. Give it a bit more time so
		// we don't time out and enter a reconnect loop. In addition it also enables
		// inactivity check on the ovsdb connection.
		options = append(options, client.WithInactivityCheck(inactivityTimeout, connectTimeout, backOff))
	} else {
		options = append(options, client.WithReconnect(connectTimeout, backOff))
	}
	c, err := client.NewOVSDBClient(dbModel, options...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	klog.Infof("connecting to %s database server %s", db, addr)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err = c.Connect(ctx); err != nil {
		klog.Errorf("failed to connect to %s database server %s: %v", db, addr, err)
		return nil, err
	}

	klog.Infof("setting up monitors for %s database on server %s", db, addr)
	monitor := c.NewMonitor(monitors...)
	monitor.Method = ovsdb.ConditionalMonitorRPC
	if _, err = c.Monitor(context.TODO(), monitor); err != nil {
		c.Close()
		klog.Errorf("failed to monitor database on %s server %s: %v", db, addr, err)
		return nil, err
	}
	return c, nil
}
