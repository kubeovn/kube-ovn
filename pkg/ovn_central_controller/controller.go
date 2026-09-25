// controller.go: orchestration of ovn-central startup + runtime loop.
//
// Lifecycle:
//
//	Run() -> parseConfig -> [preflight -> bringUp -> runtimeLoop] (retry on errRetry)
//	  preflight:   sweep reconvert temp files, wipe kicked/stale-stub DBs.
//	  bringUp:     start ovsdb-server, wait for cluster_member+leader. On
//	               failure, take the bootstrap lease and recoverCluster()
//	               picks wipe-rejoin / reconvert / bootstrap based on peer
//	               state.
//	  runtimeLoop: every TickInterval, watch leader health, label leaders,
//	               kick stale members, backup raft headers, publish status.
//	               If no-leader past NoLeaderTimeout, escalate to
//	               recoverCluster().
package ovn_central_controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// errRetry signals "this attempt failed transiently; re-enter preflight +
// bringUp after backoff." Returned for cases like a peer holding the
// bootstrap lease or peers not yet ready.
var errRetry = errors.New("retry preflight/bringUp")

// heartbeatFile name (under cfg.OVNRunDir) is touched by a dedicated
// goroutine. The liveness probe checks file mtime to detect a hung Go
// runtime (deadlock, frozen goroutines) without conflating with
// orchestration-state false negatives like a long reconvert.
const (
	heartbeatFile   = "ovn-central-controller.alive"
	heartbeatPeriod = 5 * time.Second
)

// lifecycleState tracks in-container facts that can't be derived from
// disk alone:
//   - dbInherited: did the on-disk DB exist at container start? Cleared
//     by wipeDB and by initStub fresh-create. Differentiates statusStale
//     (data carried over) from statusJoining (fresh stub).
//   - wasActive / activeSince: did we observe statusActive consecutively
//     for sustainedActiveTicks? wasActive is sticky once true so a brief
//     leader-unknown blip leaves us at leader_lost, not stale.
//   - hasCommittedData: latched once we observed cluster_member + known
//     leader + (role=leader OR logHigh>logLow) in BOTH NB and SB,
//     i.e. we have replicated state even if wasActive hasn't latched
//     yet. Distinguishes statusJoined (just-joined stub with full data)
//     from statusJoining (empty stub). Sticky once true.
type lifecycleState struct {
	dbInherited      bool
	wasActive        bool
	hasCommittedData bool
	activeSince      time.Time

	// lastHealthyOvsdbNanos / inRecovery are read by the heartbeat goroutine
	// concurrently with the main goroutine that writes them. atomic.
	//   - lastHealthyOvsdbNanos: unix-nanos of the last time
	//     updateAndComputeStatus returned statusActive. Zero == never seen
	//     active (heartbeat treats startup time as the baseline).
	//   - inRecovery: true while a recoverCluster call is in flight. The
	//     heartbeat goroutine uses a larger staleness threshold during
	//     recovery (BootstrapLeaseDuration) to avoid killing the pod
	//     during legitimate multi-minute reconvert.
	lastHealthyOvsdbNanos atomic.Int64
	inRecovery            atomic.Bool
}

// sustainedActiveTicks: brief 5-10s "active" glimpses (ovsdb-server
// reporting cluster_member while AddServer is mid-flight against a
// quorum-less leader) shouldn't latch wasActive=true; otherwise a
// stub-only pod could later claim leader_lost and hijack a reconvert.
const sustainedActiveTicks = 3

// updateAndComputeStatus derives our current status from on-disk state,
// ovsdb runtime state, and lifecycle flags. Has side effects on ls:
// advances activeSince and latches wasActive once we've been sustained-
// active. Called from preflight, bringUp, runtimeTick, and recoverCluster.
func updateAndComputeStatus(ctx context.Context, cfg *Config, ls *lifecycleState) status {
	if !readDBState(ctx, nbDB(cfg)).exists && !readDBState(ctx, sbDB(cfg)).exists {
		return ""
	}
	nbCS, errNB := readClusterStatus(ctx, nbDB(cfg))
	sbCS, errSB := readClusterStatus(ctx, sbDB(cfg))
	// Latch hasCommittedData on the first tick where BOTH DBs report
	// "committed-active" state. This is what isCommittedActive checks:
	// cluster_member + known leader + (role=leader OR logHigh>logLow).
	// Crucial because wasActive needs sustainedActiveTicks (~15s) to
	// latch, but a fresh-joined follower has full replicated data
	// long before that. Without this flag a peer racing to recover
	// might classify us as "no data" and bootstrap fresh on top of us.
	if !ls.hasCommittedData &&
		errNB == nil && errSB == nil &&
		isCommittedActive(nbCS) && isCommittedActive(sbCS) {
		ls.hasCommittedData = true
	}
	if errNB == nil && errSB == nil && isCommittedActive(nbCS) && isCommittedActive(sbCS) {
		if ls.activeSince.IsZero() {
			ls.activeSince = time.Now()
		}
		if !ls.wasActive &&
			time.Since(ls.activeSince) >= time.Duration(sustainedActiveTicks)*cfg.TickInterval {
			ls.wasActive = true
		}
		ls.lastHealthyOvsdbNanos.Store(time.Now().UnixNano())
		return statusActive
	}
	ls.activeSince = time.Time{}
	switch {
	case ls.wasActive:
		return statusLeaderLost
	case ls.hasCommittedData:
		return statusJoined
	case ls.dbInherited:
		return statusStale
	default:
		return statusJoining
	}
}

// isCommittedActive: ovsdb reports cluster_member + known leader, AND
// (we're the leader OR our raft log has entries beyond the initial
// stub). The log check filters out the optimistic "cluster_member"
// status that ovsdb-server reports while AddServer is still mid-flight
// against a quorum-less leader -- without it, a stub-only pod could
// briefly look healthy and trick peers into a wipe-rejoin.
func isCommittedActive(cs clusterStatus) bool {
	if cs.status != "cluster member" || !isKnownLeader(cs.leader) {
		return false
	}
	return cs.role == "leader" || cs.logHigh > cs.logLow
}

// Config carries env-var-driven knobs. Fields default-init to zero value;
// parseConfig fills them.
type Config struct {
	PodName, PodNamespace, PodIP, NodeName string
	DBClusterAddr                          string   // raft listen addr (= PodIP)
	DBAddr                                 string   // single db addr passed to --db-{nb,sb}-addr
	DBAddresses                            []string // listen addrs for db client port (dual-stack capable)
	EnableSSL                              bool
	NBPort, SBPort                         int
	NBClusterPort, SBClusterPort           int
	OVNNorthdNThreads                      int
	OVNNorthdProbeInterval                 int    // ms
	ProbeInterval                          int    // ms (NB/SB inactivity_probe)
	OVNDir                                 string // /etc/ovn
	OVNRunDir                              string // /var/run/ovn
	EnableCompact                          bool   // periodic ovsdb-server/compact on leader
	VersionCompatibility                   string // NB_Global options:version_compatibility
	DebugWrapper                           string // --ovn-northd-wrapper / --ovsdb-{nb,sb}-wrapper, e.g. valgrind
	TickInterval                           time.Duration
	StaggerTimeout                         time.Duration // wait for cluster member+leader
	NoLeaderTimeout                        time.Duration // watchdog threshold
	DeadMemberTimeout                      time.Duration // kicker threshold
	BackupInterval                         time.Duration
	CompactInterval                        time.Duration
	StaleStubTimeout                       time.Duration // preflight wipes notJoined stubs older than this
	SelfReadyTimeout                       time.Duration // deadline for our own Pod.Ready post-recovery
	OvsdbCrashTimeout                      time.Duration // socket-missing tolerance (fast fail in runtimeTick)
	OvsdbStuckTimeout                      time.Duration // "no statusActive" tolerance outside recovery; doubles as the BootstrapLeaseDuration bound when inside recovery (heartbeat-based, catches hangs inside recoverCluster)
	BootstrapLeaseDuration                 time.Duration // raft lease tenancy under recoverCluster
	BootstrapRenewDeadline                 time.Duration // raft lease renewal deadline
	ReconvertTimeout                       time.Duration // exec timeout for cluster-to-standalone
}

// SSLOptions returns the -p/-c/-C arg list for ovn-nbctl / ovn-sbctl /
// ovsdb-client invocations when SSL is enabled, empty otherwise.
func (c *Config) SSLOptions() []string {
	if !c.EnableSSL {
		return nil
	}
	return []string{
		"-p", "/var/run/tls/key",
		"-c", "/var/run/tls/cert",
		"-C", "/var/run/tls/cacert",
	}
}

func parseConfig() (*Config, error) {
	c := &Config{
		PodName:                getenv("POD_NAME", ""),
		PodNamespace:           getenv("POD_NAMESPACE", "kube-system"),
		PodIP:                  getenv("POD_IP", ""),
		NodeName:               getenv("NODE_NAME", ""),
		EnableSSL:              getenv("ENABLE_SSL", "false") == "true",
		NBPort:                 atoiDefault(getenv("NB_PORT", ""), 6641),
		SBPort:                 atoiDefault(getenv("SB_PORT", ""), 6642),
		NBClusterPort:          atoiDefault(getenv("NB_CLUSTER_PORT", ""), 6643),
		SBClusterPort:          atoiDefault(getenv("SB_CLUSTER_PORT", ""), 6644),
		OVNNorthdNThreads:      atoiDefault(getenv("OVN_NORTHD_N_THREADS", ""), 1),
		OVNNorthdProbeInterval: atoiDefault(getenv("OVN_NORTHD_PROBE_INTERVAL", ""), 5000),
		ProbeInterval:          atoiDefault(getenv("PROBE_INTERVAL", ""), 180000),
		OVNDir:                 getenv("OVN_DIR", "/etc/ovn"),
		OVNRunDir:              getenv("OVN_RUN_DIR", "/var/run/ovn"),
		EnableCompact:          getenv("ENABLE_COMPACT", "false") == "true",
		VersionCompatibility:   getenv("OVN_VERSION_COMPATIBILITY", ""),
		DebugWrapper:           getenv("DEBUG_WRAPPER", ""),
		TickInterval:           5 * time.Second,
		NoLeaderTimeout:        time.Duration(atoiDefault(getenv("NO_LEADER_TIMEOUT", ""), 30)) * time.Second,
		DeadMemberTimeout:      time.Duration(atoiDefault(getenv("DEAD_MEMBER_TIMEOUT", ""), 120)) * time.Second,
		BackupInterval:         time.Duration(atoiDefault(getenv("BACKUP_INTERVAL", ""), 60)) * time.Second,
		CompactInterval:        time.Duration(atoiDefault(getenv("COMPACT_INTERVAL", ""), 300)) * time.Second,
		StaleStubTimeout:       time.Duration(atoiDefault(getenv("STALE_STUB_TIMEOUT", ""), 120)) * time.Second,
		SelfReadyTimeout:       30 * time.Second,
		OvsdbCrashTimeout:      time.Duration(atoiDefault(getenv("OVSDB_CRASH_TIMEOUT", ""), 30)) * time.Second,
		OvsdbStuckTimeout:      time.Duration(atoiDefault(getenv("OVSDB_STUCK_TIMEOUT", ""), 180)) * time.Second,
		BootstrapLeaseDuration: time.Duration(atoiDefault(getenv("BOOTSTRAP_LEASE_DURATION", ""), 600)) * time.Second,
		BootstrapRenewDeadline: time.Duration(atoiDefault(getenv("BOOTSTRAP_RENEW_DEADLINE", ""), 300)) * time.Second,
		ReconvertTimeout:       time.Duration(atoiDefault(getenv("RECONVERT_TIMEOUT", ""), 300)) * time.Second,
	}
	if c.PodName == "" || c.PodIP == "" || c.NodeName == "" {
		return nil, errors.New("POD_NAME, POD_IP and NODE_NAME env vars are required")
	}
	for _, p := range []struct {
		name string
		val  int
	}{
		{"NB_PORT", c.NBPort},
		{"SB_PORT", c.SBPort},
		{"NB_CLUSTER_PORT", c.NBClusterPort},
		{"SB_CLUSTER_PORT", c.SBClusterPort},
	} {
		if p.val < 1 || p.val > 65535 {
			return nil, fmt.Errorf("%s=%d out of range (1..65535)", p.name, p.val)
		}
	}
	c.DBClusterAddr = c.PodIP

	// ENABLE_BIND_LOCAL_IP=true makes ovsdb-server bind only this pod's
	// addresses (instead of ::). For dual-stack POD_IPS may be multiple
	// comma-separated values; --db-{nb,sb}-addr takes one (we use the
	// primary PodIP), Local_Config/listen takes them all.
	if getenv("ENABLE_BIND_LOCAL_IP", "false") == "true" {
		c.DBAddr = c.PodIP
		c.DBAddresses = splitCSV(getenv("POD_IPS", c.PodIP))
		if len(c.DBAddresses) == 0 {
			c.DBAddresses = []string{c.PodIP}
		}
	} else {
		c.DBAddr = "::"
		c.DBAddresses = []string{"::"}
	}
	c.StaggerTimeout = time.Duration(atoiDefault(getenv("DYNAMIC_JOIN_TIMEOUT", ""), 30)) * time.Second
	return c, nil
}

// Run is the binary entry point. Returns nil on graceful shutdown
// (SIGTERM/SIGINT), error on fatal failure; otherwise blocks forever in
// the runtime loop. Retries preflight + bringUp after exponential backoff
// when bringUp signals errRetry (someone else is bootstrapping, peers not
// ready yet).
func Run() error {
	cfg, err := parseConfig()
	if err != nil {
		return err
	}
	kc, err := newKubeClient()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	ls := &lifecycleState{}
	// Seed lastHealthyOvsdbNanos with startup time so the heartbeat
	// goroutine has a non-zero baseline. If we never reach statusActive,
	// staleness measured from startup eventually crosses the threshold.
	ls.lastHealthyOvsdbNanos.Store(time.Now().UnixNano())

	startHeartbeat(ctx, cfg, ls)
	// Snapshot whether DB files were present at container startup. The
	// dbInherited flag is cleared by wipeDB and by initStub on fresh
	// create, so it stays true only while the on-disk DB carries over
	// from a prior container's life.
	ls.dbInherited = readDBState(ctx, nbDB(cfg)).exists || readDBState(ctx, sbDB(cfg)).exists

	// Publish initial status so peers see a fresh, accurate label
	// even if we crash before reaching runtimeLoop. Empty cid is fine
	// at this point; refreshStatus re-reads disk every call.
	if err := refreshStatus(ctx, cfg, kc, updateAndComputeStatus(ctx, cfg, ls)); err != nil {
		klog.Warningf("initial label publish: %v", err)
	}

	const (
		baseDelay = 5 * time.Second
		maxDelay  = 60 * time.Second
	)
	delay := baseDelay
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			klog.Infof("shutdown requested before attempt %d: %v", attempt, err)
			return nil
		}
		if err := preflight(ctx, cfg, kc, ls); err != nil {
			return fmt.Errorf("preflight (attempt %d): %w", attempt, err)
		}
		err := bringUp(ctx, cfg, kc, ls)
		if err == nil {
			err = runtimeLoop(ctx, cfg, kc, ls)
			if err == nil || errors.Is(err, context.Canceled) {
				return nil // graceful shutdown
			}
		}
		if !errors.Is(err, errRetry) {
			return fmt.Errorf("attempt %d: %w", attempt, err)
		}
		klog.Infof("attempt %d: %v; sleeping %s before retry", attempt, err, delay)
		stopNBSBOvsdb()
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		if delay *= 2; delay > maxDelay {
			delay = maxDelay
		}
	}
}

// preflight inspects each DB and wipes ones we can't recover from in-place.
// Also sweeps leftover temp files from a crashed reconvert. Then publishes
// our (post-wipe) state into pod labels.
func preflight(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState) error {
	for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
		// Sweep reconvert temp files. Atomic-rename design means the
		// real db file is always either the pre-reconvert original or
		// the post-reconvert new one -- never missing -- so these
		// leftovers are always safe to drop.
		_ = os.Remove(d.dbFile + ".sa-tmp")
		_ = os.Remove(d.dbFile + ".new")

		st := readDBState(ctx, d)
		switch {
		case !st.exists:
			// nothing to validate
		case st.kicked:
			klog.Warningf("%s: kicked from cluster, wiping (%s)", d.short, d.dbFile)
			if err := wipeDB(d); err != nil {
				return err
			}
			ls.dbInherited = false
			ls.hasCommittedData = false
		case st.notJoined && dbAge(d) >= cfg.StaleStubTimeout:
			klog.Warningf("%s: stale join-stub (>%s old), wiping", d.short, cfg.StaleStubTimeout)
			if err := wipeDB(d); err != nil {
				return err
			}
			ls.dbInherited = false
			ls.hasCommittedData = false
		}
	}
	return refreshStatus(ctx, cfg, kc, updateAndComputeStatus(ctx, cfg, ls))
}

// bringUp tries to make this pod a healthy cluster member. Strategy:
//
//  1. Compose the peer list from kubectl (master-labeled IPs minus self).
//  2. For each DB without a file on disk, create a stub: rejoin-cluster
//     (preserve SID, no AddServer needed) if hdr exists, else join-cluster,
//     or create-cluster when no remotes (single-master deployment).
//  3. Recreate Local_Config DB so ovsdb-server listens on the right ports.
//  4. Start ovsdb-server; wait staggerTimeout for both DBs to reach
//     `Status: cluster member` with a known leader.
//  5. On success: start northd, refresh labels, return.
//  6. On failure: take the bootstrap lease and either reconvert (we are
//     the sole survivor with committed data) or create-cluster fresh (no
//     peer has data). If we shouldn't be the one to act, exit and let
//     kubelet retry.
func bringUp(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState) error {
	peers, err := pickPeerIPs(ctx, kc, cfg.PodNamespace, cfg.PodIP)
	if err != nil {
		return fmt.Errorf("pickPeerIPs: %w", err)
	}
	klog.Infof("peer IPs from kubectl: %v", peers)

	bootstrap := false
	for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
		// Local_Config carries listen addrs (db client port). For
		// ENABLE_BIND_LOCAL_IP=true with multiple POD_IPS we listen on
		// each (dual-stack); otherwise just on ::.
		listenAddrs := make([]string, 0, len(cfg.DBAddresses))
		for _, a := range cfg.DBAddresses {
			listenAddrs = append(listenAddrs, listenAddr(a, d.port, cfg.EnableSSL))
		}
		if err := writeLocalConfigDB(d, listenAddrs); err != nil {
			return fmt.Errorf("writeLocalConfigDB %s: %w", d.short, err)
		}
		if _, errStat := os.Stat(d.dbFile); errStat == nil {
			continue
		}
		remoteAddrs := make([]string, 0, len(peers))
		for _, p := range peers {
			remoteAddrs = append(remoteAddrs, connAddr(p, d.clusterPort, cfg.EnableSSL))
		}
		boot, err := initStub(d, connAddr(cfg.DBClusterAddr, d.clusterPort, cfg.EnableSSL), remoteAddrs)
		if err != nil {
			return fmt.Errorf("initStub %s: %w", d.short, err)
		}
		// Fresh stub created in this container -- not "inherited" data,
		// and no committed entries until raft replicates them in.
		ls.dbInherited = false
		ls.hasCommittedData = false
		if boot {
			bootstrap = true
		}
	}

	if err := startNBSBOvsdb(cfg, peers); err != nil {
		return err
	}
	postOvsdbStart(ctx, cfg)
	if err := refreshStatus(ctx, cfg, kc, updateAndComputeStatus(ctx, cfg, ls)); err != nil {
		klog.Warningf("label refresh after init: %v", err)
	}

	if err := waitForLeader(ctx, cfg, cfg.StaggerTimeout); err == nil {
		klog.Infof("cluster ready, starting northd (bootstrap=%v)", bootstrap)
		if err := refreshStatus(ctx, cfg, kc, updateAndComputeStatus(ctx, cfg, ls)); err != nil {
			klog.Warningf("label refresh post-ready: %v", err)
		}
		return startNorthd(cfg, bootstrap, peers)
	}

	klog.Warningf("waitForLeader timed out, entering recovery decision")
	stopNBSBOvsdb()
	return recoverCluster(ctx, cfg, kc, ls, peers)
}

// recoverCluster acquires the bootstrap lease, then routes through the
// tier hierarchy in recoverUnderLease. The lease is held through the
// entire destructive op + restart-ovsdb + waitForLeader, so the next
// pod to take the lease only ever sees a stable, published cluster
// state.
func recoverCluster(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState, peers []string) error {
	klog.Infof("acquiring bootstrap lease for recovery decision")
	// Flag the heartbeat watchdog so it switches to the larger
	// BootstrapLeaseDuration staleness threshold. Deferred Store(false)
	// ensures the flag clears even if recoverUnderLease panics.
	ls.inRecovery.Store(true)
	defer ls.inRecovery.Store(false)
	return withBootstrapLease(ctx, kc, cfg,
		func(c context.Context) error { return recoverUnderLease(c, cfg, kc, ls, peers) })
}

// recoverUnderLease classifies self + peers and picks a path:
//
//	tier:  active+ready > leader_lost > stale > joined > joining > (no data)
//
// (1) peer active+ready                  → wipe + rejoin
// (2) self leader_lost / stale / joined  → reconvert (we have committed data)
// (3) peer leader_lost / stale / joined  → defer (they outrank us)
// (4) nobody has data                    → bootstrap fresh
//
// statusJoined is the "just-finished-joining" intermediate: a follower
// has received committed log entries but wasActive hasn't latched yet
// (needs sustainedActiveTicks). Treating it as a data-holder avoids
// new pods bootstrapping fresh on top of a peer that just synced
// (the i06 data-loss bug).
func recoverUnderLease(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState, peers []string) error {
	allPods, err := listPeers(ctx, kc, cfg.PodNamespace)
	if err != nil {
		return err
	}
	others := otherPeers(allPods, cfg.PodIP)
	mySt := updateAndComputeStatus(ctx, cfg, ls)

	if anyPeerActiveAndReady(others) {
		klog.Warningf("recover: peer active+ready; wiping local DB for fresh join")
		for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
			if err := wipeDB(d); err != nil {
				return fmt.Errorf("wipe %s: %w", d.short, err)
			}
		}
		ls.dbInherited = false
		ls.hasCommittedData = false
		return errRetry
	}
	// Data-holder statuses (in our preferred recovery-priority order).
	dataHolders := []status{statusLeaderLost, statusStale, statusJoined}
	for _, s := range dataHolders {
		if mySt == s {
			klog.Infof("recover: self %s; reconverting", s)
			return executeUnderLease(ctx, cfg, kc, ls, "reconvert", peers, reconvertFn(cfg))
		}
		if anyPeerStatus(others, s) {
			klog.Infof("recover: peer %s; deferring", s)
			return errRetry
		}
	}
	klog.Infof("recover: no peer has cluster data; bootstrapping fresh (mySt=%q)", mySt)
	return executeUnderLease(ctx, cfg, kc, ls, "bootstrap", peers, func(_ context.Context) error {
		for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
			if err := wipeDB(d); err != nil {
				return fmt.Errorf("wipe %s: %w", d.short, err)
			}
			if _, err := initStub(d,
				connAddr(cfg.DBClusterAddr, d.clusterPort, cfg.EnableSSL), nil); err != nil {
				return err
			}
		}
		ls.dbInherited = false
		ls.hasCommittedData = false
		return nil
	})
}

// reconvertFn runs cluster-to-standalone + create-cluster on both NB
// and SB. Preserves data; cid is regenerated.
func reconvertFn(cfg *Config) func(context.Context) error {
	return func(_ context.Context) error {
		for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
			if err := reconvert(d, connAddr(cfg.DBClusterAddr, d.clusterPort, cfg.EnableSSL), nil, cfg.ReconvertTimeout); err != nil {
				return err
			}
		}
		return nil
	}
}

// executeUnderLease runs fn (the destructive op), restarts ovsdb, waits
// for leader, starts northd, and blocks on our own Pod.Ready=true
// before returning -- all while still holding the lease. Holding until
// Pod.Ready closes a race window: without it, we'd publish status=active
// and release the lease before the kubelet readiness probe fires, and
// the next pod to acquire would see status=active but ready=false (so
// anyPeerActiveAndReady=false) and proceed to its own bootstrap →
// split-brain. ctx.Err() is checked once before serving so a lease
// lost mid-op aborts before any peer-visible side effect.
func executeUnderLease(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState,
	op string, peers []string, fn func(context.Context) error,
) error {
	// Diagnostic: peers don't consult statusRecovering for decisions
	// (only active+ready), but it's useful in `kubectl describe`.
	if err := refreshStatus(ctx, cfg, kc, statusRecovering); err != nil {
		klog.Warningf("label publish recovering: %v", err)
	}
	if err := fn(ctx); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("lease lost after %s, aborting: %w", op, err)
	}
	if err := startNBSBOvsdb(cfg, peers); err != nil {
		return err
	}
	postOvsdbStart(ctx, cfg)
	if err := waitForLeader(ctx, cfg, cfg.StaggerTimeout); err != nil {
		return fmt.Errorf("waitForLeader after %s: %w", op, err)
	}
	if err := refreshStatus(ctx, cfg, kc, updateAndComputeStatus(ctx, cfg, ls)); err != nil {
		klog.Warningf("label refresh post-recovery: %v", err)
	}
	if err := startNorthd(cfg, op == "bootstrap", peers); err != nil {
		return err
	}
	if err := waitForSelfReady(ctx, cfg, kc, cfg.SelfReadyTimeout); err != nil {
		klog.Warningf("waitForSelfReady after %s: %v (releasing lease anyway)", op, err)
	}
	return nil
}

// waitForSelfReady blocks until our Pod's PodReady condition is True
// (kubelet's readiness probe has confirmed our cluster is serving) or
// the deadline expires. Used to delay lease release so peers consistently
// observe our active+ready state before they try to recover themselves.
func waitForSelfReady(ctx context.Context, cfg *Config, kc kubernetes.Interface, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		getCtx, cancel := context.WithTimeout(ctx, apiCallTimeout)
		pod, err := kc.CoreV1().Pods(cfg.PodNamespace).Get(getCtx, cfg.PodName, metav1.GetOptions{})
		cancel()
		if err == nil {
			for _, c := range pod.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("self Pod.Ready did not become true within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// runtimeLoop ticks until ctx is cancelled or runtimeTick returns
// errRetry (no-leader watchdog escalation). errRetry bubbles to Run()
// which restarts the preflight+bringUp cycle; lifecycle flags persist
// (wasActive stays true), so we keep statusLeaderLost through the
// recovery rather than dropping back to stale.
func runtimeLoop(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState) error {
	state := newRuntimeState()
	tk := time.NewTicker(cfg.TickInterval)
	defer tk.Stop()
	klog.Infof("entering runtime loop (tick=%s)", cfg.TickInterval)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tk.C:
			if err := runtimeTick(ctx, cfg, kc, ls, state); err != nil {
				return err
			}
		}
	}
}

// runtimeState holds per-DB watchdog/kicker memory across ticks. Single-
// goroutine access; no synchronization needed.
type runtimeState struct {
	noLeaderSince    map[string]time.Time // db -> first time we observed Leader=unknown
	socketGoneSince  map[string]time.Time // db -> first time ovs-appctl socket was missing
	unhealthySince   map[string]time.Time // <db>:<sid> -> first time observed unhealthy
	lastBackup       time.Time
	lastCompact      time.Time
	lastStatus       status // last published lifecycle status, for change-detection
	patchedClusterID string // last cluster ID successfully written to the node label
}

func newRuntimeState() *runtimeState {
	return &runtimeState{
		noLeaderSince:   map[string]time.Time{},
		socketGoneSince: map[string]time.Time{},
		unhealthySince:  map[string]time.Time{},
	}
}

// isOvsdbSocketMissing reports whether err from cluster/status looks like
// "ovsdb-server is not there" (socket gone or refusing connections) vs a
// slow/garbled response. Only the former triggers the crash watchdog.
func isOvsdbSocketMissing(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "No such file or directory") ||
		strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "Connection refused") ||
		strings.Contains(s, "connection refused")
}

func runtimeTick(ctx context.Context, cfg *Config, kc kubernetes.Interface, ls *lifecycleState, st *runtimeState) error {
	nbLeader, sbLeader := false, false
	noLeaderTimedOut := false
	for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
		cs, err := readClusterStatus(ctx, d)
		if err != nil {
			// Reached runtimeTick only after bringUp + waitForLeader
			// succeeded, so a missing socket here means ovsdb-server
			// crashed mid-run. Tolerate transient errors for
			// OvsdbCrashTimeout (apiserver hiccup, slow host), then
			// fail fatal so kubelet restarts the pod -- a fresh
			// container goes through preflight again, which is the
			// right path for a crashed ovsdb (in-place restart can
			// hit the same corrupt-DB crash).
			if isOvsdbSocketMissing(err) {
				if t, ok := st.socketGoneSince[d.short]; !ok {
					st.socketGoneSince[d.short] = time.Now()
					klog.Warningf("%s: ovsdb-server socket gone; tolerating up to %s before pod restart", d.short, cfg.OvsdbCrashTimeout)
				} else if time.Since(t) >= cfg.OvsdbCrashTimeout {
					return fmt.Errorf("ovsdb-server %s unreachable for %s, exiting for pod restart", d.short, time.Since(t).Round(time.Second))
				}
			}
			klog.Warningf("cluster/status %s: %v", d.short, err)
			continue
		}
		delete(st.socketGoneSince, d.short)
		if d.short == "nb" && cs.clusterID != "" && cs.clusterID != st.patchedClusterID {
			if err := patchNodeClusterID(ctx, kc, cfg.NodeName, cs.clusterID); err != nil {
				klog.Warningf("patchNodeClusterID: %v", err)
			} else {
				st.patchedClusterID = cs.clusterID
			}
		}
		// Watchdog: prolonged no-leader is the sole quorum-loss signal we
		// can read locally (raft only elects/keeps a leader with majority).
		if isKnownLeader(cs.leader) {
			delete(st.noLeaderSince, d.short)
		} else {
			if t, ok := st.noLeaderSince[d.short]; !ok {
				st.noLeaderSince[d.short] = time.Now()
				klog.Warningf("%s reports no leader (will check %s for recovery)", d.short, cfg.NoLeaderTimeout)
			} else if time.Since(t) >= cfg.NoLeaderTimeout {
				noLeaderTimedOut = true
			}
		}
		if cs.role == "leader" {
			kickStaleMembers(ctx, d, cs, cfg.DeadMemberTimeout, st)
			if d.short == "nb" {
				nbLeader = true
			} else {
				sbLeader = true
			}
		}
	}

	// Pod labels signal which pod is the NB / SB / northd leader so the
	// kube-ovn-controller's Service selectors route to the right one.
	// The northd-leader label is keyed on LOCAL ovn-northd state because
	// only the pod whose own northd holds the SB lock should be in the
	// Service endpoint.
	if err := patchLeaderLabels(ctx, kc, cfg.PodNamespace, cfg.PodName,
		nbLeader, sbLeader, localNorthdActive(ctx)); err != nil {
		klog.Warningf("patchLeaderLabels: %v", err)
	}

	// SB leader steals the ovn-northd lock only if NO pod anywhere has
	// an active northd (nobody is in the Service endpoint set). The
	// previous holder probably died without releasing, so blasting the
	// lock lets a standby take over. We must NOT steal when an active
	// northd already exists -- doing so just kicks it out and creates
	// a churn loop.
	if sbLeader && !anyNorthdActive(ctx, kc, cfg.PodNamespace) {
		klog.Warningf("no active northd anywhere, stealing lock")
		if err := stealLock(ctx, cfg); err != nil {
			klog.Errorf("stealLock: %v", err)
		}
	}

	if time.Since(st.lastBackup) >= cfg.BackupInterval {
		for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
			if err := backupHeader(ctx, d); err != nil {
				klog.Warningf("backupHeader %s: %v", d.short, err)
			}
		}
		st.lastBackup = time.Now()
	}
	// Publish status only on transitions (e.g. active -> leader_lost).
	// Hung pods are caught via Pod.Ready (readiness probe), not via
	// stale labels.
	cur := updateAndComputeStatus(ctx, cfg, ls)
	if cur != st.lastStatus {
		if err := refreshStatus(ctx, cfg, kc, cur); err != nil {
			klog.Warningf("refreshStatus: %v", err)
		}
		st.lastStatus = cur
	}
	if cfg.EnableCompact && time.Since(st.lastCompact) >= cfg.CompactInterval {
		// Run on whoever's the current leader of each DB; compact is a
		// raft-replicated snapshot operation that other members follow.
		if nbLeader {
			if err := compactDB(ctx, nbDB(cfg)); err != nil {
				klog.Warningf("compactDB nb: %v", err)
			}
		}
		if sbLeader {
			if err := compactDB(ctx, sbDB(cfg)); err != nil {
				klog.Warningf("compactDB sb: %v", err)
			}
		}
		st.lastCompact = time.Now()
	}

	// No-leader watchdog. recoverCluster() always under the lease
	// decides what to do; we never short-circuit on "peer claims
	// active" because that would let us sit forever with stale local
	// membership the cluster has moved past.
	if noLeaderTimedOut {
		klog.Warningf("no leader past timeout; running recovery")
		stopNBSBOvsdb()
		peers, err := pickPeerIPs(ctx, kc, cfg.PodNamespace, cfg.PodIP)
		if err != nil {
			klog.Warningf("pickPeerIPs in no-leader path: %v", err)
			return errRetry
		}
		if err := recoverCluster(ctx, cfg, kc, ls, peers); err != nil {
			return err // errRetry → Run() retries; other errors fatal
		}
		// Reconvert/bootstrap succeeded: ovsdb is back, we're leader.
		delete(st.noLeaderSince, "nb")
		delete(st.noLeaderSince, "sb")
	}
	return nil
}

// kickStaleMembers walks the cluster/status server list. For peers other
// than self that have been silent past dead-timeout, sends cluster/kick.
// "No last_msg" is treated as needing a grace period (we may have just
// restarted; give peers a chance to phone home before we kick them).
func kickStaleMembers(ctx context.Context, d dbInfo, cs clusterStatus, dead time.Duration, st *runtimeState) {
	now := time.Now()
	seen := map[string]struct{}{}
	for _, s := range cs.servers {
		if s.self {
			continue
		}
		key := d.short + ":" + s.sid
		seen[s.sid] = struct{}{}
		var unhealthy bool
		var reason string
		var kickNow bool
		if s.lastMsgMillis < 0 {
			unhealthy = true
			reason = "no last_contact recorded"
		} else if time.Duration(s.lastMsgMillis)*time.Millisecond > dead {
			unhealthy = true
			reason = fmt.Sprintf("last contact %ds ago", s.lastMsgMillis/1000)
			kickNow = true
		}
		if !unhealthy {
			delete(st.unhealthySince, key)
			continue
		}
		if kickNow {
			klog.Infof("kicking %s member %s: %s", d.short, s.sid, reason)
			if err := kickMember(ctx, d, s.sid); err != nil {
				klog.Errorf("kick %s/%s: %v", d.short, s.sid, err)
				continue
			}
			delete(st.unhealthySince, key)
			continue
		}
		// no last_msg path: track first-observed; kick after grace.
		if t, ok := st.unhealthySince[key]; !ok {
			st.unhealthySince[key] = now
			klog.Warningf("%s member %s observed unhealthy: %s (will kick after %s)",
				d.short, s.sid, reason, dead)
		} else if now.Sub(t) >= dead {
			klog.Infof("kicking %s member %s: %s, unhealthy for %.0fs",
				d.short, s.sid, reason, now.Sub(t).Seconds())
			if err := kickMember(ctx, d, s.sid); err != nil {
				klog.Errorf("kick %s/%s: %v", d.short, s.sid, err)
				continue
			}
			delete(st.unhealthySince, key)
		}
	}
	// Prune state for members no longer in cluster (already gone).
	for key := range st.unhealthySince {
		if !strings.HasPrefix(key, d.short+":") {
			continue
		}
		sid := key[len(d.short)+1:]
		if _, ok := seen[sid]; !ok {
			delete(st.unhealthySince, key)
		}
	}
}

// refreshStatus publishes our current lifecycle status. Idempotent.
func refreshStatus(ctx context.Context, cfg *Config, kc kubernetes.Interface, st status) error {
	return publishStatus(ctx, kc, cfg.PodNamespace, cfg.PodName, st)
}

// startHeartbeat spawns a goroutine that touches cfg.OVNRunDir/heartbeatFile
// every heartbeatPeriod. The liveness probe checks file mtime: stale ==
// either the Go runtime is hung (deadlock, OOM-spin) OR ovsdb has been
// unhealthy too long. Running independently of the main goroutine means
// even a hang inside recoverCluster (where runtimeTick is suspended)
// gets caught.
//
// Two staleness thresholds based on ls.inRecovery:
//   - outside recovery: cfg.OvsdbStuckTimeout (default 3 min) -- ovsdb
//     should be active in steady state; longer means we're stuck.
//   - inside recovery: cfg.BootstrapLeaseDuration (default 10 min) --
//     legitimate reconvert can take minutes on multi-GB DBs, so we
//     tolerate the full lease window before declaring recovery stuck.
//
// When the threshold is crossed, the goroutine STOPS writing the file;
// the external liveness probe then sees a stale mtime and kubelet kills
// the pod for a fresh restart (which goes through preflight again).
func startHeartbeat(ctx context.Context, cfg *Config, ls *lifecycleState) {
	path := filepath.Join(cfg.OVNRunDir, heartbeatFile)
	go func() {
		if err := touchFile(path); err != nil {
			klog.Warningf("initial heartbeat write %s: %v", path, err)
		}
		tk := time.NewTicker(heartbeatPeriod)
		defer tk.Stop()
		var lastWarn time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				threshold := cfg.OvsdbStuckTimeout
				inRec := ls.inRecovery.Load()
				if inRec {
					threshold = cfg.BootstrapLeaseDuration
				}
				stale := time.Since(time.Unix(0, ls.lastHealthyOvsdbNanos.Load()))
				if stale > threshold {
					// Rate-limit warnings to ~30s so we don't spam logs.
					if time.Since(lastWarn) >= 30*time.Second {
						klog.Warningf("heartbeat halted: ovsdb stale %s > %s (inRecovery=%v); liveness probe will trigger pod restart",
							stale.Round(time.Second), threshold, inRec)
						lastWarn = time.Now()
					}
					continue
				}
				if err := touchFile(path); err != nil {
					klog.Warningf("heartbeat write %s: %v", path, err)
				}
			}
		}
	}()
}

// touchFile updates path's mtime to now, creating the file if absent.
func touchFile(path string) error {
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// pickPeerIPs returns peer Pod IPs for raft join/rejoin remotes. Pods
// without an IP or Terminating are excluded; not-yet-Ready ones stay
// because ovsdb-server retries until one responds.
func pickPeerIPs(ctx context.Context, kc kubernetes.Interface, ns, selfIP string) ([]string, error) {
	all, err := listPeers(ctx, kc, ns)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, p := range all {
		if p.ip == "" || p.ip == selfIP || p.terminating {
			continue
		}
		out = append(out, p.ip)
	}
	return out, nil
}

// trivial helpers

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoiDefault(s string, d int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return d
}

func splitCSV(s string) []string {
	out := []string{}
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
