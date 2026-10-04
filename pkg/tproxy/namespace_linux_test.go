package tproxy

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNamespaceRequestValidation(t *testing.T) {
	valid := NamespaceRequest{NetNS: "/var/run/netns/cni-pod", PodIP: "10.16.0.2", Port: 8080}
	require.NoError(t, valid.Validate())
	for _, path := range []string{"/var/run/netns/442b8169-919d-430a-b501-394348905ea1", "/run/netns/cni-pod"} {
		request := valid
		request.NetNS = path
		require.NoError(t, request.Validate())
	}
	for _, path := range []string{"/proc/1/ns/net", "/var/run/netns/../netns/cni-pod", "/var/run/netns/cni-pod/child", "cni-pod"} {
		request := valid
		request.NetNS = path
		require.Error(t, request.Validate())
	}
	for _, port := range []int32{0, -1, 65536} {
		request := valid
		request.Port = port
		require.Error(t, request.Validate())
	}
}

func TestNamespaceSocketHandoffAndValidation(t *testing.T) {
	if uid := os.Geteuid(); uid != 0 && uid != 65534 {
		t.Skip("namespace helper accepts only root or nobody")
	}
	dir, err := os.MkdirTemp("", "tproxy-test-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	socket := filepath.Join(dir, "helper.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
	require.NoError(t, err)
	tcpListener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tcpListener.Close() })
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveNamespaceConnections(listener, func(request NamespaceRequest) error {
			if request.PodIP != "10.16.0.2" {
				return errors.New("OVS ownership mismatch")
			}
			return nil
		}, func(_ NamespaceRequest) (*net.TCPConn, error) {
			return net.DialTCP("tcp4", nil, tcpListener.Addr().(*net.TCPAddr))
		})
	}()
	t.Cleanup(func() { _ = listener.Close(); require.ErrorIs(t, <-serverDone, net.ErrClosed) })
	request := NamespaceRequest{NetNS: "/var/run/netns/cni-pod", PodIP: "10.16.0.2", Port: 8080}
	conn, err := DialNamespace(socket, request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	peer, err := tcpListener.AcceptTCP()
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
	require.NoError(t, peer.SetDeadline(time.Now().Add(time.Second)))
	_, err = peer.Write([]byte("probe response"))
	require.NoError(t, err)
	data := make([]byte, len("probe response"))
	_, err = io.ReadFull(conn, data)
	require.NoError(t, err)
	require.Equal(t, "probe response", string(data))
	request.PodIP = "10.16.0.3"
	_, err = DialNamespace(socket, request)
	require.ErrorContains(t, err, "OVS ownership mismatch")
	request.NetNS = "/proc/1/ns/net"
	_, err = DialNamespace(socket, request)
	require.ErrorContains(t, err, "invalid Pod network namespace")
}
