package client

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	libovsdbclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestLibovsdbDebugVerbosity(t *testing.T) {
	if os.Getenv("KUBE_OVN_LOGGER_TEST_CHILD") == "1" {
		logger.V(3).Info("test-connection-marker")
		for range 150 {
			logger.V(4).Info("test-transaction-marker")
		}
		logger.V(5).Info("test-reconnect-marker")
		return
	}
	for _, verbosity := range []string{"", "3", "5", "invalid"} {
		t.Run(verbosity, func(t *testing.T) {
			// Use a fresh process because the logger reads its setting during init.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLibovsdbDebugVerbosity$")
			cmd.Env = append(os.Environ(), "KUBE_OVN_LOGGER_TEST_CHILD=1", "KUBE_OVN_LIBOVSDB_LOG_VERBOSITY="+verbosity)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Contains(t, string(output), "test-connection-marker")
			if verbosity == "5" {
				require.Equal(t, 150, strings.Count(string(output), "test-transaction-marker"))
				require.Contains(t, string(output), "test-reconnect-marker")
			} else {
				require.NotContains(t, string(output), "test-transaction-marker")
				require.NotContains(t, string(output), "test-reconnect-marker")
			}
		})
	}
}

type blockingMonitorClient struct {
	rpcMutex          sync.RWMutex
	disconnectStarted chan struct{}
	disconnectDone    chan struct{}
	releaseMonitor    chan struct{}
	monitorMethod     string
	monitorOptions    int
}

func newBlockingMonitorClient() *blockingMonitorClient {
	return &blockingMonitorClient{
		disconnectStarted: make(chan struct{}),
		disconnectDone:    make(chan struct{}),
		releaseMonitor:    make(chan struct{}),
	}
}

func (c *blockingMonitorClient) NewMonitor(opts ...libovsdbclient.MonitorOption) *libovsdbclient.Monitor {
	c.monitorOptions = len(opts)
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
		monitorDone <- monitorWithTimeout(c, []libovsdbclient.MonitorOption{
			libovsdbclient.WithTable(&vswitch.Bridge{}),
		}, 10*time.Millisecond)
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
	require.Equal(t, 1, c.monitorOptions)
	require.Eventually(t, func() bool {
		select {
		case <-c.disconnectDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}
