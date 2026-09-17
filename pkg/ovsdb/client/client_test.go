package client

import (
	"context"
	"sync"
	"testing"
	"time"

	libovsdbclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

type blockingMonitorClient struct {
	rpcMutex          sync.RWMutex
	disconnectStarted chan struct{}
	disconnectDone    chan struct{}
	releaseMonitor    chan struct{}
	monitorMethod     string
}

func newBlockingMonitorClient() *blockingMonitorClient {
	return &blockingMonitorClient{
		disconnectStarted: make(chan struct{}),
		disconnectDone:    make(chan struct{}),
		releaseMonitor:    make(chan struct{}),
	}
}

func (*blockingMonitorClient) NewMonitor(...libovsdbclient.MonitorOption) *libovsdbclient.Monitor {
	return &libovsdbclient.Monitor{}
}

func (c *blockingMonitorClient) Monitor(ctx context.Context, monitor *libovsdbclient.Monitor) (libovsdbclient.MonitorCookie, error) {
	c.monitorMethod = monitor.Method
	c.rpcMutex.RLock()
	defer c.rpcMutex.RUnlock()

	go func() {
		close(c.disconnectStarted)
		c.rpcMutex.Lock()
		close(c.disconnectDone)
		c.rpcMutex.Unlock()
	}()
	<-c.disconnectStarted
	select {
	case <-ctx.Done():
		return libovsdbclient.MonitorCookie{}, ctx.Err()
	case <-c.releaseMonitor:
		return libovsdbclient.MonitorCookie{}, nil
	}
}

func TestMonitorWithTimeoutBreaksReconnectDeadlock(t *testing.T) {
	c := newBlockingMonitorClient()
	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- monitorWithTimeout(c, nil, 10*time.Millisecond)
	}()

	var err error
	select {
	case err = <-monitorDone:
	case <-time.After(time.Second):
		close(c.releaseMonitor)
		<-monitorDone
		t.Fatal("initial monitor did not honor its timeout")
	}

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, ovsdb.ConditionalMonitorRPC, c.monitorMethod)
	require.Eventually(t, func() bool {
		select {
		case <-c.disconnectDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}
