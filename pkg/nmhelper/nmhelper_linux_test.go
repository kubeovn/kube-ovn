package nmhelper

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestValidation(t *testing.T) {
	for _, name := range []string{"eth0", "a", "eth0.100", "bond0-1", "enp2s0f1np1", "123456789012345"} {
		require.NoError(t, Request{Interface: name}.Validate(), name)
	}
	for _, name := range []string{"", ".", "..", "1234567890123456", "eth0/1", "../eth0", "eth0:1", "eth 0", "eth0\t", "\neth0", "eth\u00a00"} {
		require.Error(t, Request{Interface: name}.Validate(), "%q", name)
	}
}

// The fake server skips the peer check, so this runs as any UID, including CI's.
func TestSetUnmanagedClient(t *testing.T) {
	socket := filepath.Join(tempDir(t), "helper.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	requests := make(chan string, 2)
	go func() {
		for _, reply := range []string{"ok", "no device found"} {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			data := make([]byte, 4096)
			n, _ := conn.Read(data)
			requests <- string(data[:n])
			_, _ = conn.Write([]byte(reply))
			_ = conn.Close()
		}
	}()
	require.NoError(t, SetUnmanaged(socket, "eth0"))
	require.JSONEq(t, `{"Interface":"eth0"}`, <-requests)
	require.EqualError(t, SetUnmanaged(socket, "missing0"), `NetworkManager helper replied "no device found"`)
	require.JSONEq(t, `{"Interface":"missing0"}`, <-requests)
}

func TestServeSetsUnmanaged(t *testing.T) {
	if uid := os.Geteuid(); uid != 0 && uid != 65534 {
		t.Skip("NetworkManager helper accepts only root or nobody")
	}
	calls := make(chan string, 4)
	socket := serve(t, func(iface string) error {
		calls <- iface
		if iface == "missing0" {
			return errors.New("no device found")
		}
		return nil
	})
	require.NoError(t, SetUnmanaged(socket, "eth0"))
	require.Equal(t, "eth0", <-calls)

	err := SetUnmanaged(socket, "missing0")
	require.EqualError(t, err, `NetworkManager helper replied "no device found"`)
	require.Equal(t, "missing0", <-calls)

	require.ErrorContains(t, SetUnmanaged(socket, "eth0:1"), "invalid interface name")
	require.Empty(t, calls, "invalid names must not reach NetworkManager")
}

func TestServeRejectsMalformedRequests(t *testing.T) {
	if uid := os.Geteuid(); uid != 0 && uid != 65534 {
		t.Skip("NetworkManager helper accepts only root or nobody")
	}
	calls := make(chan string, 4)
	socket := serve(t, func(iface string) error {
		calls <- iface
		return nil
	})
	require.Equal(t, "NetworkManager helper request too large", exchange(t, socket, []byte(strings.Repeat("x", 4097))))
	require.NotEqual(t, "ok", exchange(t, socket, []byte("eth0")))
	require.Equal(t, "invalid interface name", exchange(t, socket, []byte(`{}`)))
	require.Equal(t, "invalid interface name", exchange(t, socket, []byte(`{"Interface":"../../x"}`)))
	require.Empty(t, calls, "malformed requests must not reach NetworkManager")
}

func TestServeRejectsUnauthorizedPeer(t *testing.T) {
	if uid := os.Geteuid(); uid == 0 || uid == 65534 {
		t.Skip("root and nobody are authorized NetworkManager helper peers")
	}
	calls := make(chan string, 1)
	socket := serve(t, func(iface string) error {
		calls <- iface
		return nil
	})
	// Authentication must reject this UID before reading any request payload.
	require.Equal(t, "unauthorized NetworkManager helper peer", exchange(t, socket, nil))
	require.Empty(t, calls, "unauthorized peers must not reach NetworkManager")
}

// serve starts the helper protocol on a temporary socket with a fake writer.
func serve(t *testing.T, setUnmanaged func(string) error) string {
	t.Helper()
	socket := filepath.Join(tempDir(t), "helper.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- Serve(listener, setUnmanaged) }()
	t.Cleanup(func() { _ = listener.Close(); require.ErrorIs(t, <-done, net.ErrClosed) })
	return socket
}

// exchange sends a raw request, if any, and returns the helper's reply.
func exchange(t *testing.T, socket string, request []byte) string {
	t.Helper()
	conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socket, Net: "unixpacket"})
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	if request != nil {
		_, err = conn.Write(request)
		require.NoError(t, err)
	}
	reply := make([]byte, 4096)
	n, err := conn.Read(reply)
	require.NoError(t, err)
	return string(reply[:n])
}

// tempDir keeps Unix socket paths below Linux's limit even with long test names.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "nmhelper-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	return dir
}
