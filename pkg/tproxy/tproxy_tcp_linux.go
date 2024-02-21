// This code below is referenced at https://github.com/Asphaltt/go-tproxy/blob/master/tproxy_tcp.go
// Because the code needs to be customized somewhere, the project is not directly imported
package tproxy

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"k8s.io/klog/v2"
)

// Listener describes a TCP Listener
// with the Linux IP_TRANSPARENT option defined
// on the listening socket
type Listener struct {
	base net.Listener
}

// Accept waits for and returns
// the next connection to the listener.
//
// This command wraps the AcceptTProxy
// method of the Listener
func (listener *Listener) Accept() (net.Conn, error) {
	return listener.AcceptTProxy()
}

// AcceptTProxy will accept a TCP connection
// and wrap it to a TProxy connection to provide
// TProxy functionality
func (listener *Listener) AcceptTProxy() (*Conn, error) {
	tcpConn, err := listener.base.(*net.TCPListener).AcceptTCP()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return &Conn{TCPConn: tcpConn}, nil
}

// Addr returns the network address
// the listener is accepting connections
// from
func (listener *Listener) Addr() net.Addr {
	return listener.base.Addr()
}

// Close will close the listener from accepting
// any more connections. Any blocked connections
// will unblock and close
func (listener *Listener) Close() error {
	return listener.base.Close()
}

// ListenTCP will construct a new TCP listener
// socket with the Linux IP_TRANSPARENT option
// set on the underlying socket
func ListenTCP(network string, laddr *net.TCPAddr) (net.Listener, error) {
	return listenTCP("", network, laddr)
}

func listenTCP(device, network string, laddr *net.TCPAddr) (net.Listener, error) {
	listener, err := net.ListenTCP(network, laddr)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	fileDescriptorSource, err := listener.File()
	if err != nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("get file descriptor: %s", err)}
	}

	defer func() {
		if err := fileDescriptorSource.Close(); err != nil {
			klog.Errorf("fileDescriptorSource %v Close err: %v", fileDescriptorSource, err)
		}
	}()

	if device != "" {
		if err = syscall.BindToDevice(int(fileDescriptorSource.Fd()), device); err != nil {
			return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("set socket option: SO_BINDTODEVICE(%s): %s", device, err)}
		}
	}

	if err = syscall.SetsockoptInt(int(fileDescriptorSource.Fd()), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1); err != nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("set socket option: IP_TRANSPARENT: %s", err)}
	}

	return &Listener{listener}, nil
}

// Conn describes a connection
// accepted by the TProxy listener.
//
// It is simply a TCP connection with
// the ability to dial a connection to
// the original destination while assuming
// the IP address of the client
type Conn struct {
	*net.TCPConn
}

// tcpAddToSockerAddr will convert a TCPAddr
// into a Sockaddr that may be used when
// connecting and binding sockets
func tcpAddrToSocketAddr(addr *net.TCPAddr) (syscall.Sockaddr, error) {
	switch {
	case addr.IP.To4() != nil:
		ip := [4]byte{}
		copy(ip[:], addr.IP.To4())

		return &syscall.SockaddrInet4{Addr: ip, Port: addr.Port}, nil

	default:
		ip := [16]byte{}
		copy(ip[:], addr.IP.To16())

		return &syscall.SockaddrInet6{Addr: ip, Port: addr.Port}, nil
	}
}

// tcpAddrFamily will attempt to work
// out the address family based on the
// network and TCP addresses
func tcpAddrFamily(net string, laddr, raddr *net.TCPAddr) int {
	switch net[len(net)-1] {
	case '4':
		return syscall.AF_INET
	case '6':
		return syscall.AF_INET6
	}

	if (laddr == nil || laddr.IP.To4() != nil) &&
		(raddr == nil || laddr.IP.To4() != nil) {
		return syscall.AF_INET
	}
	return syscall.AF_INET6
}

// DialTCP will open a
// TCP connection to the specified destination
// with the specified local address.
func DialTCP(laddr, raddr *net.TCPAddr, isnonblocking bool) (*net.TCPConn, error) {
	return dialTCP("", laddr, raddr, false, isnonblocking)
}

func dialTCP(device string, laddr, raddr *net.TCPAddr, dontAssumeRemote, isnonblocking bool) (*net.TCPConn, error) {
	if laddr == nil || raddr == nil {
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("empty local address or remote address")}
	}

	remoteSocketAddress, err := tcpAddrToSocketAddr(raddr)
	if err != nil {
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("build destination socket address: %s", err)}
	}

	localSocketAddress, err := tcpAddrToSocketAddr(laddr)
	if err != nil {
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("build local socket address: %s", err)}
	}

	fileDescriptor, err := syscall.Socket(tcpAddrFamily("tcp", raddr, laddr), syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("socket open: %s", err)}
	}

	if device != "" {
		if err = syscall.BindToDevice(fileDescriptor, device); err != nil {
			klog.Error(err)
			return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: SO_BINDTODEVICE(%s): %s", device, err)}
		}
	}

	if err = syscall.SetsockoptInt(fileDescriptor, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		if err := syscall.Close(fileDescriptor); err != nil {
			klog.Errorf("fileDescriptor %v Close err: %v", fileDescriptor, err)
		}
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: SO_REUSEADDR: %s", err)}
	}

	if err = syscall.SetsockoptInt(fileDescriptor, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1); err != nil {
		if err := syscall.Close(fileDescriptor); err != nil {
			klog.Errorf("fileDescriptor %v Close err: %v", fileDescriptor, err)
		}
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: IP_TRANSPARENT: %s", err)}
	}

	if err = syscall.SetNonblock(fileDescriptor, isnonblocking); err != nil {
		if err := syscall.Close(fileDescriptor); err != nil {
			klog.Errorf("fileDescriptor %v Close err: %v", fileDescriptor, err)
		}
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: SO_NONBLOCK: %s", err)}
	}

	if !dontAssumeRemote {
		if err = syscall.Bind(fileDescriptor, localSocketAddress); err != nil {
			if err := syscall.Close(fileDescriptor); err != nil {
				klog.Errorf("fileDescriptor %v Close err: %v", fileDescriptor, err)
			}
			klog.Error(err)
			return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("socket bind: %s", err)}
		}
	}

	if err = syscall.Connect(fileDescriptor, remoteSocketAddress); err != nil && !strings.Contains(err.Error(), "operation now in progress") {
		if err := syscall.Close(fileDescriptor); err != nil {
			klog.Errorf("fileDescriptor %v Close err: %v", fileDescriptor, err)
		}
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("socket connect: %s", err)}
	}

	fdFile := os.NewFile(uintptr(fileDescriptor), fmt.Sprintf("net-tcp-dial-%s", raddr.String()))
	defer func() {
		if err := fdFile.Close(); err != nil {
			klog.Errorf("fdFile %v Close err: %v", fdFile, err)
		}
	}()

	remoteConn, err := net.FileConn(fdFile)
	if err != nil {
		if err := syscall.Close(fileDescriptor); err != nil {
			klog.Errorf("fileDescriptor %v Close err: %v", fileDescriptor, err)
		}
		klog.Error(err)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("convert file descriptor to connection: %s", err)}
	}

	return remoteConn.(*net.TCPConn), nil
}
