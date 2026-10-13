//go:build linux

package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"golang.org/x/sys/unix"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	nodeGatewaySocket    = "/run/kube-ovn-node-gw/node-gw.sock"
	nodeGatewayEnsure    = "ensure"
	nodeGatewayDelete    = "delete"
	nodeGatewayMaxPacket = 64 * 1024
)

// NodeGatewayRequest is the validated intent sent from cni-server to the
// node-gateway helper. Resource names and paths are deliberately fixed in the
// helper so the unprivileged caller cannot select arbitrary host objects.
type NodeGatewayRequest struct {
	Operation  string   `json:"operation"`
	NodeName   string   `json:"nodeName"`
	IP         string   `json:"ip,omitempty"`
	Gateway    string   `json:"gateway,omitempty"`
	MAC        string   `json:"mac,omitempty"`
	MTU        int      `json:"mtu,omitzero"`
	BFDLocalIP string   `json:"bfdLocalIP,omitempty"`
	BFDPeers   []string `json:"bfdPeers,omitempty"`
}

type nodeGatewayResponse struct {
	Error string `json:"error,omitempty"`
}

func (r NodeGatewayRequest) Validate(expectedNode string) error {
	if expectedNode == "" || r.NodeName != expectedNode {
		return fmt.Errorf("node name %q does not match helper node", r.NodeName)
	}
	if r.Operation != nodeGatewayEnsure && r.Operation != nodeGatewayDelete {
		return fmt.Errorf("unsupported node gateway operation %q", r.Operation)
	}
	if r.Operation == nodeGatewayDelete {
		return nil
	}
	if r.IP == "" || r.Gateway == "" {
		return errors.New("node gateway IP and gateway are required")
	}
	if r.MTU < 576 || r.MTU > 65535 {
		return fmt.Errorf("invalid node gateway MTU %d", r.MTU)
	}
	mac, err := net.ParseMAC(r.MAC)
	if err != nil || len(mac) != 6 {
		return fmt.Errorf("invalid node gateway MAC %q", r.MAC)
	}
	ips := util.SplitTrimmed(r.IP, ",")
	gws := util.SplitTrimmed(r.Gateway, ",")
	if len(ips) == 0 || len(ips) != len(gws) || len(ips) > 2 {
		return fmt.Errorf("node gateway IP and gateway families do not match: %q, %q", r.IP, r.Gateway)
	}
	for i := range ips {
		ip, _, err := net.ParseCIDR(ips[i])
		if err != nil || ip == nil {
			return fmt.Errorf("invalid node gateway IP %q", ips[i])
		}
		gw := net.ParseIP(gws[i])
		if gw == nil || (ip.To4() != nil) != (gw.To4() != nil) {
			return fmt.Errorf("node gateway IP and gateway family mismatch: %q, %q", ips[i], gws[i])
		}
	}
	if r.BFDLocalIP != "" && net.ParseIP(r.BFDLocalIP) == nil {
		return fmt.Errorf("invalid BFD local IP %q", r.BFDLocalIP)
	}
	for _, peer := range r.BFDPeers {
		if net.ParseIP(peer) == nil {
			return fmt.Errorf("invalid BFD peer %q", peer)
		}
	}
	return nil
}

type nodeGatewayRPC interface {
	Ensure(context.Context, NodeGatewayRequest) error
	Delete(context.Context, string) error
}

type nodeGatewayClient struct {
	socket string
}

func newNodeGatewayClient(socket string) nodeGatewayRPC {
	return &nodeGatewayClient{socket: socket}
}

func (c *nodeGatewayClient) Ensure(ctx context.Context, request NodeGatewayRequest) error {
	request.Operation = nodeGatewayEnsure
	return c.call(ctx, request)
}

func (c *nodeGatewayClient) Delete(ctx context.Context, nodeName string) error {
	return c.call(ctx, NodeGatewayRequest{Operation: nodeGatewayDelete, NodeName: nodeName})
}

func (c *nodeGatewayClient) call(ctx context.Context, request NodeGatewayRequest) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unixpacket", c.socket)
	if err != nil {
		return fmt.Errorf("connect node gateway helper: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err = conn.Write(payload); err != nil {
		return fmt.Errorf("send node gateway request: %w", err)
	}
	responsePayload := make([]byte, nodeGatewayMaxPacket)
	n, err := conn.Read(responsePayload)
	if err != nil {
		return fmt.Errorf("receive node gateway response: %w", err)
	}
	var response nodeGatewayResponse
	if err := json.Unmarshal(responsePayload[:n], &response); err != nil {
		return fmt.Errorf("decode node gateway response: %w", err)
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

// ServeNodeGatewayHelper serves the narrowly scoped privileged node gateway
// operations for the unprivileged cni-server.
func ServeNodeGatewayHelper(nodeName string) error {
	if nodeName == "" {
		return errors.New("node name is required")
	}
	if err := os.MkdirAll("/run/kube-ovn-node-gw", 0o750); err != nil {
		return err
	}
	if err := os.Remove(nodeGatewaySocket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: nodeGatewaySocket, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(nodeGatewaySocket, 0o660); err != nil {
		return err
	}

	var stateMu sync.Mutex
	return serveNodeGatewayConnections(listener, func(request NodeGatewayRequest) error {
		stateMu.Lock()
		defer stateMu.Unlock()
		if err := request.Validate(nodeName); err != nil {
			return err
		}
		switch request.Operation {
		case nodeGatewayEnsure:
			return ensureNodeGateway(request)
		case nodeGatewayDelete:
			return deleteNodeGateway()
		default:
			return fmt.Errorf("unsupported node gateway operation %q", request.Operation)
		}
	})
}

func serveNodeGatewayConnections(listener *net.UnixListener, handle func(NodeGatewayRequest) error) error {
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			response := nodeGatewayResponse{}
			if err := authorizeNodeGatewayPeer(conn); err != nil {
				response.Error = err.Error()
			} else {
				payload := make([]byte, nodeGatewayMaxPacket)
				n, _, flags, _, err := conn.ReadMsgUnix(payload, nil)
				if err != nil {
					response.Error = err.Error()
				} else if flags&unix.MSG_TRUNC != 0 {
					response.Error = "node gateway request too large"
				} else {
					var request NodeGatewayRequest
					if err := json.Unmarshal(payload[:n], &request); err != nil {
						response.Error = err.Error()
					} else if err := handle(request); err != nil {
						response.Error = err.Error()
					}
				}
			}
			responsePayload, err := json.Marshal(response)
			if err == nil {
				_, _ = conn.Write(responsePayload)
			}
		}()
	}
}

func authorizeNodeGatewayPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peer *unix.Ucred
	if controlErr := raw.Control(func(fd uintptr) {
		peer, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return err
	}
	if peer.Uid != 0 && peer.Uid != 65534 {
		return errors.New("unauthorized node gateway peer")
	}
	return nil
}

func ensureNodeGateway(request NodeGatewayRequest) error {
	return withHostMountNamespace(func() error {
		return ensureNodeGatewayInHostMountNamespace(request)
	})
}

// ensureNodeGatewayInHostMountNamespace must run in the node's mount
// namespace. The helper container's /var/run/netns mount is HostToContainer;
// bind mounts created in that private mount namespace disappear on restart.
func ensureNodeGatewayInHostMountNamespace(request NodeGatewayRequest) error {
	gwNS, err := ns.GetNS(util.NodeGwNsPath)
	if err != nil {
		var notExist ns.NSPathNotExistErr
		var notNS ns.NSPathNotNSErr
		if !errors.As(err, &notExist) && !errors.As(err, &notNS) {
			return fmt.Errorf("get node gateway namespace: %w", err)
		}
		if err := newNetNSInCurrentMountNamespace(util.NodeGwNsPath); err != nil {
			return fmt.Errorf("create node gateway namespace: %w", err)
		}
		gwNS, err = ns.GetNS(util.NodeGwNsPath)
		if err != nil {
			return fmt.Errorf("open node gateway namespace: %w", err)
		}
	}
	defer gwNS.Close()
	if err := nodeGatewayReady(request, gwNS); err == nil {
		return nil
	}
	ipAddr := request.IP
	mac, err := net.ParseMAC(request.MAC)
	if err != nil {
		return err
	}
	if err := configureNodeGwNic(request.NodeName, ipAddr, request.Gateway, mac, request.MTU, gwNS); err != nil {
		_ = removeNodeGwNic()
		_ = removeNodeGwNs()
		return fmt.Errorf("configure node gateway: %w", err)
	}
	if err := nodeGatewayReady(request, gwNS); err != nil {
		return fmt.Errorf("node gateway is not ready: %w", err)
	}
	return nil
}

func nodeGatewayReady(request NodeGatewayRequest, gwNS ns.NetNS) error {
	exists, err := ovs.PortExists(util.NodeGwNic)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("node gateway OVS port does not exist")
	}
	if err := withCurrentThreadNetNS(gwNS, func(_ ns.NetNS) error {
		if err := waitNetworkReady(util.NodeGwNic, request.IP, request.Gateway, true, true, 3, nil); err != nil {
			return err
		}
		return checkNodeGatewayBFD(request)
	}); err != nil {
		return err
	}
	return nil
}

// withCurrentThreadNetNS is the synchronous counterpart of ns.WithNetNSPath.
// The latter starts a goroutine, which can run on a thread in the helper's
// private mount namespace. Node gateway operations are already running on a
// locked thread in the host mount namespace, so preserve that thread while
// switching only its network namespace.
func withCurrentThreadNetNS(target ns.NetNS, fn func(ns.NetNS) error) error {
	original, err := ns.GetNS(getCurrentThreadNetNSPath())
	if err != nil {
		return err
	}
	defer original.Close()
	if err := target.Set(); err != nil {
		return err
	}
	defer func() { _ = original.Set() }()
	return fn(target)
}

func checkNodeGatewayBFD(request NodeGatewayRequest) error {
	if len(request.BFDPeers) == 0 {
		return nil
	}
	if output, err := exec.Command("bfdd-control", "status").CombinedOutput(); err != nil {
		return fmt.Errorf("get bfdd status: %w, %s", err, output)
	}
	for _, peer := range request.BFDPeers {
		cmd := exec.Command("bfdd-control", "status", "remote", peer, "local", request.BFDLocalIP)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("check BFD peer %s: %w, %s", peer, err, output)
		}
		if containsNoSession(string(output)) {
			if err := exec.Command("bfdd-control", "allow", peer).Run(); err != nil {
				return fmt.Errorf("allow BFD peer %s: %w", peer, err)
			}
		}
	}
	return nil
}

func containsNoSession(output string) bool {
	return strings.Contains(output, "No session")
}

func deleteNodeGateway() error {
	return withHostMountNamespace(func() error {
		return deleteNodeGatewayInHostMountNamespace()
	})
}

func deleteNodeGatewayInHostMountNamespace() error {
	var errs []error
	if err := removeNodeGwNic(); err != nil {
		errs = append(errs, err)
	}
	if _, err := os.Stat(util.NodeGwNsPath); err == nil {
		if err := removeNodeGwNs(); err != nil {
			errs = append(errs, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// withHostMountNamespace temporarily enters the host mount namespace on a
// locked OS thread. The helper retains its own mount namespace for the RPC
// socket, then switches only while touching the fixed host network namespace
// and OVS paths. hostPID makes /proc/1 the node's init process; SYS_ADMIN is
// scoped to this helper container and no privileged container is required.
func withHostMountNamespace(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tid := unix.Gettid()
	currentPath := fmt.Sprintf("/proc/self/task/%d/ns/mnt", tid)
	currentFD, err := unix.Open(currentPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open current mount namespace: %w", err)
	}
	defer unix.Close(currentFD)
	hostFD, err := unix.Open("/proc/1/ns/mnt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open host mount namespace: %w", err)
	}
	defer unix.Close(hostFD)
	if err := unix.Setns(hostFD, unix.CLONE_NEWNS); err != nil {
		return fmt.Errorf("enter host mount namespace: %w", err)
	}
	operationErr := fn()
	if err := unix.Setns(currentFD, unix.CLONE_NEWNS); err != nil {
		return errors.Join(operationErr, fmt.Errorf("restore helper mount namespace: %w", err))
	}
	return operationErr
}
