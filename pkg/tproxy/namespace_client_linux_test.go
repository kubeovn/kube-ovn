package tproxy

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestNamespaceClientSocketHandoff(t *testing.T) {
	file, peer := namespaceTCPFile(t)
	socket, wait := namespaceReply(t, []byte("ok"), []*os.File{file})
	conn, err := DialNamespace(socket, NamespaceRequest{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	wait()
	// The helper has closed its TCP descriptor, Unix connection and listener.
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = conn.Write([]byte("request"))
	require.NoError(t, err)
	data := make([]byte, len("request"))
	_, err = io.ReadFull(peer, data)
	require.NoError(t, err)
	require.Equal(t, "request", string(data))
	require.NoError(t, conn.(*net.TCPConn).CloseWrite())
	_, err = peer.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
	_, err = peer.Write([]byte("response"))
	require.NoError(t, err)
	require.NoError(t, peer.CloseWrite())
	data, err = io.ReadAll(conn)
	require.NoError(t, err)
	require.Equal(t, "response", string(data))
}

func TestNamespaceClientRejectsInvalidReply(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		fds      int
	}{
		{name: "error with descriptor", response: "OVS ownership mismatch", fds: 1},
		{name: "missing descriptor", response: "ok"},
		{name: "multiple descriptors", response: "ok", fds: 2},
		{name: "truncated payload", response: strings.Repeat("x", 4097), fds: 1},
		{name: "truncated control", response: "ok", fds: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := make([]*os.File, 0, test.fds)
			peers := make([]*net.TCPConn, 0, test.fds)
			for range test.fds {
				file, peer := namespaceTCPFile(t)
				files = append(files, file)
				peers = append(peers, peer)
			}
			socket, wait := namespaceReply(t, []byte(test.response), files)
			conn, err := DialNamespace(socket, NamespaceRequest{})
			if conn != nil {
				t.Cleanup(func() { _ = conn.Close() })
			}
			require.Error(t, err)
			require.Nil(t, conn)
			wait()
			// EOF proves that neither the helper nor the client retained a FD.
			for _, peer := range peers {
				require.NoError(t, peer.SetReadDeadline(time.Now().Add(5*time.Second)))
				_, err := peer.Read(make([]byte, 1))
				require.ErrorIs(t, err, io.EOF)
			}
		})
	}
}

func TestNamespaceClientClosesInvalidDescriptor(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	socket, wait := namespaceReply(t, []byte("ok"), []*os.File{writer})
	conn, err := DialNamespace(socket, NamespaceRequest{})
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	require.Error(t, err)
	require.Nil(t, conn)
	wait()
	require.NoError(t, reader.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = reader.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}

// namespaceTCPFile leaves only the file owning the sender's TCP connection.
func namespaceTCPFile(t *testing.T) (*os.File, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer listener.Close()
	require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
	sender, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	require.NoError(t, err)
	defer sender.Close()
	peer, err := listener.AcceptTCP()
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	file, err := sender.File()
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	return file, peer
}

// namespaceReply exercises the client protocol without the helper's UID policy.
func namespaceReply(t *testing.T, response []byte, files []*os.File) (string, func()) {
	t.Helper()
	// Keep the Unix socket path below Linux's limit even with long test names.
	dir, err := os.MkdirTemp("", "tproxy-client-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	socket := filepath.Join(dir, "helper.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socket, Net: "unixpacket"})
	require.NoError(t, err)
	require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
	done := make(chan struct{})
	var serverErr error
	go func() {
		defer close(done)
		defer listener.Close()
		defer func() {
			for _, file := range files {
				_ = file.Close()
			}
		}()
		serverErr = func() error {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return err
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			if _, err := conn.Read(make([]byte, 4096)); err != nil {
				return err
			}
			fds := make([]int, 0, len(files))
			for _, file := range files {
				fds = append(fds, int(file.Fd()))
			}
			var control []byte
			if len(fds) != 0 {
				control = unix.UnixRights(fds...)
			}
			n, oobn, err := conn.WriteMsgUnix(response, control, nil)
			if err != nil {
				return err
			}
			if n != len(response) || oobn != len(control) {
				return fmt.Errorf("short helper reply: payload %d/%d, control %d/%d", n, len(response), oobn, len(control))
			}
			return nil
		}()
	}()
	wait := func() {
		t.Helper()
		select {
		case <-done:
			require.NoError(t, serverErr)
		case <-time.After(10 * time.Second):
			t.Fatal("namespace reply helper did not exit")
		}
	}
	t.Cleanup(func() { _ = listener.Close(); wait() })
	return socket, wait
}
