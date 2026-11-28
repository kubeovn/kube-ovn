package nmhelper

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

const Socket = "/run/kube-ovn-nm-helper/nm-helper.sock"

// Request asks the helper to set one host interface unmanaged by NetworkManager.
// The operation is implicit: the helper only ever sets Managed=false.
type Request struct {
	Interface string
}

// Validate applies the checks of the kernel's dev_valid_name.
func (r Request) Validate() error {
	name := r.Interface
	if name == "" || len(name) >= unix.IFNAMSIZ || name == "." || name == ".." ||
		strings.ContainsAny(name, "/:") || strings.ContainsFunc(name, unicode.IsSpace) {
		return errors.New("invalid interface name")
	}
	return nil
}

// SetUnmanaged asks the helper to set iface unmanaged by NetworkManager.
func SetUnmanaged(socket, iface string) error {
	conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socket, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	data, err := json.Marshal(Request{Interface: iface})
	if err != nil {
		return err
	}
	if _, err = conn.Write(data); err != nil {
		return err
	}
	data = make([]byte, 4096)
	n, err := conn.Read(data)
	if err != nil {
		return err
	}
	if string(data[:n]) != "ok" {
		return fmt.Errorf("NetworkManager helper replied %q", data[:n])
	}
	return nil
}

// Serve accepts only root and the installation's nobody service user, and
// validates the interface name before calling setUnmanaged.
func Serve(listener *net.UnixListener, setUnmanaged func(iface string) error) error {
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
			reply := "ok"
			if err := handle(conn, setUnmanaged); err != nil {
				reply = err.Error()
			}
			_, _ = conn.Write([]byte(reply))
		}()
	}
}

func handle(conn *net.UnixConn, setUnmanaged func(iface string) error) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peer *unix.Ucred
	if controlErr := raw.Control(func(fd uintptr) { peer, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return err
	}
	if peer.Uid != 0 && peer.Uid != 65534 {
		return errors.New("unauthorized NetworkManager helper peer")
	}
	data := make([]byte, 4096)
	n, _, flags, _, err := conn.ReadMsgUnix(data, nil)
	if err != nil {
		return err
	}
	if flags&unix.MSG_TRUNC != 0 {
		return errors.New("NetworkManager helper request too large")
	}
	var request Request
	if err := json.Unmarshal(data[:n], &request); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	return setUnmanaged(request.Interface)
}
