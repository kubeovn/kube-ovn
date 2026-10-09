// ovsdb.go: thin wrappers around ovsdb-tool / ovs-appctl / ovn-ctl.
// All on-disk and runtime DB inspection lives here. No k8s, no orchestration.
package ovn_central_controller

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

// Per-class exec timeouts. Callers pick a class by:
//   - ctx = parent ctx (SIGTERM-killable) for health / corrective ops
//   - ctx = context.Background() (detached) for mutational / ovn-ctl ops
//     where SIGTERM mid-flight risks partial on-disk state. Callers under
//     the bootstrap lease fence on the lease ctx before each such step;
//     reconvert is the exception: it only writes temp files until its final
//     rename, so it runs on the lease ctx and is killed on lease loss.
//   - timeout constant matched to op cost:
//     health=5s (read-only probes), corrective=10s (kick/compact/steal),
//     mutational=60s (initStub, writeLocalConfigDB, set NB/SB Global),
//     ovn-ctl=60s (start_*/stop_* — ovn-ctl has its own socket-wait),
//     cfg.ReconvertTimeout for cluster-to-standalone on multi-GB DBs.
const (
	execTimeoutHealth     = 5 * time.Second
	execTimeoutCorrective = 10 * time.Second
	execTimeoutMutational = 60 * time.Second
	execTimeoutOvnCtl     = 60 * time.Second
)

// dbInfo describes one of the two raft databases (NB or SB) we manage in lockstep.
type dbInfo struct {
	short        string // "nb" / "sb"
	name         string // OVN_Northbound / OVN_Southbound
	schema       string // ovs schema file
	dbFile       string // /etc/ovn/ovnnb_db.db
	hdrFile      string // /etc/ovn/ovnnb_db.hdr
	localCfgFile string // /etc/ovn/ovnnb_local_config.db
	ctlSocket    string // /var/run/ovn/ovnnb_db.ctl
	port         int    // db client port (6641 / 6642)
	clusterPort  int    // raft cluster port (6643 / 6644)
}

func nbDB(cfg *Config) dbInfo {
	return dbInfo{
		short:        "nb",
		name:         "OVN_Northbound",
		schema:       "/usr/share/ovn/ovn-nb.ovsschema",
		dbFile:       filepath.Join(cfg.OVNDir, "ovnnb_db.db"),
		hdrFile:      filepath.Join(cfg.OVNDir, "ovnnb_db.hdr"),
		localCfgFile: filepath.Join(cfg.OVNDir, "ovnnb_local_config.db"),
		ctlSocket:    filepath.Join(cfg.OVNRunDir, "ovnnb_db.ctl"),
		port:         cfg.NBPort,
		clusterPort:  cfg.NBClusterPort,
	}
}

func sbDB(cfg *Config) dbInfo {
	return dbInfo{
		short:        "sb",
		name:         "OVN_Southbound",
		schema:       "/usr/share/ovn/ovn-sb.ovsschema",
		dbFile:       filepath.Join(cfg.OVNDir, "ovnsb_db.db"),
		hdrFile:      filepath.Join(cfg.OVNDir, "ovnsb_db.hdr"),
		localCfgFile: filepath.Join(cfg.OVNDir, "ovnsb_local_config.db"),
		ctlSocket:    filepath.Join(cfg.OVNRunDir, "ovnsb_db.ctl"),
		port:         cfg.SBPort,
		clusterPort:  cfg.SBClusterPort,
	}
}

// dbState is the on-disk classification of one db file.
type dbState struct {
	exists    bool
	kicked    bool // check-cluster reports server left/not in list
	notJoined bool // join-stub that hasn't synced
}

// readDBState inspects on-disk state non-destructively.
func readDBState(ctx context.Context, d dbInfo) dbState {
	s := dbState{}
	if _, err := os.Stat(d.dbFile); err != nil {
		return s
	}
	s.exists = true

	// check-cluster on a healthy DB exits non-zero ONLY when it has
	// something to report; ignore the error and inspect the message.
	msg, _ := runOutput(ctx, execTimeoutHealth, "ovsdb-tool", "check-cluster", d.dbFile)
	if msg != "" {
		// "left the cluster" (we issued cluster/leave) or "not found in
		// server list" (leader kicked us): same outcome -- ovsdb-server
		// won't start, must wipe.
		if reCheckClusterGone.MatchString(msg) {
			s.kicked = true
		} else if strings.Contains(msg, "has not joined the cluster") {
			s.notJoined = true
		}
	}
	return s
}

// dbAge returns the time since the db file was created, or 0 if missing.
func dbAge(d dbInfo) time.Duration {
	st, err := os.Stat(d.dbFile)
	if err != nil {
		return 0
	}
	return time.Since(st.ModTime())
}

// removeFile deletes path; "already gone" is success.
func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// removeFilesBestEffort deletes paths for cleanup on paths that are already
// failing or whose leftovers are harmless; failures are logged, not returned.
func removeFilesBestEffort(paths ...string) {
	for _, p := range paths {
		if err := removeFile(p); err != nil {
			klog.Warningf("cleanup: %v", err)
		}
	}
}

// wipeDB removes db_file and hdr_file. Idempotent. Fenced on ctx: it
// refuses to delete anything once the caller's lease is gone.
func wipeDB(ctx context.Context, d dbInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A stale hdr left next to a fresh db would make initStub rejoin with
	// the old server ID, so a failed hdr removal is an error too.
	if err := removeFile(d.dbFile); err != nil {
		return err
	}
	return removeFile(d.hdrFile)
}

// initStub creates a fresh db file: create-cluster (if no remotes),
// rejoin-cluster (if hdr present, preserves SID, no AddServer needed),
// or plain join-cluster otherwise. Mutational: detached from parent ctx.
func initStub(d dbInfo, localAddr string, remotes []string) (bootstrapped bool, err error) {
	if len(remotes) == 0 {
		err = run(context.Background(), execTimeoutMutational, "ovsdb-tool", "create-cluster", d.dbFile, d.schema, localAddr)
		return true, err
	}
	args := []string{}
	if _, errStat := os.Stat(d.hdrFile); errStat == nil {
		args = append(args, "rejoin-cluster", d.dbFile, d.hdrFile, localAddr)
		args = append(args, remotes...)
		if err := run(context.Background(), execTimeoutMutational, "ovsdb-tool", args...); err == nil {
			return false, nil
		}
		// rejoin failed (corrupt header etc.) -- wipe and try plain join below.
		klog.Warningf("%s: rejoin-cluster failed, falling back to join-cluster: %v", d.short, err)
		if err := wipeDB(context.Background(), d); err != nil {
			return false, err
		}
	}
	args = []string{"join-cluster", d.dbFile, d.name, localAddr}
	args = append(args, remotes...)
	return false, run(context.Background(), execTimeoutMutational, "ovsdb-tool", args...)
}

// reconvert recreates a 1-node cluster from our existing DB, atomically:
// cluster-to-standalone -> create-cluster into .new -> rename onto
// d.dbFile. At every crash point the original DB is still on disk
// until the rename, so a half-finished reconvert never loses data.
// Leftover .sa-tmp/.new are swept by preflight on startup.
// Because nothing touches d.dbFile before the rename, every step is bound
// to ctx (the bootstrap-lease ctx) and is killed the moment the lease is
// lost; ctx is re-checked right before the rename, the only commit point.
// cluster-to-standalone uses reconvertTimeout (can be minutes on multi-GB DBs).
func reconvert(ctx context.Context, d dbInfo, localAddr string, remotes []string, reconvertTimeout time.Duration) error {
	if !readDBState(ctx, d).exists {
		return fmt.Errorf("reconvert: %s missing", d.dbFile)
	}
	sa := d.dbFile + ".sa-tmp"
	newDB := d.dbFile + ".new"
	cleanup := func() { removeFilesBestEffort(sa, newDB) }
	cleanup()
	if err := run(ctx, reconvertTimeout, "ovsdb-tool", "cluster-to-standalone", sa, d.dbFile); err != nil {
		cleanup()
		return fmt.Errorf("cluster-to-standalone %s: %w", d.dbFile, err)
	}
	args := append([]string{"create-cluster", newDB, sa, localAddr}, remotes...)
	if err := run(ctx, execTimeoutMutational, "ovsdb-tool", args...); err != nil {
		cleanup()
		return fmt.Errorf("create-cluster: %w", err)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return fmt.Errorf("reconvert %s aborted before commit: %w", d.dbFile, err)
	}
	if err := os.Rename(newDB, d.dbFile); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", newDB, d.dbFile, err)
	}
	// The rename above is the commit point, so failing here would only wedge
	// a finished reconvert. The old hdr (pre-reconvert server ID) is
	// rewritten by the next backupHeader tick if it can't be removed now.
	removeFilesBestEffort(sa, d.hdrFile)
	return nil
}

// startNBSBOvsdb starts both ovsdb-server processes via ovn-ctl. Returns
// after ovn-ctl returns (which itself waits for the unix sockets to be
// ready). Caller is responsible for waiting for the cluster to elect a
// leader (see waitForLeader). Detached from parent ctx.
func startNBSBOvsdb(cfg *Config, peers []string) error {
	common := buildOvnCtlArgs(cfg, peers)
	for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
		args := append([]string{}, common...)
		args = append(args,
			"start_"+d.short+"_ovsdb",
			"--",
			// db:Local_Config remote registers the db client port (6641/6642)
			// listener. Mirrors what the legacy bash flow does.
			"--remote=db:Local_Config,Config,connections",
			d.localCfgFile,
		)
		if err := run(context.Background(), execTimeoutOvnCtl, "/usr/share/ovn/scripts/ovn-ctl", args...); err != nil {
			return fmt.Errorf("start_%s_ovsdb failed: %w", d.short, err)
		}
	}
	return nil
}

func stopNBSBOvsdb() {
	for _, sub := range []string{"stop_nb_ovsdb", "stop_sb_ovsdb", "stop_northd"} {
		// Keep going on failure: the remaining components still need stopping.
		if err := run(context.Background(), execTimeoutOvnCtl, "/usr/share/ovn/scripts/ovn-ctl", sub); err != nil {
			klog.Warningf("ovn-ctl %s: %v", sub, err)
		}
	}
}

// startNorthd launches ovn-northd. Optionally writes NB_Global / SB_Global
// option overrides if `bootstrap` (we just created a 1-node cluster).
// Detached: ovn-ctl + ovn-{nb,sb}ctl are mutational on remote DB state.
func startNorthd(cfg *Config, bootstrap bool, peers []string) error {
	args := buildOvnCtlArgs(cfg, peers)
	args = append(args,
		"--ovn-manage-ovsdb=no",
		fmt.Sprintf("--ovn-northd-n-threads=%d", cfg.OVNNorthdNThreads),
		"start_northd",
	)
	if err := run(context.Background(), execTimeoutOvnCtl, "/usr/share/ovn/scripts/ovn-ctl", args...); err != nil {
		return fmt.Errorf("start_northd: %w", err)
	}
	// Pass SSL options when ENABLE_SSL=true so ovn-{nb,sb}ctl can talk to the (TLS) DB.
	ssl := cfg.SSLOptions()
	mk := func(bin, table, kv string) []string {
		a := append([]string{"--no-leader-only"}, ssl...)
		a = append(a, "set", table, ".", kv)
		return append([]string{bin}, a...)
	}

	// version_compatibility must be applied on every northd start so that rolling
	// upgrades and pod restarts keep the NB schema version pinned correctly.
	if cfg.VersionCompatibility != "" {
		c := mk("ovn-nbctl", "NB_Global", "options:version_compatibility="+cfg.VersionCompatibility)
		if err := run(context.Background(), execTimeoutMutational, c[0], c[1:]...); err != nil {
			return fmt.Errorf("%v: %w", c, err)
		}
	}

	if !bootstrap {
		return nil
	}
	// On a fresh 1-node cluster, write NB/SB Global tunables. These are
	// raft-replicated to followers when they later join.
	probe := strconv.Itoa(cfg.ProbeInterval)
	northdProbe := strconv.Itoa(cfg.OVNNorthdProbeInterval)
	cmds := [][]string{
		mk("ovn-nbctl", "NB_Global", "options:inactivity_probe="+probe),
		mk("ovn-sbctl", "SB_Global", "options:inactivity_probe="+probe),
		mk("ovn-nbctl", "NB_Global", "options:northd_probe_interval="+northdProbe),
		mk("ovn-nbctl", "NB_Global", "options:use_logical_dp_groups=true"),
	}
	for _, c := range cmds {
		if err := run(context.Background(), execTimeoutMutational, c[0], c[1:]...); err != nil {
			return fmt.Errorf("%v: %w", c, err)
		}
	}
	return nil
}

// postOvsdbStart enables memory-trim-on-compaction (lets the kernel
// reclaim RSS after periodic compactions, prevents long-term bloat) and
// locks down DB file permissions. Best-effort: failures are logged.
func postOvsdbStart(ctx context.Context, cfg *Config) {
	for _, d := range []dbInfo{nbDB(cfg), sbDB(cfg)} {
		if err := run(ctx, execTimeoutCorrective, "ovn-appctl", "-t", d.ctlSocket, "ovsdb-server/memory-trim-on-compaction", "on"); err != nil {
			klog.Warningf("memory-trim-on-compaction %s: %v", d.short, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(cfg.OVNDir, "*"))
	if err != nil {
		klog.Warningf("list %s for chmod: %v", cfg.OVNDir, err)
	}
	for _, p := range matches {
		if err := os.Chmod(p, 0o600); err != nil {
			klog.Warningf("chmod %s: %v", p, err)
		}
	}
}

// compactDB asks ovsdb-server to write a new snapshot now (truncates the
// raft log to that snapshot). Cheap when there's little to compact, so we
// can call it on every leader from the runtime loop with a long interval.
func compactDB(ctx context.Context, d dbInfo) error {
	return run(ctx, execTimeoutCorrective, "ovn-appctl", "-t", d.ctlSocket, "ovsdb-server/compact")
}

// localNorthdActive reports whether THIS pod's ovn-northd holds the SB
// lock (i.e. is the writing northd). Drives the ovn-northd-leader label
// on our pod, which the Service selector keys on.
func localNorthdActive(ctx context.Context) bool {
	out, err := runOutput(ctx, execTimeoutHealth, "ovn-appctl", "-t", "ovn-northd", "status")
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(out), "active")
}

// stealLock forces release of the ovn_northd lock on the local SB
// database. Used when the previous lock holder went down without
// releasing; without this, ovn-northd elsewhere can't take leadership.
func stealLock(ctx context.Context, cfg *Config) error {
	addr := connAddr(cfg.DBClusterAddr, cfg.SBPort, cfg.EnableSSL)
	args := []string{"-v", "-t", "3"}
	args = append(args, cfg.SSLOptions()...)
	args = append(args, "steal", addr, "ovn_northd")
	return run(ctx, execTimeoutCorrective, "ovsdb-client", args...)
}

// clusterStatus is the parsed view of `ovs-appctl ... cluster/status`.
// Only the fields we read elsewhere are populated.
type clusterStatus struct {
	role      string // leader / follower / candidate / "" if unknown
	leader    string // sid hex / "self" / "unknown" / ""
	status    string // cluster member / joining cluster / left cluster
	clusterID string // full UUID of the raft cluster, e.g. "b79ee8e0-e4c5-..."
	logLow    int    // raft log range low index
	logHigh   int    // raft log range high; logHigh==logLow means stub-only
	servers   []serverInfo
}

type serverInfo struct {
	sid           string
	addr          string
	self          bool
	lastMsgMillis int64 // -1 if not present
}

var (
	reLeaderLine       = regexp.MustCompile(`^Leader:\s+(\S+)`)
	reRoleLine         = regexp.MustCompile(`^Role:\s+(\S+)`)
	reStatusLine       = regexp.MustCompile(`^Status:\s+(.+)$`)
	reClusterIDLine    = regexp.MustCompile(`^Cluster ID:\s+\S+\s+\(([0-9a-f-]+)\)`)
	reSIDLine          = regexp.MustCompile(`^Server ID:\s+\S+\s+\(([0-9a-f-]+)\)`)
	reLogLine          = regexp.MustCompile(`^Log:\s+\[(\d+),\s*(\d+)\]`)
	reServerLine       = regexp.MustCompile(`^\s+([0-9a-f]+)\s+\([0-9a-f-]+\s+at\s+(\S+?)\)(.*)$`)
	reLastMsg          = regexp.MustCompile(`last msg (\d+) ms ago`)
	reCheckClusterGone = regexp.MustCompile(`shows that the server left the cluster|server [0-9a-f]+ not found in server list`)
)

// readClusterStatus calls ovs-appctl cluster/status. Returns an error
// if the unix socket is unreachable (ovsdb-server down).
func readClusterStatus(ctx context.Context, d dbInfo) (clusterStatus, error) {
	// #nosec G204 -- d.ctlSocket and d.name are derived from our own dbInfo, not user input.
	out, err := runOutput(ctx, execTimeoutHealth, "ovs-appctl", "-t", d.ctlSocket, "cluster/status", d.name)
	if err != nil {
		return clusterStatus{}, fmt.Errorf("cluster/status %s: %w (%s)", d.short, err, out)
	}
	cs := clusterStatus{}
	var selfSID string
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case reLeaderLine.MatchString(line):
			cs.leader = reLeaderLine.FindStringSubmatch(line)[1]
		case reRoleLine.MatchString(line):
			cs.role = reRoleLine.FindStringSubmatch(line)[1]
		case reStatusLine.MatchString(line):
			cs.status = strings.TrimSpace(reStatusLine.FindStringSubmatch(line)[1])
		case reClusterIDLine.MatchString(line):
			cs.clusterID = reClusterIDLine.FindStringSubmatch(line)[1]
		case reSIDLine.MatchString(line):
			selfSID = reSIDLine.FindStringSubmatch(line)[1][:4]
		case reLogLine.MatchString(line):
			m := reLogLine.FindStringSubmatch(line)
			lo, errLo := strconv.Atoi(m[1])
			hi, errHi := strconv.Atoi(m[2])
			if errLo != nil || errHi != nil {
				klog.Warningf("readClusterStatus %s: cannot parse Log line %q (lo=%v hi=%v)", d.short, line, errLo, errHi)
			}
			cs.logLow, cs.logHigh = lo, hi
		}
		if m := reServerLine.FindStringSubmatch(line); m != nil {
			si := serverInfo{sid: m[1], addr: m[2], lastMsgMillis: -1, self: m[1] == selfSID}
			if lc := reLastMsg.FindStringSubmatch(m[3]); lc != nil {
				ms, err := strconv.ParseInt(lc[1], 10, 64)
				if err != nil {
					klog.Warningf("readClusterStatus %s: cannot parse last msg %q: %v", d.short, lc[1], err)
				} else {
					si.lastMsgMillis = ms
				}
			}
			cs.servers = append(cs.servers, si)
		}
	}
	return cs, nil
}

// waitForLeader polls cluster/status until both DBs report Status: cluster
// member and a known leader, or until timeout. Returns the elapsed time.
func waitForLeader(ctx context.Context, cfg *Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		nb, errNB := readClusterStatus(ctx, nbDB(cfg))
		sb, errSB := readClusterStatus(ctx, sbDB(cfg))
		if errNB == nil && errSB == nil &&
			nb.status == "cluster member" && sb.status == "cluster member" &&
			isKnownLeader(nb.leader) && isKnownLeader(sb.leader) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out: nb=%q sb=%q", nb.leader, sb.leader)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func isKnownLeader(s string) bool { return s != "" && s != "unknown" }

// kickMember asks the local (must be leader) ovsdb-server to remove peer.
func kickMember(ctx context.Context, d dbInfo, peerSID string) error {
	return run(ctx, execTimeoutCorrective, "ovs-appctl", "-t", d.ctlSocket, "cluster/kick", d.name, peerSID)
}

// backupHeader writes the local raft header JSON to hdr_file. Used by the
// rejoin-cluster path to recreate a stub with the same SID after a DB loss.
func backupHeader(ctx context.Context, d dbInfo) error {
	c, cancel := context.WithTimeout(ctx, execTimeoutCorrective)
	defer cancel()
	// #nosec G204 -- d.dbFile is derived from our own dbInfo, not user input.
	out, err := exec.CommandContext(c, "ovsdb-tool", "db-raft-header", d.dbFile).Output()
	if err != nil {
		return fmt.Errorf("db-raft-header %s: %w", d.dbFile, err)
	}
	tmp := d.hdrFile + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.hdrFile)
}

// writeLocalConfigDB recreates ovn{nb,sb}_local_config.db with the current
// listen addresses for the db client port. ovn-ctl's
// --remote=db:Local_Config,Config,connections reads this on every start.
// Mutational: detached from parent ctx.
func writeLocalConfigDB(d dbInfo, listenAddrs []string) error {
	if err := removeFile(d.localCfgFile); err != nil {
		return err
	}
	if err := run(context.Background(), execTimeoutMutational, "ovsdb-tool", "create", d.localCfgFile, "/usr/share/openvswitch/local-config.ovsschema"); err != nil {
		return err
	}
	// Initial empty Config row.
	if err := run(context.Background(), execTimeoutMutational, "ovsdb-tool", "transact", d.localCfgFile, `[
		"Local_Config",
		{"op": "insert", "table": "Config", "row": {"connections": ["set", []]}}
	]`); err != nil {
		return err
	}
	for _, addr := range listenAddrs {
		txn := fmt.Sprintf(`[
			"Local_Config",
			{"op": "insert", "table": "Connection", "uuid-name": "n", "row": {"target": %q}},
			{"op": "mutate", "table": "Config", "where": [], "mutations": [["connections", "insert", ["set", [["named-uuid", "n"]]]]]}
		]`, addr)
		if err := run(context.Background(), execTimeoutMutational, "ovsdb-tool", "transact", d.localCfgFile, txn); err != nil {
			return err
		}
	}
	return nil
}

// buildOvnCtlArgs constructs the common --db-{nb,sb}-... flags for ovn-ctl
// invocations. Using just one peer in remote-addr is fine: ovsdb-server
// learns the rest via the join-stub or already-committed cluster config.
func buildOvnCtlArgs(cfg *Config, peers []string) []string {
	args := []string{}
	if cfg.DebugWrapper != "" {
		args = append(args,
			"--ovn-northd-wrapper="+cfg.DebugWrapper,
			"--ovsdb-nb-wrapper="+cfg.DebugWrapper,
			"--ovsdb-sb-wrapper="+cfg.DebugWrapper,
		)
	}
	args = append(args,
		"--db-cluster-schema-upgrade=yes",
		fmt.Sprintf("--db-nb-cluster-local-addr=[%s]", cfg.DBClusterAddr),
		fmt.Sprintf("--db-sb-cluster-local-addr=[%s]", cfg.DBClusterAddr),
		fmt.Sprintf("--db-nb-cluster-local-port=%d", cfg.NBClusterPort),
		fmt.Sprintf("--db-sb-cluster-local-port=%d", cfg.SBClusterPort),
		fmt.Sprintf("--db-nb-addr=[%s]", cfg.DBAddr),
		fmt.Sprintf("--db-sb-addr=[%s]", cfg.DBAddr),
		fmt.Sprintf("--db-nb-port=%d", cfg.NBPort),
		fmt.Sprintf("--db-sb-port=%d", cfg.SBPort),
		"--db-nb-use-remote-in-db=no",
		"--db-sb-use-remote-in-db=no",
	)
	if cfg.EnableSSL {
		args = append(args,
			"--ovn-nb-db-ssl-key=/var/run/tls/key",
			"--ovn-nb-db-ssl-cert=/var/run/tls/cert",
			"--ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert",
			"--ovn-sb-db-ssl-key=/var/run/tls/key",
			"--ovn-sb-db-ssl-cert=/var/run/tls/cert",
			"--ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert",
			"--ovn-northd-ssl-key=/var/run/tls/key",
			"--ovn-northd-ssl-cert=/var/run/tls/cert",
			"--ovn-northd-ssl-ca-cert=/var/run/tls/cacert",
			"--db-nb-cluster-local-proto=ssl",
			"--db-sb-cluster-local-proto=ssl",
			"--db-nb-cluster-remote-proto=ssl",
			"--db-sb-cluster-remote-proto=ssl",
		)
		// Validated in parseConfig, so tlsArgs is already known good.
		args = append(args, cfg.tlsArgs...)
	} else {
		args = append(args,
			"--db-nb-create-insecure-remote=yes",
			"--db-sb-create-insecure-remote=yes",
		)
	}
	if len(peers) > 0 {
		args = append(args,
			fmt.Sprintf("--db-nb-cluster-remote-addr=[%s]", peers[0]),
			fmt.Sprintf("--db-sb-cluster-remote-addr=[%s]", peers[0]),
			fmt.Sprintf("--db-nb-cluster-remote-port=%d", cfg.NBClusterPort),
			fmt.Sprintf("--db-sb-cluster-remote-port=%d", cfg.SBClusterPort),
		)
	}
	allPeers := append([]string{cfg.DBClusterAddr}, peers...)
	nbConn := commaJoin("tcp:[%s]:"+strconv.Itoa(cfg.NBPort), allPeers, cfg.EnableSSL)
	sbConn := commaJoin("tcp:[%s]:"+strconv.Itoa(cfg.SBPort), allPeers, cfg.EnableSSL)
	args = append(args,
		"--ovn-northd-nb-db="+nbConn,
		"--ovn-northd-sb-db="+sbConn,
	)
	return args
}

// tlsVersionIndex maps a TLS_{MIN,MAX}_VERSION value to an index into
// tlsProtocols. Accepted spellings match dist/images/ovn-db-ssl-options.sh.
func tlsVersionIndex(v string) (int, error) {
	switch v {
	case "1.0", "TLS 1.0", "TLS10":
		return 0, nil
	case "1.1", "TLS 1.1", "TLS11":
		return 1, nil
	case "1.2", "TLS 1.2", "TLS12":
		return 2, nil
	case "1.3", "TLS 1.3", "TLS13":
		return 3, nil
	}
	return 0, fmt.Errorf("unsupported TLS version: %s", v)
}

var tlsProtocols = []string{"TLSv1", "TLSv1.1", "TLSv1.2", "TLSv1.3"}

// tlsCiphers maps IANA names (TLS <= 1.2 suites) to OpenSSL cipher names,
// and tlsCipherSuites lists the TLS 1.3 suites, which OpenSSL takes via a
// separate option. Same sets as dist/images/ovn-db-ssl-options.sh.
var (
	tlsCiphers = map[string]string{
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":          "ECDHE-ECDSA-AES128-SHA",
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":          "ECDHE-ECDSA-AES256-SHA",
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":            "ECDHE-RSA-AES128-SHA",
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":            "ECDHE-RSA-AES256-SHA",
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       "ECDHE-ECDSA-AES128-GCM-SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       "ECDHE-ECDSA-AES256-GCM-SHA384",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         "ECDHE-RSA-AES128-GCM-SHA256",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         "ECDHE-RSA-AES256-GCM-SHA384",
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   "ECDHE-RSA-CHACHA20-POLY1305",
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": "ECDHE-ECDSA-CHACHA20-POLY1305",
	}
	tlsCipherSuites = map[string]bool{
		"TLS_AES_128_GCM_SHA256":       true,
		"TLS_AES_256_GCM_SHA384":       true,
		"TLS_CHACHA20_POLY1305_SHA256": true,
	}
)

// tlsPolicyArgs returns the ovn-ctl --ovn-{nb,sb}-db-ssl-{protocols,ciphers,
// ciphersuites} flags for the configured TLS policy, mirroring
// ovn_db_ssl_args in dist/images/ovn-db-ssl-options.sh so DYNAMIC_PEERS
// enforces the same policy as the static start-db.sh path. Empty when no
// policy is configured.
func tlsPolicyArgs(minVersion, maxVersion, suites string) ([]string, error) {
	var args []string
	if minVersion != "" || maxVersion != "" {
		lo, hi := 0, len(tlsProtocols)-1
		var err error
		if minVersion != "" {
			if lo, err = tlsVersionIndex(minVersion); err != nil {
				return nil, err
			}
		}
		if maxVersion != "" {
			if hi, err = tlsVersionIndex(maxVersion); err != nil {
				return nil, err
			}
		}
		if lo > hi {
			return nil, fmt.Errorf("TLS_MIN_VERSION (%s) must be less than or equal to TLS_MAX_VERSION (%s)", minVersion, maxVersion)
		}
		protocols := strings.Join(tlsProtocols[lo:hi+1], ",")
		args = append(args,
			"--ovn-nb-db-ssl-protocols="+protocols,
			"--ovn-sb-db-ssl-protocols="+protocols,
		)
	}

	var ciphers, ciphersuites []string
	for suite := range strings.SplitSeq(suites, ",") {
		suite = strings.TrimSpace(suite)
		switch {
		case suite == "":
		case tlsCipherSuites[suite]:
			ciphersuites = append(ciphersuites, suite)
		case tlsCiphers[suite] != "":
			ciphers = append(ciphers, tlsCiphers[suite])
		default:
			return nil, fmt.Errorf("unsupported TLS cipher suite: %s", suite)
		}
	}
	if len(ciphers) > 0 {
		v := strings.Join(ciphers, ":")
		args = append(args, "--ovn-nb-db-ssl-ciphers="+v, "--ovn-sb-db-ssl-ciphers="+v)
	}
	if len(ciphersuites) > 0 {
		v := strings.Join(ciphersuites, ":")
		args = append(args, "--ovn-nb-db-ssl-ciphersuites="+v, "--ovn-sb-db-ssl-ciphersuites="+v)
	}
	return args, nil
}

func commaJoin(format string, peers []string, ssl bool) string {
	if ssl {
		format = strings.Replace(format, "tcp:", "ssl:", 1)
	}
	parts := make([]string, 0, len(peers))
	for _, p := range peers {
		parts = append(parts, fmt.Sprintf(format, p))
	}
	return strings.Join(parts, ",")
}

// listenAddr formats the --remote=p{tcp,ssl}: listener used by ovsdb-server.
func listenAddr(addr string, port int, ssl bool) string {
	proto := "ptcp"
	if ssl {
		proto = "pssl"
	}
	return fmt.Sprintf("%s:%d:[%s]", proto, port, addr)
}

// connAddr formats a raft remote (peer cluster-port) address.
func connAddr(addr string, port int, ssl bool) string {
	proto := "tcp"
	if ssl {
		proto = "ssl"
	}
	return fmt.Sprintf("%s:[%s]:%d", proto, addr, port)
}

// run executes cmd with a deadline derived from timeout, bound to ctx.
// Output streams to our stderr.
func run(ctx context.Context, timeout time.Duration, name string, args ...string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runOutput is like run but returns combined stdout+stderr regardless of exit.
func runOutput(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(c, name, args...).CombinedOutput()
	return string(out), err
}
