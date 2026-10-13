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
	nodeGatewayHostMount = "/run/kube-ovn-node-gw-host-mnt"
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
			authorizeErr := authorizeNodeGatewayPeer(conn)
			switch {
			case authorizeErr != nil:
				response.Error = authorizeErr.Error()
			default:
				payload := make([]byte, nodeGatewayMaxPacket)
				n, _, flags, _, err := conn.ReadMsgUnix(payload, nil)
				switch {
				case err != nil:
					response.Error = err.Error()
				case flags&unix.MSG_TRUNC != 0:
					response.Error = "node gateway request too large"
				default:
					var request NodeGatewayRequest
					if err := json.Unmarshal(payload[:n], &request); err != nil {
						response.Error = err.Error()
					}
					if response.Error == "" {
						if err := handle(request); err != nil {
							response.Error = err.Error()
						}
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
	var gwNS ns.NetNS
	if err := withHostMountNamespace(func() error {
		var err error
		gwNS, err = getOrCreateNodeGatewayNS(util.NodeGwNsPath, newNetNSInCurrentMountNamespace)
		return err
	}); err != nil {
		return err
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
		return errors.Join(fmt.Errorf("configure node gateway: %w", err), deleteNodeGateway())
	}
	if err := nodeGatewayReady(request, gwNS); err != nil {
		return fmt.Errorf("node gateway is not ready: %w", err)
	}
	return nil
}

// Recheck the namespace in the host mount namespace before creating it. The
// helper's HostToContainer view can lag behind mount propagation; an open FD
// also avoids depending on that view for subsequent network operations.
func getOrCreateNodeGatewayNS(path string, create func(string) error) (ns.NetNS, error) {
	gwNS, err := ns.GetNS(path)
	if err == nil {
		return gwNS, nil
	}
	_, notExist := errors.AsType[ns.NSPathNotExistErr](err)
	_, notNS := errors.AsType[ns.NSPathNotNSErr](err)
	if !notExist && !notNS {
		return nil, fmt.Errorf("get node gateway namespace: %w", err)
	}
	if err := create(path); err != nil {
		return nil, fmt.Errorf("create node gateway namespace: %w", err)
	}
	gwNS, err = ns.GetNS(path)
	if err != nil {
		return nil, fmt.Errorf("open node gateway namespace: %w", err)
	}
	return gwNS, nil
}

func nodeGatewayReady(request NodeGatewayRequest, gwNS ns.NetNS) error {
	exists, err := ovs.PortExists(util.NodeGwNic)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("node gateway OVS port does not exist")
	}
	if err := gwNS.Do(func(_ ns.NetNS) error {
		if err := waitNetworkReady(util.NodeGwNic, request.IP, request.Gateway, true, true, 3, nil); err != nil {
			return err
		}
		return checkNodeGatewayBFD(request)
	}); err != nil {
		return err
	}
	return nil
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
	// OVS tools and BFD must use the helper image's root and PATH. Only fixed
	// namespace-file operations enter the host mount namespace.
	portErr := removeNodeGwNic()
	namespaceErr := withHostMountNamespace(removeNodeGwNs)
	return errors.Join(portErr, namespaceErr)
}

// withHostMountNamespace runs fixed namespace-file operations on a disposable
// OS thread. Unsharing CLONE_FS is required before entering another mount
// namespace in a multithreaded Go process. The thread stays locked and exits
// without returning its changed mount namespace/root/cwd to the runtime.
func withHostMountNamespace(fn func() error) error {
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		// Use the kubelet-mounted namespace file: directly reading /proc/1/ns
		// would require ptrace access to the root-owned host init process.
		hostFD, err := unix.Open(nodeGatewayHostMount, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			result <- fmt.Errorf("open host mount namespace: %w", err)
			return
		}
		defer unix.Close(hostFD)
		if err := unix.Unshare(unix.CLONE_FS); err != nil {
			result <- fmt.Errorf("unshare filesystem state: %w", err)
			return
		}
		if err := unix.Setns(hostFD, unix.CLONE_NEWNS); err != nil {
			result <- fmt.Errorf("enter host mount namespace: %w", err)
			return
		}
		result <- fn()
	}()
	return <-result
}
