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

	"github.com/kubeovn/kube-ovn/pkg/util"
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
	dbLogger := logger.WithValues("db", db)
	options := []client.Option{
		client.WithLeaderOnly(true),
		client.WithLogger(&dbLogger),
	}
	connectTimeout := time.Duration(ovsDbConTimeout) * time.Second
	inactivityTimeout := time.Duration(ovsDbInactivityTimeout) * time.Second
	if inactivityTimeout > 0 {
		// Reading and parsing the DB after reconnect at scale can (unsurprisingly)
		// take longer than a normal ovsdb operation. Give it a bit more time so
		// we don't time out and enter a reconnect loop. In addition it also enables
		// inactivity check on the ovsdb connection.
		options = append(options, client.WithInactivityCheck(inactivityTimeout, connectTimeout, &backoff.ZeroBackOff{}))
	} else {
		options = append(options, client.WithReconnect(connectTimeout, &backoff.ZeroBackOff{}))
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
		cert, err := tls.LoadX509KeyPair(util.SslCertPath, util.SslKeyPath)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to load x509 cert key pair: %w", err)
		}
		caCert, err := os.ReadFile(util.SslCACert)
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

	if len(monitors) != 0 {
		monitor := c.NewMonitor(monitors...)
		monitor.Method = ovsdb.ConditionalMonitorRPC
		if _, err = c.Monitor(context.TODO(), monitor); err != nil {
			c.Close()
			klog.Errorf("failed to monitor database on OVN %s server %s: %v", db, addr, err)
			return nil, err
		}
	}

	return c, nil
}
