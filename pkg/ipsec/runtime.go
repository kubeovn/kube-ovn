package ipsec

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

const strongSwanConfig = `charon {
  plugins {
    kernel-netlink {
      set_proto_port_transport_sa = yes
      xfrm_ack_expires = 10
    }
    gcm { load = yes }
  }
  load_modular = yes
}
`

type runtimeManager struct {
	dir, ovsSocket string
	store          store
	priority       int
	mu             sync.Mutex
	enabled        atomic.Bool
	healthy        atomic.Bool
	applied        atomic.Bool
	expected       runtimeConfiguration
}

type publicIdentity struct {
	Certificate string `json:"certificate"`
	Trust       string `json:"trust"`
}

type runtimeConfiguration struct {
	publicIdentity
	NodeUID, Chassis, Generation string
}

func configurationFor(status Status) runtimeConfiguration {
	return runtimeConfiguration{
		Certificate: status.CertificateHash, Trust: status.TrustHash,
		NodeUID: status.NodeUID, Chassis: status.Chassis, Generation: status.Generation,
	}
}

func (r *runtimeManager) expectConfiguration(status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := configurationFor(status)
	if next != r.expected {
		r.applied.Store(false)
		r.expected = next
	}
}

func (r *runtimeManager) configurationApplied(status Status) (healthy, applied bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	healthy = r.healthy.Load()
	current := configurationFor(status)
	applied = healthy && r.applied.Load() && current == r.expected &&
		current.NodeUID != "" && current.Chassis != "" && current.Generation != "" &&
		current.Certificate != "" && current.Trust != ""
	return healthy, applied
}

func command(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 G702 -- callers select fixed programs; argv is never interpreted by a shell.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func (r *runtimeManager) prepare() error {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}
	for path, data := range map[string][]byte{
		"/etc/strongswan.d/ovs.conf": []byte(strongSwanConfig),
		"/etc/ipsec.conf":            []byte("config setup\n    uniqueids=yes\n"),
		"/etc/ipsec.secrets":         {},
	} {
		if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// checkLegacyMonitor queries the upstream monitor's POSIX pidfile lock. The
// file's PID text is not ownership evidence and must never authorize a kill.
func checkLegacyMonitor(ovsSocket string) error {
	path := filepath.Join(filepath.Dir(ovsSocket), "ovs-monitor-ipsec.pid")
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect legacy IPsec monitor lock: %w", err)
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("legacy IPsec monitor pidfile must be a regular file")
	}
	if err == nil {
		lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
		err = unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lock)
		if err == nil && lock.Type != unix.F_UNLCK {
			err = errors.New("a legacy IPsec monitor still owns its pidfile lock")
		}
	}
	return errors.Join(err, f.Close())
}

func checkIKEPorts() error {
	// Never stop an unrelated host IKE daemon to make room for Kube-OVN.
	for _, port := range []int{500, 4500} {
		for _, network := range []string{"udp4", "udp6"} {
			conn, err := net.ListenPacket(network, fmt.Sprintf(":%d", port))
			if err != nil {
				if errors.Is(err, syscall.EAFNOSUPPORT) {
					continue
				}
				return fmt.Errorf("IKE port %d (%s) is unavailable: %w", port, network, err)
			}
			if err := conn.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func (r *runtimeManager) startChild(name string, args ...string) (*child, error) {
	args = append([]string{"-n", strconv.Itoa(r.priority), name}, args...)
	cmd := exec.Command("nice", args...) // #nosec G204 -- fixed runtime programs and validated configuration.
	cmd.Env = append(os.Environ(), "OVS_RUNDIR="+r.dir)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &child{cmd: cmd, done: make(chan struct{})}
	go func() { c.err = cmd.Wait(); close(c.done) }()
	return c, nil
}

func (c *child) stop() {
	// Terminate the owned process group even if its leader was killed: charon
	// must not survive a crashed starter and occupy the IKE ports on retry.
	if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		klog.ErrorS(err, "Terminate IPsec process group")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-c.cmd.Process.Pid, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		klog.ErrorS(err, "Kill IPsec process group")
	}
	<-c.done
}

func (c *child) failure() error {
	if c.err == nil {
		return errors.New("runtime process exited unexpectedly with status 0")
	}
	return c.err
}

func (r *runtimeManager) runPair(ctx context.Context) error {
	if err := checkIKEPorts(); err != nil {
		return err
	}
	if err := r.prepare(); err != nil {
		return err
	}
	session, err := r.prepareConnectionSession()
	if err != nil {
		return err
	}
	kernel, err := netlink.NewHandle(unix.NETLINK_XFRM)
	if err != nil {
		return err
	}
	defer kernel.Close()
	if err := kernel.SetSocketTimeout(3 * time.Second); err != nil {
		return err
	}
	starter, err := r.startChild("/usr/sbin/ipsec", "start", "--nofork")
	if err != nil {
		return err
	}
	defer func() {
		starter.stop()
		if err := r.recordDrain(*session, kernel); err != nil {
			klog.ErrorS(err, "IPsec shutdown lacks a complete drain observation; protection retained")
		}
	}()
	// The monitor's update/reread commands require a running IKE daemon.
	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startupCancel()
	for {
		checkCtx, checkCancel := context.WithTimeout(startupCtx, time.Second)
		err := command(checkCtx, "/usr/sbin/ipsec", "status")
		checkCancel()
		if err == nil {
			break
		}
		select {
		case <-starter.done:
			return fmt.Errorf("IPsec starter exited during startup: %w", starter.failure())
		case <-startupCtx.Done():
			return startupCtx.Err()
		case <-time.After(time.Second):
		}
	}
	monitor, err := r.startChild("/usr/share/openvswitch/scripts/ovs-monitor-ipsec", "unix:"+r.ovsSocket,
		"--ike-daemon=strongswan", "--no-restart-ike-daemon", "--ovn-owned-only", "--pidfile="+filepath.Join(r.dir, "monitor.pid"),
		"--connection-prefix="+session.Prefix, "--connection-owner-node-uid="+session.NodeUID,
		"--connection-owner-boot-id="+session.BootID,
		"--connection-owner-lease="+session.Lease, "--connection-intent="+session.intentPath(r.store),
		"--connection-owner-mark="+strconv.FormatUint(uint64(session.Mark), 10),
		"--connection-owner-reqid="+strconv.FormatUint(uint64(session.Reqid), 10))
	if err != nil {
		return err
	}
	defer monitor.stop()
	defer r.healthy.Store(false)
	defer r.applied.Store(false)
	// Starting the Python process does not prove its OVSDB/event loop is
	// responding. Confirm both private control endpoints and keep checking
	// them so a hung process is recovered without waiting for Pod restart.
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := r.check(probeCtx)
		cancel()
		if err == nil {
			if err := r.confirmTrust(ctx); err != nil {
				return err
			}
			// A responsive process can still be using the previous OVSDB
			// generation. Wait for its safe public-content acknowledgement.
			r.confirmIdentity(ctx)
			if r.applied.Load() {
				if err := r.observeSAs(ctx, *session, kernel); err != nil && ctx.Err() == nil {
					// Missing ownership evidence must prevent later cleanup. It
					// does not justify stopping an otherwise protected runtime.
					klog.ErrorS(err, "IPsec SA ownership snapshot unavailable")
				}
			}
		} else if r.healthy.Load() || startupCtx.Err() != nil {
			return fmt.Errorf("IPsec runtime health check failed: %w", err)
		}
		select {
		case <-starter.done:
			return fmt.Errorf("IPsec starter exited: %w", starter.failure())
		case <-monitor.done:
			return fmt.Errorf("IPsec monitor exited: %w", monitor.failure())
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *runtimeManager) monitorSocket() (string, error) {
	pidBytes, err := os.ReadFile(filepath.Join(r.dir, "monitor.pid"))
	if err != nil {
		return "", err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil || pid <= 0 {
		return "", errors.New("invalid private IPsec monitor PID")
	}
	return filepath.Join(r.dir, fmt.Sprintf("ovs-monitor-ipsec.%d.ctl", pid)), nil
}

func (r *runtimeManager) check(ctx context.Context) error {
	if err := command(ctx, "/usr/sbin/ipsec", "status"); err != nil {
		return err
	}
	socket, err := r.monitorSocket()
	if err != nil {
		return err
	}
	// Never invoke tunnels/show or xfrm/state: they can expose SA keys.
	return command(ctx, "ovs-appctl", "-t", socket, "list-commands")
}

func (r *runtimeManager) confirmIdentity(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	confirmed := false
	defer func() { r.applied.Store(confirmed) }()
	socket, err := r.monitorSocket()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ovs-appctl", "-t", socket, "configuration/get").Output()
	if err != nil {
		return
	}
	var actual publicIdentity
	if json.Unmarshal(output, &actual) == nil && actual.Certificate != "" && actual == r.expected.publicIdentity {
		confirmed = true
	}
}

func (r *runtimeManager) run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		delay := time.Second
		if r.enabled.Load() {
			started := time.Now()
			if err := r.runPair(ctx); err != nil && ctx.Err() == nil {
				if time.Since(started) >= time.Minute {
					backoff = time.Second
				}
				delay = backoff
				backoff = min(30*time.Second, backoff*2)
				klog.ErrorS(err, "IPsec runtime stopped; retrying", "retryAfter", delay)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (r *runtimeManager) reloadTrust(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.healthy.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return command(ctx, "/usr/sbin/ipsec", "rereadcacerts")
}

func (r *runtimeManager) confirmTrust(ctx context.Context) error {
	// Serialize the transition with reloadTrust. Trust written while charon
	// is starting must be reread either here or by the reconciler after this
	// transition, so a concurrent restart cannot acknowledge stale trust.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.healthy.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := command(ctx, "/usr/sbin/ipsec", "rereadcacerts"); err != nil {
		return err
	}
	r.healthy.Store(true)
	return nil
}
