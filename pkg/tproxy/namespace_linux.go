package tproxy

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"golang.org/x/sys/unix"
)

const NamespaceSocket = "/run/kube-ovn-tproxy/tproxy.sock"

type NamespaceRequest struct {
	NetNS string
	PodIP string
	Port  int32
}

func (r NamespaceRequest) Validate() error {
	if net.ParseIP(r.PodIP) == nil || r.Port < 1 || r.Port > 65535 {
		return errors.New("invalid Pod IP or TCP port")
	}
	dir := filepath.Dir(r.NetNS)
	if (dir != "/var/run/netns" && dir != "/run/netns") || filepath.Clean(r.NetNS) != r.NetNS {
		return errors.New("invalid Pod network namespace")
	}
	return nil
}

// DialNamespace receives a connected TCP socket from the helper. The daemon
// never switches namespaces and owns the returned connection's lifetime.
func DialNamespace(socket string, request NamespaceRequest) (net.Conn, error) {
	conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socket, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err = conn.Write(data); err != nil {
		return nil, err
	}
	data = make([]byte, 4096)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := conn.ReadMsgUnix(data, oob)
	if err != nil {
		return nil, err
	}
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, message := range messages {
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
			return nil, err
		}
		fds = append(fds, rights...)
	}
	defer func() {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
	}()
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || string(data[:n]) != "ok" || len(fds) != 1 {
		return nil, fmt.Errorf("namespace dial failed: %s", data[:n])
	}
	unix.CloseOnExec(fds[0])
	file := os.NewFile(uintptr(fds[0]), "tproxy")
	// FileConn duplicates the descriptor; close only through file from here.
	fds = nil
	defer file.Close()
	return net.FileConn(file)
}

// ServeNamespaceConnections accepts only root and the installation's nobody
// service user, and checks OVS ownership before opening any namespace.
func ServeNamespaceConnections(listener *net.UnixListener, validate func(NamespaceRequest) error) error {
	return serveNamespaceConnections(listener, validate, dialNamespaceTCP)
}

func serveNamespaceConnections(listener *net.UnixListener, validate func(NamespaceRequest) error, dial func(NamespaceRequest) (*net.TCPConn, error)) error {
	slots := make(chan struct{}, 64)
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return err
		}
		slots <- struct{}{}
		go func() {
			defer func() { <-slots }()
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			remote, err := namespaceConnection(conn, validate, dial)
			if err != nil {
				_, _ = conn.Write([]byte(err.Error()))
				return
			}
			defer remote.Close()
			file, err := remote.File()
			if err != nil {
				_, _ = conn.Write([]byte(err.Error()))
				return
			}
			defer file.Close()
			_, _, _ = conn.WriteMsgUnix([]byte("ok"), unix.UnixRights(int(file.Fd())), nil)
		}()
	}
}

func namespaceConnection(conn *net.UnixConn, validate func(NamespaceRequest) error, dial func(NamespaceRequest) (*net.TCPConn, error)) (*net.TCPConn, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peer *unix.Ucred
	if controlErr := raw.Control(func(fd uintptr) { peer, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); controlErr != nil {
		return nil, controlErr
	}
	if err != nil {
		return nil, err
	}
	if peer.Uid != 0 && peer.Uid != 65534 {
		return nil, errors.New("unauthorized namespace dial peer")
	}
	data := make([]byte, 4096)
	n, _, flags, _, err := conn.ReadMsgUnix(data, nil)
	if err != nil {
		return nil, err
	}
	if flags&unix.MSG_TRUNC != 0 {
		return nil, errors.New("namespace dial request too large")
	}
	var request NamespaceRequest
	if err := json.Unmarshal(data[:n], &request); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := validate(request); err != nil {
		return nil, err
	}
	return dial(request)
}

func dialNamespaceTCP(request NamespaceRequest) (*net.TCPConn, error) {
	var remote net.Conn
	err := ns.WithNetNSPath(request.NetNS, func(_ ns.NetNS) error {
		dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(request.PodIP)}, Timeout: 3 * time.Second}
		var err error
		remote, err = dialer.Dial("tcp", net.JoinHostPort(request.PodIP, strconv.Itoa(int(request.Port))))
		return err
	})
	if err != nil {
		if remote != nil {
			_ = remote.Close()
		}
		return nil, err
	}
	return remote.(*net.TCPConn), nil
}
