# Refactoring suggestions

Refactoring directions: code structure, readability, extensibility, maintainability, performance.

---

## ./cmd/cmdmain.go

- [ ] **Structure / DRY**: `dumpProfile()` has two nearly identical goroutines (SIGUSR1 CPU profile, SIGUSR2 heap profile). Extract a small helper for “create temp file, run writer fn, close” to remove duplicated file create/close/error handling.
- [ ] **Readability**: Replace magic number `30 * time.Second` with a named constant (e.g. `cpuProfileDuration`) at package or func level.
- [ ] **Extensibility**: Consider a table-driven or registry for subcommands (e.g. map[string]func() or slice of {name, mainFn, enableProfile}) so adding a new binary does not require another case in the switch.
- [ ] **Maintainability**: Optionally use a single `signal.Notify` channel for both SIGUSR1 and SIGUSR2 and branch on the received signal, so signal handling is in one place.

---

## ./cmd/cni/cni.go

- [ ] **DRY**: `assignV4Address` and `assignV6Address` are nearly identical; consider a single helper parameterized by address family (e.g. `assignAddress(ipAddress, gateway string, mask *net.IPNet, v6 bool)` or use net.IP version) to reduce duplication.
- [ ] **DRY**: The block that sets `netConf.Provider` when empty and `netConf.Type == util.CniTypeName && args.IfName == "eth0"` is duplicated in `cmdAdd` and `cmdDel`; extract to e.g. `applyDefaultProvider(netConf, args)`.
- [ ] **Error handling**: In `generateCNIResult`, `_, mask, _ := net.ParseCIDR(cniResponse.CIDR)` ignores the error; malformed CIDR could lead to nil mask or wrong behavior. Validate and return a proper CNI error on failure.
- [ ] **Readability**: The `ProtocolDual` branch in `generateCNIResult` is long; consider extracting to a function e.g. `assignDualStackAddresses(cniResponse, result, podIface)` to keep the switch concise.

---

## ./cmd/controller/cmdmain.go

- [ ] **DRY**: `dumpProfile()` is duplicated from `cmd/cmdmain.go` (same CPU/heap goroutines and magic `30 * time.Second`). Consider moving `dumpProfile()` to a shared package (e.g. `pkg/util/profiling`) and reusing from both cmd/cmdmain.go and cmd/controller/cmdmain.go.
- [ ] **Readability**: Replace magic number `30 * time.Second` with a named constant.

---

## ./cmd/controller/controller.go

- [ ] **Maintainability**: Fix typo `listerner` → `listener` (lines 74, 86, 107, 125).
- [ ] **Structure**: The pprof server setup and the metrics-vs-health server branch are duplicated with `cmd/daemon/cniserver.go`. Extract shared helpers (e.g. in `pkg/metrics` or `pkg/util`) for "start pprof on 127.0.0.1:port" and "start metrics server or health-only server" to avoid divergence.
- [ ] **Readability**: Leader election durations (20s, 30s, 6s) could be named constants for easier tuning and documentation.
- [ ] **Readability**: In `checkPermission`, the variable `ssar` is reused for both the request and the Create response; use distinct names (e.g. `req` and `resp`) to avoid confusion.

---

## ./cmd/daemon/cniserver.go

- [ ] **Naming**: Fix typo `chassesName` → `chassisName` in `initChassisAnno`.
- [ ] **Naming**: Retry parameter `ctrl` is misleading (suggests controller); use `cfg` or `config` for `*daemon.Configuration`. Align with `util.Retry`-style naming if shared later.
- [ ] **Readability**: In `Retry`, `return err` when successful is confusing; use `return nil` on success.
- [ ] **Structure**: Pprof + metrics/health server block duplicated from `cmd/controller/controller.go`; extract shared server startup helpers (see controller.go refactor).
- [ ] **Maintainability**: `util.MirrosRetryMaxTimes` / `util.MirrosRetryInterval` are typo’d (should be "Mirrors"); fix in `pkg/util/const.go` and update call sites.

---

## ./cmd/daemon/init.go

- [ ] **DRY**: `initForOS` and `setVxlanNicTxOff` share the same pattern: get link by name, if LinkNotFoundError return nil, else log and return err, else call one action. Consider a small helper e.g. `runIfLinkExists(linkName string, fn func() error) error` to reduce duplication.

---

## ./cmd/ovn_ic_controller/ovn_ic_controller.go

- [ ] **Readability**: Use a more descriptive variable name for the parsed permission (e.g. `logFilePerm` instead of `perm`) to clarify it is the log file mode.

---

## ./cmd/ovn_leader_checker/ovn_leader_checker.go

- [ ] **Consistency**: Consider adding `defer klog.Flush()` and optional version logging at startup to align with other cmd entrypoints (e.g. cmd/ovn_ic_controller/ovn_ic_controller.go).

---

## ./pkg/ovn_leader_checker/ovn.go

- [ ] **Readability**: Fix struct comment "Configuration is the controller conf" → "Configuration is the controller config".
- [ ] **Readability**: In `checkOvnIsAlive`, the result of `getCmdExitCode(cmd)` is an exit code, not an error; use `exitCode := getCmdExitCode(cmd); if exitCode != 0` instead of `err` to avoid confusion.
- [ ] **Readability**: In `getCmdExitCode`, when `cmd.ProcessState == nil` the code logs `err` which may be nil; use a distinct message e.g. "process state not available" for clarity.
- [ ] **Maintainability**: Either implement or remove the TODO "validate configuration" in `ParseFlags`.
- [ ] **Structure / Testability**: Package-level mutable var `failCount` makes behavior order-dependent and hard to test. Consider attaching to Configuration or a checker struct.
- [ ] **Readability**: Replace magic `3*time.Second` in `checkNorthdEpAvailable` with a named constant (e.g. `northdDialTimeout`).
- [ ] **Structure**: `doOvnLeaderCheck` has two long branches (!ISICDBServer and ISICDBServer). Extract e.g. `doOvnLeaderCheckStandard` and `doOvnLeaderCheckIC` for readability.
- [ ] **Correctness**: In `backupRaftHeader`, when the header file does not exist we log "created new one" but never call `os.WriteFile`; the file is only written when it already exists and content differs. Fix by writing the file when `os.IsNotExist(err)`.
- [ ] **Robustness**: In `getTSCidr`, when `len(podIps)` is 0 or > 2, `proto` is never set and `cidr` is returned empty. Consider validating length and returning an error for invalid input.
- [ ] **Correctness**: In `updateTS`, `existTSCount = len(strings.Split(lines, "\n"))` can be off by one if output has a trailing newline (extra empty string). Use `strings.TrimRight` or filter empty entries.
- [ ] **Naming**: Field `ISICDBServer` uses all caps (suggests constant); consider `IsICDBServer` for idiomatic Go.
- [ ] **DRY**: The "for each remote address, check no other leader" loop is duplicated for standard and IC branches; extract a helper e.g. `checkNoOtherLeaders(cfg, localLeaders map[string]bool, databases []string)` to reduce duplication.

---

## ./pkg/ovnmonitor/config.go

- [ ] **Structure / Extensibility**: Configuration has many flat fields that are symmetric for Northbound and Southbound (DatabaseNorthbound* / DatabaseSouthbound*). Consider nested structs e.g. `DatabaseNorthbound` and `DatabaseSouthbound` (each with Name, SocketRemote, SocketControl, FileDataPath, FileLogPath, FilePidPath, PortDefault, PortSsl, PortRaft) to reduce repetition and make adding new DB options easier.
- [ ] **DRY**: ParseFlags repeats the same pattern for northbound and southbound (9 flags each, 9 struct assignments). Extract a helper e.g. `parseDatabaseFlags(prefix string, defaultName string, ...)` returning a small struct to bind and assign in one place.
- [ ] **Readability**: Magic numbers (2 timeout, 30 poll interval, 10661 metrics port, 6631/6632 SSL ports, 640 log perm) could be named package-level constants for documentation and tuning.
- [ ] **Maintainability**: Logging full config with `klog.Infof("ovn monitor config is %+v", config)` may expose paths and settings; consider debug-level or redacted logging in production.

---

## ./pkg/ovnmonitor/exporter.go

- [ ] **Structure / Testability**: Package-level mutable vars `tryConnectCnt`, `checkNbDbCnt`, `checkSbDbCnt` make behavior order-dependent and hard to test. Attach to Exporter or a small state struct.
- [ ] **Readability**: Replace magic numbers with named constants: 5 (max reconnect attempts), `5 * time.Second` (reconnect sleep), 6 (check count before restore).
- [ ] **DRY**: `initParas` repeats the same 9-assignment pattern for Northbound and Southbound; extract a helper that maps config fields to `Client.Database.Northbound` / `Southbound`.
- [ ] **DRY**: In `exportOvnDBStatusGauge`, the switch on database (NB vs SB) duplicates check-count logic; extract e.g. `incrementAndCheckDbFailureCount(database string) (shouldRestore bool)`.
- [ ] **Maintainability**: Hardcoded path `/bin/bash`, `/kube-ovn/restore-ovn-nb-db.sh` should be configurable or a package constant (and script name suggests NB-only but is used for both DBs in the loop — verify behavior).
- [ ] **Error handling**: In `exportOvnDBStatusGauge`, when `getDBStatus` returns error the code `return`s, so the second database is never checked. Prefer `continue` and set an error metric or log so both DBs are reported.
- [ ] **Error handling**: In `exportOvnClusterEnableGauge`, when `getClusterEnableState` fails the metric is not updated; consider setting 0 or a dedicated "unknown" value so stale values are not exposed.
- [ ] **Correctness**: In `getOvnStatus` (util.go), on command error we set result[component]=0 then overwrite with `parseDbStatus(string(output))`; on error output may be stderr. Only set 0 on error and skip parse, or parse only when err == nil.

---

## ./pkg/ovnmonitor/metric.go

- [ ] **Readability**: Fix Help text typo: "the really status report" → "the actual status report" (or "real").
- [ ] **Structure**: `registerOvnMetrics()` has many repetitive `metrics.Registry.MustRegister(...)` calls; consider a slice of collectors and register in a loop.
- [ ] **Maintainability**: Fix or remove TODO comment "The metrics downside are to be implemented" (grammar: "below" not "downside").

---

## ./pkg/ovnmonitor/util.go

- [ ] **Correctness**: In `getOvnStatus`, on command error we set result[component]=0 then overwrite with `parseDbStatus(string(output))`; when err != nil output is stderr. Only assign 0 and skip parse when err != nil.
- [ ] **DRY**: `getOvnStatus` and `getOvnStatusContent` both run similar ovn-appctl commands for northbound and southbound; extract e.g. `runOvnAppctlClusterStatus(ctlPath, dbName string) (output string, err error)` and use config paths (e.g. `e.Client.Database.Northbound.Socket.Control`) instead of hardcoded `/var/run/ovn/ovnnb_db.ctl`.
- [ ] **Maintainability**: In `getDBStatus`, hardcoded ctl paths; use configuration (e.g. from Exporter) so paths are consistent with config.
- [ ] **Readability**: Add a short comment or rename `lspAddress` to clarify "lsp" = logical switch port (e.g. `logicalSwitchPortAddresses`).
- [ ] **Performance / Consistency**: `IncrementErrorCounter` uses both `errorsLocker` and `atomic.AddInt64`; the mutex is redundant if only incrementing. Use either atomic only or document why both are needed.

---

## ./cmd/ovn_monitor/ovn_monitor.go

- [ ] **Naming**: Fix typo `listerner` → `listener` (lines 59, 76).
- [ ] **Readability**: Use a descriptive variable name for the parsed permission (e.g. `logFilePerm` instead of `perm`).
- [ ] **Structure**: The health-only server block (TCP listen, mux with healthz/livez/readyz, manager.Server, Start) is duplicated with cmd/controller/controller.go and cmd/daemon/cniserver.go; consider reusing a shared helper (e.g. in pkg/metrics) for "metrics server vs health-only server" startup.
- [ ] **Readability**: Consider named constants for server timeouts (IdleTimeout 90s, ReadHeaderTimeout 32s) and MaxHeaderBytes (1<<20) to avoid magic numbers.

---

## ./cmd/pinger/pinger.go

- [ ] **Readability**: Use a descriptive variable name for the parsed permission (e.g. `logFilePerm` instead of `perm`).
- [ ] **Maintainability**: Consider a named constant for the mode value `"server"` (e.g. in pkg/pinger or here) to avoid string literal drift when comparing `config.Mode`.

---

## ./pkg/pinger/config.go

- [ ] **Structure / Extensibility**: Configuration has many flat fields; OVS Monitor section (PollTimeout, PollInterval, DatabaseVswitch*, ServiceVswitchd*, ServiceOvnController*) could be grouped into nested structs (e.g. `OVSMonitor` with `DatabaseVswitch`, `ServiceVswitchd`, `ServiceOvnController`) to reduce repetition and improve extensibility, similar to pkg/ovnmonitor/config.go.
- [ ] **Correctness**: When `config.LabelSelector == ""`, the code looks for a DaemonSet owner and sets `dsName`; if no DaemonSet owner exists, `dsName` stays empty and `Get(context.Background(), dsName, ...)` is called with empty name. Guard the DaemonSet Get with `if dsName != ""` and handle the no-owner case (e.g. leave LabelSelector empty or return a clear error).
- [ ] **Readability**: Replace magic numbers in `initKubeClient` (15*time.Second, 1000 QPS, 2000 Burst) and in ParseFlags (3 retries, time.Second sleep) with named package-level constants for tuning and documentation.
- [ ] **Maintainability**: Full config logging `klog.Infof("pinger config is %+v", config)` may expose paths and settings; consider debug-level or redacted logging.
- [ ] **Readability**: Extract the "wait for pod and enrich config" retry loop into a helper (e.g. `getPodAndEnrichConfig` or split `resolveLabelSelector` and `waitForPodIPs`) to separate concerns and shorten `ParseFlags`.

---

## ./pkg/pinger/exporter.go

- [ ] **Structure / Testability**: Package-level mutable var `tryConnectCnt` makes behavior order-dependent and hard to test. Attach reconnect attempt count to Exporter (e.g. `e.tryConnectCnt`) so each Exporter has its own state.
- [ ] **Readability**: Replace magic numbers in `tryClientConnection` (5 max attempts, `5 * time.Second` sleep) with named constants (e.g. `maxReconnectAttempts`, `reconnectSleepDuration`).
- [ ] **Error handling**: In `NewExporter`, when `GetSystemID` or `StartConnection` fail the code only logs and still returns `&e`; callers may assume the exporter is healthy. Consider returning an error from `NewExporter` when initial connection fails, or document that callers must handle an initially disconnected exporter.
- [ ] **Error handling**: In `exportOvsInfoGauge`, log the actual error: `klog.Errorf("Failed to get System Info: %v", err)` instead of omitting `err`.
- [ ] **Naming**: `appName = "ovs-monitor"` lives in package pinger; consider a name that reflects the component (e.g. `pingerOvsMonitor` or document that this exporter is the OVS monitor part of pinger).

---

## ./pkg/pinger/metrics.go

- [ ] **Structure / DRY**: `InitPingerMetrics()` has many repetitive `metrics.Registry.MustRegister(...)` calls; use a slice of collectors and register in a loop (similar to pkg/ovnmonitor/metric.go).
- [ ] **Readability**: Fix Help text typo: "ration" → "ratio" in `metricOvsDpMasksHitRatio` (dp_masks_hit_ratio).
- [ ] **Readability**: Fix grammar in Help strings: "This metrics is always 1" → "This metric is always 1" (metricOvsDp, metricOvsDpIf, interfaceMain).
- [ ] **Maintainability**: Histogram buckets `[]float64{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50}` are duplicated for apiserver, internal DNS, and external DNS latency; consider a shared constant (e.g. `latencyBucketsMs`) for consistency and easier tuning.

---

## ./pkg/pinger/ovn.go

- [ ] **Readability**: Replace magic number 10 (timeout in exec.Command `--timeout=10` and in `ovs.Query(..., 10, ...)`) with a named constant (e.g. `ovsCommandTimeoutSec`, `ovsdbQueryTimeoutSec`) for clarity and tuning.

---

## ./pkg/pinger/ping.go

- [ ] **Naming / Bug**: Fix typo `internval` → `interval` (line 26).
- [ ] **DRY**: Pinger setup/run/statistics logic is duplicated in `pingNodes`, `pingPods`, and `pingExternal` (NewPinger, SetPrivileged, Timeout/Count/Interval, Run, Statistics, loss check, metrics). Extract a helper e.g. `runPing(config *Configuration, targetIP string, timeout time.Duration, setMetrics bool, recordMetrics func(stats *goping.Statistics)) error` to reduce duplication.
- [ ] **DRY**: The verbose TCP/UDP connectivity check block is duplicated in `pingNodes` and `pingPods`. Extract e.g. `runVerboseConnCheck(config *Configuration, targetAddr, targetName string) error`.
- [ ] **DRY**: `internalNslookup` and `externalNslookup` are nearly identical; extract e.g. `nslookupWithMetrics(ctx context.Context, host string, setMetrics bool, setHealthy func(ms float64), setUnhealthy func()) ([]string, error)`.
- [ ] **Readability**: Replace magic numbers with named constants: node ping timeout (30s), pod ping timeout (1s), external timeout (5s), ping count (3), interval (100ms), DNS timeout (10s).
- [ ] **Readability**: `int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv)))` (packet loss count) is repeated many times; extract e.g. `packetLossCount(stats *goping.Statistics) int`.
- [ ] **Structure**: In `pingNodes` and `pingPods`, the inner logic is in an IIFE that captures `pingErr`; consider a named helper (e.g. `pingOneNode`, `pingOnePod`) that returns error and assign `pingErr` from it to simplify control flow.

---

## ./pkg/pinger/util.go

- [ ] **Bug / Logging**: In `ovsDatapathLookupsMetrics` and `ovsDatapathMasksMetrics`, when `strconv.ParseFloat` fails the code logs `value` which is uninitialized; log the input string (e.g. `elem[1]`) instead.
- [ ] **Performance / Consistency**: `IncrementErrorCounter` uses both mutex Lock and `atomic.AddInt64`; for a single increment the mutex is redundant. Use atomic only or document why both are needed (e.g. if other state is updated under the same lock).
- [ ] **Error handling**: In `setOvsDpIfMetric`, `value, _ := strconv.ParseFloat(flowFields[1], 64)` ignores error; guard with `len(flowFields) >= 2` and handle ParseFloat error to avoid wrong metrics.
- [ ] **Structure**: `resetOvsDatapathMetrics` and `resetOvsInterfaceMetrics` have many repetitive `Reset()` calls; consider a slice of collectors and loop (similar to pkg/ovnmonitor/metric.go).
- [ ] **Readability**: Fix comment typo "There is two line" → "There are two lines" (line 50).

---

## ./pkg/request/client.go

- [ ] **Readability**: Fix function comment "return" → "returns" (NewCniServerClient). Optionally document that `socketAddress` is the Unix socket path.

---

## ./pkg/request/cniserver.go

- [ ] **DRY / Structure**: `Add` and `Del` share the same pattern (Post, Send, check errors, check status code). Extract a small helper (e.g. `doRequest(path string, body interface{}, result interface{})`) for error handling and status check to reduce duplication and make adding new operations (e.g. CNI Check) easier.
- [ ] **Maintainability**: Replace hardcoded `"http://dummy"` with a package-level constant (e.g. `cniserverBaseURL`) for clarity and a single place to change.
- [ ] **Error handling**: In `Add()`, when `res.StatusCode != http.StatusOK` the error uses `resp.Err` which may be empty; consider including response body or ensuring the server always sets `Err` for non-OK responses for better diagnostics.
- [ ] **Readability**: Struct field alignment in `CniRequest` uses inconsistent spacing (e.g. `IfName`, `Provider`); align consistently or let gofmt handle.

---

## ./pkg/speaker/bgp.go

- [ ] **Readability**: In `reconcileIPFamily` line 55, add parentheses to make condition explicit: `(len(route) == 1 && route[0].Type == unix.RTN_LOCAL) || nextHop.Equal(c.config.RouterID)` to avoid reliance on operator precedence.
- [ ] **Error handling**: In the `fn` callback, `route, _ := netlink.RouteGet(nextHop)` discards the error; consider logging on error in debug mode for diagnostics.
- [ ] **DRY**: `addRoute` and `delRoute` share the same pattern (getPathRequest, then loop over paths with AddPath/DeletePath). Consider extracting a helper e.g. `applyPathRequest(route string, add bool) error` to reduce duplication.
- [ ] **Readability**: In `getPathRequest`, the path construction block is long; consider extracting e.g. `buildPath(prefix, family, nextHopStr string) (*api.Path, error)` to shorten the function.
- [ ] **Maintainability**: Fix comment typo "NRLIs" → "NLRIs" (line 137).
- [ ] **Maintainability**: In `announceAndWithdraw`, errors from `addRoute`/`delRoute` are only logged; consider collecting and returning an aggregated error or at least counting failures for observability.

---

## ./pkg/speaker/config.go

- [ ] **Correctness**: In neighbor IPv4 validation loop (line ~181), error message uses `*argNeighborAddress` (whole slice); use the invalid `addr` in the message like the IPv6 branch does (e.g. `fmt.Errorf("invalid neighbor-address format: %s is not an IPv4 address", addr)`).
- [ ] **Structure**: `ParseFlags()` is long and does flag parsing, validation, RouterID resolution, and client/BGP init; consider extracting e.g. `resolveRouterID(config, podIPv4)`, `validateNeighborAddresses(config, argNeighborAddress, argNeighborIPv6Address) error` to shorten and separate concerns.
- [ ] **Structure**: `initBgpServer()` is long (~110 lines); extract peer construction into a helper e.g. `buildPeerConfig(addr net.IP, ipFamily api.Family_Afi, config *Configuration) *api.Peer` and optionally a small `addPeers(s, config, peersMap)` to improve readability.
- [ ] **Readability**: Replace magic numbers in `initBgpServer` (256<<20 for maxSize) and in `checkGracefulRestartOptions` (4095 seconds, 18 hours) with named package-level constants for documentation and tuning.
- [ ] **Maintainability**: When multiple `--node-ips` are given, the loop keeps only the last IPv4 and last IPv6; document this behavior in a comment or validate that at most one per family is expected.
- [ ] **Readability**: `logBgpPeer` logs full `peer.AfiSafis` which can be verbose; consider structured logging or a summarized form (e.g. family list only) for production.

---

## ./pkg/speaker/config_test.go

- [ ] **Maintainability**: Only `validateRequiredFlags` is unit-tested; consider adding tests for `checkGracefulRestartOptions` (valid/invalid bounds) to guard against regressions and document expected ranges.

---

## ./pkg/speaker/controller.go

- [ ] **Correctness**: Add `c.natgatewaySynced` to `cache.WaitForCacheSync` so all registered informers are synced before workers run; otherwise use of `natgatewayLister` (if added later) could read stale cache.
- [ ] **Naming**: Rename `subnetSynced` to `subnetsSynced` for consistency with `podsSynced`, `servicesSynced`, `eipSynced`.
- [ ] **Readability**: Replace magic number `5*time.Second` in `wait.Until(c.Reconcile, ...)` with a named package constant (e.g. `reconcileInterval`).
- [ ] **Maintainability**: `natgatewayLister` is never used in the package (only eipLister is used for EIP sync); either document it as reserved for future use or remove the informer/lister to reduce confusion.

---

## ./pkg/speaker/eip.go

- [ ] **Readability**: Extract the condition `eip.Annotations[util.BgpAnnotation] != "true" || !eip.Status.Ready` to a helper e.g. `isEIPReadyForBGP(eip *v1.IptablesEIP) bool` so the loop in `announceEIPs` is clearer and the intent is reusable.
- [ ] **Consistency**: The value `"true"` for BgpAnnotation is used in eip.go and subnet.go; consider a shared constant (e.g. in util) like `BgpAnnotationValueTrue = "true"` to avoid string literal drift.
- [ ] **Error handling**: In `syncEIPRoutes`, errors are both wrapped with fmt.Errorf and logged with klog.Error before return; consider letting the caller log to avoid double logging, or document that this layer intentionally logs.

---

## ./pkg/speaker/subnet.go

- [ ] **Structure**: `syncSubnetRoutes` is long (~100 lines) and mixes listing, service prefix collection, subnet prefix collection (with localSubnets), pod prefix collection, and reconcile. Extract helpers e.g. `collectServicePrefixes(services, bgpExpected)`, `collectSubnetPrefixes(subnets, bgpExpected) (localSubnets map[string]string)`, `collectPodPrefixes(pods, localSubnets, bgpExpected)` to shorten and separate concerns.
- [ ] **DRY**: The policy switch (`""` / `"true"` / announcePolicyCluster / announcePolicyLocal / default) is duplicated for subnets and pods; extract e.g. `isClusterPolicy(policy string) bool` returning true for `"true"` or announcePolicyCluster to avoid duplication.
- [ ] **Consistency**: The value `"true"` for BgpAnnotation appears in lines 48, 67, 98; use a shared constant (e.g. in util) as in eip.go refactor to avoid string literal drift.
- [ ] **Error handling**: `syncSubnetRoutes` returns void; errors are only logged. Consider returning error so the caller (e.g. Reconcile loop) can retry or record a metric.
- [ ] **Maintainability**: Remove or narrow `//nolint:staticcheck` at package level once the SplitSeq range bug is fixed; avoid suppressing other staticcheck findings.
- [ ] **Readability**: Line 129 uses `err.Error()` in klog.Errorf; prefer `%v`, err for consistency with the rest of the codebase.

---

## ./pkg/speaker/utils.go

- [ ] **Readability**: Fix comment typo on line 17: "associating an BGP" → "associating a BGP".
- [ ] **Consistency**: Replace magic string `"Evicted"` in `isPodAlive` (line 46) with a named constant or `corev1.PodReasonEvicted` if available in the k8s.io/api version in use; same pattern exists in pkg/controller/pod.go.
- [ ] **Error handling (optional)**: `addExpectedPrefix` logs parse failures and returns without surfacing an error; callers cannot aggregate or react. Consider returning error so callers can count failures or retry (low priority).

---

## ./pkg/tproxy/tproxy_tcp_linux.go

- [ ] **Correctness**: In `tcpAddrFamily`, line 148: `(raddr == nil || laddr.IP.To4() != nil)` uses `laddr.IP` for the raddr branch; should be `raddr.IP` so address family is derived from both local and remote addresses correctly.
- [ ] **Naming**: Fix comment typo line 109: "tcpAddToSockerAddr" → "tcpAddrToSocketAddr" and "Socker" → "Socket".
- [ ] **DRY**: In `dialTCP`, the pattern "on error: close fd (with klog), klog.Error(err), return nil, &net.OpError{...}" is repeated 5+ times; extract a helper e.g. `closeFdAndReturnDialErr(fd int, err error, op string, wrapErr error)` to reduce duplication.
- [ ] **Readability**: The string `"operation now in progress"` (line 217) is the EINPROGRESS errno message; use `errors.Is(err, syscall.EINPROGRESS)` if available, or a named constant for the string to make intent clear.
- [ ] **Structure**: `dialTCP` is long (~90 lines) with many sequential steps; consider extracting helpers e.g. `createTransparentSocket(family, device string, laddr, raddr *net.TCPAddr) (fd int, err error)` and socket-option setup to improve readability.

---

## ./cmd/speaker/speaker.go

- [ ] **Readability**: Use a descriptive variable name for the parsed permission (e.g. `logFilePerm` instead of `perm`) inside the `!config.NatGwMode` block.

---

## ./cmd/webhook/server.go

- [ ] **Consistency**: Add `defer klog.Flush()` at the start of CmdMain to align with other cmd entrypoints.
- [ ] **Error handling**: Replace `panic(err)` with `util.LogFatalAndExit(err, "message")` (or equivalent) for manager creation, webhook creation, Add hookServer, AddHealthzCheck, AddReadyzCheck, and mgr.Start, so errors are logged and the process exits gracefully instead of panicking.

---

## ./mocks/doc.go

- [ ] **Readability**: Fix package doc: the package is `ovs` but the comment says "Package mocks". Change to e.g. "Package ovs provides mocks for pkg/ovs (OvnClient and related interfaces)."

---

## ./pkg/apis/kubeovn/register.go

- [ ] **Naming/Discoverability**: File is named `register.go` but only defines `GroupName`; actual scheme registration lives in `v1/register.go`. Consider renaming to `group.go` or adding a short package comment that scheme registration is in `v1/register.go` for discoverability.

---

## ./pkg/apis/kubeovn/v1/condition.go

- [ ] **Readability**: Remove or complete the incomplete comment `// check message ?` in `SetCondition` (line 83).
- [ ] **Correctness**: Fix doc comment for `IsValidated` — it says "returns true if ready condition is set" but should say "validated condition".
- [ ] **Structure**: Consider extracting a small helper e.g. `findCondition(c *Conditions, ctype ConditionType) *Condition` or index-based variant; `SetCondition` and `GetCondition` both loop over conditions by type — DRY.
- [ ] **Maintainability**: `RemoveCondition` swaps with last element, changing order; if order should be stable for API consumers, rebuild slice without the element instead of swap-remove. Otherwise document that order is unspecified.
- [ ] **Readability**: Group constant block: put `ReasonInit` (and any future reason constants) in a separate `const` block or comment so condition-type constants (Ready, Validated, Init, Error) are clearly separate from reason constants.

---

## ./pkg/apis/kubeovn/v1/condition_test.go

- [ ] **Naming**: Fix typo `expctedLen` → `expectedLen` and `expcted` → `expected` in test table fields throughout the file.
- [ ] **Correctness**: In `TestSetCondition`, the call uses literal `1` for generation: `tt.conditions.SetCondition(tt.ctype, tt.status, tt.reason, tt.message, 1)`; should use `tt.generation` so cases that expect generation 2 are actually tested.

---

## ./pkg/apis/kubeovn/v1/ip.go

- [ ] **Maintainability**: Document the relationship between `IPAddress` and `V4IPAddress`/`V6IPAddress` in `IPSpec` (e.g. when dual-stack, which field is canonical, or that IPAddress is legacy/single-stack) to avoid misuse.

---

## ./pkg/apis/kubeovn/v1/ippool.go

- [ ] **Readability**: Remove or complete the incomplete comment `// check message ?` in `setConditionValue` (line 81).
- [ ] **DRY**: `Bytes()` (Marshal status, wrap in `{"status": ...}`, log V(5), return) is duplicated across 15+ status types in this package (IPPoolStatus, IptablesDnatRuleStatus, IptablesEIPStatus, etc.). Extract a shared helper e.g. `StatusPatchBytes(s interface{}) ([]byte, error)` in pkg/apis/kubeovn/v1 or pkg/util and reuse to reduce duplication.
- [ ] **Consistency/Safety**: `IPPoolStatus.GetCondition` returns `&s.Conditions[i]` (pointer into slice); callers could mutate. `condition.go`’s `(Conditions).GetCondition` returns `DeepCopy()`. Consider returning a copy here for consistency and to avoid accidental mutation.

---

## ./pkg/apis/kubeovn/v1/iptables-dnat-rule.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/iptables-eip.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/iptables-fip-rule.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/iptables-snat-rule.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/ovn-dnat-rule.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/ovn-eip.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/ovn-fip.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/ovn-snat-rule.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/provider-network.go

- [ ] **Readability**: Remove or complete the incomplete comment `// check message ?` in `setNodeConditionValue` (line 96).
- [ ] **Maintainability**: Document semantics of `IsReady()` and `NodeIsReady(node)`: when there are no conditions, both return true; clarify whether "no condition" should mean ready or unknown.
- [ ] **Consistency/Safety**: `GetNodeCondition` returns `&s.Conditions[i]` (pointer into slice); callers could mutate. Consider returning a copy (e.g. deep copy or value) for consistency with condition.go’s `GetCondition` and to avoid accidental mutation.
- [ ] **Maintainability**: `RemoveNodeCondition` uses swap-with-last, changing slice order. Document that order is unspecified or rebuild slice without the element if stable order is required.
- [ ] **Readability**: Add a short doc comment for `NodeIsReady` and `IsReady` describing the "all Ready conditions must be True" / "no Ready condition for node means true" behavior.

---

## ./pkg/apis/kubeovn/v1/qos-policy.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.
- [ ] **Readability**: Fix comment typo "an bandwidth" → "a bandwidth" in `QoSPolicyBandwidthLimitRule` doc (line 55).
- [ ] **Readability**: Add blank line between `QoSPolicy` and `QoSPolicySpec` type definitions for consistency with other API files.

---

## ./pkg/apis/kubeovn/v1/register.go

- [ ] **Maintainability**: Add a short comment above `addKnownTypes` list reminding to register both Type and TypeList when adding new API types, to reduce omission errors.
- [ ] **Readability**: Consider sorting the type list in `addKnownTypes` alphabetically (e.g. DNSNameResolver, IP, IPPool, …) for easier lookup when adding or auditing types.

---

## ./pkg/apis/kubeovn/v1/register_test.go

- [ ] **Readability**: Add a one-line doc comment for `getPackagePath` explaining that it returns the package path of the test for type discovery (used to compare with registered types’ PkgPath).

---

## ./pkg/apis/kubeovn/v1/security-group.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.
- [ ] **Structure/Dependency**: API package `pkg/apis/kubeovn/v1` imports `pkg/ovsdb/ovnnb` for SgPolicy values. Consider defining allow/drop as string constants in this package and document alignment with ovnnb when used, to keep API layer independent of ovsdb if desired.
- [ ] **Readability**: Align struct field spacing in `SecurityGroupRule` (e.g. RemoteSecurityGroup, PortRangeMin, PortRangeMax, Policy) for consistent column alignment.

---

## ./pkg/apis/kubeovn/v1/subnet.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.
- [ ] **Readability**: Remove or complete the incomplete comment `// check message ?` in `setConditionValue` (line 168).
- [ ] **Correctness**: Fix doc comments: `IsNotReady` says "returns true if ready condition is set" — should say "returns true when not ready" (or "if ready condition is not true"). Same for `IsNotValidated`: should say "returns true when not validated".
- [ ] **Naming**: Fix typo `NatOutGoingPolicyMatch` → `NatOutgoingPolicyMatch` (line 107) for consistent Go naming (Outgoing, not OutGoing). Update struct name and all references.
- [ ] **Consistency/Safety**: `GetCondition` returns `&s.Conditions[i]`; consider returning a copy for consistency with condition.go and to avoid mutation.
- [ ] **Maintainability**: Document or change `RemoveCondition` swap-with-last behavior if stable condition order is required.

---

## ./pkg/apis/kubeovn/v1/vip.go

- [ ] **DRY**: `Bytes()` is identical to other status types; see ippool.go refactor — use shared `StatusPatchBytes` helper.

---

## ./pkg/apis/kubeovn/v1/vlan.go

- [ ] **Readability**: Remove or complete the incomplete comment `// check message ?` in `setVlanConditionValue` (line 67).
- [ ] **Structure/DRY**: Condition update logic (find by type, update or append, LastTransitionTime on status change) mirrors `condition.go`, `ippool.go`, `subnet.go`, `provider-network.go`. Consider a shared helper e.g. `SetConditionValue(conditions *[]Condition, ctype, status, reason, message)` in pkg/apis/kubeovn/v1 to reduce duplication and keep behavior consistent.
- [ ] **Maintainability**: Add `GetVlanCondition(ctype ConditionType) *Condition` and optionally `RemoveVlanCondition(ctype ConditionType)` for consistency with Subnet, ProviderNetwork, IPPool status types; document whether RemoveVlanCondition should preserve order (rebuild slice) or allow swap-with-last.
- [ ] **Readability**: Add one-line doc comments for `SetVlanError` and `SetVlanCondition` describing purpose (e.g. "sets error condition with reason and message").

---

## ./pkg/apis/kubeovn/v1/vpc-dns.go

- [ ] **Readability**: Add short doc comments for `VpcDns`, `VpcDNSSpec` (replicas, vpc, subnet, corefile), and `VpcDNSStatus` to improve discoverability and align with other API types.
- [ ] **Consistency**: `VpcDNSStatus` has `Conditions` but no `SetCondition`/`GetCondition`/`Bytes()` helpers. If this type will be patched like Subnet/IPPool status, consider adding the same pattern for consistency; otherwise add a one-line comment that status is minimal and conditions are used for raw patching only.

---

## ./pkg/apis/kubeovn/v1/vpc-egress-gateway.go

- [ ] **Readability**: Add one-line doc comments for `TrafficPolicyLocal` and `TrafficPolicyCluster` (e.g. "valid values for Spec.TrafficPolicy") and reference them in `VpcEgressGatewaySpec.TrafficPolicy` comment to avoid string literal drift.
- [ ] **Readability**: Add short type-level doc comments for `VpcEgressGatewaySelector`, `VpcEgressGatewayBFDConfig`, `VpcEgressGatewayPolicy`, `VpcEgressGatewayNodeSelector`, `VpcEgressGatewayStatus`, and `VpcEgressWorkload` for discoverability.

---

## ./pkg/apis/kubeovn/v1/vpc.go

- [ ] **Structure/Dependency**: API package `pkg/apis/kubeovn/v1` imports `pkg/ovsdb/ovnnb` only for `PolicyRouteAction` values (Allow, Drop, Reroute). Consider defining these as string constants in this package and document alignment with ovnnb when used in controllers, to keep API layer independent of ovsdb (same pattern as security-group.go refactor).
- [ ] **DRY**: `Bytes()` (Marshal status, wrap in `{"status": ...}`, klog.V(5), return) is identical to other status types; use shared `StatusPatchBytes` helper (see ippool.go refactor).
- [ ] **Readability**: Fix comment grammar "EnableExternal only handle" → "EnableExternal only handles" (or "handles only the default external subnet"); "ExtraExternalSubnets only handle" → "ExtraExternalSubnets only handle" similarly.
- [ ] **Readability**: Add one-line doc comments for `RoutePolicy`, `PolicyRouteAction`, `Vpc`, `VpcSpec`, `VpcStatus`, `BFDPort`, `VpcPeering`, `StaticRoute`, `PolicyRoute`, `BFDPortStatus` for discoverability.

---

## ./pkg/apis/kubeovn/v1/vpc-nat-gateway.go

- [ ] **DRY**: `Bytes()` (Marshal status, wrap in `{"status": ...}`, klog.V(5), return) is identical to other status types; use shared `StatusPatchBytes` helper (see ippool.go refactor).
- [ ] **Readability**: Add short type-level doc comments for `VpcNatGateway`, `VpcNatGatewaySpec`, `VpcNatGatewayStatus`, `VpcBgpSpeaker`, and `Route` for discoverability and consistency with vpc.go, vpc-egress-gateway.go.
- [ ] **Maintainability**: Document key Spec fields (e.g. LanIP as gateway LAN-side IP, ExternalSubnets for NAT egress, Routes for static routes) to clarify usage for API consumers.

---

## ./pkg/apis/kubeovn/v1/zz_generated.deepcopy_test.go

- [ ] **Maintainability/DRY**: Replace the 30+ repeated `deepCopyObjectTestHelper(t, &X{})` / `deepCopyObjectTestHelper(t, &XList{})` calls with a table-driven test: e.g. a slice of `func() runtime.Object` constructors or `[]struct{ name string; obj runtime.Object }`, then loop and call `deepCopyObjectTestHelper` for each. Adding a new API type becomes a single table entry and reduces omission errors when new types are added to the codebase.

---

## ./pkg/controller/admin_network_policy.go

- [ ] **Naming**: Fix typo "releated" → "related" in comment (line ~371, "ACLs releated to port_group").
- [ ] **Error handling**: In `handleUpdateAnp` when `!ok || err != nil` for PortGroupExists, returning `err` can be nil when `!ok`; return a descriptive error e.g. `fmt.Errorf("port-group for anp %s does not exist", desiredAnp.Name)` when !ok.
- [ ] **DRY**: Ingress and egress ACL creation in `handleAddAnp` (lines ~218–268 and ~305–356) are very similar; extract a helper e.g. `createACLsForAnpDirection(anpName, pgName, rules, direction string, ...)` to reduce duplication.
- [ ] **DRY**: In `enqueueUpdateAnp`, the ingress vs egress rule comparison blocks are nearly identical; extract a small helper that compares two rule slices (action, ports) and returns whether to re-add, to avoid duplication.
- [ ] **Readability/Structure**: `handleAddAnp` is long (~210 lines); consider extracting `createIngressACLsForAnp` and `createEgressACLsForAnp` (or similar) so the main function is a clear sequence of steps.
- [ ] **Performance**: `resolveDomainNames` lists all DNSNameResolvers for every domain in a loop (O(domains * resolvers)). Build a map[domainName]*DNSNameResolver once from `c.dnsNameResolversLister.List()`, then look up each domain to make it O(domains + resolvers).
- [ ] **DRY**: `updateAnpsByLabelsMatch` has two almost identical blocks for ANPs and BANPs; extract a generic helper that takes lister, updateQueue, and policy type ("anp"/"banp") to reduce duplication and keep behavior in sync.

---

## ./pkg/controller/admin_network_policy_test.go

- [ ] **Readability**: In `TestIsRulesArrayEmpty`, use named fields in the test table (e.g. `name`, `arg`, `ret`) instead of positional struct literals, to match `TestFetchCIDRAddrs` and `TestGetAnpName` and avoid confusion.
- [ ] **Correctness/Isolation**: In `TestValidateAnpConfig`, the subtest "priority out of range" mutates `anp.Spec.Priority` (line 43) without restoring it. Copy the anp for that subtest (e.g. `anpCopy := anp.DeepCopy()` or build a new struct) so the mutation does not affect other subtests if execution order changes.
- [ ] **Readability**: In `TestIsLabelsMatch`, replace `make(map[string]string, 1)` plus single assignment with literal maps (e.g. `map[string]string{"nsName": "test-ns"}`) for brevity.
- [ ] **Consistency**: In `TestGetAnpName` and `TestGetAnpAddressSetName`, use named fields in table struct literals (e.g. `name`, `arg`, `ret`) for consistency with `TestFetchCIDRAddrs` and easier maintenance.

---

## ./pkg/controller/baseline_admin_network_policy.go

- [ ] **Naming**: Fix typo "releated" → "related" in comment (line 325, "ACLs releated to port_group").
- [ ] **Error handling**: In `handleUpdateBanp` when `!ok || err != nil` for PortGroupExists (lines 369–372), return a descriptive error when !ok (e.g. `fmt.Errorf("port-group for banp %s does not exist", desiredBanp.Name)`) instead of returning err which may be nil.
- [ ] **DRY**: In `handleAddBanp`, ingress ACL creation (lines 186–241) and egress ACL creation (254–305) are nearly identical; extract a helper e.g. `createACLsForBanpDirection(banp, pgName, banpName, direction string, ...)` to reduce duplication.
- [ ] **DRY**: In `enqueueUpdateBanp`, the ingress vs egress rule comparison blocks (56–63 and 65–71) and the changed rule names blocks (89–101 and 105–117) are nearly identical; extract small helpers to avoid duplication.
- [ ] **Error handling**: In `handleDeleteBanp`, the first two errors (DeletePortGroup, first DeleteAddressSets) are only logged; the function returns only the last error. Consider returning the first non-nil error or document that only the last error is returned.

---

## ./pkg/controller/baseline_admin_network_policy_test.go

- [ ] **Performance/Consistency**: Add `t.Parallel()` inside the `for _, tt := range tests` loop (in `t.Run(tt.name, func(t *testing.T) { ... })`) so that subtests run in parallel and align with other controller tests that use parallel subtests.

---

## ./pkg/controller/cluster_network_policy.go

- [ ] **Performance**: In `resolveDomainNamesForCnp`, `c.dnsNameResolversLister.List(labels.Everything())` is called for every domain in the loop (O(domains × resolvers)). Build a map[domainName]*DNSNameResolver once from the lister, then look up each domain to make it O(domains + resolvers).
- [ ] **DRY**: In `handleAddCnp`, ingress ACL creation (lines 164–201) and egress ACL creation (225–264) are nearly identical; extract a helper e.g. `createACLsForCnpDirection(cnp, pgName, cnpName, cnpACLTier, logActions, direction string, rules, ...)` to reduce duplication.
- [ ] **DRY**: In `handleUpdateCnp`, the blocks for `ChangedIngressRule` (312–334) and `ChangedEgressRule` (336–371) are similar (loop over rules, setAddrSetForCnpRule, delete old address sets if name changed); extract a helper for “update address sets for rules and delete old address sets when rule name changed” to avoid duplication.
- [ ] **Readability/Structure**: `handleAddCnp` is long (~170 lines); consider extracting `createIngressACLsForCnp` and `createEgressACLsForCnp` (or a single parameterized function) so the main function is a clear sequence of steps.
- [ ] **Consistency**: In `handleAddCnp`, error messages use `key` in some places (lines 184, 248, 258, 266, 268) and `cnp.Name` in others; use `cnp.Name` consistently for user-facing error messages.
- [ ] **Naming**: `reconcileDNSNameResolversForANP` is called for CNP (lines 219, 354, 417); consider renaming to e.g. `reconcileDNSNameResolversForPolicy` or add a comment that it is shared by ANP/BANP/CNP.
- [ ] **Error handling**: In `handleDeleteCnp`, the first errors (deleteCnpPriorityMapEntries, DeletePortGroup, DeleteAddressSets, reconcileDNSNameResolversForANP) are only logged; the function never returns an error. Consider returning the first non-nil error or document that deletion is best-effort and errors are only logged.

---

## ./pkg/controller/config.go

- [ ] **Naming**: Fix typo "extentsion" → "extension" in log message "init extentsion client failed" (line 317).
- [ ] **DRY**: `initKubeClient()` and `initKubeFactoryClient()` both build rest.Config from KubeConfigFile (empty → InClusterConfig, else BuildConfigFromFlags) and set QPS/Burst. Extract a helper e.g. `buildRestConfig(config *Configuration) (*rest.Config, error)` and reuse so config building and QPS/Burst are in one place.
- [ ] **Readability**: Replace magic numbers with named constants: API server dial timeout (3*time.Second), retry count (10), rest config timeout (30*time.Second), QPS (1000), Burst (2000) for easier tuning and documentation.
- [ ] **Structure**: `ParseFlags()` is long (~200 lines). Consider splitting: e.g. `parseFlagsIntoConfig()` for flag declarations and config struct literal, and `validateAndCompleteConfig(config *Configuration)` for validation and default gateway computation, so the flow is clearer.
- [ ] **Maintainability**: Configuration struct has 80+ fields with no grouping. Add short comment blocks (e.g. `// OVN/OVS`, `// Kubernetes clients`, `// Default subnet`, `// Feature flags`) to improve discoverability; optionally split into embedded structs for very large configs.
- [ ] **Maintainability**: Resolve or remove TODO at line 139 ("TODO: validate configuration") — either add more validation (e.g. WorkerNum > 0, port ranges) and document, or remove the TODO.
- [ ] **DRY**: Default gateway and NodeSwitchGateway defaulting (lines 274–298) share the same pattern (if empty, GetGwByCidr, assign). Extract a small helper e.g. `setDefaultGatewayIfEmpty(config *Configuration, cidrField *string, gwField *string)` or inline comment to reduce duplication.

---

## ./pkg/controller/controller.go

- [ ] **Structure**: Controller struct has 100+ fields in a single block. Consider grouping into embedded structs (e.g. PodResources, VpcResources, SubnetResources) with comment blocks to improve discoverability and readability.
- [ ] **DRY**: Run() has 30+ nearly identical AddEventHandler blocks (if _, err = X.Informer().AddEventHandler(...); err != nil { util.LogFatalAndExit(err, "...") }). Extract a helper e.g. `registerEventHandler(cache.SyncChecker, cache.ResourceEventHandlerFuncs, string) error` to reduce duplication.
- [ ] **DRY**: shutdown() has 60+ sequential queue.ShutDown() calls. Consider collecting queues in a slice during setup and iterating in shutdown(), or grouping by feature to reduce repetition and omission risk when adding new resources.
- [ ] **Readability**: Run() is very long (~400+ lines for setup). Extract buildController() for the large struct literal, registerAllEventHandlers() for the AddEventHandler blocks, and getCacheSyncs() for cache sync list building.
- [ ] **Readability**: Replace magic numbers (e.g. 3*time.Second for wait.PollUntilContextCancel and node-ready sleep) with named constants (e.g. subnetReadyPollInterval, nodeReadyPollInterval).
- [ ] **Consistency**: Fix alignment of constant clusterNetworkPolicyKey (line 65: inconsistent spacing before =).
- [ ] **Extensibility**: Adding a new resource type requires changes in Run() in 5+ places (informer, cacheSyncs, AddEventHandler, shutdown, startWorkers). Consider a table-driven or registry pattern (resource kind -> synced, handlers, queues, worker) to centralize and reduce omission errors.

---

## ./pkg/controller/controller_test.go

- [ ] **Naming**: fakeController struct has field `fakeController *Controller` (same name as outer type). Rename the field to `ctrl` or `controller` to avoid confusion and improve readability.
- [ ] **DRY**: kubeInformerFactory, nadInformerFactory, and kubeovnInformerFactory all use the same TweakListOptions (Watch: true, AllowWatchBookmarks: true). Extract a shared list-options helper or constant to reduce duplication.
- [ ] **Readability**: Fix subtest name "some subnet are not ready" → "some subnets are not ready" (plural and subject-verb agreement).
- [ ] **Structure**: newFakeControllerWithOptions is long (~130 lines). Consider extracting buildKubeObjects(opts), buildInformerFactories(...), and buildController(...) to shorten the function and clarify the setup flow.

---

## ./pkg/controller/deployment.go

- [ ] **Robustness**: In `enqueueAddDeployment`, use safe type assertion: `deploy, ok := obj.(*appsv1.Deployment); if !ok { return }` to avoid panic if informer passes wrong type.
- [ ] **Readability**: Add a short package-level or file-level comment describing that this file enqueues VpcEgressGateway when a Deployment (owned by it) is added or updated.

---

## ./pkg/controller/cluster_network_policy_test.go

- [ ] **Readability/Consistency**: Use named fields in test table structs for `TestGetCnpName`, `TestGetCnpACLAction`, `TestGetCnpACLTier`, `TestGetCnpDomainsNames`, `TestHasCnpDomainNames`, and `TestGetCnpAclPriority` (e.g. `name`, `arg`, `ret` or `name`, `cnp`, `result`) instead of positional struct literals, to match other tests in the file (e.g. `TestGetCnpPortGroupName`) and avoid confusion.
- [ ] **Readability**: Fix test case name "no ingress or egressrule" → "no ingress or egress rule" (line 558).
- [ ] **Readability**: Fix test case name "start with digital" → "start with digit" (line 817).
- [ ] **Readability**: Rename table field `error` to `wantErr` or `expectError` in `TestValidateCnpConfig`, `TestCheckNetworkAndDomainRules`, and `TestCheckCnpPriorities` to avoid shadowing the built-in `error` type and clarify intent.
- [ ] **Test isolation**: In `TestDeleteCnpPriorityMapEntries`, the "unknown tier" subtest repopulates `ctrl` maps after "admin tier" and "base tier" have cleared them; tests share the same controller and are order-dependent. Consider creating a fresh `newFakeController(t)` for the "unknown tier" subtest so state is isolated and order-independent.

---

## ./pkg/controller/dns_name_resolver.go

- [ ] **Robustness**: In `enqueueAddDNSNameResolver`, use safe type assertion: `dnsNameResolver, ok := obj.(*kubeovnv1.DNSNameResolver); if !ok { klog.Warningf(...); return }` to avoid panic if informer passes wrong type.
- [ ] **Robustness**: In `enqueueUpdateDNSNameResolver`, use safe type assertions for `oldObj` and `newObj` instead of direct casts.
- [ ] **Naming/Consistency**: `createOrUpdateDNSNameResolver` and `deleteDNSNameResolver` take a first parameter named `anpName` but are used for any network policy (ANP/CNP) via `reconcileDNSNameResolversForNP`. Rename to `npName` and consider log text "for NP %s" instead of "in ANP %s" when the CR is shared by ANP and CNP.
- [ ] **Naming**: `generateDNSNameResolverName` produces the prefix "anp-" but the name is used for both ANP and CNP. Consider a neutral prefix (e.g. "dnsresolver-%s-%s" or "np-%s-%s") or add a comment that "anp-" is used for all policy types.
- [ ] **Maintainability**: Add a short comment on `reconcileDNSNameResolversForANP` that it is used for both ANP and CNP (callers pass policy name and label key) to align with cluster_network_policy.go usage and avoid confusion.

---

## ./pkg/controller/endpoint_slice.go

- [ ] **Robustness**: In `enqueueAddEndpointSlice` and `enqueueUpdateEndpointSlice`, use safe type assertion: `ep, ok := obj.(*discoveryv1.EndpointSlice); if !ok { return }` (and similarly for oldObj/newObj in update) to avoid panic if informer passes wrong type.
- [ ] **Error handling**: In `enqueueStaticEndpointUpdateInNamespace`, when `findStaticEndpointSlicesInNamespace` returns an error, the code logs but does not return; callers cannot detect failure. Return the error after logging, or document that enqueue is best-effort.
- [ ] **Performance**: `findEndpointSlicesForServices` is O(services × endpointSlices). Build a map[serviceName][]*EndpointSlice once from eps (group by `getServiceForEndpointSlice(ep)`), then for each service look up; reduces to O(eps) + O(services).
- [ ] **Performance**: `findVpcAndSubnetWithNoTargets` has triple nested loop O(slices × endpoints × pods). For many pods consider building a set of endpoint addresses first, then iterate pods once and match by address to reduce work.
- [ ] **Readability/Structure**: `handleUpdateEndpointSlice` is very long (~200 lines). Extract: (1) computation of lbVips, ignoreHealthCheck, isPreferLocalBackend into a helper e.g. `getServiceLBContext(svc) (...)`; (2) the inner lbVips×ports loop into `reconcileLoadBalancerVips(...)` so the main flow is a clear sequence of steps.
- [ ] **Maintainability**: In `getHealthCheckVip`, the `time.Sleep(1 * time.Second)` after Create with TODO "WATCH VIP" is fragile. Replace with a wait/poll loop with timeout and max retries, or document why a single sleep is acceptable.
- [ ] **DRY**: `findVpcAndSubnetWithTargets` and `findVpcAndSubnetWithNoTargets` share the same pattern (iterate slices/endpoints, resolve vpc/subnet, set if empty, return when both set). Extract a common helper parameterized by how to resolve (vpc, subnet) for an endpoint to reduce duplication.
- [ ] **Readability**: The assignment `if svcVpc = svc.Annotations[util.VpcAnnotation]; svcVpc != vpcName` combines assignment and condition; consider assigning on a separate line then `if svcVpc != vpcName` for clarity.
- [ ] **Naming**: Fix typo in error message in `findVpcAndSubnetWithTargets` (line ~382): "couldn't retrieve get subnet/vpc" → "couldn't retrieve subnet/vpc".
- [ ] **Robustness**: In `getEndpointBackend`, when matching port by name, `port.Port` can be nil; dereferencing `*port.Port` may panic. Add nil check or use a safe default.
- [ ] **Consistency**: Consider named constants for magic values (e.g. health check VIP wait duration 1s) to align with other controller files.

---

## ./pkg/controller/endpoint_slice_test.go

- [ ] **Correctness**: In `TestServiceHealthChecksDisabled`, the failure message uses `findServiceKey()` but the test is for `serviceHealthChecksDisabled()`; change to `serviceHealthChecksDisabled() = %t` for correct diagnostics.

---

## ./pkg/controller/exporter.go

- [ ] **DRY**: `exportSubnetIPAMInfo` and `exportSubnetIPAssignedInfo` share the same pattern: get `ipamSubnet` from `c.ipam.Subnets[subnet.Name]`, check `!ok` and return with `klog.Errorf`, then `RLock`/`defer RUnlock`. Extract a helper e.g. `withIPAMSubnet(c *Controller, subnet *kubeovnv1.Subnet, fn func(*ipam.Subnet)) bool` to reduce duplication and keep locking in one place.
- [ ] **DRY**: In `exportSubnetIPAMInfo`, the three switch cases (IPv4, IPv6, Dual) each call `metricSubnetIPAMInfo.WithLabelValues(...).Set(1)` with similar label sets. Extract a small helper e.g. `setIPAMInfoMetric(subnetName, cidr, free, reserved, available, using string)` to avoid repeating the long argument list and make adding labels easier.
- [ ] **Readability**: In `exportCentralizedSubnetInfo`, line 88 has a long `WithLabelValues` call; break into multiple lines or extract a helper for label values to improve readability.
- [ ] **Readability**: In `exportSubnetAvailableIPsGauge` and `exportSubnetUsedIPsGauge`, the protocol-to-value logic (IPv4 vs IPv6 vs default) is repeated; consider a tiny helper e.g. `getAvailableIPs(subnet)` / `getUsedIPs(subnet)` returning float64 to centralize protocol handling and make the gauge setters a single line.
- [ ] **Performance** (optional): In `exportSubnetIPAssignedInfo`, iterating all V4IPToPod/V6IPToPod and calling `WithLabelValues().Set(1)` per IP creates many time series for large subnets. If only aggregate counts are needed, consider exporting a single gauge per subnet (e.g. `metricSubnetIPCount`) instead of per-IP metrics; otherwise document that per-IP metrics are intentional for observability.

---

## ./pkg/controller/external_gw.go

- [ ] **Naming**: Variable `chassises` uses non-standard plural; consider `chassisList` or `chassisNames` for clarity (chassis is often used as both singular and plural).
- [ ] **Readability**: Extract the complex condition at lines 71–73 `(lastExGwCM["type"] == ... && cm.Data["type"] == ...) || (lastExGwCM != nil && !reflect.DeepEqual(...))` into a helper e.g. `externalGwConfigRequiresRemove(lastExGwCM, newData map[string]string) bool` with a short comment to clarify intent.
- [ ] **Readability**: Extract the "no config change" check at lines 67–69 into a helper e.g. `externalGwConfigUnchanged(cm *corev1.ConfigMap, lastExGwCM map[string]string) bool` so `resyncExternalGateway` reads as a clear sequence of decisions.
- [ ] **Structure**: `resyncExternalGateway` is long (~65 lines). Consider extracting: `shouldDisableExternalGw(cm, err) bool`, `removeAndDisableExternalGw(c) error`, and `shouldRemoveBeforeRecreate(lastExGwCM, newData map[string]string) bool` so the main function is a short sequence of conditionals and actions.
- [ ] **Maintainability**: ConfigMap keys `"enable-external-gw"`, `"type"`, `"external-gw-nodes"`, `"nic-ip"`, `"nic-mac"` are magic strings. Define named constants (e.g. in const.go or this file) to avoid typos and document valid keys.
- [ ] **Readability**: In `createDefaultVpcLrpEip`, use positive logic: e.g. "if cachedEip exists and has V4Ip and MacAddress, use them; else acquire and create" instead of `needCreateEip` and `if !needCreateEip` to reduce double negative.

---

## ./pkg/controller/external_vpc.go

- [ ] **Correctness**: In the create-new-VPC block (lines 63–66), the loop appends logical switch names to `vpc.Status.Subnets`, then `vpc.Status.Subnets = []string{}` immediately clears them, so newly created external VPCs always get empty Subnets. Remove the erroneous assignment after the loop (or initialize Subnets before the loop and do not clear after).
- [ ] **Error handling / Consistency**: In the create loop (lines 52–78), on Create or UpdateStatus failure the code returns immediately, so remaining routers in `logicalRouters` are not processed. Consider using `continue` and logging so other external VPCs can still be created, consistent with the update branch which uses `continue` on failure.
- [ ] **DRY**: The pattern of setting `vpc.Status.Subnets` from logical switch names appears in both the update branch (lines 32–35) and the create branch (63–65). Extract a small helper e.g. `subnetNamesFromLogicalSwitches(logicalRouter) []string` and use it in both places.
- [ ] **Readability / Structure**: The nested loop in `getNonKubeovnRouterStatus` (lines 111–136) that resolves logical switches per port is complex. Extract a helper e.g. `resolveLogicalSwitchesForRouter(c *Controller, lr *util.LogicalRouter) error` to make the main function easier to follow.
- [ ] **Naming**: In `getNonKubeovnRouterStatus`, `tmpRouter` is used to allow appending while iterating; use a more descriptive name e.g. `resolvedRouter` or `routerWithSwitches`.
- [ ] **Structure**: `syncExternalVpc` does three things: get routers and VPCs, update/delete existing external VPCs, create new VPCs for remaining routers. Consider extracting `reconcileExistingExternalVpcs(vpcs, logicalRouters)` and `createMissingExternalVpcs(logicalRouters)` for clarity.

---

## ./pkg/controller/gc.go

- [ ] **Correctness**: In `gcChassis` (lines 654–657), when `ListChassis()` returns an error the code only logs it and does not return. Execution then uses `*chassises`, which may be nil and can panic. Return the error when `ListChassis()` fails.
- [ ] **Correctness**: In `gcVip` (line 309), after releasing one VIP the function returns `nil` inside the loop, so at most one VIP is GC'd per run. Use `continue` to process all vips, or document that only one VIP is cleaned per run by design.
- [ ] **Naming**: Variable `chassises` (line 653) uses non-standard plural; consider `chassisList` or `chassisSlice` for clarity.
- [ ] **Error message**: In `markAndCleanLSP` (line 394), the log says "failed to list ip" but the call is `c.podsLister.List`; change to "failed to list pods".
- [ ] **Performance**: In `gcRoutePolicy` (lines 611–616), `podIPs` is built as a slice and `slices.Contains(podIPs, srcIP)` is used per policy (O(policies × podIPs)). Use `strset` or `set.Set[string]` for O(1) lookup and O(policies) total.
- [ ] **Performance**: In `gcVPCDNS` and `gcLbSvcPods`, the "canFind" pattern loops over vds/slrs for each dep (O(deps × vds)). Build a set of `genVpcDNSDpName(vd.Name)` once, then O(deps) lookup. Same for SwitchLBRules.
- [ ] **DRY**: In `gcLoadBalancer`, the six repeated blocks `if err = removeVip(xxxLb, xxxVips); err != nil { ... return err }` can be a loop over a slice of `struct{ lbName string; vips *strset.Set }` to reduce repetition.
- [ ] **DRY**: In `checkIPOwnerExists`, the pattern "if err != nil && k8serrors.IsNotFound(err) { return false, nil }; return true, err" repeats in five branches. Extract a small helper e.g. `ownerNotFound(err error) (bool, error)` to reduce duplication.
- [ ] **Maintainability**: Package-level var `lastNoPodLSP` is global mutable state; consider attaching to Controller struct so GC state is per-controller and tests can isolate it.
- [ ] **Readability**: In `gcLogicalSwitch`, extract the DHCP options GC block (list options, filter by subnetNames, delete by UUIDs) to a helper e.g. `gcDHCPOptions(c, subnetNames)` for single responsibility.
- [ ] **Readability**: In `gcNetworkPolicy`, the condition for distributed overlay subnets (line ~576) is long; extract to a helper e.g. `isDistributedSubnetForNP(subnet, config) bool`.
- [ ] **Structure**: The closure `removeVip` inside `gcLoadBalancer` could be a Controller method or package helper for testability and reuse.
- [ ] **Documentation**: In `checkIPOwnerExists`, the final `return true, nil` is the default for unrecognized PodType; add a short comment so future maintainers know the intent.
- [ ] **Readability**: In `gc()`, add a one-line comment that the order of gc steps matters (e.g. LSP gc is deferred to markAndCleanLSP).
- [ ] **Maintainability**: When `deleteStaticRouteFromVpc` or `NatExists` fails in `gcStaticRoute`, the code continues with other routes. Document that static route gc is best-effort per route, or align with other gc functions that return on first error.
- [ ] **Performance**: In `gcLogicalSwitch`, pre-allocate `uuidToDeleteList` with `make([]string, 0, len(dhcpOptions))` when building the delete list.
- [ ] **Readability/Structure**: `getVMLsps` is long and mixes default multus, NAD annotation, and network.Multus.NetworkName. Consider extracting a helper that returns LSP names for a single VM to clarify logic.
- [ ] **Testability**: The inline `removeVip` in `gcLoadBalancer` could be a package-level helper or method for unit testing.

---

## ./pkg/controller/gc_test.go

- [ ] **Coverage**: Only `logicalRouterPortFilter` is tested. Consider adding unit tests for `checkIPOwnerExists` (with mocked listers) to improve coverage of GC logic.
- [ ] **Correctness**: Add a test case for an LRP with `Peer == nil` or empty Peer; the filter should return false (do not delete). Current test only covers "in except set" vs "not in except set with Peer set".

---

## ./pkg/controller/init.go

- [ ] **Naming**: Fix typo "extrenalID" → "externalID" in klog at line 634 (`batchMigrateNodeRoute`).
- [ ] **Readability**: In `initDefaultLogicalSwitch` (line 128), when `err == nil` the condition `subnet != nil` is redundant (Get returns non-nil when err is nil). Remove for clarity.
- [ ] **Structure**: `InitIPAM` is very long (~250 lines). Consider extracting helpers e.g. `initIPAMFromSubnets`, `initIPAMFromIPCRs`, `initIPAMFromPods`, `initIPAMFromVips`, `initIPAMFromEips`, `initIPAMFromNodes` to improve readability and testability.
- [ ] **DRY**: In `initLoadBalancer`, the six `initLB` calls follow the same pattern. Use a slice of `struct{ name, protocol string; sessionAffinity bool }` and loop to reduce repetition.
- [ ] **DRY**: In `syncFinalizers`, the 14 `syncXFinalizer(cl)` calls are repetitive. Use a table-driven approach e.g. `[]struct{ name string; fn func(client.Client) error }` and loop so adding new resource types is a single entry.
- [ ] **Dead code**: In `initNodeChassis`, `chassisNodes` is built from `chassises` but never used; only `nodes` are iterated and `UpdateChassisTag` is called. Remove the unused map and possibly the `GetKubeOvnChassisses()` call if not needed, or use `chassisNodes` for filtering/skipping.
- [ ] **Readability**: In `initDefaultProviderNetwork`, the defer that patches nodes closes over `err`; the logic (skip patching only when Create failed) could be clearer with a named result or explicit variable e.g. `createErr := ...; defer func() { if createErr != nil { return }; ... }()`.
- [ ] **Correctness**: In `InitIPAM` node annotation block (lines 404–407), the condition `v4IP != "" && v6IP != ""` updates the node annotation only for dual-stack. For single-stack (only v4IP or only v6IP) the annotation is not updated. Clarify intent or fix for single-stack.
- [ ] **Readability**: In `syncSubnetCR` (line 471), the condition `!subnet.Spec.EnableEcmp && subnet.Spec.EnableEcmp != c.config.EnableEcmp` can be simplified to `!subnet.Spec.EnableEcmp && c.config.EnableEcmp`.
- [ ] **Maintainability**: Fix "Depreciated" → "Deprecated" in finalizer name if the constant lives in this package or document the typo in util for a global fix.

---

## ./pkg/controller/inspection.go

- [ ] **Error message**: In `inspectPod`, log says "failed to list ip" but the call is `c.podsLister.List`; change to "failed to list pods".
- [ ] **Readability**: The condition at lines 56–57 (allocated-but-not-routed) is long; extract to a helper e.g. `podNeedsRoutedReconcile(pod *v1.Pod, providerName string) bool` to clarify intent.
- [ ] **Readability**: Use early continue when `podNet.Type == providerTypeIPAM` to reduce nesting in the inner loop.
- [ ] **DRY**: Annotation key `fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)` (and Routed) is built multiple times per iteration; assign to local variables at the start of the inner loop.
- [ ] **Naming**: `oriPod` is cryptic; consider `listPod` or document that "ori" means original from the lister.
- [ ] **Structure**: Consider extracting the per-pod logic into `inspectPodNetworks(pod *v1.Pod, key string) (requeued bool, err error)` so the main loop is a thin iteration and the logic is easier to unit test.
- [ ] **Naming**: `filterSubnets` filters by allocated annotation; consider a more descriptive name e.g. `netsWithAllocatedIP` or add a one-line doc comment.

---

## ./pkg/controller/ippool.go

- [ ] **Robustness**: In `enqueueAddIPPool`, use safe type assertion: `ippool, ok := obj.(*kubeovnv1.IPPool); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic if informer passes wrong type; align with `enqueueDeleteIPPool` which uses switch and type check.
- [ ] **Robustness**: In `enqueueUpdateIPPool`, use safe type assertion for `oldObj` and `newObj` (e.g. `oldIPPool, ok := oldObj.(*kubeovnv1.IPPool); if !ok { return }`) instead of direct cast to avoid panic.
- [ ] **Naming**: Fix typo `DepreciatedFinalizerName` → `DeprecatedFinalizerName` in util and update call site in `handleDelIPPoolFinalizer`; align with other controller files.
- [ ] **DRY** (optional): In `handleAddOrUpdateIPPool`, the pattern "on error, patch status condition then return err" repeats twice (reconcileIPPoolAddressSet and AddOrUpdateIPPool); extract a small helper e.g. `reportIPPoolError(c *Controller, ippool *kubeovnv1.IPPool, reason, errMsg string) error` to reduce duplication.
- [ ] **Readability**: In `syncIPPoolFinalizer`, the callback returns `ippools.Items[i].DeepCopy(), ippools.Items[i].DeepCopy()`; assign to a variable e.g. `item := ippools.Items[i].DeepCopy()` and return `(item, item)` for clarity.

---

## ./pkg/controller/ippool_test.go

- [ ] **Structure**: Tests in this file exercise only `pkg/util` functions (ExpandIPPoolAddresses, ExpandIPPoolAddressesForOVN, CanonicalizeIPPoolEntries, etc.), not controller/ippool logic. Consider moving these tests to `pkg/util/ippool_test.go` so util package owns its unit tests and controller package can add tests for handleAddOrUpdateIPPool, reconcileIPPoolAddressSet, etc., for better discoverability and maintainability.
- [ ] **DRY** (optional): Subtest "Unaligned IPv4 range" in `TestExpandIPPoolAddressesRangeEdgeCases` duplicates the same input/expected as `TestExpandIPPoolAddressesRange` (10.0.0.1..10.0.0.5); remove or merge to avoid redundancy.

---

## ./pkg/controller/kubevirt.go

- [ ] **Robustness**: In `enqueueAddVMIMigration`, use safe type assertion: `migration, ok := obj.(*kubevirtv1.VirtualMachineInstanceMigration); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic; align with `enqueueDeleteVM` which uses switch.
- [ ] **Robustness**: In `enqueueUpdateVMIMigration`, use safe type assertion for `oldObj` and `newObj` instead of direct cast.
- [ ] **Naming**: In `enqueueDeleteVM`, log message says "enqueue add VM" (line 56); change to "enqueue delete VM" for consistency with function and queue name.
- [ ] **Readability**: Fix log message typos "migrated succeed" → "migration succeeded" (line 190) and "migrated fail" → "migration failed" (line 198).
- [ ] **Readability**: Replace magic number `10 * time.Second` in `StartKubevirtInformerFactory` with a named constant (e.g. `kubevirtCRDCheckInterval`) for tuning and documentation.
- [ ] **Maintainability**: In `handleAddOrUpdateVMIMigration`, when `vmiMigration.Status.Phase` is not one of MigrationScheduling, MigrationSucceeded, or MigrationFailed, the function returns nil without logging. Add a default case that logs "unknown migration phase %s" for observability.
- [ ] **Structure**: `handleAddOrUpdateVMIMigration` is long (~110 lines). Consider extracting phase handlers e.g. `handleMigrationSchedulingPhase(c, vmiMigration, vmi, portName, srcNodeName, targetNodeName) error`, `handleMigrationSucceededPhase(...)`, `handleMigrationFailedPhase(...)` so the switch is a thin dispatcher and each phase is testable.
- [ ] **Readability** (optional): In `handleDeleteVM`, `vmKey` is identical to `key` (both namespace/name). Use `key` directly in ListNormalLogicalSwitchPorts and logs to avoid redundancy, or keep vmKey and add a one-line comment that it equals key for clarity.

---

## ./pkg/controller/ip.go

- [ ] **Error message**: In `handleAddReservedIP`, log says "failed to list logical switch ports" but the call is `GetLogicalSwitchPort` (single port); change to "failed to get logical switch port".
- [ ] **Robustness**: In `enqueueAddIP` and `enqueueUpdateIP`, use safe type assertion: `ipObj, ok := obj.(*kubeovnv1.IP); if !ok { return }` (and similarly for oldObj/newObj in enqueueUpdateIP) to avoid panic if informer passes wrong type.
- [ ] **DRY**: In `enqueueUpdateIP`, the six "X can not change" blocks (Subnet, Namespace, PodName, PodType, MacAddress, V4IPAddress) are nearly identical; use a table-driven check e.g. `[]struct{ name string; old, new string }` and loop to reduce duplication.
- [ ] **Naming**: Comment "migrate depreciated finalizer" (line 292) should be "deprecated"; align with util constant name if it is `DepreciatedFinalizerName` (consider renaming to DeprecatedFinalizerName in util).
- [ ] **Readability**: `util.U2OInterconnName[0:19]` appears multiple times; consider a named constant e.g. `u2oInterconnPrefix` in util or this file.
- [ ] **Structure**: `createOrUpdateIPCR` is long (~120 lines); consider extracting `resolveIPCRKeyAndName(...) (key, ipName string)`, and optionally the create/update branches into helpers for readability.
- [ ] **Structure**: In `handleUpdateIP`, the deletion branch (~40 lines) could be extracted to `handleIPDeletion(cachedIP *kubeovnv1.IP) error` to shorten the main function.
- [ ] **Readability**: In `handleAddReservedIP`, the label patch block (lines 206–222) could be extracted to e.g. `patchIPReservedLabel(c *Controller, ip *kubeovnv1.IP) error` to clarify the main flow.

---

## ./pkg/controller/namespace.go

- [ ] **Robustness**: In `enqueueAddNamespace`, use safe type assertion: `ns, ok := obj.(*v1.Namespace); if !ok { klog.Warningf("unexpected type: %T", obj); return }` before using `obj.(*v1.Namespace)` (lines 21, 26). Align with `enqueueDeleteNamespace` which uses a type switch.
- [ ] **Robustness**: In `enqueueUpdateNamespace`, use safe type assertions for `oldObj` and `newObj` (e.g. `oldNs, ok := oldObj.(*v1.Namespace); if !ok { return }`) instead of direct casts to avoid panic if informer passes wrong type.
- [ ] **DRY**: Subnet-matching logic (subnets that bind namespace via Namespaces or NamespaceSelectors) is duplicated between `handleAddNamespace` (lines 126–172) and `getNsExpectSubnets` (lines 245–264). Extract a shared helper e.g. `subnetsForNamespace(c *Controller, ns *v1.Namespace) ([]string, error)` or a struct that returns names + cidrs + excludeIps, and use it in both to avoid divergence.
- [ ] **Readability**: Rename `lss` to `subnetNames` or `logicalSwitchNames` and `ls` to `defaultLogicalSwitch` for clarity.
- [ ] **Naming**: Fix typo "namespaceLabelSeletcor" → "namespaceLabelSelector" in comment (line 134).
- [ ] **Performance**: In `getNsExpectSubnets`, `slices.Contains(expectSubnets, subnet.Name)` is O(n) per subnet; use a set (e.g. `strset.Set`) to deduplicate in O(1) per add and avoid O(subnets²) total.
- [ ] **Readability**: The condition at lines 213–217 (compare annotations with expected) is long; extract to a helper e.g. `namespaceAnnotationsMatchExpected(ns *v1.Namespace, subnetNames, cidrs, excludeIps, ipPools []string) bool`.
- [ ] **Structure**: `handleAddNamespace` is long (~130 lines). Consider extracting: (1) `getMatchedSubnetsForNamespace(c, namespace) (subnetNames, cidrs, excludeIps []string, err error)` for the subnet-binding and VPC default-subnet logic, (2) `getMatchedIPPoolsForNamespace(c, key) ([]string, error)` for the IPPool loop, so the main function is a short sequence of steps.

---

## ./pkg/controller/net_metrics.go

- [ ] **Naming**: Metric name "subnet_ip_assign_info" uses "assign" while Help text says "assigned info"; consider renaming to "subnet_ip_assigned_info" for consistency with the help text.
- [ ] **DRY**: `registerMetrics()` has five repeated `metrics.Registry.MustRegister(...)` calls; use a slice of collectors and loop to reduce repetition and omission risk when adding new metrics.

---

## ./pkg/controller/network_attachment.go

- [ ] **Readability**: Replace magic number `10 * time.Second` (line 66) with a named constant (e.g. `netAttachCRDCheckInterval`) for tuning and documentation.

---

## ./pkg/controller/node.go

- [ ] **Robustness**: In `enqueueAddNode`, use safe type assertion: `node, ok := obj.(*v1.Node); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic if informer passes wrong type; align with `enqueueDeleteNode`.
- [ ] **Robustness**: In `enqueueUpdateNode`, use safe type assertions for `oldObj` and `newObj` (e.g. `oldNode, ok := oldObj.(*v1.Node); if !ok { return }`) instead of direct casts.
- [ ] **Readability**: In `retryDelDupChassis`, when `err == nil` the code does `return err`; use `return nil` on success for clarity.
- [ ] **Correctness**: In `addNodeGatewayStaticRoute`, when `c.vpcsLister.Get` returns an error, `vpc` may be nil and `vpc.Spec.StaticRoutes` can panic. Check `err != nil` first and skip the block or return; only access `vpc.Spec` when `vpc != nil`.
- [ ] **Naming**: Fix typo `getPolicyRouteParas` → `getPolicyRouteParams` (or keep name but fix doc: "Paras" → "Params").
- [ ] **Structure**: `handleAddNode` is very long (~270 lines). Consider extracting: node IP allocation (static/random + LSP cleanup), policy route per-IP loop, annotation patch + IP CR creation, distributed subnet port groups, and chassis update into helpers (e.g. `allocateNodeIP`, `addPolicyRoutesForNodeIPs`, `patchNodeAnnotationsAndIPCR`, `ensureDistributedSubnetPortGroupsForNode`) so the main flow is a short sequence of steps.
- [ ] **DRY**: The distributed-subnet filter `(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != c.config.ClusterRouter || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWDistributedType` appears in `handleAddNode`, `syncDistributedSubnetRoutes`, `deletePolicyRouteForNode`, and `addPolicyRouteForCentralizedSubnetOnNode`. Extract a helper e.g. `isDistributedSubnetInDefaultVpc(subnet *kubeovnv1.Subnet, nodeSwitch, clusterRouter string) bool` to avoid duplication and drift.
- [ ] **DRY**: The centralized-subnet filter (Vlan, Vpc, NodeSwitch, GatewayType == GWCentralizedType) is repeated in `handleUpdateNode`, `deletePolicyRouteForNode`, `addPolicyRouteForCentralizedSubnetOnNode`. Extract a helper e.g. `isCentralizedSubnetInDefaultVpc(subnet *kubeovnv1.Subnet, config) bool` for consistency.
- [ ] **Readability**: Replace magic numbers in `checkSubnetGatewayNode` (ping count 5, timeout count*time.Second, interval 1*time.Second) with named constants (e.g. `gatewayPingCount`, `gatewayPingTimeout`, `gatewayPingInterval`) for tuning and documentation.
- [ ] **Readability**: In `updateProviderNetworkForNodeDeletion`, the swap `pn, newPn = newPn, nil` (line 383) is subtle; add a one-line comment that pn is updated to the new copy so subsequent spec update uses the latest resource version.
- [ ] **Structure**: `checkSubnetGatewayNode` is long (~100 lines) with deep nesting (subnet → cidrBlock → nodes → ip, ping, policy update). Extract e.g. `reconcileEcmpPolicyRouteForSubnet(c, subnet, nodes) error` or per-node `updateEcmpNextHopsForNode(c, subnet, node, ...)` to shorten and make testable.
- [ ] **Consistency**: In `handleUpdateNode`, error message says "failed to get subnets"; use "failed to list subnets" to align with other controller files.

---

## ./pkg/controller/node_test.go

- [ ] **Consistency**: Consider adding `t.Parallel()` inside the subtest loop (in `t.Run(tt.name, func(t *testing.T) { ... })`) so subtests run in parallel, aligning with other controller tests.

---

## ./pkg/controller/network_policy.go

- [ ] **Robustness**: In `enqueueAddNp`, use safe type assertion: `np, ok := obj.(*netv1.NetworkPolicy); if !ok { return }` before using the object. In `enqueueUpdateNp`, use safe type assertions for `oldObj` and `newObj` instead of direct casts to avoid panic if informer passes wrong type.
- [ ] **Error message**: In egress block (line 339), log says "failed to set default block exceptions for **ingress** acl" but the context is egress; change to "egress acl".
- [ ] **DRY**: Ingress and egress branches in `handleUpdateNp` are ~200 lines each with nearly identical structure (block ACL ops, default exceptions, loop over protocols/rules, create AS, ACL ops, Transact, SetACLLog, GC stale address sets). Extract a helper e.g. `reconcileDirectionACLs(c, key, np, pgName, direction string, protocolSet *strset.Set, ...) error` or split into `handleUpdateNpIngress` and `handleUpdateNpEgress` to reduce duplication.
- [ ] **DRY**: In `handleDeleteNp`, the three `DeleteAddressSets` calls (service, ingress, egress) differ only by direction; loop over `[]string{"service","ingress","egress"}` to avoid repetition.
- [ ] **Structure**: `handleUpdateNp` is very long (~380 lines). Extract building of npName, pgName, prefixes and CreatePortGroup into a helper, and the ingress/egress blocks into separate functions so the main flow is a short sequence.
- [ ] **Maintainability**: In `isNamespaceMatchNetworkPolicy`, `ns.Labels = map[string]string{}` when nil mutates the caller’s namespace object; use a local variable (e.g. `labelsToCheck := ns.Labels; if labelsToCheck == nil { labelsToCheck = map[string]string{} }`) and pass that to `Matches` to avoid mutating shared objects.
- [ ] **Robustness**: In address set GC loops (lines 296, 352), `idx, _ := strconv.Atoi(idxStr)` ignores parse errors; validate or handle non-numeric idxStr to avoid wrong deletions.
- [ ] **Naming**: In `svcMatchPods`, variable `matchSvcs` holds cluster IP strings, not services; consider renaming to `matchSvcIPs` for clarity.

---

## ./pkg/controller/ovn_dnat.go

- [ ] **Robustness**: In `enqueueAddOvnDnatRule`, use safe type assertion: `dnat, ok := obj.(*kubeovnv1.OvnDnatRule); if !ok { return }` instead of direct cast to avoid panic if informer passes wrong type.
- [ ] **Robustness**: In `enqueueUpdateOvnDnatRule`, use safe type assertions for `newObj` (line 30) and `oldObj` (line 41) instead of direct casts.
- [ ] **Correctness/Readability**: In `handleAddOvnDnatRule`, the check at lines 181–184 (`if v4Eip == "" && v6Eip == ""`) is redundant — same condition was already validated at 139–143. Remove duplicate check.
- [ ] **DRY**: The block that resolves internal IPs from VIP or IP CR (lines 149–175 in `handleAddOvnDnatRule`) is duplicated in `handleUpdateOvnDnatRule` (377–403). Extract a helper e.g. `resolveOvnDnatInternalIPs(c *Controller, dnat *kubeovnv1.OvnDnatRule) (internalV4Ip, internalV6Ip, subnetName, vpcName string, err error)`.
- [ ] **DRY**: EIP validation (eipName empty, GetOvnEip, OvnEipTypeLSP check) is duplicated in `handleAddOvnDnatRule` and `handleUpdateOvnDnatRule`. Extract e.g. `validateOvnDnatEip(c *Controller, key, eipName string) (*kubeovnv1.OvnEip, error)`.
- [ ] **DRY**: The V4/V6 `DelDnatRule` call pair appears in `handleDelOvnDnatRule` (264–276) and in `handleUpdateOvnDnatRule` deletion branch (316–328). Extract a helper e.g. `deleteOvnDnatRulesFromOVN(c *Controller, cachedDnat *kubeovnv1.OvnDnatRule) error`.
- [ ] **Correctness**: In `handleUpdateOvnDnatRule`, the "not support change" block has duplicate checks: InternalPort at 409–413 and again at 431–434; ExternalPort at 401–405 and again at 435–439. Remove redundant checks (431–439).
- [ ] **Error handling**: In `patchOvnDnatAnnotations` (line 374), `raw, _ := json.Marshal(dnat.Annotations)` ignores error; check and return err on Marshal failure.
- [ ] **Error handling**: In `patchOvnDnatStatus` (line 396), `raw, _ := json.Marshal(dnat.Labels)` ignores error; check and return err.
- [ ] **Naming**: In `patchOvnDnatAnnotations` and `patchOvnDnatStatus`, variable `oriDnat` is cryptic; use `cachedDnat` or `currentDnat` for consistency with other handlers.
- [ ] **Naming**: Comment "migrate depreciated finalizer" (line 451) → "deprecated"; align with util constant (consider renaming `DepreciatedFinalizerName` to `DeprecatedFinalizerName` in util).
- [ ] **Structure**: `handleAddOvnDnatRule` is long (~150 lines). Consider extracting: (1) `resolveOvnDnatEipAndInternalIPs(...)` returning v4Eip, v6Eip, internalV4, internalV6, vpcName; (2) port/protocol/duplicate validation into `validateOvnDnatSpec(...)` so the main function is a clear sequence of steps.

---

## ./pkg/controller/ovn_eip.go

- [ ] **Robustness**: In `enqueueAddOvnEip`, use safe type assertion: `eip, ok := obj.(*kubeovnv1.OvnEip); if !ok { klog.Warningf("unexpected type: %T", obj); return }` instead of direct cast to avoid panic if informer passes wrong type; align with `enqueueDelOvnEip` which uses type switch.
- [ ] **Robustness**: In `enqueueUpdateOvnEip`, use safe type assertions for `newObj` (line 30) and `oldObj` (line 41) instead of direct casts.
- [ ] **Readability/Correctness**: In `createOrUpdateOvnEipCR` (lines 373–391), the conditions `ovnEip.Status.V4Ip == "" && ovnEip.Status.V4Ip != v4ip` are redundant (when Status.V4Ip is empty, `!= v4ip` is always true unless v4ip is ""). Write as `ovnEip.Status.V4Ip == "" && v4ip != ""` for clarity; same for V6Ip, MacAddress, Type.
- [ ] **DRY**: The block that deletes LSP or LRP (handleUpdateOvnEip lines 183–195 and handleDelOvnEip 273–284) is duplicated. Extract a helper e.g. `deleteOvnEipResources(c *Controller, eip *kubeovnv1.OvnEip) error` that deletes LSP if type LSP and LRP if type LRP.
- [ ] **Error handling**: In `natLabelAndAnnoOvnEip` (lines 384, 402), `raw, _ := json.Marshal(eip.Labels)` and `json.Marshal(eip.Annotations)` ignore errors; check and return err on Marshal failure.
- [ ] **Readability**: At end of `natLabelAndAnnoOvnEip`, use `return nil` instead of `return err` for the success path so intent is clear (err at that point is from the initial Get).
- [ ] **Naming**: Comment "migrate depreciated finalizer" (line 413) → "deprecated"; align with util constant (consider renaming `DepreciatedFinalizerName` to `DeprecatedFinalizerName` in util).
- [ ] **Readability**: Replace magic numbers with named constants: `time.Sleep(1 * time.Second)` (line 332) for cache wait after Create; `300*time.Millisecond` (lines 408, 433) for subnet status queue delay.
- [ ] **DRY**: In `handleUpdateOvnEip`, the "not support change" validation block (lines 220–245) has five nearly identical blocks (V4Ip, V6Ip uppercase, V6Ip, MacAddress, Type). Consider a table-driven check to reduce duplication.
- [ ] **DRY**: In `getOvnEipNat`, the pattern "list by selector, if len != 0 append tag" repeats for dnats, fips, snats. Use a slice of list+tag pairs and loop to reduce repetition.

---

## ./pkg/controller/ovn_fip.go

- [ ] **Robustness**: In `enqueueAddOvnFip`, use safe type assertion: `fip, ok := obj.(*kubeovnv1.OvnFip); if !ok { klog.Warningf("unexpected type: %T", obj); return }` instead of direct cast to avoid panic if informer passes wrong type; align with `enqueueDelOvnFip` which uses type switch.
- [ ] **Robustness**: In `enqueueUpdateOvnFip`, use safe type assertions for `newObj` and `oldObj` (e.g. `newFip, ok := newObj.(*kubeovnv1.OvnFip); if !ok { return }`) instead of direct casts.
- [ ] **DRY**: The block that resolves internal IPs from VIP or IP CR (lines 143–180 in `handleAddOvnFip`) is nearly duplicated in `handleUpdateOvnFip` (346–378). Extract a helper e.g. `resolveOvnFipInternalIPs(c *Controller, fip *kubeovnv1.OvnFip) (v4IP, v6IP, subnetName, vpcName, mac string, err error)`.
- [ ] **DRY**: EIP validation (eipName empty, GetOvnEip, OvnEipTypeLSP check) is duplicated in `handleAddOvnFip` (109–134) and `handleUpdateOvnFip` (316–341). Extract e.g. `validateOvnFipEip(c *Controller, key, eipName string) (*kubeovnv1.OvnEip, error)`.
- [ ] **DRY**: The V4/V6 `DeleteNat` call pair appears in `handleUpdateOvnFip` deletion branch (284–295) and `handleDelOvnFip` (414–425). Extract a helper e.g. `deleteOvnFipNatsFromOVN(c *Controller, fip *kubeovnv1.OvnFip) error`.
- [ ] **DRY**: The "not support change" validation in `handleUpdateOvnFip` (384–408) has six nearly identical blocks; consider a table-driven check to reduce duplication.
- [ ] **Error handling**: In `patchOvnFipAnnotations` (line 358), `raw, _ := json.Marshal(fip.Annotations)` ignores error; check and return err on Marshal failure.
- [ ] **Error handling**: In `patchOvnFipStatus` (lines 386, 396), `raw, _ := json.Marshal(fip.Labels)` ignores error; check and return err on Marshal failure.
- [ ] **Naming**: In `patchOvnFipAnnotations` and `patchOvnFipStatus`, variable `oriFip` is cryptic; use `cachedFip` or `currentFip` for consistency with other handlers.
- [ ] **Naming**: In `patchOvnFipStatus`, parameter `podIP` is used for V4Ip; consider renaming to `v4IP` for clarity.
- [ ] **Naming**: Fix typo `staleless` → `stateless` for the variable (lines 198, 204) if the intent is "stateless"; if OVN API uses "staleless", add a one-line comment.
- [ ] **Correctness**: In `handleUpdateOvnFip` line 384, error message uses `cachedEip.Name` but should use `cachedFip.Name` for the FIP resource name.
- [ ] **Readability**: In `isOvnFipDuplicated`, fix grammar in error message: "%s is using by" → "%s is used by".
- [ ] **Naming**: Comment "migrate depreciated finalizer" (line 422) → "deprecated"; align with util constant (consider renaming `DepreciatedFinalizerName` to `DeprecatedFinalizerName` in util).
- [ ] **Structure**: `handleAddOvnFip` is long (~160 lines). Consider extracting: (1) `resolveOvnFipInternalIPs` and `validateOvnFipEip`; (2) the four AddNat branches (v4:v4, v6:v6, v4:v6, v6:v4) into a helper e.g. `addOvnFipNatsToOVN(c *Controller, vpcName, v4Eip, v6Eip, v4IP, v6IP, mac, ipName string, options map[string]string) error` so the main flow is a clear sequence of steps.
- [ ] **Maintainability**: `GetOvnEip` is defined in ovn_fip.go but used across FIP/EIP/DNAT handlers; consider moving to ovn_eip.go or a shared helper file for discoverability.

---

## ./pkg/controller/ovn_snat.go

- [ ] **Correctness**: In `patchOvnSnatStatus` (line 393), `snat.Labels[util.EipV6IpLabel] = v4Eip` should be `v6Eip`; V6 label is incorrectly set to v4Eip (copy-paste bug).
- [ ] **Correctness**: In `handleDelOvnSnatRule`, `resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)` is called at line 363 unconditionally and again at 368 when `OvnEip != ""`; remove the unconditional call or consolidate so we do not add twice when OvnEip is set, and do not add empty string when OvnEip is "".
- [ ] **Robustness**: In `enqueueAddOvnSnatRule`, use safe type assertion: `snat, ok := obj.(*kubeovnv1.OvnSnatRule); if !ok { klog.Warningf("unexpected type: %T", obj); return }` instead of direct cast; align with `enqueueDelOvnSnatRule`.
- [ ] **Robustness**: In `enqueueUpdateOvnSnatRule`, use safe type assertions for `newObj` and `oldObj` instead of direct casts.
- [ ] **DRY**: The block that resolves v4IpCidr, v6IpCidr, vpcName from VpcSubnet or IPName (lines 115–139 in `handleAddOvnSnatRule`) is duplicated in `handleUpdateOvnSnatRule` (272–296). Extract a helper e.g. `resolveOvnSnatVpcAndCidrs(c *Controller, snat *kubeovnv1.OvnSnatRule) (v4IpCidr, v6IpCidr, vpcName string, err error)`.
- [ ] **DRY**: EIP validation (eipName empty, GetOvnEip, OvnEipTypeLSP check) is duplicated in `handleAddOvnSnatRule` (91–108) and `handleUpdateOvnSnatRule` (249–266). Extract e.g. `validateOvnSnatEip(c *Controller, key, eipName string) (*kubeovnv1.OvnEip, error)`.
- [ ] **DRY**: The V4/V6 `DeleteNat` pair appears in `handleUpdateOvnSnatRule` deletion branch (216–227) and `handleDelOvnSnatRule` (352–363). Extract a helper e.g. `deleteOvnSnatNatsFromOVN(c *Controller, snat *kubeovnv1.OvnSnatRule) error`.
- [ ] **DRY**: The "not support change" validation in `handleUpdateOvnSnatRule` (302–334) has six nearly identical blocks; consider a table-driven check.
- [ ] **Error handling**: In `patchOvnSnatStatus` (line 398) and `patchOvnSnatAnnotation` (line 358), `raw, _ := json.Marshal(...)` ignores error; check and return err on Marshal failure.
- [ ] **Naming**: In `patchOvnSnatAnnotation`, variable `oriFip` is wrong (this is SNAT); use `cachedSnat` or `currentSnat`. In `patchOvnSnatStatus`, `oriSnat` could be renamed to `cachedSnat` for consistency.
- [ ] **Naming**: Comment "migrate depreciated finalizer" (line 369) → "deprecated"; align with util constant.
- [ ] **Readability**: In EIP type check comments, fix grammar "eip is using by" → "eip is used by" (lines 104, 259).
- [ ] **Readability**: In `enqueueUpdateOvnSnatRule`, the condition `oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip` is checked twice (lines 41 and 46); the first block only adds to reset queue. Consider a single branch: if eip changed, add to reset queue and to update queue, then return; else check other spec changes.
- [ ] **Structure**: `handleAddOvnSnatRule` is long (~120 lines). Consider extracting `resolveOvnSnatVpcAndCidrs`, `validateOvnSnatEip`, and the AddNat v4/v6 block into helpers so the main flow is a short sequence of steps.

---

## ./pkg/controller/pki.go

- [ ] **DRY**: The two `os.Stat` blocks (lines 35–46) are identical in structure: Stat path, if err and IsNotExist return custom error, else return err. Extract a helper e.g. `ensureFileExists(path string) error` to avoid duplication.
- [ ] **Readability**: Secret data keys `"cacert"` and `"cakey"` are magic strings. Define named constants (e.g. `secretKeyCACert`, `secretKeyCAKey`) in util or this file to avoid typos and improve discoverability.
- [ ] **Readability**: Variable names `cacert` and `cakey` could be `caCertBytes` and `caKeyBytes` to clarify they are byte slices.
- [ ] **Structure** (optional): Consider extracting “read CA cert/key and build Secret” into a helper e.g. `buildOVNIPsecCASecret(namespace string, cacertPath, cakeyPath string) (*v1.Secret, error)` so the main function is a short sequence: check existing secret → run ovs-pki → ensure files exist → build and create secret; improves testability of the secret-building logic.

---

## ./pkg/controller/pod.go

- [ ] **Robustness**: In `enqueueAddPod` (line 188), use safe type assertion: `p, ok := obj.(*v1.Pod); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic if informer passes wrong type; align with `enqueueDeletePod` which uses type switch.
- [ ] **Robustness**: In `enqueueUpdatePod` (lines 302–303), use safe type assertions for `oldObj` and `newObj` (e.g. `oldPod, ok := oldObj.(*v1.Pod); if !ok { return }`) instead of direct casts.
- [ ] **DRY**: `isStatefulSetPodToDel` and `isStatefulSetPodToGC` share nearly identical logic (get STS, NotFound/DeletionTimestamp/UID checks, parse index from pod name, check replicas). Extract a helper e.g. `getStatefulSetPodDeletionContext(c kubernetes.Interface, pod *v1.Pod, stsName string, stsUID types.UID) (deleted, downscaled bool, err error)` or share the “parse ordinal + compare replicas” block to reduce duplication.
- [ ] **Readability**: Inline comment at line 515: “// todo: isVmPod, getPodType, getNameByPod has duplicated logic” — address the duplication or convert to a tracked TODO.
- [ ] **Structure**: `reconcileRouteSubnets` is very long (~200 lines) with repeated patterns (RemovePortFromPortGroups + PortGroupAddPorts, EIP/SNAT per-IP loops). Extract helpers e.g. `reconcilePodPortGroups(c, pod, podNet, portName, nodePortGroup, subnetPortGroups)`, `reconcileEipSnatForPod(c, pod, podIP, podName)` to shorten and clarify the main flow.
- [ ] **Structure**: `handleDeletePod` has duplicated keepIPCR logic for StatefulSet and VM (check isOwnerRefToDel, set keepIPCR, appendCheckPodNetToDel, clear keepIPCR). Extract e.g. `resolveKeepIPForPodDeletion(c *Controller, pod *v1.Pod) (keepIP bool, ipcrToDelete []string, err error)` to unify STS/VM handling.
- [ ] **Readability**: In `GetNamedPortByNs`, the loop logs for every port in the namespace; consider klog.V(4) or reduce verbosity to avoid log flood in large namespaces.
- [ ] **Naming**: Fix comment typo “named port %s has already be defined” → “has already been defined” (line 87).
- [ ] **Error handling**: In `getNodeTunlIP`, `net.ParseIP(ip)` can return nil; the code appends without checking, so nil can be appended to the slice. Filter out invalid IPs or return an error when ParseIP returns nil.

---

## ./pkg/controller/pod_test.go

- [ ] **DRY**: The NAD config JSON string (cniVersion, name, type, server_socket, provider) is duplicated in multiple test cases in `TestCheckIsPodVpcNatGw` and `TestGetPodKubeovnNetsNonPrimaryCNI`. Extract a helper e.g. `net1NADConfig(provider string) string` or a constant to reduce duplication and drift.
- [ ] **Test isolation**: In `TestCheckIsPodVpcNatGw`, the "Edge cases" subtest (lines 142–175) runs three scenarios (nil pod, empty gw name, no annotations) in one subtest. Split into three separate `t.Run` subtests so failures are isolated and output clearly identifies which scenario failed.
- [ ] **Consistency**: Add `t.Parallel()` inside the `for _, tt := range tests` loop in `TestCheckIsPodVpcNatGw`, `TestGetPodKubeovnNetsNonPrimaryCNI`, and `TestAcquireAddressWithSpecifiedSubnet` so subtests run in parallel, aligning with other controller tests (e.g. baseline_admin_network_policy_test.go).
- [ ] **Readability/Structure**: In `TestGetPodKubeovnNetsNonPrimaryCNI`, the case "Primary CNI mode vs Non-primary CNI behavior" (lines 323–328) mutates `controller.config.EnableNonPrimaryCNI = true` inside the subtest and runs a second assertion. Split into two table rows (one for primary with expectedNetCount 2, one for non-primary with expectedNetCount 1) so each case tests one mode and the table is easier to follow.
- [ ] **Error handling**: In `newIPAMForTest`, `ipam.NewSubnet` errors are panicked on. For consistency with the rest of the file (require.NoError), consider changing the signature to `newIPAMForTest(t *testing.T, subnets []*kubeovnv1.Subnet) *ipam.IPAM` and using `require.NoError(t, err)` so invalid subnets fail the test instead of panicking.
- [ ] **Readability**: In `newIPAMForTest`, the assignment `if len(excludeIPs) == 0 { excludeIPs = []string{} }` can be written as `if excludeIPs == nil { excludeIPs = []string{} }` if the intent is only to avoid passing nil to NewSubnet; document or simplify for clarity.

---

## ./pkg/controller/provider_network.go

- [ ] **Performance**: In `resyncProviderNetworkStatus` (line 88), `slices.Contains(expectNodes, c.Node)` inside the loop over conditions creates O(n×m) complexity (n = expectNodes length, m = conditions length). Build a set/map from `expectNodes` once (e.g. `expectNodeSet := make(map[string]bool); for _, n := range expectNodes { expectNodeSet[n] = true }`), then use `if !expectNodeSet[c.Node]` for O(1) lookup and O(n+m) total.
- [ ] **Readability**: The condition at lines 95–96 (`conditionsUpdated || len(util.DiffStringSlice(...)) != 0 || len(util.DiffStringSlice(...)) != 0`) is long and combines multiple concerns. Extract a helper e.g. `shouldUpdateProviderNetworkStatus(pn *kubeovnv1.ProviderNetwork, readyNodes, notReadyNodes []string, conditionsUpdated bool) bool` to clarify intent and make the main function easier to read.
- [ ] **Readability/Structure**: The error message construction block (lines 67–77) that checks pod existence and extracts error message from annotations is complex. Extract to a helper e.g. `getProviderNetworkErrorForNode(podMap map[string]*corev1.Pod, nodeName, errMsgAnnotation string) string` to improve readability and testability.
- [ ] **Structure**: `resyncProviderNetworkStatus` is long (~90 lines). Consider extracting: (1) `computeNodeStatusForProviderNetwork(c *Controller, pn *kubeovnv1.ProviderNetwork, nodes []*corev1.Node, podMap map[string]*corev1.Pod) (readyNodes, notReadyNodes []string, conditionsUpdated bool, err error)` for the per-node status computation loop, (2) `cleanupStaleNodeConditions(pn *kubeovnv1.ProviderNetwork, expectNodes []string) bool` for the condition cleanup loop, so the main function is a clear sequence of steps.
- [ ] **Error handling**: In `resyncProviderNetworkStatus`, when `UpdateStatus` fails (line 100), the error is only logged and the function continues to the next provider network. Consider collecting errors and returning them, or document that resync is best-effort per provider network and errors are only logged.
- [ ] **DRY**: The pattern of building annotation keys using `fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)` and `fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, pn.Name)` (lines 44–45) appears only twice here, but if similar patterns exist elsewhere, consider a helper e.g. `providerNetworkAnnotationKey(pnName, template string) string` for consistency.

---

## ./pkg/controller/qos_policy.go

- [ ] **Robustness**: In `enqueueAddQoSPolicy` (line 25), use safe type assertion: `qos, ok := obj.(*kubeovnv1.QoSPolicy); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic if informer passes wrong type; align with `enqueueDelQoSPolicy` which uses type switch.
- [ ] **Robustness**: In `enqueueUpdateQoSPolicy` (lines 42–43), use safe type assertions for `oldObj` and `newObj` (e.g. `oldQos, ok := oldObj.(*kubeovnv1.QoSPolicy); if !ok { return }`) instead of direct casts to avoid panic.
- [ ] **Correctness/Naming**: In `compareQoSPolicyBandwidthLimitRules` (line 30), the function mutates `newObj` by sorting it (line 35), but the function name suggests it only compares. Either copy `newObj` before sorting (e.g. `sortedNew := make(kubeovnv1.QoSPolicyBandwidthLimitRules, len(newObj)); copy(sortedNew, newObj); sort.Slice(sortedNew, ...)`) or rename the function to `compareAndSortQoSPolicyBandwidthLimitRules` to indicate mutation.
- [ ] **Naming**: Fix typo `reconcileEIPBandtithLimitRules` → `reconcileEIPBandwidthLimitRules` (line 227) and `delEIPBandtithLimitRules` → `delEIPBandwidthLimitRules` (line 236). Update all call sites.
- [ ] **Naming**: Fix typo `matachValue` → `matchValue` in `validateIPMatchValue` function parameter (line 257) and update all references within the function.
- [ ] **Naming**: Fix typo "depreciated" → "deprecated" in comment (line 185) to align with standard spelling.
- [ ] **Naming**: In `patchQoSStatus`, variable `oriQoS` (line 126) is cryptic; use `cachedQoS` for consistency with other handlers (e.g. `handleAddQoSPolicy`, `handleUpdateQoSPolicy`).
- [ ] **DRY**: In `handleUpdateQoSPolicy`, the blocks that check if QoS policy is in use by EIPs (lines 316–328) and by NatGws (lines 331–343) are nearly identical. Extract a helper e.g. `isQoSPolicyInUse(c *Controller, key string, bindingType kubeovnv1.QoSPolicyBindingType) (bool, error)` to reduce duplication.
- [ ] **DRY**: The pattern of sorting bandwidth limit rules by name (lines 96–99 in `handleAddQoSPolicy` and 406–409 in `handleUpdateQoSPolicy`) is duplicated. Extract a helper e.g. `sortQoSPolicyBandwidthLimitRules(rules kubeovnv1.QoSPolicyBandwidthLimitRules) kubeovnv1.QoSPolicyBandwidthLimitRules` to avoid duplication.
- [ ] **DRY**: The status comparison logic in `handleAddQoSPolicy` (lines 101–107) that checks if Status matches Spec is duplicated in `enqueueUpdateQoSPolicy` (lines 50–53). Extract a helper e.g. `qoSPolicyStatusMatchesSpec(status *kubeovnv1.QoSPolicyStatus, spec *kubeovnv1.QoSPolicySpec) bool` to avoid divergence.
- [ ] **Readability/Structure**: In `handleUpdateQoSPolicy`, the EIP reconciliation block (lines 382–404) is complex with a switch on eips length. Extract to a helper e.g. `reconcileEIPQoSPolicyRules(c *Controller, key string, added, deleted, updated kubeovnv1.QoSPolicyBandwidthLimitRules) error` to improve readability and testability.
- [ ] **Readability**: In `validateIPMatchValue` (line 275), the comment "// invalid cidr" is misplaced (appears after the CIDR validation that returns on error). Remove or move to the correct location (e.g. before the ParseCIDR call) to clarify intent.
- [ ] **Readability**: In `handleUpdateQoSPolicy`, the error message at line 359 says "not support qos %s change shared" but the condition checks both Shared and BindingType; update message to "not support qos %s change shared or binding type" for accuracy.
- [ ] **Structure**: `handleUpdateQoSPolicy` is long (~118 lines). Consider extracting: (1) `checkQoSPolicyInUseBeforeDeletion(c *Controller, cachedQos *kubeovnv1.QoSPolicy) error` for the deletion branch checks, (2) `reconcileQoSPolicyBandwidthRules(c *Controller, cachedQos *kubeovnv1.QoSPolicy, added, deleted, updated kubeovnv1.QoSPolicyBandwidthLimitRules) error` for the rule reconciliation, so the main function is a clear sequence of steps.

---

## ./pkg/controller/security_group.go

- [ ] **Robustness**: In `enqueueAddSg` (line 28), use safe type assertion: `sg, ok := obj.(*kubeovnv1.SecurityGroup); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic if informer passes wrong type; align with `enqueueDeleteSg` which uses type switch.
- [ ] **Robustness**: In `enqueueUpdateSg` (lines 34–35), use safe type assertions for `oldObj` and `newObj` (e.g. `oldSg, ok := oldObj.(*kubeovnv1.SecurityGroup); if !ok { return }`) instead of direct casts to avoid panic.
- [ ] **Correctness**: In `handleAddOrUpdateSg` (line 242), when egress ACL update fails, the code sets `sg.Status.IngressLastSyncSuccess = false` but should set `sg.Status.EgressLastSyncSuccess = false` (copy-paste bug from ingress block).
- [ ] **DRY**: In `handleAddOrUpdateSg`, the ingress ACL update block (lines 224–239) and egress ACL update block (240–256) are nearly identical. Extract a helper e.g. `updateSgACLForDirection(c *Controller, sg *kubeovnv1.SecurityGroup, direction ovnnb.ACLDirection, md5 string) error` to reduce duplication.
- [ ] **DRY**: In `handleAddOrUpdateSg`, the pattern of creating PortGroup and two AddressSets (lines 171–194) is repeated. Extract a helper e.g. `ensureSgPortGroupAndAddressSets(c *Controller, sg *kubeovnv1.SecurityGroup) error` to reduce duplication and keep resource creation in one place.
- [ ] **Readability/Structure**: In `handleAddOrUpdateSg`, the MD5 calculation and comparison logic (lines 196–221) that determines `ingressNeedUpdate` and `egressNeedUpdate` is complex. Extract a helper e.g. `shouldUpdateSgACLs(sg *kubeovnv1.SecurityGroup, force bool) (ingressNeedUpdate, egressNeedUpdate bool, ingressMd5, egressMd5 string)` to improve readability and testability.
- [ ] **Readability/Structure**: In `syncSgLogicalPort`, the address parsing logic (lines 345–366) that extracts v4 and v6 addresses from LSP addresses/port security is complex. Extract a helper e.g. `extractIPv4AndIPv6FromLSP(lsp *ovnnb.LogicalSwitchPort) (v4s, v6s []string)` to improve readability and testability.
- [ ] **Naming**: In `syncSgLogicalPort` (line 353), variable `as` may be confused with "address set"; consider renaming to `addressStr` or `addrStr` for clarity.
- [ ] **Performance**: In `syncSgLogicalPort`, the address parsing loop (lines 353–365) could pre-allocate slices with estimated capacity if the number of addresses is known, or use `make([]string, 0, len(sgPorts)*2)` to reduce allocations.
- [ ] **Error handling**: In `patchSgStatus` (line 310), errors are only logged and not returned. Consider returning the error so callers can handle failures (e.g. retry), or document that status patching is best-effort and errors are only logged.
- [ ] **Structure**: `handleAddOrUpdateSg` is long (~121 lines). Consider extracting: (1) `ensureSgPortGroupAndAddressSets` for resource creation, (2) `shouldUpdateSgACLs` for update decision logic, (3) `updateSgACLForDirection` for ACL updates, so the main function is a clear sequence of steps.
- [ ] **Readability**: In `reconcilePortSg` (line 427), `newSgList := strings.Split(securityGroups, ",")` may include empty strings if the input has trailing commas. Consider filtering empty strings or using `strings.FieldsFunc` with a custom splitter to handle edge cases.
- [ ] **Consistency**: In `reconcilePortSg`, the external ID key format conversion (line 444: `strings.ReplaceAll(securityGroups, ",", "/")`) should match the format expected by `updateDenyAllSgPorts` (line 119: `strings.Split(lsp.ExternalIDs[sgsKey], "/")`). Document the format (comma-separated in annotation, slash-separated in external IDs) or extract a helper to ensure consistency.

---

## ./pkg/controller/security_group_test.go

- [ ] **Naming**: Fix typo in subtest name (line 59): "does't" → "don't" (or "do not").
- [ ] **Naming**: Rename test `Test_securityGroupALLNotExist` → `Test_securityGroupAllNotExist` to match the method name and Go camelCase (ALL → All).
- [ ] **Readability**: Fix subtest name "when some port group exist" → "when some port group exists" (subject-verb agreement).
- [ ] **Consistency**: Add `t.Parallel()` inside each subtest in `Test_getPortSg` so subtests run in parallel, aligning with `Test_securityGroupALLNotExist` and other controller tests (e.g. baseline_admin_network_policy_test.go).

---

## ./pkg/controller/service.go

- [ ] **Robustness**: In `enqueueAddService` (line 39), use safe type assertion: `svc, ok := obj.(*v1.Service); if !ok { klog.Warningf("unexpected type: %T", obj); return }` to avoid panic if informer passes wrong type; align with `enqueueDeleteService` which uses type switch.
- [ ] **Robustness**: In `enqueueUpdateService` (lines 122-123), use safe type assertions for `oldObj` and `newObj` (e.g. `oldSvc, ok := oldObj.(*v1.Service); if !ok { return }`) instead of direct casts to avoid panic.
- [ ] **Readability**: Log at line 41 says "enqueue add endpoint %s" but the handler is `enqueueAddService`; consider "enqueue add service %s" or "enqueue service %s for endpoint slice" to avoid confusion.
- [ ] **Readability**: The condition at line 80 (`ok || svc.Spec.ClusterIP != ... || svc.Annotations[...] != ""`) is complex; extract a helper e.g. `shouldEnqueueServiceDeletion(svc *v1.Service) bool` and add parentheses if needed for clarity.
- [ ] **DRY**: In `enqueueDeleteService` (lines 95-104), the construction of `ips` from vip annotation, ServiceClusterIPs, and LoadBalancer.Ingress duplicates `getVipIps(svc)`. Use `ips := getVipIps(svc)` (and then override with `strings.Split(vip, ",")` when `ok`) to avoid divergence.
- [ ] **Performance**: In `enqueueUpdateService` (lines 130-134), `slices.Contains(newClusterIps, oldClusterIP)` is O(n) per old IP. Build a set from `newClusterIps` (e.g. `strset` or `map[string]struct{}`) for O(1) lookup. Similarly in `updateVip` (lines 316, 337), consider sets for `ips` and `ipsToDel` when slices are large.
- [ ] **Structure**: `handleUpdateService` is long (~200 lines). Extract the inner closure `updateVip` to a method e.g. `(c *Controller) updateVpcLbVips(lbName, oLbName string, svcVips []string, ips, ipsToDel []string, ignoreHealthCheck bool) error`; consider a slice of `struct{ lb, oLb string; vips []string }` and loop for the three protocol LBs to reduce repetition.
- [ ] **Maintainability**: In `handleAddService` (lines 433-459), the busy-wait loop with `time.Sleep(time.Second)` has no timeout and can run indefinitely. Add a context timeout or `wait.PollUntilContextTimeout` with max retries and document; or document that the loop is intentional and under what conditions it exits.
- [ ] **Error handling**: `parseVipAddr` returns "" on parse error; callers use the value without checking. Consider returning `(string, error)` or document that empty string is used on failure and ensure all call sites handle it (e.g. skip or log).

---

## ./pkg/controller/service_lb.go

- [ ] **Naming**: In `parseAttachNetworkProvider` (lines 47-59), return variables `attachmentName` and `attachmentNs` shadow package-level constants of the same names. Rename to `parsedName, parsedNs` or `name, ns` to avoid confusion and improve readability.
- [ ] **Readability**: Fix grammar in error message (line 64): "should be consisted of" → "should consist of".
- [ ] **DRY**: The protocol switch (TCP→util.ProtocolTCP, UDP→util.ProtocolUDP, SCTP→util.ProtocolSCTP) is duplicated in `updatePodAttachNets` (lines 332-340) and `delDnatRules` (lines 341-349). Extract a helper e.g. `protocolToString(p corev1.Protocol) string` to avoid duplication.
- [ ] **DRY**: The pattern "targetPort := port.TargetPort.IntValue(); if targetPort == 0 { targetPort = int(port.Port) }; build rules string" appears in both `updatePodAttachNets` and `delDnatRules`. Extract a small helper e.g. `formatDnatRule(port corev1.ServicePort, loadBalancerIP, clusterIP, defaultGateway string) (rules []string, targetPort int)` (or two helpers for add vs del format) to reduce duplication.
- [ ] **Readability**: In `updatePodAttachNets` (lines 321-322), `var addRules []string` then `addRules = append(addRules, ...)` can be simplified to `addRules := []string{fmt.Sprintf(...)}`.
- [ ] **Maintainability**: Label keys "namespace", "service", "app" (lines 367, 371, 375) are magic strings. Consider named constants (e.g. lbSvcLabelNamespace, lbSvcLabelService, lbSvcLabelApp) to align with `genLbSvcDeployment` and avoid typos.
- [ ] **Correctness / Lock scope**: In `checkAndReInitLbSvcPod` (line 382), `c.svcKeyMutex.LockKey(svcName)` uses only the service name. Elsewhere (e.g. service.go) the queue key is `namespace/name`. If two services in different namespaces share the same name, they would contend on the same lock. Consider `LockKey(nsName + "/" + svcName)` to match the service key format and avoid cross-namespace contention.
- [ ] **Error handling**: In `getNodeSelectorFromCm` (line 275), when `cm.Data["nodeSelector"] == ""` the code logs `klog.Error(err)` but `err` at that point is from the previous Get (or nil). Use a dedicated error e.g. `errors.New("nodeSelector field is empty")` for accurate logging.

---

## ./pkg/controller/service_test.go

- [ ] **Coverage / Maintainability**: File is a stub (only `package controller`). Consider adding unit tests for service controller logic (e.g. `handleAddService`, `handleUpdateService`, `enqueueAddService`, `getVipIps`, `parseVipAddr`, `shouldEnqueueServiceDeletion`) to improve coverage and regression safety, aligned with other controller test files (e.g. pod_test.go, node_test.go).

---

## ./pkg/controller/signer.go

- [ ] **Robustness**: In `enqueueAddCsr` (line 60), use safe type assertion: `req, ok := obj.(*csrv1.CertificateSigningRequest); if !ok { return }` to avoid panic if informer passes wrong type; align with other controller handlers.
- [ ] **Robustness**: In `enqueueUpdateCsr` (lines 72–73), use safe type assertions for `oldObj` and `newObj` instead of direct casts.
- [ ] **Correctness**: In `handleAddOrUpdateCsr`, `newCertificateTemplate` can return nil on serial number generation failure; caller does not check and passes nil to `signCSR`, which can panic. Check for nil and call `signerFailure` (or have `newCertificateTemplate` return `(*x509.Certificate, error)` and handle error).
- [ ] **Maintainability**: Remove debug `fmt.Println(block.Type)` in `decodePrivateKey` (line 296); use `klog.V(5).Infof` if block type logging is needed.
- [ ] **Readability**: Secret data keys `"cacert"` and `"cakey"` (lines 143, 151) are magic strings; define named constants (e.g. in pki.go or util) for consistency with pki.go refactor.
- [ ] **Readability**: Replace magic numbers with named constants: certificate validity `10 * 365 * 24 * time.Hour` (line 248), e.g. `certValidityPeriod`; `NotBefore: time.Now().Add(-1 * time.Second)` (line 247) for clock skew.
- [ ] **Naming**: Import alias `c` for `crypto` can be confused with receiver `c *Controller`; consider renaming to `cr` or `cryptopkg` for clarity.
- [ ] **Comment**: Fix typo "We dont" → "We don't" (line 134); "From this, point" → "From this point" (line 123).
- [ ] **DRY**: PEM decode pattern (Decode, check block nil/type, return error) is repeated in `decodeCertificateRequest`, `decodeCertificate`, `decodePrivateKey`. Extract a helper e.g. `decodePEMBlock(pemBytes []byte, expectedType string) (*pem.Block, error)` to reduce duplication.
- [ ] **Readability**: In `getCertApprovalCondition`, rename loop variable `c` to `cond` to avoid confusion with controller receiver and clarify intent.

---

## ./pkg/controller/subnet.go

- [ ] **Robustness**: In `enqueueAddSubnet` (line 32), use safe type assertion: `subnet, ok := obj.(*kubeovnv1.Subnet); if !ok { return }` then use `subnet` for key, to avoid panic if informer passes wrong type; align with `enqueueDeleteSubnet`.
- [ ] **Robustness**: In `enqueueUpdateSubnet` (lines 75–76), use safe type assertions for `oldObj` and `newObj` instead of direct casts.
- [ ] **Correctness**: In `handleDeleteSubnet` (line 822), when `vpcsLister.Get` returns a non-NotFound error, `vpc` may be nil; logging `vpc.Name` can panic. Use `subnet.Spec.Vpc` in the error message (e.g. `"get vpc %s: %v", subnet.Spec.Vpc, err`).
- [ ] **Naming**: Fix typo "depreciated" → "deprecated" in comment (line 262) and align with util constant `DepreciatedFinalizerName` (consider renaming to `DeprecatedFinalizerName` in util); same at lines 275, 299.
- [ ] **Structure / Readability**: `handleAddOrUpdateSubnet` is very long (~280 lines). Consider extracting helpers e.g. `validateAndFormatSubnet`, `ensureLogicalSwitchAndDHCP`, `reconcileSubnetLoadBalancers`, `reconcileSubnetACLAndPrivate` so the main flow is a short sequence of steps and each step is testable.
- [ ] **DRY**: The pattern "if err = c.patchSubnetStatus(subnet, reason, msg); err != nil { klog.Error(err); return err }" repeats many times in handleAddOrUpdateSubnet and elsewhere; consider a helper e.g. `patchSubnetStatusAndReturn(c, subnet, reason, msg) error` to reduce repetition.
- [ ] **Performance**: In `reconcileNamespaces` and similar, `slices.Contains(expectNss, ns.Name)` is O(n) per check; build a set (e.g. map[string]struct{}) from expectNss for O(1) lookup when lists are large.
- [ ] **Performance**: In `syncVirtualPort` (lines 980–1007), for each VIP we iterate all LSPs (O(vips × lsps)). Build a map vip -> []lspName once from lsps, then loop vips and look up to reduce to O(vips + lsps).
- [ ] **Robustness**: In `reconcileDistributedSubnetRouteInDefaultVpc` (line 1165), `node.Annotations[util.AllocatedAnnotation]` can panic if `node.Annotations` is nil; use safe access e.g. `util.GetNodeAllocated(node) == "true"` or check annotations != nil first.
- [ ] **Maintainability**: Complete or remove TODO at line 1110: "TODO:// support v6" → "TODO: support IPv6 for BFD static route" or track in refactor list.

---

## ./pkg/controller/subnet_status.go

- [ ] **Correctness**: In `handleUpdateSubnetStatus`, `subnet := cachedSubnet.DeepCopy()` is called before checking `err` from `c.subnetsLister.Get(key)`. When Get returns an error, cachedSubnet may be nil and DeepCopy() will panic. Check err before using cachedSubnet (e.g. get, then if err != nil return, then DeepCopy).
- [ ] **Error handling**: In `calcSubnetStatusIP`, `net.ParseCIDR` return values (error) are ignored at lines 201-202, 205-206, 210-211. Malformed CIDR could cause nil dereference or wrong calculation; validate and handle ParseCIDR errors.
- [ ] **Readability**: In `patchSubnetStatus`, reason strings like "ValidateLogicalSwitchFailed", "SetPrivateLogicalSwitchSuccess" are magic strings; consider named constants (e.g. in pkg/controller or pkg/apis) for consistency and discoverability.
- [ ] **Readability**: The long equality check in `calcSubnetStatusIP` (lines 225-232) comparing all status fields could be extracted to a helper e.g. `subnetStatusIPUnchanged(subnet, v4/v6 available/using/range) bool` to improve readability.
- [ ] **Performance**: In `updateNatOutgoingPolicyRulesStatus`, building `retBytes` with repeated `append(retBytes, ...)` causes multiple allocations; pre-allocate or use bytes.Buffer for building the byte slice before hashing.

---

## ./pkg/controller/subnet_test.go

- [ ] **Naming**: In Test_reconcileVips, subtest name "existent vips and new vips has intersection" / "existent vips is empty" use "existent"; prefer "existing" for standard English.
- [ ] **Readability**: In Test_formatSubnet, case name "complete subnet that do not need to be formatted" has subject-verb disagreement; use "that does not need".
- [ ] **Readability**: mockLsp is defined in both Test_reconcileVips and Test_syncVirtualPort with different shapes (Options["virtual-ip"] vs ExternalIDs["vips"]); consider renaming to e.g. mockLspWithVirtualIP and mockLspWithVips to avoid confusion.
- [ ] **Maintainability**: Consider adding unit tests for subnet_status.go helpers (e.g. filterNonGatewayExcludeIPs, calculateUsingIPs, calcSubnetStatusIP) in this file or a dedicated subnet_status_test.go to improve coverage.

---

## ./pkg/controller/switch_lb_rule.go

- [ ] **Robustness**: In `enqueueAddSwitchLBRule`, use safe type assertion: `slr, ok := obj.(*kubeovnv1.SwitchLBRule); if !ok { return }` then use slr for key, to avoid panic if informer passes wrong type; align with enqueueDeleteSwitchLBRule.
- [ ] **Robustness**: In `enqueueUpdateSwitchLBRule`, use safe type assertions for oldObj and newObj instead of direct casts.
- [ ] **Readability**: In `handleAddOrUpdateSwitchLBRule`, building formatPorts via repeated `fmt.Sprintf("%s,%d/%s", formatPorts, ...)` is fragile; consider building a slice of "port/protocol" strings and using strings.Join.
- [ ] **Readability**: In `handleDelSwitchLBRule` error messages, fix redundant "health checks health checks" (line 247) to "health check".
- [ ] **Maintainability**: In `generateHeadlessService`, `make(map[string]string, 0)` — the 0 is redundant; use make(map[string]string).

---

## ./pkg/controller/switch_lb_rule_test.go

- [ ] **Readability**: Fix typo in error message: `familiyPolicy` → `familyPolicy` (line 79).
- [ ] **Readability**: In `Test_setUserDefinedNetwork`, the table field `result` holds the expected service state after mutation; consider renaming to `expected` or `expectedService` for clarity.

---

## ./pkg/controller/vpc_dns.go

- [ ] **Naming**: Inconsistent naming: `enqueueAddVpcDNS` / `enqueueUpdateVpcDNS` use `VpcDNS` vs `enqueueDeleteVPCDNS` uses `VPCDNS`. Use consistent casing (e.g. all `VpcDNS`).
- [ ] **Naming**: Fix typo "no found" → "not found" in `getDefaultCoreDNSImage` error message (line 411).
- [ ] **Dead code**: In `checkVpcDNSDuplicated`, `if k8serrors.IsNotFound(err)` after `List()` is redundant — List does not return NotFound. Remove the branch.
- [ ] **Readability**: ConfigMap keys ("coredns-image", "nad-name", "nad-provider", "coredns-vip", "k8s-service-host", "k8s-service-port", "enable-vpc-dns") are magic strings in `resyncVpcDNSConfig`. Consider package-level constants for maintainability and discoverability.
- [ ] **Structure / Testability**: Global variables (`corednsImage`, `corednsVip`, `nadName`, etc.) are mutable and set in `resyncVpcDNSConfig`. This makes unit testing hard and is not obviously thread-safe. Consider holding VPC-DNS config in a struct (e.g. on Controller or a dedicated config holder) protected by mutex or updated in a single goroutine.
- [ ] **Error handling**: In the defer in `handleAddOrUpdateVPCDNS`, when `UpdateStatus` fails we only log; the returned `err` is unchanged. Consider documenting that status update failure is best-effort, or record the error so callers can observe it.
- [ ] **Maintainability**: In `createOrUpdateVpcDNSDep`, when updating an existing deployment we pass `newDp` which may omit server-applied defaults (e.g. resource limits). Consider merging with existing deploy (e.g. preserve deploy.ObjectMeta.ResourceVersion, deploy.Status) or document that full replacement is intentional.

---

## ./pkg/controller/vpc_egress_gateway.go

- [ ] **Robustness**: In `enqueueAddVpcEgressGateway` and `enqueueUpdateVpcEgressGateway`, direct type assertion `obj.(*kubeovnv1.VpcEgressGateway)` can panic if informer passes wrong type. Use safe type assertion (e.g. `gw, ok := obj.(*kubeovnv1.VpcEgressGateway); if !ok { return }`) as in `enqueueDeleteVpcEgressGateway`.
- [ ] **Error handling**: In `handleDelVpcEgressGateway` (lines 627–632), when `Update` after `RemoveFinalizer` fails, `err` is assigned but the function returns `nil`. Return `err` so callers observe the failure.
- [ ] **Error handling**: In `fnFilter` (line 361), `util.CIDRContainsCIDR(internalCIDR, cidr)` second return value (error) is discarded. Handle or log the error to avoid ignoring invalid CIDRs.
- [ ] **Naming**: Function `vpcEgressGatewayContainerBFDD` has trailing "D" (container name is "bfdd"); consider renaming to `vpcEgressGatewayBFDContainer` for consistency with other helpers.
- [ ] **Structure**: `reconcileVpcEgressGatewayWorkload` is long (~170 lines). Consider extracting: policy/source collection, route building, deployment spec construction into helpers (e.g. `collectEgressPolicySources`, `buildVpcEgressGatewayDeployment`) to improve readability and testability.
- [ ] **Structure**: `reconcileVpcEgressGatewayOVNRoutes` is long (~230 lines). Consider extracting: port group reconciliation, address set reconciliation, BFD reconciliation, LR policy (local vs cluster), drop policy into separate functions.
- [ ] **Readability**: In `reconcileVpcEgressGatewayOVNRoutes`, address family `4` and `6` are magic numbers; use named constants (e.g. `util.IPv4`/`util.IPv6` if available) or a short comment. Document that `-1` in `DeleteLogicalRouterPolicies(lrName, -1, ...)` means all priorities or use a named constant.
- [ ] **Maintainability**: Resolve or document TODOs: line 331 "check subnet's vpc and vlan"; line 451 "update min_rx, min_tx and multiplier".
- [ ] **Readability**: Complex format strings for LR policy match (e.g. line 469) could be extracted to a small helper e.g. `localPolicyMatch(af int, localPgName, pgName string) string` to clarify intent.

---

## ./pkg/controller/vpc.go

- [ ] **Robustness**: In `enqueueAddVpc` and `enqueueUpdateVpc`, direct type assertions `obj.(*kubeovnv1.Vpc)` and `oldObj.(*kubeovnv1.Vpc)`, `newObj.(*kubeovnv1.Vpc)` can panic. Use safe type assertions (e.g. check `ok`) as in `enqueueDelVpc`.
- [ ] **Robustness**: In `enqueueUpdateVpc` (line 66), `oldVpc.Labels[util.VpcExternalLabel] != newVpc.Labels[util.VpcExternalLabel]` may panic if `oldVpc.Labels` is nil. Use a helper or check nil before indexing.
- [ ] **Maintainability**: Resolve or document TODOs: line 68 "label VpcExternalLabel replace with spec enable external"; line 675 "dualstack"; line 681 "support multi external nic".
- [ ] **Naming**: In `reconcileVpcBfdLRP` (line 702), loop variable `nodes` shadows the outer `nodes` slice. Rename to e.g. `node` for clarity.
- [ ] **Readability**: In `reconcileVpcBfdLRP`, when `vpc.Spec.BFDPort.NodeSelector` is nil and `c.nodesLister.List(selector)` fails, the error message uses `vpc.Spec.BFDPort.NodeSelector` which is nil; use a clearer message (e.g. "failed to list nodes") when NodeSelector is nil.
- [ ] **Structure**: `handleAddOrUpdateVpc` is very long (~340 lines). Consider extracting: peering reconciliation, static route handling, policy route handling, status update, external subnet handling, BFD handling into helpers (e.g. `reconcileVpcPeerings`, `reconcileVpcStaticRoutes`, `reconcileVpcPolicyRoutes`) to improve readability and testability.
- [ ] **DRY**: In `deletePolicyRouteFromVpc`, `batchDeletePolicyRouteFromVpc`, and `batchDeleteStaticRouteFromVpc`, the same pattern appears after OVN delete: get Vpc from lister, DeepCopy, Update with comment "make sure custom policies not be deleted". Extract a helper e.g. `touchVpcAfterOVNUpdate(vpcName string) error` to reduce duplication.
- [ ] **Readability**: Magic number `-1` in `ListLogicalRouterPolicies(vpc.Name, -1, nil, true)` (line 518) and similar; document that -1 means all priorities or use a named constant.
- [ ] **Clarity**: In `diffStaticRoute`, the named return `err` is never assigned (always nil). Return `routeNeedDel, routeNeedAdd, nil` explicitly or remove the named `err` return.

---

## ./pkg/controller/vpc_lb.go

- [ ] **Error handling / Semantics**: In `createVpcLb`, when `genVpcLbDeployment` returns `(nil, nil)` (no subnets), the code treats it as failure and logs "failed to generate vpc lb deployment". Clarify semantics: either return `nil` when `deployment == nil && err == nil` (no deployment needed), or document that (nil, nil) means "skip" and handle in caller so "no subnets" is not logged as an error.
- [ ] **Convention**: In `createVpcLb`, check `err != nil` before `deployment == nil` for idiomatic error-first style; e.g. `if err != nil { ... }; if deployment == nil { return nil }`.
- [ ] **DRY**: In `genVpcLbDeployment`, the IPv4 and IPv6 init container blocks (route + iptables containers) are nearly identical. Extract a helper e.g. `initContainersForIPFamily(v4 bool, gw, svcCIDR string, image string, privileged, allowEsc bool) []corev1.Container` to reduce duplication and simplify adding another IP family.
- [ ] **Readability**: The NAD annotation built with `fmt.Sprintf(`[{"name": "%s", "default-route": ["%s"]}]`, ...)` and `strings.ReplaceAll(gateway, ",", ...)` is dense. Extract to a helper e.g. `buildVpcLbNADAnnotation(provider, gateway string) string` or use a struct + `json.Marshal` for clarity and safer escaping.
- [ ] **Maintainability**: `vpcNatImage` is a package-level var set in `vpc_nat.go`; `vpc_lb.go` depends on init order. Consider passing the image via Controller config or document that vpc_nat init must run before VPC LB logic.
- [ ] **Readability**: The repeated `allowPrivilegeEscalation` and `privileged` booleans for every container could be a shared struct or named constants to avoid repetition and make security policy changes in one place.

---

## ./pkg/controller/vpc_nat_gateway.go

- [ ] **Robustness**: In `enqueueAddVpcNatGw` and `enqueueUpdateVpcNatGw`, direct type assertions `obj.(*kubeovnv1.VpcNatGateway)` and `newObj.(*kubeovnv1.VpcNatGateway)` can panic. Use safe type assertions (e.g. check `ok`) as in `enqueueDeleteVpcNatGw`.
- [ ] **Correctness / Maintainability**: Package-level vars `vpcNatEnabled`, `VpcNatCmVersion`, and `natGwCreatedAT` are global mutable state. `natGwCreatedAT` is especially problematic: when multiple NAT gateways exist, each handler calls `initCreateAt(natGwKey)` which overwrites the single global; concurrent or sequential handling of different gateways can use the wrong creation time. Store per-gateway creation time on the Controller (e.g. `map[string]string`) or pass it through so Redo comparisons are correct per gateway.
- [ ] **DRY**: `handleUpdateVpcFloatingIP`, `handleUpdateVpcEip`, `handleUpdateVpcSnat`, and `handleUpdateVpcDnat` share the same pattern (check vpcNatEnabled, LockKey, initCreateAt, list resources by label, loop and redo if Status.Redo != natGwCreatedAT). Extract a generic helper e.g. `handleUpdateVpcNatGwResources(natGwKey string, listFn func() ([]T, error), redoFn func(item, createdAT string) error)` or a small coordinator to reduce duplication.
- [ ] **Error message**: In `handleAddOrUpdateVpcNatGw` default branch (lines 255–262), when updating QoS the log for "add" says "failed to del qos" and the log for "del" says "failed to add qos"; swap messages so they match the operation.
- [ ] **Structure**: `handleAddOrUpdateVpcNatGw` is long; consider extracting `createNatGwStatefulSet`, `updateNatGwStatefulSet`, and `reconcileNatGwQoSChange` so the main function is a short sequence of conditionals and calls.
- [ ] **Maintainability**: The string "iptables nat gw not enable" is repeated in many handlers; use a named constant or package-level `errVpcNatGwNotEnabled` for consistency and easier change.
- [ ] **Naming**: Fix typo `execNatGwBandtithLimitRules` → `execNatGwBandwidthLimitRules` (and same typo in callers / other files e.g. vpc_nat_gw_eip.go, qos_policy.go).
- [ ] **Readability / Robustness**: `handleInitVpcNatGw` uses `time.Sleep(10 * time.Second)` when pod is not running; `getNatGwPod` uses `time.Sleep(5 * time.Second)` on "too many pod" or "pod is not active". Replace with a wait/poll loop with configurable timeout and max retries, or document why a single sleep is acceptable.
- [ ] **Error handling**: In `updateCrdNatGwLabels`, `raw, _ := json.Marshal(gw.Labels)` ignores the error; validate and return error on Marshal failure so invalid patch is not sent.
- [ ] **Maintainability**: Complete or remove the incomplete comment `// TODO:// check NAD if has ipam to disable ipam` at line 984 in `genNatGwStatefulSet`.

---

## ./pkg/controller/vpc_nat_gateway_test.go

- [ ] **DRY / Structure**: In `TestGetSubnetProvider`, the standalone `t.Run("Multiple provider scenarios", ...)` duplicates the pattern of the table-driven loop (newFakeControllerWithOptions, GetSubnetProvider, assert). Merge these cases into the table-driven `tests` slice as 2–3 additional rows (e.g. "default provider", "custom provider", "missing subnet") to remove duplication and keep one test style.
- [ ] **Readability**: The test struct field `description` is redundant with `name` for assertion messages; use `tt.name` in `assert`/`require` messages and remove `description` to simplify the struct.
- [ ] **DRY**: In `TestGetExternalSubnetNad`, many cases use the same `gw` (Name: "test-gw", ExternalSubnets: one element). Consider a shared helper or table-level default gw and override only when needed to reduce repetition.
- [ ] **Maintainability**: Both tests repeat the pattern `fakeController, err := newFakeControllerWithOptions(...); require.NoError(t, err); controller := fakeController.fakeController`. If this pattern appears in other controller test files, extract a helper e.g. `mustControllerFromOptions(t, opts) *Controller` to centralize and shorten setup.

---

## ./pkg/controller/vpc_nat.go

- [ ] **Maintainability / Robustness**: Package-level vars `vpcNatImage`, `vpcNatGwBgpSpeakerImage`, `vpcNatAPINadProvider` are global mutable state. When the ConfigMap is missing or invalid (e.g. no "image" field), the function returns without updating them, so previous values persist. Either reset these to empty on error or document that they retain last-good values. Prefer storing config on the Controller (e.g. a struct) to improve testability and avoid globals.
- [ ] **Readability**: The prefix assignment (lines 29–34) can be shortened to e.g. `util.VpcNatGwNamePrefix = cm.Data["natGwNamePrefix"]; if util.VpcNatGwNamePrefix == "" { util.VpcNatGwNamePrefix = util.VpcNatGwNameDefaultPrefix }` to reduce branching.

---

## ./pkg/controller/vpc_nat_gw_eip.go

- [ ] **Robustness**: In `enqueueAddIptablesEip` and `enqueueUpdateIptablesEip`, direct type assertions `obj.(*kubeovnv1.IptablesEIP)` and `oldObj.(*kubeovnv1.IptablesEIP)` can panic. Use safe type assertions (check `ok`) as in `enqueueDelIptablesEip`.
- [ ] **Naming**: Fix typo `delEIPBandtithLimitRules` → `delEIPBandwidthLimitRules`; align with vpc_nat_gateway.go and qos_policy.go.
- [ ] **Naming**: In `GetGwBySubnet` error message, fix "faile" → "failed".
- [ ] **Maintainability**: The string "iptables nat gw not enable" is repeated (handleAddIptablesEip, handleUpdateIptablesEip). Use a shared constant (e.g. with vpc_nat_gateway.go) for consistency.
- [ ] **Readability / Robustness**: In handleUpdateIptablesEip redo block (line 318), `eipRedo, _ := time.ParseInLocation(...)` ignores parse error; validate and handle so incorrect redo time does not affect comparison. Also the inner condition `cachedEip.Status.Ready && cachedEip.Status.IP != "" && ...` is inside a block where `!cachedEip.Status.Ready` is required—so it is always false; remove dead code or fix logic.
- [ ] **Error handling**: `eipV4Cidr, _ := util.SplitStringIP(subnet.Spec.CIDRBlock)` (and similar v4Cidr, _ in handleUpdateIptablesEip) ignores second return; document or handle if needed for dual-stack.
- [ ] **Structure**: `handleUpdateIptablesEip` is long (~170 lines); consider extracting redo reconciliation, QoS update, and deletion branches into helpers to improve readability and testability.
- [ ] **DRY**: addEipQoSInPod and delEipQoSInPod share getNatGwPod, build rules, switch on direction, execNatGwRules; consider a small helper that takes operation type to reduce duplication.
- [ ] **Readability**: Magic number `300*time.Millisecond` for subnet status queue delay appears twice; use a named constant.
- [ ] **Maintainability**: Comment "depreciated finalizer" in syncIptablesEipFinalizer should be "deprecated"; align with util constant naming.
- [ ] **Maintainability**: Complete or remove incomplete comment `// TODO:// ipv6` in createOrUpdateEipCR (line 562).

---

## ./pkg/controller/vpc_nat_gw_nat.go

- [ ] **Correctness**: In `redoFip` (line 754), the function ends with `return err`; when the `if redo != "" && redo != fip.Status.Redo` block is not entered, `err` may be nil or stale. Should be `return nil` on the success path after the block.
- [ ] **Correctness**: In `patchFipLabel` (line 638), comparison uses `fip.Labels[util.SubnetNameLabel]` but the value set is `eip.Spec.NatGwDp` (gateway name). Use `util.VpcNatGatewayNameLabel` for consistency with the label being set. Same in `patchDnatLabel` (line 767) and `patchSnatLabel` (line 866).
- [ ] **Naming / Log**: In `handleUpdateIptablesDnatRule` (line 346), log says "handle update iptables fip" but should say "dnat".
- [ ] **Naming**: Comment "migrate depreciated finalizer" in `syncIptablesFipFinalizer`, `syncIptablesDnatFinalizer`, `syncIptablesSnatFinalizer` — use "deprecated" (and align with util constant naming if DeprecatedFinalizerName is introduced).
- [ ] **Readability**: Magic string `"eip"` in `patchIptableInfo` (line 1069) — use a named constant (e.g. in util) for consistency with FipUsingEip, SnatUsingEip, DnatUsingEip.
- [ ] **Readability**: Time layout `"2006-01-02T15:04:05"` appears multiple times in redo blocks; extract a package-level constant (e.g. `redoTimeLayout`).
- [ ] **Error handling**: `v4Cidr, _ := util.SplitStringIP(...)` and similar in handleAddIptablesSnatRule, handleUpdateIptablesSnatRule, patchSnatStatus — document or handle the second return value for dual-stack correctness.
- [ ] **Error handling**: In redo blocks, `time.ParseInLocation("2006-01-02T15:04:05", cachedFip.Status.Redo, time.Local)` (and dnat/snat) ignore parse error; validate and handle so bad redo time does not skew comparison.
- [ ] **DRY**: `enqueueDelIptablesFip`, `enqueueDelIptablesDnatRule`, `enqueueDelIptablesSnatRule` share the same pattern (type switch for direct vs DeletedFinalStateUnknown, get key, log, add to queue). Extract a small generic helper to reduce duplication.
- [ ] **DRY**: `handleAddIptablesFipFinalizer`, `handleAddIptablesDnatFinalizer`, `handleAddIptablesSnatFinalizer` (and corresponding handleDel*) are identical except for lister and Patch API. Consider a generic finalizer add/remove helper parameterized by getter and patcher to reduce ~100 lines of duplication.
- [ ] **DRY**: `patchFipLabel`, `patchDnatLabel`, `patchSnatLabel` share the same structure (get, deep copy, needUpdateLabel/needUpdateAnno, update labels then annotations). Extract a helper parameterized by natType and optional extra label (e.g. Dnat's ExternalPort) to reduce duplication.
- [ ] **DRY**: `fipChangeEip`, `dnatChangeEip`, `snatChangeEip` are identical (status.V4ip vs eip.Status.IP). Replace with a single helper e.g. `ruleV4ipChangeEip(statusV4ip, eipIP string) bool`.
- [ ] **DRY**: The "redo" block in handleUpdateIptablesFip, handleUpdateIptablesDnatRule, handleUpdateIptablesSnatRule is nearly the same (get gw pod, parse redo time, compare with StartedAt, create in pod, patch status). Consider extracting a shared redo-reconcile helper parameterized by create/patch funcs.
- [ ] **Maintainability**: For consistency with FIP (finalizer added before createFipInPod to avoid orphan rules), consider adding finalizer before createDnatInPod/createSnatInPod in handleAddIptablesDnatRule and handleAddIptablesSnatRule to avoid the same race.
- [ ] **Structure**: File is large (~1170 lines) with repeated patterns for FIP/DNAT/SNAT. Consider splitting into vpc_nat_gw_nat_fip.go, vpc_nat_gw_nat_dnat.go, vpc_nat_gw_nat_snat.go and a shared vpc_nat_gw_nat_common.go for finalizer/label/status helpers to improve navigability and testability.

---

## ./pkg/controller/vpc_nat_gw_nat_test.go

- [ ] **DRY / Maintainability**: Test cases for `TestValidateDnat`, `TestValidateFip`, `TestValidateSnat` repeat full rule struct construction in every case. Consider helper constructors (e.g. `defaultValidDnatRule()` or `newDnatRule(eip, extPort, intPort, intIP, protocol string) *kubeovnv1.IptablesDnatRule`) so each case starts from a valid default and overrides only the field under test; reduces duplication and makes adding new cases easier.

---

## ./pkg/controller/vpc_test.go

- [ ] **DRY**: VPC object construction is repeated in all four subtests with minor variations (StaticRoutes). Extract a helper e.g. `testVpcWithStaticRoutes(routes []*kubeovnv1.StaticRoute) *kubeovnv1.Vpc` so each subtest only specifies the routes under test.
- [ ] **DRY**: Setup (newFakeController, vpcKeyMutex, Create VPC, Add to informer store) and common mock expectations (CreateLogicalRouter, UpdateLogicalRouter, ListLogicalRouterStaticRoutes, GetLogicalRouter, ClearLogicalRouterPolicy, ListLogicalSwitch/ListLogicalRouter AnyTimes, DeleteLogicalRouterPort, DeleteHAChassisGroup) are repeated in every subtest. Consider a helper that sets up controller + VPC and registers common expectations, so each t.Run only adds the route-specific expectations (ListLogicalRouterStaticRoutes return value, DeleteLogicalRouterStaticRoute, AddLogicalRouterStaticRoute).
- [ ] **Readability**: `externalIDs := map[string]string{"vendor": util.CniTypeName}` is repeated in every test; use a package-level test var or constant.

---

## ./pkg/controller/vip.go

- [ ] **Robustness**: In `enqueueAddVirtualIP`, use safe type assertion: `vip, ok := obj.(*kubeovnv1.Vip); if !ok { return }; key := cache.MetaObjectToName(vip).String()` to avoid panic if informer passes wrong type; align with `enqueueDelVirtualIP`.
- [ ] **Robustness**: In `enqueueUpdateVirtualIP`, use safe type assertions for `oldObj` and `newObj` instead of direct casts.
- [ ] **Error handling**: In `podReuseVip` and `releaseVip`, `json.Marshal(vip.Labels)` return value (error) is ignored; validate and return error on Marshal failure so invalid patch is not sent.
- [ ] **Naming**: In `handleAddOrUpdateVipFinalizer` and `handleDelVipFinalizer`, error log says "ovn eip" but the resource is VIP; change to "vip" for correct diagnostics.
- [ ] **Readability**: Remove or complete the incomplete comment `// TODO:// Ready = true as subnet.Status.Ready` at line 426 in `createOrUpdateVipCR`.
- [ ] **Naming**: In `syncVipFinalizer` comment, fix "depreciated" → "deprecated"; align with util constant fix (DepreciatedFinalizerName → DeprecatedFinalizerName) if applied elsewhere.
- [ ] **DRY**: In `podReuseVip` and `releaseVip`, the label patch payload construction (template, json.Marshal labels, fmt.Sprintf) is duplicated; extract a helper e.g. `buildVipLabelsPatchPayload(labels map[string]string) ([]byte, error)` and reuse.
- [ ] **Structure**: `handleAddVirtualIP` is long (~95 lines); consider extracting helpers e.g. `acquireVipAddress(c, vip, subnet, portName) (v4ip, v6ip, mac string, err error)`, and type-specific LSP creation blocks for SwitchLBRuleVip and KubeHostVMVip to improve readability and testability.
- [ ] **Readability**: In `handleUpdateVirtualParents`, the condition `strings.Split(pod.Annotations[util.AAPsAnnotation], ","); !slices.Contains(aaps, cachedVip.Name)` is dense; consider a helper e.g. `podHasAAP(pod *corev1.Pod, vipName string) bool` to clarify intent.

---

## ./pkg/controller/vlan.go

- [ ] **Naming**: In `handleDelVlan`, the variable `subnet` holds the result of `c.subnetsLister.List()` which is a slice of subnets; rename to `subnets` and use `for _, s := range subnets` for clarity.
- [ ] **DRY**: The block that sets `vlan.Spec.Provider = c.config.DefaultProviderName` when empty and updates the VLAN via API is duplicated in `handleAddVlan` and `handleUpdateVlan`; extract a helper e.g. `ensureVlanProvider(vlan *kubeovnv1.Vlan) (*kubeovnv1.Vlan, error)` to reduce duplication.
- [ ] **Maintainability**: Complete or remove the incomplete comment `// todo: check if vlan conflict in webhook` in `checkVlanConflict` (line 127); if deferred, use standard "TODO" and describe the intended webhook validation.
- [ ] **Readability**: In `checkVlanConflict`, once `conflict` is set to true and `conflictErr` is assigned, the loop can `break` since we only need to record that a conflict exists; avoids overwriting `conflictErr` and clarifies intent.
- [ ] **Structure**: `delLocalnet` is defined in vlan.go but only called from subnet.go; consider moving it to subnet.go (or provider_network.go if localnet is provider-scoped) so the caller and implementation live together, or add a brief comment that it is used by subnet for localnet port cleanup.

---

## ./pkg/daemon/config_linux.go

- [ ] **Correctness**: In `getIfaceByIP`, `addr.Contains(net.ParseIP(ip))` is called without validating `ip`. If `net.ParseIP(ip)` returns nil (invalid IP), passing it to `Contains` may panic or behave unexpectedly. Parse IP once before the loop, check for nil, then use the parsed IP in `Contains`.
- [ ] **Readability**: Add a one-line comment in `getSrcIPsByRoutes` explaining why only `r.Scope == netlink.SCOPE_LINK` routes are used (e.g. link-scope routes as candidate source IPs for tunnel).
- [ ] **Documentation**: Add a doc comment for `getIfaceByIP` documenting return values (ifaceName, mtu, err) for discoverability.

---

## ./pkg/daemon/config.go

- [ ] **Maintainability**: Implement or remove the TODO on line 100: "validate configuration" so invalid flag combinations or missing required fields are caught at startup.
- [ ] **Structure**: `ParseFlags()` is long (~100 lines) with many flag declarations and config population. Consider grouping flags by category (CNI, tunnel, TLS, etc.) or splitting into `defineFlags() (flagVals struct)` and `buildConfig(flagVals) *Configuration` to improve readability.
- [ ] **Structure**: `initNicConfig()` is long with a large branch when `config.Iface != ""`. Consider extracting the "resolve encap IP and MTU from specified iface" logic into a helper e.g. `resolveEncapIPFromIface(config *Configuration, tunnelNic string, nicBridgeMappings map[string]string) (encapIP string, mtu int, err error)` to shorten the function.
- [ ] **Readability**: Replace magic numbers with named constants: `3*time.Second` and `10` in `DialAPIServer`; `1000` QPS and `2000` Burst in `initKubeClient`; document or constant for IPv6 MTU adjustment (comment says "IPv6 header size is 40" but code subtracts 20 — clarify or fix).
- [ ] **Naming**: Inconsistent pflag variable prefixes (`argCniConfDir` vs `argsCniConfName`, `argsNetworkType`); use a consistent prefix (e.g. all `arg*`) for discoverability.

---

## ./pkg/daemon/config_test.go

- [ ] **Structure/Consistency**: Merge `TestGetEncapIPByNetworkEmptyNodeNetworks` into `TestGetEncapIPByNetwork` as additional table-driven cases (e.g. config with nil NodeNetworks + empty network name → default IP; nil NodeNetworks + non-existent network → error) so all GetEncapIPByNetwork behavior lives in one place and config setup is not duplicated.
- [ ] **Readability**: In `TestGetEncapIPByNetworkEmptyNodeNetworks`, use `t.Run` subtests for the two scenarios ("empty network returns default", "unknown network returns error") so failure messages pinpoint which case failed.

---

## ./pkg/daemon/controller.go

- [ ] **Naming**: Fix typo `runAddOrUpdateServicekWorker` → `runAddOrUpdateServiceWorker` (line 416); update call site in `Run()` (line 569).
- [ ] **DRY**: The work-item processing pattern (Get, shutdown check, defer Done, call handler, Forget/AddRateLimited, HandleError) is repeated in 7 methods. Consider a generic helper or small interface to reduce duplication while keeping type safety for different queue payload types.
- [ ] **Readability**: In `enqueueUpdatePod`, extract the long annotation comparison lists (default network and multi-net) into helpers e.g. `podDefaultNetworkAnnotationsChanged(oldPod, newPod *v1.Pod) bool` and `multiNetPodAnnotationsChanged(oldPod, newObj *v1.Pod, provider string) bool` to shorten the method and clarify intent.
- [ ] **Structure**: `initProviderNetwork` is long (~150 lines). Consider extracting: building `vlans` set and `vlanInterfaceMap` into a helper; and/or patch construction into a separate function to improve readability and testability.
- [ ] **Consistency**: In `processNextIPSecWorkItem`, `Done(key)` is deferred at the outer level (line 519); other processNext* methods defer `Done` inside the IIFE. Move the defer inside the IIFE for consistency.
- [ ] **Readability**: In `Run()`, replace magic durations (e.g. `time.Minute`, `10*time.Minute`, `5*time.Second`, `3*time.Second`) with named constants for worker intervals to ease tuning and documentation.
- [ ] **Consistency**: AddEventHandler error handling differs: servicesInformer uses `util.LogFatalAndExit`, others return `err` from `NewController`. Use the same strategy (either return all errors or fatal for all) for consistency.
- [ ] **Maintainability**: `ovnEipsLister` and `ovnEipsSynced` are set in `NewController` but no event handler is registered for OvnEips; cache sync also does not wait for `ovnEipsSynced`. Verify if OvnEip handling is needed and add handler/sync, or remove unused fields.

---

## ./pkg/daemon/controller_linux.go

- [ ] **Naming**: In `evalCommandSymlinks`, fix error message typo "failed to read evaluate symbolic links" → "failed to resolve symbolic links" or "failed to evaluate symbolic links".
- [ ] **Readability**: In `handleU2OInterconnectionMACChange`, remove redundant condition `if newMAC == "" && oldMAC == "" { return nil }` (already covered by `if oldMAC == newMAC { return nil }`).
- [ ] **Structure**: `reconcileRouters` is long (~200 lines). Consider extracting: building cidrs and joinCIDR from subnets into a helper; route diff and apply into a helper to improve readability and testability.
- [ ] **DRY**: In `initRuntime`, the IPv4 and IPv6 blocks (lines 114–136 and 137–159) are nearly identical; extract a helper e.g. `initProtocolRuntime(c *Controller, protocol string) error` to reduce duplication.
- [ ] **DRY**: In `handleUpdatePod`, the default-nic QoS block (SetInterfaceBandwidth, ConfigInterfaceMirror, SetNetemQos) and the multus-nic loop body are similar; extract a helper e.g. `applyPodQosForIface(podName, namespace, ifaceID string, ingress, egress, mirror string, netemAnnotations ...) error` to reduce duplication.
- [ ] **Naming**: `getRulesToAdd(oldRules, newRules)` returns rules in newRules not in oldRules; when called as `getRulesToAdd(newRules, oldRules)` it returns "rules to delete". Rename to e.g. `rulesInSecondNotInFirst(a, b []netlink.Rule) []netlink.Rule` and use for both add and del; same for `getRoutesToAdd`.
- [ ] **DRY**: In `rotateLog`, the three logrotate blocks are identical except for config path; extract `runLogrotate(configPath string)` and call it three times.
- [ ] **Readability**: In `routeDiff`, `joinIPv6` is explicitly unused (`_ = joinIPv6`). Either use it for dual-stack logic or remove the parameter to avoid confusion.

---

## ./pkg/daemon/exporter_metric.go

- [ ] **DRY**: Five functions (`setTCPTwRecycleMetric`, `setTCPMtuProbingMetric`, `setConntrackTCPLiberalMetric`, `setBridgeNfCallIptablesMetric`, `setIPv6RouteMaxsizeMetric`) share the same pattern: read single-line proc file, parse int, set metric. Extract a helper e.g. `setProcIntMetric(procPath string, setMetric func(float64))` that handles ReadFile, os.IsNotExist, TrimSpace, strconv.Atoi, and calling the setter; pass metric setter as closure to reduce ~50 lines of duplication.
- [ ] **Error handling**: Multiple places ignore `strconv.Atoi` error (e.g. lines 48, 95, 109, 124, 137, 152). On parse failure the metric gets 0 silently. At least log parse errors or set a sentinel value so misparsed proc files are visible.
- [ ] **DRY**: `setIPLocalPortRangeMetric` and `setTCPMemMetric` share the pattern: read proc file, strings.Fields, validate length (2 vs 3), set metric with label values. Consider a small helper e.g. `setProcFieldsMetric(procPath string, expectedLen int, setMetric func(...string))` to reduce duplication and centralize error handling.
- [ ] **Readability/Maintainability**: Proc paths are string literals (e.g. `/proc/sys/net/ipv4/tcp_tw_recycle`). Consider named constants (e.g. `procTCPTwRecycle`) at package level so paths are documented and easier to change.

---

## ./pkg/daemon/flow_rules_linux.go

- [ ] **DRY**: Protocol-to-OpenFlow string mapping (TCP/UDP × IPv4/IPv6 → tcp/tcp6/udp/udp6) is inline in `AddOrUpdateUnderlaySubnetSvcLocalFlowCache`. Extract a helper e.g. `underlayProtoOpenFlow(protocol string, isIPv6 bool) (string, error)` so the mapping is reusable and the main function stays shorter.
- [ ] **Readability**: The flow format string (lines 53–55) is long and mixes cookie, priority, in_port, proto, nw_dst, tp_dst, actions. Consider building parts (cookie, nwDst, protoStr) into a small struct or separate variables and then a single fmt.Sprintf for the final flow string to improve readability.
- [ ] **Maintainability**: The literal `"patch-localnet."` is OVS-specific. Consider a package-level const (e.g. `patchLocalnetPortName`) so the port name is documented and changeable in one place.

---

## ./pkg/daemon/flow_sync_linux.go

- [ ] **Correctness/Determinism**: In `storeFlowCache`, iteration over `entries` (map[string][]string) is non-deterministic. The order of flows in `snapshot[bridgeName]` may vary between runs. If OVS flow order matters, iterate over sorted keys (e.g. `maps.Keys` + sort) when building the snapshot; otherwise document that order is unspecified.
- [ ] **Readability**: In `filterUnmanagedFlows`, use positive condition: `if !isManagedFlow(flow) { filtered = append(filtered, flow) }` instead of "if isManagedFlow continue; else append", so the intent (keep unmanaged) is clearer.

---

## ./pkg/daemon/gateway.go

- [ ] **DRY**: `getSubnetsNeedNAT`, `getSubnetsNatOutGoingPolicy`, and `getSubnetsDistributedGateway` all call `c.subnetsLister.List(labels.Everything())` and iterate with similar filtering. Consider a helper e.g. `listSubnetsWithFilter(predicate func(*kubeovnv1.Subnet) bool) ([]*kubeovnv1.Subnet, error)` to centralize list + filter and error handling.
- [ ] **Error handling**: In `setGatewayBandwidth`, discard `ovs.IsHtbQos(ifaceID)` error with `htbQos, _ := ...`. Consider logging the error when non-nil for debugging.
- [ ] **Readability**: Long condition in `isSubnetNeedNat` could be split: extract a helper for "default VPC + protocol match" (e.g. `subnetInDefaultVpcWithProtocol(subnet, protocol) bool`) and keep NatOutgoing-specific logic in `isSubnetNeedNat`.
- [ ] **Readability**: In `getEgressNatIPByNode`, nested loops and gateway-format parsing are dense. Extract a helper e.g. `parseGatewayNodeNatIPs(subnet *kubeovnv1.Subnet, nodeName string) map[string]string` and add a short doc for the "node:ip" format to clarify intent.
- [ ] **Naming**: Function `getCidrByProtocol` uses inconsistent abbreviation; consider `getCIDRByProtocol` for CIDR.
- [ ] **Performance**: Multiple functions (`getSubnetsNeedNAT`, `getSubnetsNatOutGoingPolicy`, `getSubnetsDistributedGateway`, `getDefaultVpcSubnetsCIDR`) each call `c.subnetsLister.List(labels.Everything())`. If called in sequence from gateway logic, consider a single pass that returns needed subsets (e.g. struct with NAT CIDRs, NAT policy subnets, distributed GW CIDRs, default VPC CIDRs) to avoid repeated list operations.

---

## ./pkg/daemon/gateway_linux.go

- [ ] **Correctness**: In `generateNatOutgoingPolicyChainRules`, inside the inner loop over `subnet.Status.NatOutgoingPolicyRules`, the code reassigns the parameter `protocol` with `protocol = getMatchProtocol(rule.Match.SrcIPs)` and `protocol = getMatchProtocol(rule.Match.DstIPs)`. This mutates the function's `protocol` for subsequent subnets, so the next subnet's `getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)` may use the wrong protocol. Use a local variable (e.g. `matchProtocol`) for the match result instead of overwriting `protocol`.
- [ ] **Error message**: Line 103: log says "failed to get subnets with centralized gateway" but the function is `getSubnetsDistributedGateway`; change to "distributed gateway".
- [ ] **Error handling**: In `setExGateway`, after the `ovs.Exec(ovs.MayExist, "add-br", ...)` block (lines 902–905), `err` is assigned but the following `if err = addOvnMapping(...)` overwrites it. If Exec failed, return that error before calling addOvnMapping.
- [ ] **DRY**: The logic to build `protocols` (dual → [IPv4, IPv6], else → [c.protocol]) is repeated in `setIPSet`, `gcIPSet`, `setPolicyRouting`, and `setIptables` with two styles (make+index vs make+append). Extract a helper e.g. `c.getGatewayProtocols() []string` and reuse so style and behavior are consistent.
- [ ] **DRY**: `addPodPolicyRouting` and `deletePodPolicyRouting` share nearly identical logic to build `prMetas` from `egw` and `ips`. Extract a helper e.g. `buildPolicyRouteMetas(podProtocol, externalEgressGateway string, ips []string) []policyRouteMeta` and reuse in both.
- [ ] **DRY**: `addPolicyRouting` and `deletePolicyRouting` share the same block (maskBits, rule setup, loop over ips with rule.Src, then RuleAdd vs RuleDel). Extract a helper that builds rule specs and accepts an action (add/del) to reduce duplication.
- [ ] **DRY**: `v4Rules` and `v6Rules` in `setIptables` (and similarly `v4ObsoleteRules`/`v6ObsoleteRules` in `cleanObsoleteIptablesRules`) differ only by ipset prefix ("ovn40" vs "ovn60"). Consider `buildOvnIptablesRules(ipsetPrefix string) []util.IPTableRule` to generate rules from a prefix and avoid large duplicated slices.
- [ ] **Readability**: Magic number `1048576` (IPSet MaxSize) appears many times; extract a const e.g. `defaultIPSetMaxSize = 1048576`.
- [ ] **Readability**: Line 252: "failed to get local pod ips failed" is redundant; use "failed to get local pod IPs: %+v".
- [ ] **Readability**: In `deleteObsoleteSnatRules`, the loop variable `rule` is shadowed by `rule := rule[4+len(chain):]`; use distinct names (e.g. `ruleLine` and `ruleSpec`) to avoid confusion.
- [ ] **Structure**: `setIptables` is very long (~270 lines). Consider splitting: e.g. `buildBaseIptablesRules(protocol) ([]util.IPTableRule, []util.IPTableRule)` and a smaller `reconcileProtocolIptables(protocol, ...) error` so the main loop is shorter and pieces are testable.

---

## ./pkg/daemon/gateway_test.go

- [ ] **Naming**: Fix typo `expetced` → `expected` in the test table struct and all case fields (consistent with condition_test.go and other test files).
- [ ] **Readability**: For cases "get ipv4 from ipv6" and "get ipv6 from ipv4", explicitly set `expected: ""` so the expected empty result is self-documenting instead of relying on zero value.

---

## ./pkg/daemon/handler.go

- [ ] **Structure**: `handleAdd` is very long (~335 lines). Consider splitting: e.g. `waitForPodAnnotations(csh, podRequest) (*v1.Pod, *kubeovnv1.Subnet, error)` for the retry loop, and `configurePodNicAndRespond(...)` or similar for the OVN-provider block (gateway check, MTU, encapIP, configureNic/configureDpdkNic, mirror, egress), so the main flow is shorter and testable.
- [ ] **DRY**: The error-response pattern (WriteHeaderAndEntity with CniResponse{Err: ...}, then klog.Errorf on write failure) is repeated many times in handleAdd and handleDel. Extract a helper e.g. `writeCniError(resp *restful.Response, status int, errMsg error)` to reduce duplication and ensure consistent logging.
- [ ] **Readability**: Replace magic numbers: `for range 20` (handleAdd wait loop, UpdateIPCR) and `time.Sleep(1 * time.Second)` with named constants (e.g. `podAnnotationWaitRetries = 20`, `podWaitInterval = 1 * time.Second`) for tuning and documentation.
- [ ] **Readability**: Gateway check mode resolution (subnetHasVlan, DisableGatewayCheck, MigrationJobNameAnnotation, ActivationStrategyTemplate) is nested and dense. Extract e.g. `resolveGatewayCheckMode(pod *v1.Pod, podSubnet *kubeovnv1.Subnet) int` to name the concept and simplify handleAdd.
- [ ] **Comment**: Fix typo "For Support kubevirt" → "To support KubeVirt" or "For KubeVirt hotplug" (line 157 and similar).
- [ ] **Maintainability**: In UpdateIPCR, when Get succeeds but Update fails, the code overwrites `err` and retries; after 20 retries it returns nil without returning the last error. Consider returning the last error after max retries or adding a short comment that ignoring update failure is intentional.
- [ ] **Performance**: `providerExists(provider)` calls `csh.Controller.subnetsLister.List(labels.Everything())` and iterates; if handleAdd/handleDel are called frequently, consider caching subnets-by-provider or passing the already-fetched podSubnet to avoid repeated list when the same provider is used.

---

## ./pkg/daemon/handler_linux.go

- [ ] **Readability**: Fix error message typos: "can not found" → "cannot find" (line 36); "volume name %s is exists" → "volume %s already exists" (line 67).
- [ ] **Structure**: In `createShortSharedDir`, extracting the volume lookup (loop over `pod.Spec.Volumes` by name) into a helper e.g. `findVolumeByName(pod *v1.Pod, name string) *v1.Volume` would shorten the function and clarify intent.
- [ ] **Error handling**: In `removeShortSharedDir`, when `os.Stat(sharedDir)` returns an error that is not `os.IsNotExist(err)`, the code falls through and `err` is later overwritten by `os.ReadDir`; the Stat error (e.g. permission denied) is never returned. Add `if err != nil { return err }` after the first `if os.IsNotExist(err)` block so non-IsNotExist errors are surfaced.
- [ ] **Readability**: The condition `strings.Contains(newSharedDir, util.DefaultHostVhostuserBaseDir)` controls whether to bind-mount; consider a named variable e.g. `needsBindMount` or a short comment to clarify intent.

---

## ./pkg/daemon/init.go

- [ ] **Correctness**: Line 82: error message uses `mac` but at that point only `macAddr` (string) is defined; `mac` is assigned on the next line. Use `macAddr` in the format string: `fmt.Errorf("failed to parse mac %s %w", macAddr, err)`.
- [ ] **Naming**: Fix typo `changeProvideNicName` → `changeProviderNicName` (Provide → Provider); update call sites in init.go and init_linux.go.
- [ ] **Readability**: Replace magic numbers `3 * time.Second` (retry interval in InitNodeGateway) with a named constant for easier tuning.

---

## ./pkg/daemon/init_linux.go

- [ ] **Naming**: Fix typo `changeProvideNicName` → `changeProviderNicName` (Provide → Provider).
- [ ] **Readability**: In `waitNetworkdConfiguration`, replace magic numbers (100ms, 50ms) with named constants for timeout and wait duration.
- [ ] **Readability**: The string literal `"openvswitch"` (line 59) could be a package-level constant for consistency and to avoid typos.

---

## ./pkg/daemon/ipsec.go

- [ ] **Correctness**: Line 33: `ipsecReqPath = ipsecKeyDir + "ipsec-req.pem"` is missing a slash; the path becomes `/etc/ovs_ipsec_keysipsec-req.pem`. Use `ipsecKeyDir + "/ipsec-req.pem"` to match other path specs (lines 35–37).
- [ ] **DRY**: `needNewCert` and `untilCertRefresh` both read a cert file, decode PEM, and parse with x509; the half-validity logic is duplicated. Extract helpers e.g. `parseCertFromPath(path string) (*x509.Certificate, error)` and reuse for both.
- [ ] **Readability**: In `linkCACertToIPSecDir`, the variable `filepath` (line 316) shadows the imported package `filepath`; rename to e.g. `certFilePath` or `path`.
- [ ] **Structure**: In `SyncIPSecKeys`, the two consecutive `if needNewCert { ... }` blocks (create keys, then configure OVS and clear dir) can be merged into one block to avoid repeating the condition and clarify the flow.
- [ ] **Maintainability**: `clearCACertToIPSecDir` removes only `ipsec-cacert.pem`, while `linkCACertToIPSecDir` writes `ca-001.pem`, `ca-002.pem`, etc. If both are used in the same flow, consider clearing the same files (e.g. remove all under ipsecCADir) for consistency.
- [ ] **Readability**: Magic numbers (e.g. 300*time.Second in CreateIPSecKeys, 2048 in genrsa) could be named constants for tuning and documentation.

---

## ./pkg/daemon/listen.go

- [ ] **Maintainability**: Add brief godoc for `listen` documenting that the second return value is a cleanup function that removes the socket file and logs (but does not return) errors, so callers know cleanup failures are not surfaced.
- [ ] **Testability**: Consider adding a unit test that creates a listener on a temp socket, closes it, calls the cleanup, and verifies the socket file is removed (or that listening again on the same path succeeds).

---

## ./pkg/daemon/metrics.go

- [ ] **Correctness**: `cniWaitRouteResult` is defined and used in `pkg/daemon/handler.go` but never registered in `InitMetrics()`. Add `metrics.Registry.MustRegister(cniWaitRouteResult)` so the metric is exposed.
- [ ] **Structure**: InitMetrics registers three cni* metrics inline and uses helpers for gateway and system metrics; consider `registerCniMetrics()` that registers all four cni* metrics for consistency and to avoid missing one.
- [ ] **Readability**: Help strings for `cniWaitAddressResult`, `cniWaitRouteResult`, and `cniConnectivityResult` say "Latency" but they are CounterVec; clarify in Help that these are cumulative seconds (e.g. "Total seconds that cni waits for ...").

---

## ./pkg/daemon/netns_linux.go

- [ ] **Readability/Style**: Use `go func() { ... }()` instead of `go (func() { ... })()`; the extra parentheses around the func literal are redundant and the former is more idiomatic.

---

## ./pkg/daemon/nm_linux.go

- [ ] **Naming**: Fix typo "mannaged" → "managed" in log message (line 249).
- [ ] **DRY**: The two branches in the event loop (IP4Config vs IP6Config, lines 106–127) are nearly identical; extract a helper that returns the device for the given event path to reduce duplication.
- [ ] **Readability**: Replace magic number `200*time.Millisecond` with a named constant (e.g. `dbusConnectionTimeout`).
- [ ] **Structure**: `SetManaged` is long (~110 lines); consider extracting the "should skip when setting managed=false" logic (vlan check, version check, DNS config check) into a helper to improve readability.
- [ ] **Correctness/Race**: In `ProcessNextItem`, `bridge` is read from `n.bridgeMap` after releasing the lock; another goroutine could remove the device in between. Consider holding the lock while reading `bridge` and passing to handler, or document that handler must tolerate empty bridge.
- [ ] **Maintainability**: The intent of the `dbus.SystemBus(ctx)` + `<-ctx.Done()` pattern is unclear (comment says "wait for connection to be closed" but the connection is not explicitly closed); add a brief comment or clarify the purpose.

---

## ./pkg/daemon/ovs.go

- [ ] **DRY**: `configureGlobalMirror` and `configureEmptyMirror` share nearly identical logic; only the mirror creation args differ (`select_all=true` vs absent). Extract e.g. `configureMirrorPort(portName string, mtu int, selectAll bool) error` to build OVS args and call `configureMirrorLink`, reducing duplication and fixing the inconsistent error log in `configureEmptyMirror` (line 142 vs 144: one includes `%v` for err, the other omits it).
- [ ] **Readability**: In `removeOvnMapping`, replace `length := len(mappings); delete(mappings, key); if len(mappings) == length` with the idiomatic `if _, ok := mappings[key]; !ok { return nil }` before delete.
- [ ] **Structure**: `configExternalBridge` is long; consider extracting "ensure bridge exists and set options" and "clean up unmanaged ports on bridge" into helpers (e.g. `ensureExternalBridge`, `removeUnmanagedPorts`) to improve readability and testability.
- [ ] **Maintainability**: `pingGateway` depends on package-level `cniConnectivityResult` and `nodeName`; either pass them as parameters (or via a struct) for testability, or document the dependency in a package comment.
- [ ] **Extensibility**: `encodeOvnMappings` / `decodeOvnMappings` do not escape ":" in values; if a value can contain ":", decoding will split incorrectly. Document the format or add escaping if needed for future use.

---

## ./pkg/daemon/ovs_linux.go

- [ ] **Correctness**: In `configureNodeNic`, `_, cidr, _ := net.ParseCIDR(c)` (line 471) ignores the parse error; malformed CIDR can lead to nil cidr and panic. Check err and skip or return.
- [ ] **Correctness**: In `loopOvn0Check`, when link is down we call `util.LogFatalAndExit(err, "node nic %s is down", util.NodeNic)` but `err` is still nil (from the previous successful LinkByName). Pass a proper error e.g. `fmt.Errorf("node nic %s is down", util.NodeNic)`.
- [ ] **Correctness**: In `removeNodeGwNic`, the error format expects (port, bridge, err) but arguments are passed as ("br-int", util.NodeGwNic, err). Swap to (util.NodeGwNic, "br-int", err) so the message is correct.
- [ ] **Error message**: In `configureNic` (line 174), `fmt.Errorf("failed to parse mac %s %w", macAddr, err)` uses `macAddr` before it is set (ParseMAC failed). Use the input string `mac` instead of `macAddr`.
- [ ] **Error message**: In `setVfMac` (line 1189), same issue: use input `mac` in the error message instead of `macAddr` when ParseMAC fails.
- [ ] **Naming**: Fix typo "faild" → "failed" in error message in `checkNodeGwNicInNs` (line 559).
- [ ] **Readability**: Replace magic numbers with named constants (e.g. ovs interface ready timeout 30s, wait interval 500ms, TxQLen 1000, gateway check retries 3/5, waitNetworkReady 10×500ms in waitIPv6AddressPreferred).
- [ ] **Structure**: `configureContainerNic` is long (~170 lines); consider extracting "configure default gateway", "add extra routes", and "gateway readiness check" into helpers to improve readability and testability.
- [ ] **Structure**: `configureNodeGwNic` switch (ProtocolIPv4/ProtocolIPv6/ProtocolDual) repeats RouteReplace logic; extract a helper e.g. `addDefaultRoute(linkIndex, gw string, dst *net.IPNet) error` to reduce duplication.
- [ ] **DRY**: `configureNic` and `configureHostNic` both do "get link, set up if down, set txqlen"; consider sharing a small helper for the common "bring link up and set qlen" steps if more call sites emerge.
- [ ] **Maintainability**: In `waitIPv6AddressPreferred`, the loop variable `retry` and parameter `retryInterval` are similar; consider naming the counter `attempt` to avoid confusion with the interval.

---

## ./pkg/daemon/server.go

- [ ] **Readability**: Replace magic number `3 * time.Second` (ReadHeaderTimeout) with a named constant (e.g. `httpReadHeaderTimeout`).
- [ ] **Readability**: In `requestAndResponseLogger`, variable `elapsed` is in milliseconds; consider renaming to `elapsedMs` and add a brief comment or constant for the conversion to seconds (elapsedMs/1000) when passing to `Observe`, to avoid confusion.
- [ ] **Style**: Use `&restful.WebService{}` instead of `new(restful.WebService)` for consistency with common Go struct literal style.
- [ ] **Structure**: The "listen, defer cleanFunc, Serve" pattern in `RunServer` is similar to other daemon entrypoints; if a shared server startup helper is introduced elsewhere (e.g. pkg/metrics), consider reusing it here for consistency.

---

## ./pkg/daemon/tproxy_linux.go

- [ ] **Correctness/Log**: In `cleanTProxyRoutes`, the third error log (for `delRouteIfExist`) says "delete tproxy route rule mark" but the operation deletes a route; use "delete tproxy route failed" so the message matches the operation.
- [ ] **Naming/Log**: In `cleanTProxyRoutes` and rule/route helpers, keep "rule" vs "route" distinct in error messages (use "rule" when deleting rules, "route" when deleting routes) so logs are unambiguous.
- [ ] **Safety**: In `probePortInNs`, use a type assertion with ok (e.g. `nsStr, ok := podNs.(string); if !ok { return }`) before calling `ns.GetNS` to avoid panic if a non-string was stored in the map.
- [ ] **Readability**: In `streamConn`, the log "copy stream from dst %v to src %v" is reversed relative to `io.Copy(dst, src)`; use "copy stream from src to dst" or "copy stream to %v from %v" so it matches the direction of data flow.
- [ ] **DRY**: `addRuleIfNotExist` and `deleteRuleIfExists` both call `netlink.RuleListFiltered(family, &netlink.Rule{Mark: mark}, ...)`; consider extracting e.g. `getRulesByMark(family, mark) ([]netlink.Rule, error)` to reduce duplication.
- [ ] **Performance/Simplicity**: In `delRouteIfExist`, the code lists all routes in the table then deletes one; consider building the route and calling `netlink.RouteDel`, ignoring `syscall.ENOENT`, to avoid the list and simplify.
- [ ] **Extensibility**: `GetDefaultRouteDst` returns zero value for `ProtocolDual`; add a default case or document that callers must not pass ProtocolDual, to avoid misuse.
- [ ] **Style**: Use "cannot" instead of "can't" in the error "can't find device lo" for consistency with Go documentation style.

---

## ./pkg/metrics/client_go_adapter.go

- [ ] **Readability**: In `latencyAdapter.Observe`, the parameter `u url.URL` could be named `reqURL` (or `targetURL`) to clarify it is the request URL, since single-letter `u` is vague.
- [ ] **Maintainability**: Add a short comment that the "url" label uses `path.Dir(u.Path)` to limit Prometheus label cardinality (per-URL would be unbounded); helps future readers understand the choice.
- [ ] **Structure** (optional): `InitClientGoMetrics()` is a thin wrapper around `registerClientMetrics()`; consider inlining into the single call site if there is only one, or keep as explicit public API for clarity—low priority.

---

## ./pkg/metrics/dynamic_cert_key.go

- [ ] **Readability**: Replace magic numbers with named constants: RSA key size `2048` (lines 58, 116), cert validity `time.Hour * 24 * 365`, renewal window `30*24*time.Hour` in `generateCertKeyPair` (e.g. `renewBeforeExpiry = 30 * 24 * time.Hour`).
- [ ] **Naming**: In `tlsGetConfigForClient`, the inner `if controller, ok := certKeyProvider.(dynamiccertificates.ControllerRunner)` shadows the outer `controller` (the DynamicServingCertificateController). Use a different name (e.g. `runner`) inside the if to avoid confusion.
- [ ] **Maintainability**: `stopCh` in `tlsGetConfigForClient` is never closed, so the context passed to certKeyProvider's Run is never cancelled and the goroutine selecting on stopCh/ctx.Done() never exits. Either wire stopCh to a process-level shutdown or document that this is long-lived and intentionally not closed.
- [ ] **Safety**: `CurrentCertKeyContent()` does `c.certKeyPair.Load().(*certKeyPair)` with no nil check; if Load() returns nil (e.g. before first `generateCertKeyPair`), this panics. Use type assertion with ok or ensure Store is always called before any caller uses CurrentCertKeyContent, and document the ordering.
- [ ] **Readability**: `Name()` returns `""`; consider returning a meaningful identifier (e.g. `"dynamic-in-memory-cert-key"` or `c.host`) for logging/debugging, or add a comment explaining why empty is required by the interface.

---

## ./pkg/metrics/klog.go

- [ ] **DRY**: The pattern of setting Lines and Bytes for INFO, WARN, ERROR is repeated (six `WithLabelValues` calls). Use a loop over levels (e.g. `[]string{"INFO","WARN","ERROR"}`) and a small helper or two loops to reduce duplication.
- [ ] **Readability**: Replace magic `5*time.Second` in `wait.Until` with a named constant (e.g. `klogMetricsInterval`) for easier tuning.

---

## ./pkg/metrics/server.go

- [ ] **DRY**: The health paths `"/healthz"`, `"/livez"`, `"/readyz"` appear in both `filterProvider` (switch) and `Run` (ExtraHandlers). Define a shared constant slice or var (e.g. `healthPaths`) and use it in both places to avoid drift.
- [ ] **Performance / Logic**: When `secureServing` is false, `Run` still parses TLS versions, builds TLS config, and calls `tlsGetConfigForClient` (which generates CA and certs). Skip TLS parsing and `tlsGetConfigForClient` when `!secureServing` to avoid unnecessary crypto work and startup cost.
- [ ] **DRY** (optional): The pprof handlers (`/debug/pprof/`, cmdline, profile, symbol, trace) are added in five separate lines; consider a loop over a map of path -> handler to reduce repetition.

---

## ./pkg/informer/kubevirt.go

- [ ] **Correctness/Doc**: Fix interface comment for `VirtualMachine()`: it says "handles the VMIs that are stopped or not running" but the method returns an informer for VirtualMachine resources, not VMI; clarify the comment.
- [ ] **DRY**: In `GetVirtualMachineInformerIndexers`, the "dv" and "pvc" indexers share the same pattern (type assert, range over volumes, collect namespace/name for a volume type); extract a helper e.g. `volumeKeys(vm *kubev1.VirtualMachine, extract func(vol) (string, bool)) ([]string, error)` to reduce duplication.
- [ ] **Consistency**: VM indexers return `errUnexpectedObject` for wrong type while migration indexers return `nil, nil`; consider unifying (e.g. return nil, nil for wrong type in VM indexers) so behavior is consistent.
- [ ] **Performance**: In `WaitForCacheSync`, the lock is held while building the `syncs` slice; copy informer references under lock, then build syncs and call `WaitForCacheSync` outside the lock to avoid blocking `Start`/`getInformer`.
- [ ] **Readability**: Consider reducing log verbosity for "SKIPPING informer" in `Start` (e.g. klog.V(4)) since multiple idempotent calls can be noisy.

---

## ./pkg/internal/big_int.go

- [ ] **Correctness**: `DeepCopyInto` uses `n.FillBytes(b.Bytes())`; `big.Int.Bytes()` returns the absolute value and `FillBytes` sets magnitude only, so the sign of the source is lost. Use `n.Set(&b.Int)` to copy the full Int including sign.
- [ ] **Performance**: In `Add` and `Sub`, `big.NewInt(0)` allocates on every call. Use `var z big.Int; z.Add(&b.Int, &n.Int); return BigInt{z}` (and similarly for Sub) to avoid the extra allocation.
- [ ] **Performance** (minor): In `UnmarshalJSON`, `string(p) == "null"` allocates. Consider `bytes.Equal(p, []byte("null"))` for the null check to avoid allocation when input is "null".

---

## ./pkg/ipam/ipam.go

- [ ] **Correctness**: In `AddOrUpdateSubnet`, lines 234 and 281: `_, cidr, _ := net.ParseCIDR(...)` ignores the error; nil cidr could cause panic. Check and return error. Similarly, lines 239–240 and 286–287: `util.FirstIP`/`LastIP` errors are ignored.
- [ ] **Correctness / Naming**: In the `for name, p := range subnet.IPPools` blocks (lines 253, 301), the loop variable `name` shadows the function parameter `name` (subnet name). Inside the loop, klog and logic that intend “subnet name” accidentally use the pool key. Use a different loop variable (e.g. `poolName`) and keep `name` for the subnet.
- [ ] **Readability / Variable shadowing**: In `GetStaticAddress`, parameter `ip` (string) is shadowed by the loop variable `ip` (IP) from `ip, err := NewIP(ipStr)`. The switch at 102–105 then uses `ip` (the last loop value). Rename the loop variable (e.g. `parsedIP`) to avoid confusion and ensure the correct value is returned (e.g. input `ip` string vs parsed IP’s string).
- [ ] **DRY / Structure**: The V4 update block (lines 232–276) and the V6 update block (278–322) in `AddOrUpdateSubnet` are almost identical. Extract a helper (e.g. `updateSubnetCIDRForFamily(subnet, cidrStr, gw, reserved, "v4"|"v6")`) to reduce duplication and improve maintainability.
- [ ] **Error handling**: `GetSubnetV4Mask` ignores the error from `subnet.V4CIDR.Mask.Size()`; `GetSubnetIPRangeString` ignores errors from `NewIPRangeListFrom`. Handle or propagate errors to avoid silent wrong behavior.
- [ ] **Maintainability**: `checkAndAppendIpsForDual`: when `len(ips)==1` and the IP is neither IPv4 nor IPv6, the function returns `(nil, nil)`. Consider returning an explicit error for unsupported/invalid single-IP in dual-stack to make behavior clear.

---

## ./pkg/ipam/ipam_test.go

- [ ] **DRY**: Repeated setup (v4/v6/dual exclude IPs, NewSubnet, ipam.Subnets[name] = subnet) appears in many tests. Extract test helpers (e.g. `mustAddV4Subnet(t, ipam, name, cidr, excludeIps)`) or use table-driven tests for GetRandomAddress/GetStaticAddress/ContainAddress to reduce duplication.
- [ ] **Correctness**: `TestIPAMAddOrUpdateSubnet` lines 478–481: "test invalid v6 cidr" uses the same invalid CIDR as the v4 case (`"10.0.0./24,2001:db8::/64"`). Use a distinct invalid v6 CIDR (e.g. `"10.0.0.0/24,2001:g6::/64"`) so the v6 branch is actually tested.
- [ ] **Correctness**: `TestIPAMAddOrUpdateSubnetWithIPPools` lines 559–562: After allocating from `dualSubnetName` and adding `dualPoolName`, assertions check `ipam.Subnets[v4SubnetName].IPPools[v4PoolName]` and `ipam.Subnets[v6SubnetName].IPPools[v6PoolName]`. The dual-stack allocation belongs to `dualSubnetName`’s `dualPoolName`; assert `ipam.Subnets[dualSubnetName].IPPools[dualPoolName].V4Using` / `.V6Using` instead so the test validates the correct pool.
- [ ] **Readability**: Line 577 comment says "remove already exist pool" but the block removes an already-removed pool; use "remove non-existent pool (idempotent)" or similar.
- [ ] **Consistency**: Use `require.Equal(t, expected, actual)` throughout; fix `require.Equal(t, err, ErrInvalidCIDR)` and `require.Equal(t, err, ErrNoAvailable)` to `require.Equal(t, ErrInvalidCIDR, err)` and `require.Equal(t, ErrNoAvailable, err)` (lines 441, 461, 479, 481, 641).

---

## ./pkg/ipam/ip.go

- [ ] **Performance**: In `LessThan` and `GreaterThan`, four `big.NewInt(0)` allocations per call. Introduce a private `cmp(a, b IP) int` that reuses two big.Ints (or use local variables) so both methods become one-liners and allocations drop.
- [ ] **DRY**: `LessThan` and `GreaterThan` share the same SetBytes/Cmp logic; extract `cmp(a, b IP) int` and implement `LessThan` as `return cmp(a, b) < 0`, `GreaterThan` as `return cmp(a, b) > 0`.
- [ ] **Naming**: Rename unexported `bytes2IP` to `bytesToIP` to follow Go naming (avoid "2" abbreviation).
- [ ] **Performance**: In `Add` and `Sub`, multiple `big.NewInt(0)` and repeated `a.To4()`/`a.To16()` calls; reuse big.Int and cache To4/To16 result in a local variable to reduce allocations.
- [ ] **Readability**: Add a one-line comment for `bytes2IP` (or `bytesToIP`) explaining it normalizes byte length (pad or trim to `length` bytes).

---

## ./pkg/ipam/ippool.go

- [ ] **Structure / Extensibility**: `IPPool` has repeated V4/V6 field pairs (IPs, Free, Available, Reserved, Released, Using). Consider a nested struct e.g. `type FamilyPool struct { IPs, Free, Available, Reserved, Released, Using *IPRangeList }; type IPPool struct { V4, V6 FamilyPool }` to reduce duplication and make adding new buckets easier. This is a breaking API change; only do if call sites can be updated.

---

## ./pkg/ipam/ip_range.go

- [ ] **Error handling**: `NewIPRangeFromCIDR` line 21: `start, _ := NewIP(...)` ignores the error from `NewIP`. Invalid CIDR or mask could yield nil start and cause panics. Return `(*IPRange, error)` and propagate the error, or document and panic with a clear message.
- [ ] **Error handling**: `Random()` line 58: `n, _ := rand.Int(rand.Reader, ...)` ignores the read error. Handle or document that random read failure is treated as zero.
- [ ] **Performance**: `Count()` and `Random()` allocate multiple `big.NewInt(0)` per call. Reuse big.Int (e.g. local variables or a small helper) to reduce allocations in hot paths.
- [ ] **Readability**: In `Remove`, the condition `!r1.start.GreaterThan(r1.end)` expresses "valid non-empty range". Consider a one-line comment or a small helper (e.g. `validRange(r *IPRange) bool`) for clarity.

---

## ./pkg/ipam/ip_range_list.go

- [ ] **Robustness**: In `NewIPRangeListFrom`, when parsing ".." ranges, `strings.Split(s, "..")` does not trim spaces. Input like `"1.1.1.1 .. 1.1.1.2"` can make `NewIP(ips[0])` fail. Use `strings.TrimSpace(ips[0])` and `TrimSpace(ips[1])` before calling `NewIP`.
- [ ] **Maintainability**: In `Merge` (line 271), `ret.ranges[i].end = ret.ranges[i+1].end` directly mutates the unexported field. Use `ret.ranges[i].SetEnd(ret.ranges[i+1].End())` to keep encapsulation and consistency with the rest of the codebase.
- [ ] **Performance**: In `ToCIDRs`, the loop allocates multiple `new(big.Int)` per iteration (networkInt, increment). Reuse big.Int variables (e.g. pool or locals) to reduce allocations for large range lists.
- [ ] **Readability**: The coalescing loop in `Merge` (lines 268–274) uses `i--` after `slices.Delete` to re-check the same index; add a one-line comment explaining this so future edits don’t remove it by mistake.
- [ ] **Logging**: In `NewIPRangeListFrom`, `klog.Error(err)` before each `return nil, err` may duplicate logs when callers also log. Consider logging only at V(4) or removing if callers always log.

---

## ./pkg/ipam/ip_range_list_test.go

- [ ] **DRY**: `TestNewIPRangeList` and others repeat nearly identical v4 and v6 blocks (Contains, Add, Remove, Separate, Merge). Extract helpers (e.g. `mustNewIP(t, s string) IP`, `mustNewIPRangeList(t, pairs ...string) *IPRangeList`) or use table-driven tests with address family as a dimension to cut duplication.
- [ ] **Consistency**: Use `require.Equal(t, expected, actual)` throughout. Fix calls like `require.Equal(t, v4MergedRangeList.Len(), 1)` to `require.Equal(t, 1, v4MergedRangeList.Len())` (and similar) so the expected value is first.
- [ ] **Maintainability**: In `TestRemove`, tests access unexported fields (`v4IPRange.start`, `removed[0].start`, etc.). Use `.Start()` and `.End()` methods instead so tests stay valid if internal representation changes.
- [ ] **Naming**: Fix typo `v4SplitedExpect` / `v6SplitedExpect` → `v4SplitExpect` / `v6SplitExpect` (line 255 and similar).

---

## ./pkg/ipam/ip_range_test.go

- [ ] **Correctness**: In TestIPRangeClone (lines 59-61), the second assertion uses r.Start().Equal(clone.End()) with message "modified the original range end"; it should verify that the original range end was not modified. Use !r.End().Equal(end) and error "Clone() should create a new copy, but it modified the original range end".
- [ ] **Consistency**: TestIPRangeRemove uses .To4() for IPv4 literals; other tests (e.g. TestNewIPRange, TestIPRangeContains) do not. Use .To4() consistently for IPv4 to normalize representation and avoid flaky behavior, or add a short helper (e.g. mustIPv4(t, s string) IP) for test readability.
- [ ] **DRY**: Repeated construction of start/end (e.g. IP(net.ParseIP("192.168.1.1")), IP(net.ParseIP("192.168.1.10"))) appears in many tests. Consider test helpers (e.g. mustNewIP(t, s string) IP) or package-level test vars for common ranges to reduce duplication.

---

## ./pkg/ipam/ip_test.go

- [ ] **Structure**: TestAddOrUpdateSubnet (lines 410-641) tests IPAM-level behavior (AddOrUpdateSubnet, GetStaticAddress, GetRandomAddress, etc.), not the IP type. Consider moving it to ipam_test.go so ip_test.go only contains IP type tests and discoverability is improved.
- [ ] **Maintainability**: TestAddOrUpdateSubnet is ~230 lines and repeats the same flow three times (IPv4, IPv6, DualStack). Extract helpers (e.g. runSubnetFlow(t, ipam, name, cidr, gw, excludeIPs, family)) or use a table-driven dimension for address family to reduce duplication.
- [ ] **Consistency**: Tests for IP type (TestNewIP, TestIPClone, etc.) use t.Errorf; TestAddOrUpdateSubnet uses require. Use require.* throughout (require.NoError, require.Equal, require.False) for consistency with the rest of the file and project.
- [ ] **Consistency**: Fix require.Equal argument order to (expected, actual), e.g. require.Equal(t, ip, "10.17.0.2") → require.Equal(t, "10.17.0.2", ip) (and similar throughout TestAddOrUpdateSubnet).
- [ ] **Maintainability**: Remove commented-out code at line 447: `// require.Equal(t, nil, ipam.Subnets[subnetName].V6Reserved.ranges)`.
- [ ] **Naming**: Fix comment typo at line 396: `// release pod with single nic")` → `// release pod with single nic` (remove stray quote and parenthesis).
- [ ] **Reproducibility**: TestAddOrUpdateSubnet uses rand.Int()+32 and rand.Int()+128 for invalid mask length; prefer fixed values (e.g. 33, 129) so the test is deterministic.
- [ ] **Correctness**: TestIPTo16 "nil IP" case calls tt.ip.To16() which may panic if To16() does not handle nil receiver; add a nil check or skip the case with t.Skip, or use require.Panics if nil is documented as invalid.

---

## ./pkg/ipam/subnet.go

- [ ] **Error handling**: NewSubnet ignores errors from util.FirstIP, util.LastIP, and NewIPRangeListFrom (lines 93-94, 97-98, 95, 100, 105-110). Invalid CIDR could yield wrong or nil range; return or handle errors.
- [ ] **Variable shadowing**: In getV4RandomAddress and getV6RandomAddress, loop variable `s` in `for _, s := range skippedAddrs` shadows the receiver `s` (*Subnet). Rename to e.g. `addrStr` or `skippedAddr` for readability.
- [ ] **DRY**: getV4RandomAddress and getV6RandomAddress (lines 227-282 and 284-339) are nearly identical; only V4 vs V6 fields and return order differ. Extract a helper parameterized by address family to reduce duplication and ease maintenance.
- [ ] **DRY**: GetStaticAddress has large duplicated v4 and v6 blocks (existPod check, Reserved/Free/Released handling). Consider a helper or table-driven logic by family to reduce duplication.
- [ ] **DRY**: releaseAddr has two nearly identical V4 and V6 blocks (lines 534-574 and 575-615). Extract e.g. releaseAddrForFamily(v4 bool, ...) to reduce duplication.
- [ ] **DRY**: AddOrUpdateIPPool V4 and V6 blocks (700-720 and 721-741) are nearly identical; extract a helper for "init pool range list for one family" (CIDR, IPs, conflict check).
- [ ] **Performance**: IPPoolStatistics holds Mutex.Lock for read-only access; use RLock to allow concurrent reads.
- [ ] **Robustness**: GetRandomMac uses an unbounded for loop; consider a max retry count and return error if MAC space is exhausted to avoid theoretical spin.

---

## ./pkg/ipam/subnet_test.go

- [ ] **Correctness**: Lines 187, 201, 291, 305, 372, 384, 389: `require.NotNil(nil, err)` and `require.Nil(nil, ip)` pass `nil` as the first argument instead of `t`. Use `require.Error(t, err)` and `require.Nil(t, ip)` so assertions run correctly.
- [ ] **Naming**: Fix test name typos: `TestGetGetV4RandomAddress`, `TestGetGetV4RandomAddressPTP`, `TestGetGetV6RandomAddress` — remove duplicate "Get" (e.g. `TestGetV4RandomAddress`).
- [ ] **Naming**: Fix subnet name typos in tests: "testV4RleasedSubnet" → "testV4ReleasedSubnet" (line 413), "testV6RleasedSubnet" → "testV6ReleasedSubnet" (line 424), "testDualRleasedSubnet" → "testDualReleasedSubnet" (line 437).
- [ ] **Readability**: Replace `// TODO://` with `// TODO:` in comments (lines 37, 77, 46, 86).
- [ ] **Consistency**: Use `require.Equal(t, expected, actual)` throughout; e.g. `require.Equal(t, subnet.V4Using.Len(), 0)` → `require.Equal(t, 0, subnet.V4Using.Len())` so expected is first.
- [ ] **DRY**: TestNewSubnetIPv4, TestNewSubnetIPv6, TestNewSubnetDualStack repeat similar field checks (CIDR, Free, Reserved, Available, Using, NicToIP, IPToPod). Extract helpers e.g. `assertSubnetV4Fields(t, subnet, name, cidr, expectedFree, expectedReserved)` to reduce duplication.
- [ ] **DRY**: TestGetV4StaticAddress, TestGetV6StaticAddress, TestGetDualStaticAddress share the same scenario structure (pod with IP only, pod with IP+MAC, pod with MAC only error, duplicate MAC error, IP already assigned). Consider table-driven subtests or a shared helper parameterized by protocol.
- [ ] **DRY**: TestReleaseAddrForV4Subnet, TestReleaseV6SubnetAddrForV6Subnet, TestReleaseAddrForDualSubnet are nearly identical (get address, assert Using, release, assert empty). Use table-driven test with subnet CIDR and protocol as dimensions.
- [ ] **Maintainability**: TestSubnetAddOrUpdateIPPool and TestSubnetRemoveIPPool have long repeated blocks asserting all pool fields (V4IPs, V6IPs, V4Free, V6Free, ...). Extract `assertPoolFields(t, pool, v4IPs, v6IPs, v4Free, v6Free, ...)` or `assertDefaultPoolState(t, subnet, expectedV4IPs, expectedV6IPs)` to shorten and centralize assertions.
- [ ] **Readability**: Remove redundant assertions: TestNewSubnetIPv4 (and v6/dual) assert V4Available.Equal(V4Free) and require.NotNil(V4Available) twice; remove duplicate.
- [ ] **Maintainability**: TestSubnetAddOrUpdateIPPool has duplicate require.NotNil/Equal for same fields (e.g. defaultPool.V4IPs twice, v4ValidPool.V4IPs and V6IPs twice, v6ValidPool.V4Reserved twice, dualValidPool.V4IPs twice). Remove duplicate assertions.
- [ ] **Structure**: TestGetStaticAddressReleaseExisting uses t.Run subtests; consider refactoring other long tests (TestGetV4StaticAddress, TestSubnetAddOrUpdateIPPool) into t.Run("case1", ...) for consistency and selective execution.
- [ ] **Readability**: TestSubnetReleaseAddr comments "1.1", "1.2", "2.", "2.2" could be clearer; use "case: release non-existent IP", "case: release from excluded IP" for readability.

---

## ./pkg/netconf/netconf.go

- [ ] **Structure / Readability**: In `MarshalJSON`, extract the "merge IPAM into fixupObj" block (lines 39-47) into a helper e.g. `mergeIPAMInto(fixupObj map[string]any, ipam *IPAMConf) error` so the main flow (marshal via type alias, unmarshal to map, merge IPAM, marshal back) is clearer.
- [ ] **Maintainability**: Replace broad `//nolint:all` on line 29 with a specific directive (e.g. the actual linter rule being suppressed) so future readers know why the lint is disabled.
- [ ] **Documentation**: Add a short comment above `MarshalJSON` explaining that the type alias and map merge are used to avoid recursion with embedded `types.NetConf` which may define its own `MarshalJSON`.

---

## ./pkg/net/yusur/yusur_sriovnet.go

- [ ] **Correctness**: In `GetYusurNicVfIndexByPciAddress`, `virtFnRe.FindStringSubmatch(match)` can return nil when the regex does not match; accessing `result[1]` then panics. Check `result == nil || len(result) < 2` before using `result[1]`.
- [ ] **Maintainability**: Error messages `errors.New("pfPath is not ")` (lines 46, 78) are incomplete and misleading (line 78 refers to vfPath). Use descriptive messages e.g. "path is not under PciSysDir" and fix vfPath case.
- [ ] **DRY**: The pattern `filepath.Abs(...); err != nil || !strings.HasPrefix(absPath, PciSysDir)` is repeated in `IsYusurSmartNic`, `GetYusurNicPfPciFromVfPci`, and `GetYusurNicVfIndexByPciAddress`. Extract a helper e.g. `resolvePathUnderPciSys(relPath string) (string, error)` for consistent validation and error handling.
- [ ] **Documentation**: Fix `GetYusurNicPfIndexByPciAddress` doc: it says "gets a VF PCI address" but the parameter is PF PCI (`pfPci`). Use "gets a PF PCI address and returns the corresponding PF index" (and fix "correlate" → "corresponding" in both function comments).

---

## ./pkg/ovn_ic_controller/config.go

- [ ] **Readability**: Fix struct comment "Configuration is the controller conf" → "Configuration is the controller config" for consistency.
- [ ] **Readability**: Replace or clarify the vague comment "change the behavior of cmdline // not exit. not good" (lines 68-69) with a clear explanation e.g. "Init with ContinueOnError so parse errors do not exit the process", or remove if redundant.

---

## ./pkg/ovn_ic_controller/controller.go

- [ ] **Correctness**: `Run()` calls `cache.WaitForCacheSync(stopCh, c.subnetSynced, c.nodesSynced)` but does not wait for `c.configMapsSynced` or `c.vpcSynced`. `resyncInterConnection` uses `configMapsLister` and `vpcsLister` is used in `ovn_ic_controller.go`. Add `c.configMapsSynced` and `c.vpcSynced` to `WaitForCacheSync` so workers do not run before all required caches are synced.
- [ ] **Readability**: Replace magic intervals `time.Second` and `5*time.Second` in `Run()` with named constants (e.g. `resyncInterval`, `routeSyncInterval`) for tunability and documentation.
- [ ] **Structure**: `NewController` is long and mixes scheme registration, event broadcaster, informer setup, struct build, and OVN client creation. Consider extracting helpers (e.g. `newEventRecorder`, `newOvnClients`) to improve readability and testability.
- [ ] **Dead code**: The `return` after `util.LogFatalAndExit(nil, "failed to wait for caches to sync")` in `Run()` is unreachable; remove it for clarity.

---

## ./pkg/ovn_ic_controller/ovn_ic_controller.go

- [ ] **Structure / Maintainability**: Package-level mutable globals (`icEnabled`, `lastIcCm`, `lastTSs`, `curTSs`) make state hard to test and reason about. Consider moving into Controller struct or a dedicated state holder for testability.
- [ ] **Structure**: `resyncInterConnection()` is long (~100 lines) with nested conditionals. Split into smaller functions e.g. `handleICDisabled()`, `handleICFirstEstablish()`, `handleICConfigChange()` for readability.
- [ ] **DRY**: In `icConfigChange` branch, after `establishInterConnection(cm.Data)` the assignments `icEnabled = "true"`, `lastIcCm = cm.Data`, `lastTSs = curTSs` duplicate the success path of `icFirstEstablish`; `curTSs` may be stale here (not re-fetched after reestablish). Consider re-fetching transit switches after establish and extracting common "set IC state on success" helper.
- [ ] **Readability**: Config keys ("enable-ic", "az-name", "ic-db-host", "gw-nodes", etc.) are magic strings. Define named constants (e.g. in config.go or this file) for consistency and documentation.
- [ ] **Readability**: `getICState` returns int (iota); consider a named type or doc comment for the three states (NoAction, FirstEstablish, ConfigChange) so call sites are self-explanatory.
- [ ] **Correctness**: In `RemoveOldChassisInSbDB`, error message uses `lastIcCm["az-name"]` but parameter is `azName`; use `azName` in the error for consistency (lastIcCm may be nil).
- [ ] **Robustness**: In `acquireLrpAddress`, infinite loop `for { ... time.Sleep(time.Second) }` has no backoff or max retries. Consider max retries or exponential backoff to avoid busy-wait under contention.
- [ ] **Naming**: Variable `chassises` is non-standard plural; use `chassisList` or `chassisNames`. `icTSs` could be `icTransitSwitches` for readability.
- [ ] **Readability**: `gwNodes := strings.Split(strings.Trim(config["gw-nodes"], ","), ",")` can produce empty strings for trailing commas; consider filtering empty entries or using a helper that returns trimmed non-empty slice.
- [ ] **DRY**: `genHostAddress` loop (build string from hostList) could use `strings.Join` with a mapped slice (e.g. each element `fmt.Sprintf("tcp:[%s]:%s", host, port)`) for clarity and to avoid manual index handling.

---

## ./pkg/ovs/ovn-nb-acl.go

- [ ] **Error message**: Line 455: UpdateACL returns `errors.New("address_set is nil")` but the parameter is `acl`; fix to "acl is nil".
- [ ] **DRY**: UpdateIngressACLOps and UpdateEgressACLOps share the same structure (meter create/delete, newNetworkPolicyACLMatch, loop over matches with options, CreateAclsOps). Extract a helper e.g. `updateDirectionACLOps(pgName, direction string, ..., namedPortMap)` parameterized by direction to reduce duplication.
- [ ] **DRY**: The options closure pattern (setACLName, apply-after-lb for from-lport, Log/Meter) repeats in UpdateDefaultBlockACLOps, UpdateDefaultBlockExceptionsACLOps, UpdateIngressACLOps, UpdateEgressACLOps, UpdateAnpRuleACLOps, UpdateCnpRuleACLOps. Consider a small helper that returns option funcs based on direction and logging config.
- [ ] **DRY**: newAnpACLMatch and newCnpACLMatch are nearly identical (ipSuffix, srcOrDst, portDirection, allIPMatch, selectIPMatch, loop over ports). Extract shared logic or a generic helper parameterized by port type to reduce duplication.
- [ ] **DRY**: newSgRuleACL and sgRuleNoACL duplicate the same match-building logic (ipSuffix, srcOrDst, allIPMatch, allowedIPMatch, protocol switch). Extract e.g. `buildSgRuleMatch(rule, pgName, direction) (match string, priority int)` and reuse.
- [ ] **Readability**: In options at lines 84–90, `if loggingEnabled && logRate > 0` is redundant inside `if loggingEnabled`; simplify to `if logRate > 0 { acl.Meter = ptr.To(meterName) }`. Same pattern in UpdateIngressACLOps and UpdateEgressACLOps (logEnable checked twice).
- [ ] **Structure**: UpdateLogicalSwitchACL (~85 lines) mixes allowEWTraffic loop, subnet ACL loop, delOps/addOps, Transact. Extract e.g. `buildAllowEWTrafficACLs(lsName, cidrBlock, options)` and `buildSubnetACLs(lsName, subnetAcls, options)` for clarity.
- [ ] **Structure**: SetLogicalSwitchPrivate (~120 lines) has nodeSubnetACLFunc and allowSubnetACLFunc; consider extracting the outer cidr loop into e.g. `buildPrivateSwitchACLs(lsName, cidrBlock, nodeSwitchCIDR, allowSubnets)` returning []*ovnnb.ACL.
- [ ] **Maintainability**: DHCP port magic strings "67", "68", "547", "546" appear in UpdateDefaultBlockExceptionsACLOps and CreateSgBaseACL. Define named constants (e.g. dhcpv4ServerPort, dhcpv4ClientPort, dhcpv6ServerPort, dhcpv6ClientPort).
- [ ] **Naming**: sgRuleNoACL returns true when the rule has no ACL (ACL missing). Consider sgRuleMissingACL or ruleACLNotPresent for clarity.
- [ ] **Performance**: CleanNoParentKeyAcls does two WhereCache List calls per ACL (port groups, logical switches). For many ACLs consider listing port groups and logical switches once and building aclUUID -> parents map to avoid O(n) list per ACL.
- [ ] **Error handling**: In UpdateDefaultBlockExceptionsACLOps and CreateSgBaseACL, the newACL closure on error logs and returns from the closure but does not set an outer err; acls may be partial and CreateAclsOps is still called. Accumulate errors or make newACL return error and check in caller.
- [ ] **Readability**: In GetACL, `intPriority, _ := strconv.Atoi(priority)` ignores parse error; invalid priority yields 0. Consider validating and returning error for non-numeric priority.

---

## ./pkg/ovs/ovn-nb-acl_test.go

- [ ] **DRY**: The `expect` helper (e.g. `expect := func(row ovsdb.Row, action, direction, match, priority string)`) is duplicated in testUpdateDefaultBlockExceptionsACLOps, testUpdateDefaultBlockACLOps, testUpdateIngressACLOps, testUpdateEgressACLOps, testUpdateAnpRuleACLOps, testUpdateCnpRuleACLOps. Extract to a suite-level or package-level helper to reduce duplication.
- [ ] **Maintainability**: TODO comments at lines 1012 and 1744 ("// TODO:// should err but not for now") should be resolved or removed.
- [ ] **Naming**: Test method `testnewNetworkPolicyACLMatch` (line 1212) should be `testNewNetworkPolicyACLMatch` for consistency with other test method names (capital N).
- [ ] **Readability**: Remove commented-out code at line 1738 (`// nbClient := suite.ovnNBClient`).
- [ ] **Readability**: Line 1754: `fmt.Println(newACL.Priority)` in test produces side-effect output; replace with `require.Equal(t, 1005, newACL.Priority)` or remove.

---

## ./pkg/ovs/ovn-nb-bfd.go

- [ ] **Correctness**: `ListDownBFDs` (line 42) and `ListUpBFDs` (line 61) dereference `bfd.Status` without nil check; if Status is nil this will panic. Add nil check before `*bfd.Status`.
- [ ] **Naming / Typo**: Line 136 and 163: error message "failed to delete BFD with with UUID" has double "with"; fix to "failed to delete BFD with UUID".
- [ ] **DRY**: `bfdAddL3HAHandler` and `bfdUpdateL3HAHandler` both use the same retry loop (try 1..3/4, sleep 5s, call `isLrpBfdUp`). Extract a helper e.g. `recheckBfdStatusUntilUp(lrpName, dstIP string, maxTries int)` to reduce duplication.
- [ ] **Readability**: Magic numbers (5 * time.Second, "15 seconds" in comment, 3/4 tries) should be named package-level constants (e.g. `bfdStatusCheckInterval`, `bfdRecheckMaxTries`) for documentation and tuning.
- [ ] **Structure**: `bfdUpdateL3HAHandler` is long (~150 lines) with nested branches. Extract handlers: e.g. `handleBfdAdminDownToDown`, `handleBfdDownToUp` (raise chassis priority), `handleBfdUpToDown` (lower chassis, recheck loop) for readability and testability.
- [ ] **Maintainability**: The infinite `for { ... }` loop in `bfdUpdateL3HAHandler` (when status Up->Down) has no max retries; could run forever if BFD never comes up. Consider max attempt count or timeout.
- [ ] **Readability**: Chassis priority deltas (`util.GwChassisMaxPriority + 1` and `util.GwChassisMaxPriority - 5`) use magic 1 and 5; consider named constants (e.g. `gwChassisRaisedPriorityOffset`, `gwChassisLoweredPriorityOffset`) for clarity.

---

## ./pkg/ovs/ovn-nb-dhcp_options.go

- [ ] **Correctness**: In `GetDHCPOptions`, when `len(dhcpOptList) == 0` and `!ignoreNotFound`, the return uses `fmt.Errorf("... %w", lsName, protocol, err)` but at that point `err` is from the preceding `ListDHCPOptions` which succeeded (err is nil). Use a distinct error e.g. `fmt.Errorf("not found logical switch %s %s dhcp options", lsName, protocol)` instead of wrapping nil.
- [ ] **Naming**: In `GetDHCPOptions`, the error message says "the logical router name is required" but the parameter is logical switch name (lsName). Change to "the logical switch name is required".
- [ ] **DRY**: `updateDHCPv4Options` and `updateDHCPv6Options` share the same flow (validate protocol, GetDHCPOptions, update or create, GetDHCPOptions again). Consider extracting a helper parameterized by protocol and option-builder to reduce duplication.
- [ ] **Readability**: Magic strings ("protocol", "vendor", "lease_time", "router", "server_id", "server_mac", "mtu") could be named constants for external_ids and option keys to avoid typos and improve documentation.

---

## ./pkg/ovs/ovn-nb-gateway_chassis.go

- [ ] **Correctness**: In `DeleteGatewayChassises`, when `DeleteGatewayChassisOp` (lines 66–69) or `Mutate` (lines 77–80) returns an error, the code does `return nil` instead of `return err`, so callers get success while operations failed. Return the error to the caller.
- [ ] **DRY**: The pattern `lrpName + "-" + chassisName` for gateway chassis name appears in `CreateGatewayChassisesOp`, `newGatewayChassis`, and `DeleteGatewayChassises`. Extract a helper e.g. `func gatewayChassisName(lrpName, chassisName string) string` for consistency and single place to change if naming changes.

---

## ./pkg/ovs/ovn-nb-address_set.go

- [ ] **Documentation**: Comment for `BatchDeleteAddressSetByNames` says "BatchDeleteAddressSetByAsName"; fix to "ByNames" to match the function name.
- [ ] **Maintainability**: In `addressSetFilter`, the doc comment refers to "to-lport and from-lport acls" but this filter is for address sets, not ACLs; fix comment to refer to address sets.
- [ ] **Correctness / API**: In `AddressSetUpdateAddress`, the CIDR-formatting loop mutates the caller's `addresses` slice in place. Document that the slice may be modified, or copy the slice at the start to avoid surprising callers.
- [ ] **Robustness**: In `DeleteAddressSet`, when `asName` contains duplicate names, the same AddressSet is appended to `delList` multiple times. Dedupe by name before building `delList` to avoid redundant delete operations.
- [ ] **Error handling**: Several methods call `klog.Error(err)` then return a wrapped error; consider a consistent pattern (e.g. wrap only, or log at debug) to avoid duplicate or noisy logging.

---

## ./pkg/ovs/ovn-nb-address_set_test.go

- [ ] **Maintainability**: Two `t.Run` in `testUpdateAddressSet` share the same description "update with nil address set"; rename the second to e.g. "UpdateAddressSet with nil receiver" to distinguish them.
- [ ] **Documentation**: Test "update with invalid address" expects NoError for invalid CIDR "192.168.1.1/xx"; add a short comment that invalid CIDRs are currently skipped (not an error) so future behavior changes are intentional.

---

## ./pkg/ovs/ovn-nb.go

- [ ] **Maintainability**: Fix typo "transcation" → "transaction" in comment (line 151: "ovsdb ACID transcation").

---

## ./pkg/ovs/ovn-nb_global.go

- [ ] **DRY**: `SetNodeLocalDNSIP` and `SetSkipConntrackCidrs` share the same pattern: if value non-empty call `SetNbGlobalOptions`, else get nb global, copy options, delete key, update. Extract a helper e.g. `removeNbGlobalOption(key string) error` to reduce duplication.
- [ ] **Error handling**: Consistent use of `klog.Error(err)` before return across the file; consider a single pattern (e.g. log at debug in helper or only wrap) to avoid duplicate logging when errors propagate.

---

## ./pkg/ovs/adapter.go

- [ ] **Readability**: Comment says "OVN NB metrics" but the metric is used for ovsdb, ovn-nb, ovn-sb, ovn-ic-nb, ovn-ic-sb (see ovs-vsctl.go, ovn.go, ovn-ic-*ctl.go). Change to e.g. "OVS/OVN client request latency metrics" for accuracy.
- [ ] **Readability**: Histogram bucket args (1, 2, 10) are magic numbers; consider named constants (e.g. `histogramBucketStart`, `histogramBucketFactor`, `histogramBucketCount`) for documentation and tuning.
- [ ] **Extensibility**: When more OVS client metrics are added, consider registering collectors from a slice in a loop (consistent with pkg/ovnmonitor/metric.go refactor) to avoid repeated `MustRegister` calls.

---

## ./pkg/ovs/const.go

- [ ] **Naming**: File is named const.go but contains only a function `CmdSSLArgs`, no constants. Consider renaming to ssl.go or cmd_args.go to match content, or move SSL-related constants here if they live elsewhere.
- [ ] **Documentation**: Add a one-line doc comment for `CmdSSLArgs` (e.g. "returns CLI arguments for OVS/OVN SSL connections") for discoverability.

---

## ./pkg/ovs/ovn.go

- [ ] **DRY**: `NewOvnNbClient` and `NewOvnSbClient` have nearly identical retry logic (lines 179-200 and 221-242). Extract a helper function e.g. `createOvsDbClientWithRetry(dbName, addr string, dbModel model.ClientDBModel, monitors []client.MonitorOption, conTimeout, inactivityTimeout, maxRetry int) (client.Client, error)` to eliminate duplication.
- [ ] **Readability**: Replace magic numbers with named constants: `2 * time.Second` (retry sleep interval) and `500` (slow operation threshold in milliseconds). Define e.g. `const retrySleepInterval = 2 * time.Second` and `const slowOperationThresholdMs = 500`.
- [ ] **Structure**: The monitor setup in `NewOvnNbClient` (lines 156-177) is a long list of `client.WithTable` calls. Consider extracting to a helper function e.g. `nbMonitors() []client.MonitorOption` for better organization and readability.
- [ ] **Readability**: In `Transact`, the elapsed time calculation `elapsed := float64((time.Since(start)) / time.Millisecond)` could be simplified to `elapsed := float64(time.Since(start).Milliseconds())` for clarity.
- [ ] **Maintainability**: The TODO comment on line 253 "support ic-nb ic-sb client" should either be implemented or removed if not planned.
- [ ] **Error handling**: In `NewOvnNbClient` and `NewOvnSbClient`, when retry succeeds, the code breaks and returns the client, but the error variable from the last failed attempt is still in scope. Consider clearing it or using a more explicit success flag for clarity.

---

## ./pkg/ovs/ovn-ic-nbctl.go

- [ ] **DRY**: The elapsed time calculation `elapsed := float64((time.Since(start)) / time.Millisecond)` is duplicated across multiple files (ovn-ic-nbctl.go, ovn-ic-sbctl.go, ovs-vsctl.go, ovn-nb-logical_router_policy.go). Extract to a helper function e.g. `elapsedMs(start time.Time) float64` returning `float64(time.Since(start).Milliseconds())` for consistency and readability.
- [ ] **Readability**: Replace magic number `500` (slow operation threshold in milliseconds) with a named constant (e.g. `const slowCommandThresholdMs = 500`) shared across all command execution files.
- [ ] **DRY**: The method extraction pattern (lines 21-26) that finds the first non-flag argument is likely duplicated in other command execution functions. Extract to a helper e.g. `extractMethodName(cmdArgs []string) string` for reuse.
- [ ] **Structure**: The command execution, timing, metrics recording, and error handling pattern is very similar to `ovn-ic-sbctl.go`, `ovs-vsctl.go`, and other command files. Consider extracting a shared helper function that handles timing, metrics, and error reporting to reduce duplication.

---

## ./pkg/ovs/ovn-ic-nbctl_test.go

- [ ] **Testability / Maintainability**: Tests only assert that commands fail (ovn-ic-nbctl not found). Implement mock IC NB DB (as per TODO in code) so IC NB client behavior can be tested without real ovn-ic-nbctl.
- [ ] **DRY**: The three tests (`testOvnIcNbCommand`, `testGetTsSubnet`, `testGetTs`) share the same pattern: call method, `require.Error`, `require.Empty`. Consider table-driven tests with subtests to reduce duplication.

---

## ./pkg/ovs/ovn-ic-sbctl_test.go

- [ ] **Testability / Maintainability**: All 11 tests only assert that commands fail (ovn-ic-sbctl not found). Implement mock IC SB DB so IC SB client behavior can be tested without real ovn-ic-sbctl.
- [ ] **DRY**: The 11 tests share the same pattern (call method, require.Error, optionally require.Empty). Consider table-driven tests with subtests to reduce duplication.

---

## ./pkg/ovs/ovn-ic-sbctl.go

- [ ] **DRY**: Same elapsed time calculation duplication as `ovn-ic-nbctl.go`; use shared helper `elapsedMs(start time.Time) float64`.
- [ ] **Readability**: Replace magic number `500` with named constant `slowCommandThresholdMs`.
- [ ] **DRY**: Method extraction pattern (lines 20-25) should use shared helper `extractMethodName`.
- [ ] **DRY**: Command execution pattern duplicated from `ovn-ic-nbctl.go`; extract shared helper.
- [ ] **DRY**: The `DestroyGateways`, `DestroyRoutes`, `DestroyPortBindings` functions (lines 110-138) follow the same pattern: iterate over UUIDs and call `DestroyTableWithUUID` with different table names. Extract to a generic helper e.g. `destroyTableRecords(uuids []string, table string) error` to reduce duplication.
- [ ] **Readability**: In `GetAzUUID` (line 80), the error message "two same-name chassises in one db is insane" is informal; use a more professional message e.g. "multiple availability zones with the same name found".

---

## ./pkg/ovs/interface.go

- [ ] **Naming**: Chassis interface method `GetKubeOvnChassisses()` has a typo: "Chassisses" (three s's). Use `GetKubeOvnChassises()` to match `CreateGatewayChassises` and fix all call sites (pkg/controller/init.go, pkg/ovs/ovn-sb-chassis.go, tests, mocks).
- [ ] **API / Idiom**: `GetKubeOvnChassisses() (*[]ovnsb.Chassis, error)` returns a pointer to slice, which is uncommon in Go. Consider returning `([]ovnsb.Chassis, error)` and updating call sites for consistency and simpler usage.

---

## ./pkg/ovsdb/client/client.go

- [ ] **Error message**: Line 118: "failed to connect to OVN NB server" hardcodes "NB"; the function is generic for any db. Use e.g. `klog.Errorf("failed to connect to OVN %s server %s: %v", db, addr, err)`.
- [ ] **Structure / DRY**: TLS setup (LoadX509KeyPair, ReadFile CA, NewCertPool, tls.Config) is a ~15-line block; extract e.g. `newTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error)` for readability and potential reuse.
- [ ] **Readability**: Parameters `ovsDbConTimeout` and `ovsDbInactivityTimeout` are in seconds but not documented; add doc comment or name (e.g. `ovsDbConTimeoutSec`) to make units explicit.

---

## ./pkg/ovs/ovn-nb-ha_chassis_group.go

- [ ] **Naming**: Parameter `chassises` is non-idiomatic (chassis is already plural); use `chassisNames` or `chassisList` for clarity. Similarly, `haChassises` holds HAChassis entities—consider `haChassisList` to distinguish from chassis name strings.
- [ ] **Readability**: Magic number `100 - i` for priority could be a named constant (e.g. `defaultHAChassisPriorityStart = 100`) so tuning and intent are explicit.
- [ ] **Error handling**: Multiple `klog.Error(err); return err` blocks in `CreateHAChassisGroup`; consider wrapping with context once (e.g. `fmt.Errorf("...: %w", err)`) at function exit for clearer error chain.
- [ ] **Maintainability**: In the create branch, `group.ExternalIDs = map[string]string{"vendor": ...}` then `maps.Insert`; in the update branch the same pattern is repeated. Extract a small helper e.g. `mergeHAChassisGroupExternalIDs(group *ovnnb.HAChassisGroup, externalIDs map[string]string)` to avoid drift.

---

## ./pkg/ovs/ovn-nb-ha_chassis_group_test.go

- [ ] **Readability / Naming**: Loop variable `uuid` shadows the imported package `uuid` (e.g. `for _, uuid := range group.HaChassis`). Rename to `chassisUUID` or `haChassisUUID` to avoid shadowing and clarify meaning.
- [ ] **DRY**: In `testCreateHAChassisGroup`, the block that fetches the group and verifies HaChassis (GetHAChassisGroup, require ExternalIDs/Len, loop over group.HaChassis and assert chassis name/priority/ExternalIDs) is repeated after create and after update. Extract a helper e.g. `requireHAChassisGroupMatches(t, nbClient, name string, expectedChassises []string, expectedExternalIDs map[string]string)` to reduce duplication.
- [ ] **Readability**: Magic number `100` in `100-slices.Index(chassises, chassis.ChassisName)` could be a named constant (e.g. `defaultHAChassisPriorityStart`) for consistency with ovn-nb-ha_chassis_group.go and to document intent.

---

## ./pkg/ovs/ovn-nb-load_balancer.go

- [ ] **Naming**: In `LoadBalancerExists`, the variable `lrp` (suggests Logical Router Port) is used for the load balancer; use `lb` for clarity.
- [ ] **Maintainability**: Doc comment for `SetLoadBalancerPreferLocalBackend` says "sets the LB's affinity timeout" but the function sets prefer_local_backend; fix the comment.
- [ ] **DRY**: `SetLoadBalancerAffinityTimeout` and `SetLoadBalancerPreferLocalBackend` share the same pattern (GetLoadBalancer, compare option value and return if unchanged, copy options and set value, UpdateLoadBalancer). Extract e.g. `setLoadBalancerOption(lbName string, optionKey string, value string) error` to reduce duplication.
- [ ] **Readability**: In `LoadBalancerAddVip`, when GetLoadBalancer fails the error message says "failed to get lb health check"; the call is for adding a VIP, not health check. Use a more accurate message e.g. "failed to get load balancer".
- [ ] **Readability**: In `GetLoadBalancer`, the comment "it is because of lack name index that does't use" has a typo ("does't" → "doesn't") and could be clarified.
- [ ] **Performance / Readability**: `getMapKeys(m map[string]bool)` can be replaced with `maps.Keys(m)` (Go 1.21+) to avoid a custom helper; remove getMapKeys if no other callers need a []string from map[string]bool with different semantics.
- [ ] **Correctness / Verify**: In `deleteUnusedIPPortMappings`, `c.Transact("lb-del", ops)` is used for mutating IPPortMappings; other mutations in this file use "lb-add". Confirm with OVN semantics whether delete of IPPortMappings should use "lb-del" or "lb-add".

---

## ./pkg/ovs/ovn-nb-load_balancer_health_check.go

- [ ] **Readability**: In `AddLoadBalancerHealthCheck`, `err := fmt.Errorf("failed to new lb health check: %w", err)` shadows outer `err`; consider returning the wrapped error directly and klog once to simplify.
- [ ] **Readability**: Replace magic strings in health check options ("timeout": "20", "interval": "5", "success_count": "3", "failure_count": "3") with named constants for easier tuning and documentation.
- [ ] **Correctness**: In `DeleteLoadBalancerHealthCheck`, the Transact error message says "delete lb %s" but the operation deletes a health check; use e.g. "delete lb health check for lb %s" to avoid confusion.
- [ ] **Readability**: In `LoadBalancerHealthCheckExists`, return `return lbhc != nil, nil` explicitly when err is nil for clarity.
- [ ] **DRY**: Default LBHC Options map is duplicated in `newLoadBalancerHealthCheck` and in tests; consider a shared helper or constant for default options.
- [ ] **Readability**: In `ListLoadBalancerHealthChecks`, var block plus `lbhcList = make(...)` can be simplified to a single declaration.

---

## ./pkg/ovs/ovn-nb-load_balancer_health_check_test.go

- [ ] **Maintainability**: Fix typo in subtest name: "fliter by vip" → "filter by vip".
- [ ] **Readability**: In `testDeleteLoadBalancerHealthChecks`, after the loop `lbName` is the last iteration value; `GetLoadBalancerHealthCheck(lbName, ip, true)` uses it for all vips. Consider pairing lbName with vip (e.g. slice of struct) and iterating so the test intent is clearer.

---

## ./pkg/ovs/ovn-nb-load_balancer_test.go

- [ ] **Maintainability**: Fix typos in subtest names: "load balancerand" → "load balancer and" (line 155); "fliter" → "filter" (lines 202, 224).
- [ ] **Naming**: In `testGetLoadBalancer`, variable `lr` holds the result of `GetLoadBalancer`; rename to `lb` for consistency. Subtest uses "test-get-lr-non-existent" — use "test-get-lb-non-existent" to match load balancer naming.
- [ ] **DRY**: The pattern of creating two duplicate load balancers (lb1, lb2 with same name via Create+Transact) appears in testDeleteLoadBalancerOp, testSetLoadBalancerAffinityTimeout, testLoadBalancerAddHealthCheck, testLoadBalancerDeleteVip. Extract a helper e.g. `createDuplicateLoadBalancers(t, nbClient, lbName string)` to reduce duplication.
- [ ] **DRY**: Building backend host→host mappings from "ip:port,ip:port" (Split backends, SplitHostPort, mappings[host]=host) is repeated in testLoadBalancerAddHealthCheck, testLoadBalancerAddIPPortMapping, testLoadBalancerDeleteIPPortMapping, testLoadBalancerWithHealthCheck. Extract e.g. `backendHostMappingsFromBackends(backends string) (map[string]string, error)`.
- [ ] **Readability**: In testListLoadBalancers, variable `except` (line 205) holds names to exclude from the filter; rename to `excludeNames` or `excludedLbNames` for clarity.
- [ ] **Maintainability**: Fix subtest names: "Create load balancer when multiple load balancer exist" (testDeleteLoadBalancerOp, line 291) describes the setup, not the assertion — use e.g. "return error when multiple load balancers with same name exist". Same for testLoadBalancerAddHealthCheck (line 415). "set loadbalancer" → "set load balancer" (line 363).
- [ ] **Maintainability**: In testLoadBalancerDeleteIPPortMapping, two subtests are named "delete ip port mappings from load balancer repeatedly" (lines 517 and 536); the third (line 536) covers IPv6 — rename to e.g. "delete ip port mappings from load balancer repeatedly (IPv6)" to distinguish.

---

## ./pkg/ovs/ovn-nb-logical_router.go

- [ ] **Readability**: Fix comment typo "does't" → "doesn't" in GetLogicalRouter (line 93).
- [ ] **Naming**: In `LogicalRouterExists`, variable `lrp` suggests Logical Router Port; use `lr` for the logical router return value.
- [ ] **Readability**: In mutation helpers (LogicalRouterUpdateLoadBalancers, LogicalRouterUpdatePortOp, LogicalRouterUpdatePolicyOp, LogicalRouterUpdateNatOp, LogicalRouterUpdateStaticRouteOp), inner variable `mutation` shadows the outer func name; rename inner to e.g. `m` or `mut` to avoid confusion.
- [ ] **Performance**: In `LogicalRouterUpdateLoadBalancers`, GetLoadBalancer is called once per lbName; for many LBs consider batching with ListLoadBalancers and filtering by name to reduce round-trips.

---

## ./pkg/ovs/ovn-nb-logical_router_test.go

- [ ] **Maintainability**: Fix comment for helper `createLogicalRouter`: it says "delete logical router in ovn" but the function creates a logical router; change to "create logical router in ovn".

---

## ./pkg/ovs/ovn-nb-logical_router_policy.go

- [ ] **Naming**: Fix typo `batchUpdatetLogicalRouterPolicies` → `batchUpdateLogicalRouterPolicies` (lines 125, 428).
- [ ] **Maintainability**: Fix duplicate doc comments: "DeleteLogicalRouterPolicy" appears for DeleteLogicalRouterPolicy, BatchDeleteLogicalRouterPolicy, and DeleteLogicalRouterPolicies; use distinct descriptions (e.g. "BatchDeleteLogicalRouterPolicy batch remove...", "DeleteLogicalRouterPolicies delete some policies...").
- [ ] **Readability**: In AddLogicalRouterPolicy (lines 66–75), inner `err := fmt.Errorf(...)` shadows outer err; use a distinct name (e.g. `updateErr`) or return the wrapped error directly to avoid confusion.
- [ ] **Readability**: Comment "// not found,skip" (line 214) → "// not found, skip".
- [ ] **Readability**: In GetLogicalRouterPolicy, comment "this is necessary because may exist same..." → "this is necessary because there may exist policies with the same priority and match in different logical routers".
- [ ] **Correctness / Semantics**: In batchListLogicalRouterPoliciesByFilter, policySet[key] = policy overwrites when multiple input policies share the same priority and match; verify intended behavior (one entry per priority+match vs. all requested policies).

---

## ./pkg/ovs/ovn-nb-logical_router_port.go

- [ ] **Correctness**: Line 55: error message "logical router%s" missing space; use "logical router %s".
- [ ] **Maintainability**: DeleteLogicalRouterPorts (line 212) has comment "Delete logical router port" (singular); should be "Delete logical router ports" to match function name and behavior.
- [ ] **Readability**: In UpdateLogicalRouterPortOptions, initialize newOptions at start: if lrp.Options is nil use make(map[string]string), else maps.Clone(lrp.Options); then in the loop only mutate. Avoids in-loop "if len(newOptions)==0 then make" and clarifies nil handling.
- [ ] **Readability**: LogicalRouterPortExists final return is `return lrp != nil, err`; when reaching that line err is always nil (errors returned earlier). Consider `return lrp != nil, nil` for clarity.

---

## ./pkg/ovs/ovn-nb-logical_router_port_test.go

- [ ] **Naming**: Line 291: typo "test-create-lrp-fail-clenit" → "test-create-lrp-fail-client".
- [ ] **Naming**: Line 351: subtest "update nil lsp" → "update nil lrp" (lsp is logical switch port).
- [ ] **Naming**: Line 541: typo "upadate" → "update" in subtest "upadate gateway chassis op with nil uuids".
- [ ] **Naming**: Line 428: "when does't exist ExternalIDs" → "when ExternalIDs don't exist".
- [ ] **Maintainability**: Lines 354, 424: use require.ErrorContains(t, err, "logical_router_port is nil") to assert error message instead of require.Error(t, err, "logical_router_port is nil").
- [ ] **DRY**: testLogicalRouterPortFilter repeats the pattern filterFunc := ...; count := 0; for ...; require.Equal(t, count, N). Extract e.g. countFiltered(lrps []*ovnnb.LogicalRouterPort, filter func(*ovnnb.LogicalRouterPort) bool) int and assert on return value.
- [ ] **Readability**: In testLogicalRouterPortFilter, use require.Equal(t, expected, actual) order: require.Equal(t, 6, count) instead of require.Equal(t, count, 6) (and similarly for 5, 2).
- [ ] **Maintainability**: In testLogicalRouterPortFilter, last subtest "filter out LRP with empty value" appends to shared lrps; consider building a separate slice for that case so test order does not affect other counts.
- [ ] **Readability**: Subtest names "should log err" / "fail nb client should log err" could be "should return error" / "failed NB client returns error" for consistency with require.Error.
- [ ] **Cleanup**: Line 357: remove or document commented-out code in testGetLogicalRouterPortByUUID.

---

## ./pkg/ovs/ovn-nb-logical_router_route.go

- [ ] **Correctness**: In AddLogicalRouterStaticRoute, only run delete block (LogicalRouterUpdateStaticRouteOp + Transact) when len(toDel) > 0; when toDel is empty skip to avoid unnecessary or empty transact.
- [ ] **Correctness**: In BatchDeleteLogicalRouterStaticRoute, when GetLogicalRouter returns (nil, err) we fall through and use lr.StaticRoutes; add "if err != nil { return err }" and "if lr == nil { return nil }" before using lr.
- [ ] **Maintainability**: Line 192: duplicate comment "DeleteLogicalRouterStaticRoute" for DeleteLogicalRouterStaticRouteByUUID; use "DeleteLogicalRouterStaticRouteByUUID delete ... by UUID".
- [ ] **Naming**: Line 281: typo "exits" → "exists" (variable nexthop, exits := staticRoutesMap[key]).
- [ ] **Readability**: Line 314: "generate operations for clear" → "generate operations for clearing".
- [ ] **Readability**: Line 341: comment "this is necessary because may exist same" → "this is necessary because the same static route may exist".
- [ ] **Readability**: Line 402: error message "batch list logical staric router" — typo "staric" → "static"; "lr staric route" → "lr static routes".
- [ ] **Readability**: Line 413/416: comment "create several ... route once" → "create several ... routes at once".
- [ ] **Readability**: Line 377: "list route which match" → "list routes that match".
- [ ] **API**: ListLogicalRouterStaticRoutesByOption(lrName, _, key, value string) has unused second parameter; remove or document (e.g. routeTable).

---

## ./pkg/ovs/ovn-nb-logical_router_route_test.go

- [ ] **DRY**: In testDeleteLogicalRouterStaticRouteByExternalIDs, the pattern "ListLogicalRouterStaticRoutes + require.NoError + require.Len/Empty" is repeated many times. Extract a helper e.g. `assertRouteCount(t, nbClient, lrName, filter map[string]string, expected int)` to reduce repetition and improve readability.
- [ ] **Maintainability**: In testBatchDeleteLogicalRouterStaticRoute, `staticRouter` is mutated across subtests (e.g. staticRouter.IPPrefix, staticRouter.Nexthop changed in "delete non-exist static route" and "delete ecmp policy route"). This makes tests order-dependent. Create a fresh *LogicalRouterStaticRoute per subtest so each case is independent.
- [ ] **Consistency**: In testBatchDeleteLogicalRouterStaticRoute subtest "delete ecmp policy route", the first route is deleted via BatchDeleteLogicalRouterStaticRoute and the second via DeleteLogicalRouterStaticRoute. Consider using the same API for both for consistency, or add a brief comment that both code paths are intentionally exercised.
- [ ] **Readability**: In testAddLogicalRouterStaticRoute, the outer `err` is reused and overwritten in loops inside subtests; the last iteration's err is asserted. Use a local `err` (or named return) inside each subtest to avoid confusion and make failures easier to attribute.

---

## ./pkg/ovs/ovn-nb-logical_switch.go

- [ ] **Maintainability**: Fix typo "ingnore" → "ignore" in CreateBareLogicalSwitch comment (line 89).
- [ ] **Readability**: Fix typo "list switch switch" → "list logical switch" in GetLogicalSwitch error message (line 246).
- [ ] **Readability**: Fix comment in GetLogicalSwitch (line 231): "does't" → "doesn't", "lack name index" → "lack of name index".
- [ ] **Structure**: In LogicalSwitchUpdatePortOp, when lsName == "" and op == Delete, the resolution of logical switch by LSP UUID is a non-trivial block. Consider extracting e.g. `resolveLogicalSwitchNameByPort(lspUUID string) (string, error)` for readability and unit testing.
- [ ] **Readability**: CreateLogicalSwitch has multiple nested branches (exist vs !exist, needRouter vs !needRouter). Consider extracting helpers e.g. `updateRouterPortIfExists(...)` and `ensurePatchPort(...)` to reduce nesting and improve readability.
- [ ] **DRY**: LogicalSwitchUpdateOtherConfigOp, LogicalSwitchUpdateLoadBalancerOp, and logicalSwitchUpdateACLOp share the same structure (empty check, mutation closure, LogicalSwitchOp). Consider a small generic helper if more such mutation ops are added.

---

## ./pkg/ovs/ovn-nb-logical_switch_port.go

- [ ] **Maintainability**: Fix comment typos: line 308 "CreateVirtualLogicalSwitchPorts update..." refers to SetLogicalSwitchPortVirtualParents; line 347 "CreateVirtualLogicalSwitchPort update..." refers to SetVirtualLogicalSwitchPortVirtualParents. Use the correct function name in the comment.
- [ ] **Naming**: In DeleteLogicalSwitchPorts (line 680), the filter parameter is named `lrp` (suggests logical router port) but the type is *ovnnb.LogicalSwitchPort; rename to `lsp` for consistency.
- [ ] **DRY**: buildLogicalSwitchPort sets `lsp.ExternalIDs["vendor"] = util.CniTypeName` twice (lines 37 and 76); remove the redundant assignment.
- [ ] **DRY**: SetLogicalSwitchPortVirtualParents and SetVirtualLogicalSwitchPortVirtualParents share the same logic for setting/clearing virtual-parents (Options["virtual-parents"] = parents; delete if empty). Consider extracting a small helper or having the single-port path call the batch logic with one IP to reduce duplication.
- [ ] **Readability**: In SetLogicalSwitchPortSecurityGroup (line 406), "ignore existent" could be "ignore existing" for consistency with "ignore non-existent" (line 414).

---

## ./pkg/ovs/ovn-nb-logical_router_policy_test.go

- [ ] **Maintainability**: Fix typos "create three polices" / "create two polices" → "policies" (lines 429, 435).
- [ ] **Maintainability**: In testDeleteLogicalRouterPolicy, subtest name "no err when delete existent logical switch port" (line 177) refers to switch port; change to "logical router policy".
- [ ] **Readability**: In testDeleteLogicalRouterPolicies, subtest "delete some policies with nil priority" uses priority -1; rename to e.g. "with priority -1 (any)" or "with ignore priority" for clarity.
- [ ] **DRY**: testPolicyFilter repeats the pattern `filterFunc := ...; count := 0; for _, policy := range policies { if filterFunc(policy) { count++ } }; require.Equal(t, N, count)` five times; extract e.g. `countFiltered(policies []*ovnnb.LogicalRouterPolicy, filter func(*ovnnb.LogicalRouterPolicy) bool) int` and assert on the return value.

---

## ./pkg/ovs/ovn-nb-dhcp_options_test.go

- [ ] **DRY**: `testUpdateDHCPv4Options` and `testUpdateDHCPv6Options` share the same structure (create without/with options, update, invalid cidr, invalid lsName, append options). Consider table-driven subtests or a shared helper parameterized by protocol to reduce duplication.
- [ ] **DRY**: In `testDhcpOptionsFilter`, the pattern `filterFunc := ...; count := 0; for _, dhcpOpt := range dhcpOpts { if filterFunc(dhcpOpt) { count++ } }; require.Equal(t, count, N)` is repeated six times. Extract e.g. `countFiltered(dhcpOpts []*ovnnb.DHCPOptions, filter func(*ovnnb.DHCPOptions) bool) int` and assert on the return value.
- [ ] **Correctness / Comment**: In `testListDHCPOptions`, the comment `/* list all direction acl */` is copy-paste from ACL tests; change to "list all dhcp options" or remove.
- [ ] **Maintainability**: In `testUpdateDHCPOptions`, `subnet` is mutated across subtests (e.g. `subnet.Spec.EnableDHCP = false`); tests can become order-dependent. Consider a fresh copy per subtest (e.g. `subnet := mockSubnet(...)` or deep copy) so each case is independent.

---

## ./pkg/ovs/ovn-nb_global_test.go

- [ ] **DRY**: Many test methods repeat the same Cleanup: `nbClient.DeleteNbGlobal()`, then `require.ErrorContains(t, nbClient.GetNbGlobal(), "not found nb_global")`. Extract a helper e.g. `cleanupNbGlobal(t, nbClient)` or use suite-level setup/teardown to reduce duplication.
- [ ] **Readability**: In `testSetAzName`, the subtest "set az name when it's the same" sets "new-az" again after "set az name when it's different"; the assertion only checks output. Consider explicitly testing idempotency (e.g. call SetAzName twice with same value and assert no error and same result).

---

## ./pkg/ovs/ovn-nb-logical_switch_port_test.go

- [ ] **DRY**: Many test functions repeat the pattern of creating a logical switch and logical switch port (e.g. `CreateBareLogicalSwitch` + `CreateBareLogicalSwitchPort` appears 73 times). Extract a helper function e.g. `setupTestLSAndLSP(t *testing.T, nbClient, lsName, lspName string)` to reduce duplication.
- [ ] **DRY**: In `testCreateLogicalSwitchPort`, the assertion pattern for checking `Addresses`, `PortSecurity`, and `ExternalIDs` is repeated across multiple subtests (lines 56-70, 83-95, 108-122, etc.). Extract a helper function e.g. `assertLSPProperties(t *testing.T, lsp *ovnnb.LogicalSwitchPort, expected ...)` to reduce duplication.
- [ ] **Readability / Structure**: `testCreateLogicalSwitchPort` is very long (246 lines) with 9 subtests. Consider splitting into separate test functions or using table-driven tests for similar cases (e.g. "with vips", "without vips", "with default-securitygroup" could be table-driven).
- [ ] **DRY / Structure**: `testSetLogicalSwitchPortSecurityGroup` has highly repetitive subtests (lines 813-1094) with the same pattern: create LSP, set initial ExternalIDs, call SetLogicalSwitchPortSecurityGroup, assert. Consider table-driven tests with a struct containing test name, initial SGs, operation, new SGs, and expected results.
- [ ] **DRY**: In `testSetLogicalSwitchPortSecurityGroup`, the pattern of creating a bare LSP, getting it, modifying ExternalIDs, updating, and then calling SetLogicalSwitchPortSecurityGroup is repeated 10+ times. Extract a helper e.g. `setupLSPWithSGs(t *testing.T, nbClient, lsName, lspName string, initialSGs []string)`.
- [ ] **Readability**: Hard-coded test data (IPs, MACs, names) are scattered throughout. Consider extracting test constants at package level or using a test data builder pattern (e.g. `testData{ips: "...", mac: "...", podName: "..."}`) for better maintainability.
- [ ] **DRY**: In `testSetLogicalSwitchPortsSecurityGroup`, the loop pattern `for i := range 3 { lspName := fmt.Sprintf("%s-%d", lspNamePrefix, i); ... }` appears twice (lines 1122-1135, 1159-1168). Extract to a helper that creates N LSPs and returns their names.
- [ ] **Readability**: In `testLogicalSwitchPortFilter`, the pattern `filterFunc := ...; count := 0; for _, lsp := range lsps { if filterFunc(lsp) { count++ } }; require.Equal(t, count, N)` is repeated 11 times. Extract e.g. `countFiltered(lsps []*ovnnb.LogicalSwitchPort, filter func(*ovnnb.LogicalSwitchPort) bool) int`.
- [ ] **DRY**: Many test functions have similar error case testing (e.g. "should print err log when logical switch port does not exist", "failed client should log err"). Consider extracting a helper e.g. `testErrorCases(t *testing.T, nbClient, failedNbClient, testCases []errorTestCase)` for common error scenarios.
- [ ] **Maintainability**: In `testSetLogicalSwitchPortSecurityGroup`, the helper functions `addOpExpect` and `removeOpExpect` are defined inside the test function (lines 789-805). Consider moving them to package level or a test helper file if they could be reused elsewhere, or at least document their purpose more clearly.
- [ ] **Readability**: Some test names are very long (e.g. "test-update-port-security-lsp-nil-eid", "test-create-lsp-lsp-in-default-vpc-with-sg"). Consider shorter, more descriptive names or using test case structs with descriptive names.
- [ ] **DRY**: In `testCreateLogicalSwitchPort`, the DHCP options setup (lines 33-44) is duplicated. If other tests need DHCP options, extract to a helper e.g. `setupDHCPOptions(t *testing.T, nbClient, lsName string) *DHCPOptionsUUIDs`.

---

## ./pkg/ovs/ovn-nb-logical_switch_test.go

- [ ] **DRY**: Multiple test functions repeat the pattern of creating a logical switch and logical switch port (e.g. `CreateBareLogicalSwitch` + `CreateBareLogicalSwitchPort` + `GetLogicalSwitchPort` appears in `testLogicalSwitchAddPort`, `testLogicalSwitchDelPort`, `testLogicalSwitchUpdatePortOp`). Extract a helper function e.g. `setupTestLSAndLSP(t *testing.T, nbClient *OVNNbClient, lsName, lspName string) (*ovnnb.LogicalSwitchPort, error)` to reduce duplication.
- [ ] **DRY**: In `testCreateLogicalSwitch`, hard-coded CIDR and gateway strings (e.g. `"192.168.2.0/24,fd00::c0a8:6400/120"`, `"192.168.2.1,fd00::c0a8:6401"`) are repeated across multiple subtests. Extract as package-level test constants (e.g. `testCIDRBlock`, `testGateway`) for better maintainability.
- [ ] **DRY**: In `testListLogicalSwitch`, the filtering and counting pattern (lines 422-428 and 451-457) is nearly identical: `count, names := 0, make([]string, 0, N); for _, ls := range lss { if strings.Contains(ls.Name, namePrefix) { names = append(names, ls.Name); count++ } }; require.Equal(t, N, count)`. Extract a helper e.g. `filterAndCountLS(lss []*ovnnb.LogicalSwitch, prefix string) (int, []string)`.
- [ ] **DRY**: `testLogicalSwitchUpdateLoadBalancerOp` and `testLogicalSwitchUpdateACLOp` have very similar structures (create LS, create UUIDs, test insert/delete operations with identical mutation assertions). Consider extracting a generic helper e.g. `testLogicalSwitchUpdateCollectionOp(t *testing.T, nbClient *OVNNbClient, lsName, column string, uuids []string)` to reduce duplication.
- [ ] **Readability**: In `testGetLogicalSwitch`, the variable is named `lr` (line 361) but should be `ls` since it's a logical switch, not a logical router. This is confusing and likely a copy-paste error.
- [ ] **Readability**: In `testLogicalSwitchUpdateLoadBalancerOp`, the subtest name "del port from logical switch" (line 600) should be "del lb from logical switch" since it's testing load balancer deletion, not port deletion.
- [ ] **Consistency**: Some tests use `require.Nil(t, err)` (lines 147, 150, 154, 230) while others use `require.NoError(t, err)`. Standardize on `require.NoError(t, err)` for consistency and better error messages.
- [ ] **Structure**: In `testLogicalSwitchOp`, the mutation functions `lspMutation` and `lbMutation` (lines 708-727) could be extracted as helper functions or use a builder pattern if similar patterns appear elsewhere.
- [ ] **Readability**: Magic strings for test names (e.g. `"test-create-ls-ls"`, `"test-add-port-ls"`, `"test-del-port-ls"`) are scattered throughout. Consider extracting common prefixes as constants (e.g. `testLSPrefix = "test-"`) or using a test name generator helper.
- [ ] **Maintainability**: In `testLogicalSwitchDelPort`, the last subtest (lines 216-231) tests multiple failure scenarios in one test case. Split into separate subtests (e.g. "failed client create switch", "failed client create port", "failed client get port", "failed client add port", "failed client del port") for better isolation and clearer failure messages.
- [ ] **DRY**: The pattern of creating a logical switch, creating a port, adding the port to the switch, then testing operations appears in multiple tests. Consider a setup helper that returns a configured LS and LSP for reuse.
- [ ] **Readability**: In `testCreateLogicalSwitch`, test names contain typos: "does't" should be "doesn't" (lines 45, 88, 93). Fix for better readability.

---

## ./pkg/ovs/ovn-nb-meter.go

- [ ] **Performance**: In `ListAllMeters`, pre-allocate the result slice with `make([]*ovnnb.Meter, 0, len(meterList))` before the loop to avoid repeated slice growth.
- [ ] **Readability**: In `updateMeterAndBand`, the condition `bandUUID == "" || len(bandUpdateOps) == 0` relies on `bandUpdateOps` staying nil when Get fails (e.g. ErrNotFound). Use an explicit boolean (e.g. `bandUpdated bool`) to make the "create new band" branch intent clear and avoid depending on slice length.
- [ ] **Correctness**: In `updateMeterAndBand`, when an existing band UUID is present but `c.Get(ctx, band)` returns ErrNotFound (orphaned reference), the code creates a new band and uses MutateOperationInsert to add the new band UUID. The old UUID remains in `meter.Bands`, so the meter can end up with two entries (one invalid). Replace the meter's Bands with the new band only (e.g. set Bands to `[]string{newBandUUID}` via Update or equivalent) so the meter does not retain an orphaned reference.
- [ ] **Documentation**: In `CreateOrUpdateMeter`, document that `rate <= 0` causes the meter to be deleted (idempotent with DeleteMeter).
- [ ] **Maintainability**: In `updateMeterAndBand`, clarify when `len(ops) == 0` can occur before Transact, or remove the early return if it is unreachable (e.g. if `Where(meter).Update` always returns at least one operation).

---

## ./pkg/ovs/ovn-nb-migration.go

- [ ] **DRY**: `getKubeOvnRouterNames` and `getKubeOvnSwitchNames` share the same structure (WhereCache filtering by vendor == util.CniTypeName, then building map[string]bool of names). Consider extracting a generic helper or documenting the intentional parallelism to simplify future changes.
- [ ] **DRY / Extensibility**: The five migrate* functions (migrateLogicalRouterPorts, migratePortGroups, migrateAddressSets, migrateLoadBalancers, migrateACLs) share the same pattern: list with WhereCache, if empty return nil, loop to set ExternalIDs["vendor"] and collect Update ops, Transact. Consider a callback-based helper e.g. `migrateVendorTagForItems(listItems, resourceName, transactTag string, getUpdateOp func(item) ([]ovsdb.Operation, error)) error` to reduce duplication and make adding new resource types easier.
- [ ] **Readability**: Replace magic string `"1.15.0"` in `needsVendorMigration` with a package-level constant (e.g. `vendorTagIntroduceVersion = "1.15.0"`) for documentation and single place to change.
- [ ] **Error handling**: In each migrate* function, when `c.Where(item).Update(...)` returns an error the code logs and continues; if all items fail, ops is empty and the function returns nil without indicating that every item failed. Consider returning an error when ops is empty but at least one Update failed (e.g. track failed count and return last error).
- [ ] **Performance**: In `MigrateVendorExternalIDs`, `getKubeOvnRouterNames` and `getKubeOvnSwitchNames` are independent; run them in parallel (e.g. errgroup) to reduce startup latency.

---

## ./pkg/ovs/ovn-nb-migration_test.go

- [ ] **DRY**: `testMigrateVendorExternalIDs`, `testMigrateVendorExternalIDsIdempotent`, `testMigrateSkipsWhenVersionSet`, `testMigrateRunsWhenOldVersion`, and `testMigrateVendorExternalIDsSkipsNonKubeOvn` all use the same cleanup and ensureNbGlobalExists pattern. Consider a helper e.g. `withMigrationTestEnv(t, nbClient, fn func())` that runs ensureNbGlobalExists, registers t.Cleanup(DeleteNbGlobal), and runs fn, to reduce repetition.
- [ ] **Readability**: In `TestLoadBalancerPatterns`, use `require.Equal(t, tc.expected, result, ...)` instead of manual if + t.Errorf for consistency with the rest of the codebase and clearer failure messages.

---

## ./pkg/ovs/ovn-nb-nat.go

- [ ] **DRY**: `GetNat` and `newNat` share nearly identical validation (lrName required, natType not DNAT, natType in [SNAT, DNATAndSNAT], SNAT requires logicalIP, DNATAndSNAT requires externalIP). Extract a shared helper e.g. `validateNatParams(lrName, natType, externalIP, logicalIP string) error` to avoid duplication and drift.
- [ ] **Readability**: Fix grammar in error messages: "nat type must one of" → "nat type must be one of" (in GetNat line 284 and newNat line 361).
- [ ] **Maintainability**: In `UpdateNat`, Transact uses method name "net-update"; consider renaming to "nat-update" for consistency with other resources (e.g. lb-update, lrp-update) and clarity in logs.
- [ ] **Performance**: `listLogicalRouterNatByFilter` fetches each NAT by UUID in a loop (N+1). If the OVSDB client supports batch get by UUIDs, consider batching to reduce round-trips for routers with many NATs.
- [ ] **Robustness**: In `CreateNats`, when all input nats are nil (e.g. nats = [nil, nil]), models and natUUIDs are empty and the code still proceeds; add a check after the loop and return an error when len(models) == 0.
- [ ] **Consistency**: Replace block comments `/* create nat */` with `// create nat` (UpdateSnat, UpdateDnatAndSnat) for style consistency with the rest of the file.

---

## ./pkg/ovs/ovn-nb-nat_test.go

- [ ] **Naming**: Fix typo in comment line 262: "filed update" → "field update".
- [ ] **DRY**: Two identical subtests "fail to new snat rule" in testNewNat (lines 447–468 and 470–491); remove the duplicate.
- [ ] **Consistency**: Use require.NoError(t, err) instead of require.Nil(t, err) for error assertions (e.g. lines 265, 455, 458) to align with testify conventions and clearer failure messages.
- [ ] **Structure**: In testUpdateNat, the subtest "failed to update nat" contains a nested subtest "empty lrName" that calls UpdateDnatAndSnat; move "empty lrName" to testUpdateDnatAndSnat or a dedicated validation test so each test focuses on one API.
- [ ] **DRY**: testNatFilter repeats the pattern (create filter, count matching nats, require.Equal count). Extract a helper e.g. countFiltered(nats []*ovnnb.NAT, filter func(*ovnnb.NAT) bool) int and use require.Equal(t, expectedCount, countFiltered(...)).
- [ ] **Readability**: In testNatFilter, use require.Equal(t, expected, actual) order: require.Equal(t, 7, count) instead of require.Equal(t, count, 7) for consistency with testify convention.
- [ ] **Correctness**: testAddNat uses options map key "staleless" (line 584); verify if this should be "stateless" to match production code (see ovn-nb-nat.go Options["stateless"]).

---

## ./pkg/ovs/ovn-nb-port_group.go

- [ ] **Correctness / Naming**: In `PortGroupExists` (lines 262–264), the variable is named `lsp` but holds the result of `GetPortGroup` (a port group). Rename to `pg` and use `return pg != nil, err` for clarity and to avoid confusion with logical switch port.
- [ ] **Correctness**: In `ListPortGroups` (line 255), `klog.Errorf` message says "list logical switch ports" but the function lists port groups. Change to "list port groups".
- [ ] **Readability**: In `PortGroupSetPorts` (line 103), error message "failed generate" is missing "to"; use "failed to generate".
- [ ] **DRY**: `portGroupUpdatePortOp` and `portGroupUpdateACLOp` share the same closure pattern (build `*model.Mutation` for a field). Consider a small helper e.g. `portGroupMutation(field *[]string, value []string, op ovsdb.Mutator) func(*ovnnb.PortGroup) *model.Mutation` to reduce duplication.
- [ ] **Readability**: In `RemovePortFromPortGroups`, `portGroups` is initialized with `make([]ovnnb.PortGroup, 0, len(portGroupNames))` but in the `else` branch it is reassigned by `ListPortGroups(nil)`. Use `var portGroups []ovnnb.PortGroup` and assign in both branches for clearer intent.

---

## ./pkg/ovs/ovn-nb-port_group_test.go

- [ ] **Naming**: Fix test name typo `testGetGetPortGroup` → `testGetPortGroup` (line 236).
- [ ] **Naming**: In `testDeletePortGroup`, subtest "no err when delete non-existent logical router" (line 230) refers to logical router but the test deletes a port group; rename to "no err when delete non-existent port group".
- [ ] **Naming**: In `testListPortGroups`, subtest "result should include lsp when key exists in pg column" (line 287) should say "pg" not "lsp" (we are listing port groups).
- [ ] **Consistency**: In `testListPortGroups` (lines 315, 324), use testify convention `require.Equal(t, expected, actual)` i.e. `require.Equal(t, 4, count)` instead of `require.Equal(t, count, 4)`.
- [ ] **DRY**: In "result should include all pg when externalIDs is empty", the same count loop (range out, strings.Contains, count++) is repeated for `ListPortGroups(nil)` and `ListPortGroups(map[string]string{})`; extract a helper e.g. `countPortGroupsWithPrefix(pgs []ovnnb.PortGroup, prefix string) int` to avoid duplication.
- [ ] **Readability**: In `testPortGroupOp` (line 418), `require.Nil(t, ops)` for a slice is better expressed as `require.Empty(t, ops)` for clarity.

---

## ./pkg/ovs/ovn-nb-suite_test.go

- [ ] **Naming**: Fix test method name `Test_GetGetPortGroup` → `Test_GetPortGroup` (double "Get" is a typo).
- [ ] **Naming**: Fix test method name `Test_testCleanLogicalSwitchPortMigrateOptions` → `Test_CleanLogicalSwitchPortMigrateOptions`; the "test" prefix is redundant and inconsistent with other Test_* methods.
- [ ] **Readability**: In `newOVSDBServer`, the return variable `server` shadows the imported package `server` (libovsdb/server). Rename the return value to e.g. `ovsdbServer` or `srv` to avoid confusion.
- [ ] **Structure**: `SetupSuite` is long and does four distinct setups (failed NB client, NB client, SB client, legacy client, ovs socket). Extract helpers e.g. `setupFailedOvnNBClient()`, `setupOvnNBClient()`, `setupOvnSBClient()` to improve readability and testability.
- [ ] **DRY**: `newNbClient` and `newSbClient` share the same pattern (dbModel, logger, options, endpoints loop, NewOVSDBClient, Connect, Monitor). Extract a generic helper e.g. `newOvsdbClient(addr string, timeout int, dbModel model.ClientDBModel, monitorOpts []client.MonitorOption)` to reduce duplication and keep client creation consistent.
- [ ] **Maintainability**: The monitor table list in `newNbClient` (lines 891–911) is long and could be extracted to a package-level variable or helper (e.g. `nbMonitorTables() []client.MonitorOption`) so adding/removing tables does not clutter the constructor.
- [ ] **Test hygiene**: Remove or move `Test_scratch` (lines 834–843); it is skipped and references a real endpoint and DeleteAcls, adding no value to the suite and cluttering the file.
- [ ] **Readability**: Replace magic number `10` (timeout in seconds) used in multiple places with a named constant (e.g. `testOvnClientTimeout`) at package or suite level.
- [ ] **Structure**: Over 150 test methods are one-line wrappers delegating to private `test*` methods. Consider table-driven registration or a small code generator if more suites are added, to reduce boilerplate; alternatively document the pattern for consistency.

---

## ./pkg/ovs/ovn-nb_test.go

- [ ] **DRY**: The CIDR string `"192.168.230.1/24,fc00::0af4:01/112"` is repeated many times across testCreateGatewayLogicalSwitch, testCreateLogicalPatchPort, testDeleteLogicalGatewaySwitch, etc. Extract to a package-level constant (e.g. `testGatewayDualStackCIDRs`) to avoid drift and improve readability.
- [ ] **Consistency**: Use `require.NoError(t, err)` instead of `require.Nil(t, err)` when asserting no error (e.g. lines 183–184, 211–212, 273–274), for consistency with testify and clearer failure messages.
- [ ] **Readability**: Replace magic number `210` (VLAN ID in CreateGatewayLogicalSwitch) with a named constant (e.g. `testGatewayVLANID`) at package or test level.
- [ ] **Naming**: Variable `chassises` is used for chassis UUID lists; consider `chassisIDs` or `chassisList` for clarity (plural "chassis" is unchanged in English; "chassises" may be project convention—align with rest of codebase).
- [ ] **DRY**: testCreateLogicalPatchPort subtests repeat the pattern CreateLogicalRouter, CreateBareLogicalSwitch, CreateLogicalPatchPort with different chassis args; consider a small helper e.g. `createTestPatchPort(t, nbClient, lsName, lrName, chassises...)` to reduce duplication.
- [ ] **Style**: Fix double space in comment "//  create with normal pg" (line 251).

---

## ./pkg/ovs/ovn-sb-chassis.go

- [ ] **Naming**: Fix typo `GetKubeOvnChassisses` → `GetKubeOvnChassis` (or keep plural as `Chassis`; "Chassisses" is incorrect). Update call sites (e.g. suite test name) accordingly.
- [ ] **Error message**: In `UpdateChassisTag` (line 151), use "failed to get" instead of "fail to get" for consistency with other error messages in the file.
- [ ] **API / Idiom**: `ListChassis` and `GetKubeOvnChassisses` return `*[]ovnsb.Chassis`. In Go, returning a slice directly (`[]ovnsb.Chassis`) is more idiomatic and avoids nil vs empty-slice confusion; consider changing return type and updating callers.
- [ ] **Readability**: In `GetKubeOvnChassisses`, the WhereCache predicate can be simplified to `return chassis.ExternalIDs != nil && chassis.ExternalIDs["vendor"] == util.CniTypeName` (single return) instead of if/return false.

---

## ./pkg/ovs/ovn-sb-chassis_test.go

- [ ] **DRY**: The pattern "create chassis, sbClient.Create(chassis), Transact(\"chassis-add\", ops)" is repeated in every test. Extract a helper e.g. `addChassisToSB(t, sbClient, chassis *ovnsb.Chassis)` to reduce duplication.
- [ ] **Consistency**: Use `require.NoError(t, err)` instead of `require.Nil(t, err)` when asserting no error (e.g. lines 50, 57, 63, 94); use `require.Error(t, err)` when asserting error.
- [ ] **Naming**: In `newChassis`, parameter `nbcfg` should be `nbCfg` (camelCase) for Go style and consistency with struct field `NbCfg`.
- [ ] **Maintainability**: In testUpdateChassisTag subtest "test update chassis tag with non-existent chassis" (line 398), the expected error string is "fail to get chassis by name=...". When fixing the production error message in ovn-sb-chassis.go to "failed to get", update this assertion accordingly.
- [ ] **Readability**: testGetChassisByHost has two subtests that both get a single chassis by host ("test get all chassis by host with single chassis" and "test get chassis by host with valid hostname"); consider merging or clarifying the distinction to avoid redundancy.

---

## ./pkg/ovs/ovn_test.go

- [ ] **Readability**: In `TestNewLegacyClient`, magic number `30` could be a named constant (e.g. `testLegacyClientTimeout`) or a variable with a descriptive name for clarity.
- [ ] **DRY**: `testNewOvnNbClient` and `testNewOvnSbClient` are nearly identical (only differ in NewOvnNbClient vs NewOvnSbClient and ovnnb vs ovnsb schema/model). Extract a helper e.g. `testNewOvnClient(t *testing.T, name string, newClient func(string, int, int, int, int) (interface{}, error), getSchema func() ovsdb.Schema, getModel func() (ovsdb.Model, error))` or use a table-driven approach to reduce duplication.
- [ ] **DRY**: Timeout values 5, 10, 20 and max retry 1 are repeated in both suite methods and in the "ovsdb client error with max retry" subtests. Consider package-level test constants (e.g. `testOvnTimeout`, `testOvsDbConTimeout`, `testOvsDbInactivityTimeout`, `testMaxRetry`) for consistency and easier tuning.
- [ ] **Maintainability**: The two ConstructWait* tests assert many fields in sequence; if the struct evolves, consider a small helper that compares only the fields that matter for the operation (e.g. Op, Table, Timeout, Where, Until) to reduce brittle field-by-field assertions.

---

## ./pkg/ovs/ovs-appctl_linux.go

- [ ] **Readability**: In `Appctl`, assign `pidFields[0]` to a variable (e.g. `pid`) before building the target path so the intent is clearer.
- [ ] **Extensibility**: Component-to-runDir mapping (ovs* -> ovsRunDir, ovn* -> ovnRunDir) is a two-branch switch; if more components are added, consider a map or slice of {prefix, runDir} for easier extension.
- [ ] **Robustness**: Optionally validate that the first field in the pid file is numeric (e.g. strconv.Atoi) to fail fast on malformed pid files instead of passing invalid target to appctl.
- [ ] **Maintainability**: Add a short comment documenting the expected pid file format and ctl socket naming (e.g. `component.pid.ctl`) so future changes to OVS/OVN socket layout are easier to track.

---

## ./pkg/ovs/ovsdb-client.go

- [ ] **Testability**: `Query` uses `exec.Command` directly; consider an injectable command runner interface (e.g. `type OvsdbClientRunner interface { Query(ctx, address, database string, timeout int, operations ...ovsdb.Operation) ([]byte, error) }`) so unit tests can mock ovsdb-client without invoking the binary.
- [ ] **Error handling**: When `json.Unmarshal` fails, the error message includes full command output; consider truncating long output (e.g. first 200 chars) in the returned error to avoid log noise and potential sensitive data leakage.

---

## ./pkg/ovs/ovs-ofctl.go

- [ ] **Robustness**: `ReplaceFlows` uses `exec.Command` without timeout; a hung ovs-ofctl could block indefinitely. Consider `exec.CommandContext` with a context and timeout (e.g. 30s) so callers can cancel or limit wait time.
- [ ] **Error handling**: In `ClearU2OFlows`, on first `DumpFlows` or `DelFlows` error we return immediately; other bridges are not processed. Consider continuing with remaining bridges and aggregating errors, or document fail-fast behavior.
- [ ] **Readability**: Extract a helper e.g. `clearU2OFlowsOnBridge(client *ovs.Client, bridge string) error` to reduce nesting in `ClearU2OFlows` and make the control flow easier to follow.

---

## ./pkg/ovs/ovs-vsctl.go

- [ ] **Structure / Testability**: Package-level mutable `lastInterfacePodMap` makes `ListInterfacePodMap` order-dependent and hard to test; consider attaching to a struct or passing as parameter.
- [ ] **Readability**: Magic numbers (30s timeout in `--timeout=30`, 1s limiter wait, 500ms slow-command threshold) could be named package-level constants for tuning and documentation.
- [ ] **DRY**: `GetQosList` has two nearly identical branches differing only by the ovsFind condition; use a single condition string (ifaceID vs pod) and one `ovsFind` call to reduce duplication.
- [ ] **DRY**: `ListInterfacePodMap`, `ListExternalIDs`, and `ListQosQueueIDs` share the pattern "Exec find, Split by newline, parse CSV, build map"; consider extracting a small helper (e.g. `parseOvsFindCsv(output string, parseRow func(parts []string) (key, value string)) (map[string]string, error)`) to reduce repetition.
- [ ] **Error handling**: `CleanDuplicatePort` ignores `ovsFind` error (`uuids, _`) and only logs `Exec` errors; consider returning an aggregated error so callers can handle failures.
- [ ] **Readability**: In `ConfigInterfaceMirror`, the range variable `mirrorPortIDs` is a string that may contain multiple port IDs; consider renaming to e.g. `selectDstPortValue` for clarity.

---

## ./pkg/ovs/ovs-vsctl_linux.go

- [ ] **Error handling**: `SetInterfaceBandwidth` and `SetNetemQos` ignore `strconv.Atoi`/`ParseFloat` errors for ingress, egress, latency, jitter, limit, loss; invalid input becomes 0. Consider validating and returning an error for invalid numeric input.
- [ ] **Maintainability**: Fix typo in `deleteNetemQosByID` log: "bingding" → "binding".
- [ ] **Error handling**: `deleteNetemQosByID` ignores `Get("qos", qosID, "type", ...)` error; `CheckAndUpdateHtbQos` ignores `IsHtbQos` error. Consider handling errors so failures are visible.
- [ ] **DRY**: `ClearHtbQosQueue` has the same if/else (iface vs pod condition) as `GetQosList`; consider a shared helper e.g. `findQueueOrQosByIfaceOrPod(iface, podName, podNamespace string, table string) ([]string, error)`.
- [ ] **Readability**: `SetNetemQos` is long (~100 lines) with deep nesting; extract helpers e.g. `applyNetemQosToInterface`, `deleteAllNetemQosForInterface` to reduce complexity.
- [ ] **Maintainability**: `SetHtbQosQueueRecord` and `SetQosQueueBinding` mutate `queueIfaceUIDMap`/`qosIfaceUIDMap` in place; document this side effect in the function doc comments.

---

## ./pkg/ovs/util.go

- [ ] **Robustness**: In `getIpv6Prefix`, `strings.Split(network, "/")[1]` panics if `network` has no "/"; use `strings.Cut(network, "/")` or validate format before indexing.
- [ ] **Readability**: In `AndACLMatch.Match` and `OrACLMatch.Match`, on error the message uses `match` which may be from a previous iteration or empty; use the failing sub-match (e.g. `r.String()`) in the error for clearer context.
- [ ] **Maintainability**: Fix comment typo in `OrACLMatch.Match`: "has more then one" → "has more than one".
- [ ] **Maintainability**: In `parseDHCPOptions`, comment says "return default Ipv6RaConfigs" but the function returns nil when raw is empty; fix comment to "return nil when raw is empty".
- [ ] **Performance**: `Limiter.Wait` uses a busy loop with `time.Sleep(10*time.Millisecond)` when at limit; consider sync.Cond or a channel to avoid spinning and reduce latency when a slot frees.

---

## ./pkg/util/external_vpc.go

- [ ] **Readability**: Add brief godoc for exported types (LogicalRouter, LogicalSwitch, Port) to clarify their role in external VPC context.

---

## ./pkg/util/hash.go

- [ ] **DRY**: Sha256Hash and Sha256HashObject duplicate the pattern (hasher.Write, Sum(nil), hex.EncodeToString). Extract a helper e.g. hashBytesToHex(data []byte) string and use it in both.
- [ ] **Naming**: Use idiomatic Go acronym casing: Sha256Hash → SHA256Hash, Sha256HashObject → SHA256HashObject (breaking API change; consider for major version).
- [ ] **Correctness**: Sha256HashObject uses json.Marshal; map key order in JSON is non-deterministic, so hashing values containing maps may yield different hashes for the same logical content. Document this or use deterministic encoding for map types.

---

## ./pkg/util/hash_test.go

- [ ] **Readability**: In TestSha256HashObject, when wantErr is true the test still compares hash to tt.hash; skip the hash comparison when err != nil to make intent clear and avoid relying on zero value.

---

## ./pkg/util/health_check.go

- [ ] **Readability**: Add godoc for DefaultHealthCheckHandler describing that it responds with 200 and "ok" for health probes.

---

## ./pkg/util/ip.go

- [ ] **Robustness**: IPv4ToUint32(ip) assumes len(ip) >= 4; nil or short ip panics. Add a bounds check and return (0, error) or document that ip must be a valid 4-byte IPv4.
- [ ] **Readability**: Add brief godoc for Uint32ToIPv4, IPv4ToUint32, Uint32ToIPv6 describing input/output and assumptions (e.g. ip must be 4-byte for IPv4ToUint32).

---

## ./pkg/util/address_family_test.go

- [ ] **Readability**: Rename test case "correct" to "ipv4" for consistency with "v6" and "dual".
- [ ] **Structure**: `ErrorContains` is used by net_test.go and validator_test.go; consider moving to a shared test helper (e.g. pkg/util/testing.go or testhelper_test.go) so it is defined once and discoverable.

---

## ./pkg/util/const.go

- [ ] **Naming**: Fix typo `DepreciatedFinalizerName` → `DeprecatedFinalizerName` (deprecated, not depreciated).
- [ ] **Naming**: Fix typo `MirrosRetryMaxTimes` / `MirrosRetryInterval` → `MirrorsRetryMaxTimes` / `MirrorsRetryInterval`; update all call sites (e.g. cmd/daemon/cniserver.go, pkg/daemon).
- [ ] **Naming**: Verify `GeneveNic = "genev_sys_6081"` — if the interface name follows OVS convention, add a short comment; if typo, use "geneve_sys_6081".
- [ ] **Maintainability**: The main const block is long (~280 lines); add section comments (e.g. "// OVN annotations", "// Labels", "// Priorities") to group related constants and improve navigation.

---

## ./pkg/util/arp.go

- [ ] **Correctness**: ArpResolve creates `time.NewTimer(timeout)` but never calls `timer.Stop()` when returning early (success or done). This can leak the timer. Use `defer timer.Stop()` after creating the timer.
- [ ] **Readability**: Fix typos `probeMinmum` and `probeMaxmum` → `probeMinimum` and `probeMaximum`.
- [ ] **Readability**: Extract broadcast MAC `net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}` to a package-level constant (e.g. `broadcastMAC`) — used in ArpDetectIPConflict and AnnounceArpAddress.
- [ ] **Structure**: ArpResolve has three consecutive retry loops sharing `count` and `timer`; the meaning of `count` is unclear (attempt index vs retries). Consider extracting each phase into helpers (e.g. `getInterfaceWithRetry`, `dialArpWithRetry`, `resolveWithRetry`) and use a single total-attempts counter for the return value.
- [ ] **Readability**: In ArpDetectIPConflict, the goroutine shadows outer `pkt` with `pkt, _, err := client.Read()`; rename to `replyPkt` or `recvPkt` to avoid confusion with the probe packet.
- [ ] **Performance**: ArpDetectIPConflict reader goroutine uses `for !time.Now().After(deadline)` (busy-wait). Consider a single read with SetReadDeadline and select on a done channel or context for cleaner cancellation.
- [ ] **Readability**: macEqual can be simplified with `bytes.Equal(a, b)` (handles nil: bytes.Equal(nil, nil) is true in Go); keep explicit nil handling if both-nil must be distinguished from empty slice.
- [ ] **Maintainability**: AnnounceArpAddress creates a new `time.NewTimer(announceInterval)` each loop iteration; could use a single timer reset or time.Sleep between sends to avoid allocating a timer per iteration.

---

## ./pkg/util/arp_test.go

- [ ] **DRY**: The pattern "get default route, nic index, link by index, and optionally first IPv4 addr" is duplicated in TestArpResolve, TestDetectIPConflict, and TestAnnounceArpAddress. Extract a test helper (e.g. `getDefaultRouteNicAndGW(t)` or `getDefaultNicAndIP(t)`) to reduce duplication and simplify tests.
- [ ] **Readability**: Remove redundant `return` after `t.Fatalf(...)` — Fatal already exits the test.
- [ ] **Naming**: Fix comment typo "invalid mc" → "invalid mac" in TestAnnounceArpAddress (line 289).
- [ ] **Consistency**: TestMacEqual uses raw `t.Errorf`; other tests in the file use `require`. Consider using `require.Equal(t, test.expected, result)` in TestMacEqual for consistency.

---

## ./pkg/util/ippool.go

- [ ] **Naming / Maintainability**: In `CanonicalizeIPPoolEntries`, the variable `set` (map[string]bool) shadows the imported package `set` (k8s.io/utils/set). Rename to `result` or `canonical` to avoid confusion when reading the function and to avoid accidental shadowing if more code is added later.
- [ ] **Readability**: `IPRangeToCIDRs` implements a non-trivial minimal-CIDR-cover algorithm. Add a short godoc or comment at the top explaining the approach (e.g. greedy: at each step take the largest power-of-two block from start that fits in the range).
- [ ] **Performance** (minor): In `IPRangeToCIDRs` loop, `increment := new(big.Int).Lsh(big.NewInt(1), uint(size))` allocates two big.Ints per iteration. Consider reusing a local buffer for the increment to reduce allocations when processing large IP ranges.

---

## ./pkg/util/ippool_test.go

- [ ] **Naming**: Rename `TestIpToCIDR` to `TestIPToCIDR` per Go convention (acronym IP in capitals).
- [ ] **Readability / DRY**: In `TestExpandIPPoolAddressesMixedAllowed` subtest "Complex mixed scenario", the IPv4/IPv6 detection loop (strings.Contains(addr, ":")) duplicates the heuristic used in production. Consider a one-line test helper or a short comment that the check mirrors the output format of ExpandIPPoolAddresses to avoid drift if the representation changes.

---

## ./pkg/util/iptables.go

- [ ] **Naming**: GwIPtableCounters uses "IPtable" (one word); consider GwIptablesCounters for consistency with common "iptables" spelling (update call sites in pkg/daemon/gateway_linux.go and pkg/daemon/controller_linux.go).
- [ ] **Readability**: Add brief godoc for GwIPtableCounters describing its purpose (e.g. packet and byte counters for gateway iptables).
- [ ] **Readability**: IPTableRule.Pos is opaque; add godoc or rename to Position to clarify it denotes the rule position in the chain.

---

## ./pkg/util/ip_test.go

- [ ] **Readability / Maintainability**: Use subtests with `t.Run(tt.expected, func(t *testing.T){...})` (or a short case name) in TestUint32ToIPv4, TestIPv4ToUint32, and TestUint32ToIPv6 so failures report which input/expected pair failed.
- [ ] **Robustness**: TestIPv4ToUint32 has a nil check for net.ParseIP(tt.input).To4() but no table entry uses an invalid IP; add a negative test case (e.g. "invalid" or "256.0.0.1") to assert behavior on invalid input, or document that the table is valid-only.

---

## ./pkg/util/k8s.go

- [ ] **Correctness / Resource leak**: DialAPIServer creates `time.NewTimer(interval)` but never calls `timer.Stop()` when returning (success or after retries). The timer can leak. Use `defer timer.Stop()` after creating the timer.
- [ ] **Robustness**: GetTruncatedUID(uid) does `uid[len(uid)-12:]`; if len(uid) < 12 this panics. Add a bounds check (e.g. if len(uid) < 12 return uid or return "", false) or document that uid must be at least 12 characters.
- [ ] **Error handling / Readability**: DialTCP returns `fmt.Errorf("timed out dialing host %q", host)` on any dial failure; the error might be connection refused or other, not necessarily timeout. Return the underlying err (e.g. `fmt.Errorf("failed to dial %q: %w", host, err)`) so callers can distinguish.
- [ ] **Readability**: ObjectMatchesLabelSelector comment says "if the selector is nil, it returns false" but the code relies on LabelSelectorAsSelector(nil) returning an error. Add an explicit `if selector == nil { return false }` at the start for clarity and to match the documented behavior.

---

## ./pkg/util/k8s_test.go

- [ ] **Correctness / Test naming**: TestDialTCP has duplicate test case names "Valid HTTP Host" (lines 106–107). Use unique names (e.g. "Valid HTTP Host" and "Valid HTTP Host verbose") so test output is clear when a case fails.
- [ ] **Correctness**: TestPodIPs only asserts `len(ret) != len(tt.exp)`; it does not verify slice contents. Use `require.ElementsMatch(ret, tt.exp)` (or equivalent) so wrong values (e.g. ["a","b"] vs ["x","y"]) fail.
- [ ] **Correctness**: TestServiceClusterIPs has the same issue—only checks length. Use `require.ElementsMatch(ret, tt.exp)` or compare contents so invalid IPs or wrong order are caught.
- [ ] **Maintainability**: In TestDialTCP, mutating `tt.expected` inside the run loop (lines 147–149) makes the table-driven test harder to follow. Prefer building the expected error per run (e.g. local variable or table field that is a func() error) so cases are not mutated.
- [ ] **Readability**: TestDialTCP error assertion (lines 153–155) is a long boolean expression. Extract to a helper (e.g. `errorsMatch(err, expected error) bool`) or use `require` with `ErrorContains` for clearer failure messages.
- [ ] **Readability**: Both "Valid HTTP Host" and "Valid HTTPS Host" cases use `httpServer.URL` after `StartTLS()`, so the URL is HTTPS. The name "Valid HTTP Host" is misleading. Either use a separate HTTP server for the HTTP case or rename cases to reflect HTTPS (e.g. "Valid HTTPS Host (non-verbose)" / "Valid HTTPS Host (verbose)").

---

## ./pkg/util/klog_test.go

- [ ] **Readability**: In TestLogFatalAndExit, assign `cmd.Stderr.(*bytes.Buffer)` to a variable (e.g. `stderr`) to avoid repeating the type assertion and make the assertion/Contains check easier to read.

---

## ./pkg/util/link_test.go

- [ ] **Readability**: Fix comment typos "should failed" → "should fail", "should ok" → "should succeed".
- [ ] **Maintainability**: When SetLinkUp fails (non-skip case), the test does t.Errorf and then require.NoError(t, err), causing double failure reporting. Use a single path: if err != nil { if strings.Contains(...) { t.Skip(...); return }; t.Fatalf("Error setting link up: %v", err) } and remove the trailing require.NoError(t, err).
- [ ] **Correctness**: Error message "Error resolving ARP" is incorrect (operation is setting link up). Change to "Error setting link up" or similar.

---

## ./pkg/util/log.go

- [ ] **Maintainability**: Hardcoded path "/var/log/kube-ovn/" could be a package-level constant (e.g. defaultLogDir) or configurable for testability and different environments.
- [ ] **Error handling**: The return value of `f.Close()` is ignored; use `defer f.Close()` and consider logging or handling close errors.
- [ ] **Readability / DRY**: The two branches (create vs existing) can be unified: open or create the file (e.g. with 0644), then always call `os.Chmod(path, perm)` so one code path handles both cases.
- [ ] **Robustness**: If `os.Stat` fails for a reason other than `IsNotExist` (e.g. permission denied), the code goes to the else branch and Chmod may fail with a confusing error; consider handling non-IsNotExist errors explicitly.

---

## ./pkg/util/named_port.go

- [ ] **Readability / Maintainability**: Add godoc for `NamedPortInfo` and its fields (PortID, Pods) so the purpose of the struct and the meaning of PortID vs named port name key are clear to callers.

---

## ./pkg/util/ndp.go

- [ ] **Resource leak**: In the LOOP, `timer := time.NewTimer(dadRetransTimer)` is never stopped. When breaking early via macChan or errChan, the timer leaks. Use `defer timer.Stop()` or call `timer.Stop()` in the select when breaking.
- [ ] **Structure / Readability**: `DuplicateAddressDetection` is long (~200 lines). Consider extracting helpers: e.g. `resolveSrcIP(iface, target, addresses) (net.IP, error)`, `buildNSPacket(...) ([]byte, error)`, and a read-loop helper to improve testability and readability.
- [ ] **Readability**: BPF init uses magic offsets (14+40+32, 14+6, 14+40). Define named constants (e.g. ethHdrLen=14, ipv6HdrLen=40, icmpv6TypeOff=40) for clarity.
- [ ] **DRY**: Repeated pattern `err = fmt.Errorf(...); klog.Error(err); return false, nil, err` appears many times. Consider a small helper (e.g. `func failDAD(err error) (bool, net.HardwareAddr, error)`) to reduce duplication.

---

## ./pkg/util/net.go

- [ ] **Correctness / Error handling**: SubnetNumber and SubnetBroadcast ignore errors from `net.ParseCIDR(subnet)`; malformed subnet can cause nil deref or wrong result. Validate and return error or empty.
- [ ] **Correctness**: IP2BigInt does not check `net.ParseIP(ipStr)` result; invalid IP can cause nil deref on To4()/To16(). Add nil check or return error.
- [ ] **Correctness**: GenerateMac returns a MAC even when `rand.Read(buf)` fails (buf may be zero). Consider returning an error or retrying on failure.
- [ ] **Error handling**: TCPConnectivityCheck ignores the return value of `conn.Close()`. Use `defer conn.Close()` and/or handle close error.
- [ ] **Readability / DRY**: CIDRGlobalUnicast repeats the same "if CIDROverlap(cidrBlock, X) { return fmt.Errorf(...) }" pattern for many CIDR constants. Consider a slice of (cidr, name) and a loop to reduce duplication.
- [ ] **Readability**: Magic number `3*time.Second` in TCPConnectivityCheck and UDPConnectivityCheck; use a named constant (e.g. connectivityCheckTimeout).
- [ ] **Resource leak / Design**: TCPConnectivityListen and UDPConnectivityListen start a goroutine that runs forever but do not return the listener/conn; the listener can never be closed. Consider returning the listener so callers can shut it down, or document that it runs until process exit.

---

## ./pkg/util/net_test.go

- [ ] **Correctness**: TestSplitIpsByProtocol asserts with `if slices.Equal(ans4, c.want4) && slices.Equal(ans6, c.want6) { t.Errorf(...) }`, so it fails when the result equals expected. Invert to `if !slices.Equal(ans4, c.want4) || !slices.Equal(ans6, c.want6) { t.Errorf(...) }`.
- [ ] **Correctness / Readability**: TestGenerateRandomV4IP: for valid CIDR "base", the else branch errors when `IPNets.Contains(GenerateRandomIP(c.cidr))` is true, i.e. it expects the random IP to be outside the CIDR. That contradicts GenerateRandomIP (which should return an IP inside the network). Clarify and fix test logic or expectations.
- [ ] **Readability**: Unify loop variable naming (some use `c`, others `tt` or `test`); standardize on one (e.g. `tt`) for consistency.
- [ ] **Readability**: TestGenerateMac has an unused field `want bool` in the test struct; remove it or add meaningful assertions.
- [ ] **Readability**: Duplicate test case names: TestCIDRContainIP has two "base"; Test_CIDRContainIP has two "different family"; TestFirstIP has two "base31netmask". Use unique names so failures identify the case.
- [ ] **Maintainability**: TestGetDefaultListenAddr modifies env (POD_IPS, ENABLE_BIND_LOCAL_IP) without restoring; can cause flakiness in parallel or subsequent tests. Use t.Cleanup to restore env or a helper that saves/restores.
- [ ] **Maintainability**: TestTCPConnectivityListen/Check and TestUDPConnectivityListen/Check use fixed ports (65531–65534); parallel or repeated runs can cause port conflict. Consider dynamic ports or skip if port in use.
- [ ] **DRY**: TestCIDRContainIP and Test_CIDRContainIP both test CIDRContainIP; merge into one table-driven test and remove duplication.
- [ ] **Readability**: TestGetIPAddrWithMask does not explicitly assert error vs success; the condition `(err != nil && tt.want != "") || (err == nil && got != tt.want)` does not require err != nil when want is "". Add wantErr or explicit err checks for error cases.
- [ ] **Readability**: Use require.NoError(t, err) instead of require.Nil(t, err) for success in TestInvalidNetworkMask and similar; idiomatic and clearer intent.
- [ ] **Documentation**: TestSubnetBroadcast has a TODO comment "this is a bug, the broadcast address should be 192.128.23.1" for v4/31; either fix implementation and test or document the intended behavior.

---

## ./pkg/util/network_attachment.go

- [ ] **Readability**: In GetNadInterfaceFromNetworkStatusAnnotation, return early when the interface is found (e.g. `return iface, nil` inside the loop) and only after the loop return the "no interface name found" error; removes the need for the `interfaceName` variable and the final empty check.
- [ ] **Readability / Maintainability**: Add brief godoc for exported functions (IsOvnNetwork, IsDefaultNet, GetNadInterfaceFromNetworkStatusAnnotation) to clarify purpose and parameters.

---

## ./pkg/util/network_attachment_test.go

- [ ] **Readability**: Use full names instead of abbreviations: `expt` → `expected` (or `want`), `rslt` → `result` (or `got`) in TestIsOvnNetwork and TestIsDefaultNet for consistency with TestGetNadInterfaceFromNetworkStatusAnnotation.
- [ ] **Consistency**: Unify loop variable naming (TestIsOvnNetwork and TestIsDefaultNet use `c`; TestGetNadInterfaceFromNetworkStatusAnnotation uses `tt`). Prefer `tt` for table-driven tests so all tests in the file follow the same style.
- [ ] **Structure**: "Additional error scenarios" subtest (empty status, empty array, malformed JSON) could be merged into the main table in TestGetNadInterfaceFromNetworkStatusAnnotation so all cases live in one place and share the same assertion pattern.

---

## ./pkg/util/ovn.go

- [ ] **Readability**: Add godoc for NodeLspName documenting that it returns the OVN logical switch port name for a node (i.e. NodeLspPrefix + node), and that Lsp stands for logical switch port.

---

## ./pkg/util/ovn_test.go

- [ ] **Consistency**: Use loop variable `tt` instead of `test` and optionally `want`/`got` instead of `expected`/`result` to align with table-driven test style in other pkg/util test files (e.g. network_attachment_test.go).

---

## ./pkg/util/patch.go

- [ ] **DRY**: GenerateStrategicMergePatchPayload and GenerateMergePatchPayload duplicate the same pattern (marshal original, marshal modified, call create*Patch, return). Extract a helper e.g. generatePatchPayload(original, modified runtime.Object, createFn func([]byte, []byte, any) ([]byte, error)) ([]byte, error) to reduce duplication.
- [ ] **Extensibility / API**: patchMetaKVs uses context.Background() instead of accepting ctx; PatchLabels and PatchAnnotations cannot pass context for cancellation or timeout. Consider adding ctx context.Context to patchMetaKVs and the public Patch* functions so callers can pass context.
- [ ] **Readability**: Add godoc for exported symbols (KVPatch, PatchLabels, PatchAnnotations, GenerateStrategicMergePatchPayload, GenerateMergePatchPayload).
- [ ] **Maintainability**: In GenerateStrategicMergePatchPayload and GenerateMergePatchPayload, klog.Error(err) loses context; use klog.Errorf with a short message (e.g. "failed to marshal original: %v", err) for consistency with patchMetaKVs and easier debugging.

---

## ./pkg/util/patch_test.go

- [ ] **Correctness**: In TestPatchLabels the comment says "create a node" but the code creates a Namespace; fix comment to "create a namespace".
- [ ] **DRY**: TestGenerateStrategicMergePatchPayload and TestGenerateMergePatchPayload share nearly identical test tables and assertion logic (marshal remote, apply patch, unmarshal, assert). Extract a shared helper e.g. runPatchPayloadTestCase(t, original, modified, remote, want, generateFn) or a table-driven runner to reduce duplication.
- [ ] **Error handling**: In both payload tests, json.Marshal(tt.args.remote), strategicpatch.StrategicMergePatch, and json.Unmarshal errors are ignored; use require.NoError(t, err) or t.Fatalf for robustness.
- [ ] **Correctness / Test semantics**: TestGenerateMergePatchPayload applies the result of GenerateMergePatchPayload (RFC 7396 merge patch) using strategicpatch.StrategicMergePatch (Kubernetes strategic merge). Verify that this is intended; if merge patch and strategic merge patch semantics differ for the test cases, use the appropriate apply (e.g. jsonpatch merge) so the test validates merge patch behavior correctly.
- [ ] **DRY**: The type unsupportedType (and args struct with comments) is duplicated in TestGenerateStrategicMergePatchPayload and TestGenerateMergePatchPayload; move to file level or a shared test helper to avoid duplication.

---

## ./pkg/util/pod_exec.go

- [ ] **Extensibility / API**: execute uses context.TODO() so callers cannot cancel or set timeout for exec. Add ctx context.Context to ExecuteWithOptions (and optionally ExecuteCommandInContainer) and pass it to remotecommand.StreamWithContext.
- [ ] **Readability**: Add godoc for ExecOptions (and its fields), ExecuteCommandInContainer, and ExecuteWithOptions to document purpose and parameters.

---

## ./pkg/util/pod_exec_test.go

- [ ] **Readability**: TestExecute exercises the unexported execute with invalid method and resource ("xxxx"); rename to e.g. TestExecute_InvalidRequest or add a brief comment so readers know it is testing the error path.
- [ ] **DRY**: TestExecuteCommandInContainer and TestExecuteWithOptions repeat the same setup (cfg, kubeClient, namespace, podName, containerName); optional: extract a small helper e.g. newExecTestClient(t) returning (kubernetes.Interface, *rest.Config) and common defaults to reduce duplication.

---

## ./pkg/util/pod_routes.go

- [ ] **Error handling**: In `ToAnnotations()`, `json.Marshal(routes)` error is ignored (`buf, _ := json.Marshal(routes)`). Per CODE_STYLE (return/log errors), consider returning the error from `ToAnnotations()` when Marshal fails, or document that Marshal of `[]request.Route` is considered infallible in this context.
- [ ] **API / Deduplication**: `Add(provider, destination, gateway)` appends to the slice; calling it twice with the same (provider, destination, gateway) yields duplicate entries in the annotation. If the intended semantic is a set of routes, deduplicate in `Add()` (e.g. check before append) or in `ToAnnotations()`; otherwise document the append-only behavior.
- [ ] **Readability**: Add brief godoc for `NewPodRoutes`, `Add`, and `ToAnnotations` (e.g. that `Add` ignores empty gateway or destination) to clarify public API.

---

## ./pkg/util/pod_routes_test.go

- [ ] **Test coverage**: Add a case for IPv6 destination (e.g. `routes.Add("foo", "::1", "fe80::1")`) and assert the annotation contains `"/128"` for the destination, to guard against regressions in CIDR normalization for IPv6.

---

## ./pkg/util/provider_network.go

- [ ] **Robustness**: `NodeMatchesSelector` and `IsNodeExcludedFromProviderNetwork` do not guard against nil `node` or nil `pn`; passing nil would panic (e.g. `labels.Set(node.Labels)` when node is nil). Consider adding nil checks and returning an error, or document in godoc that callers must pass non-nil arguments.
- [ ] **Readability**: When `pn.Spec.NodeSelector != nil`, `ExcludeNodes` is ignored (only NodeSelector is used). Add a one-line comment in `IsNodeExcludedFromProviderNetwork` that NodeSelector takes precedence so future readers do not assume both are combined.

---

## ./pkg/util/slice.go

- [ ] **Performance**: `DiffStringSlice` and `IsStringsOverlap` use `slices.Contains` inside a loop, yielding O(n*m). For larger inputs, build a set from one slice (e.g. `set.New(slice2...)`) and use set membership for O(n+m) lookups.
- [ ] **Readability**: `DiffStringSlice` uses an in-loop swap of `slice1`/`slice2` to run two passes; consider two explicit loops (elements in slice1 not in slice2; then elements in slice2 not in slice1) appending to `diff`, to avoid mutating loop variables and clarify intent.
- [ ] **Readability**: Add godoc for `DiffStringSlice`, `UnionStringSlice`, and `RemoveString`. Fix `IsStringsOverlap` godoc: "check" → "checks".

---

## ./pkg/util/slice_test.go

- [ ] **Structure**: `TestDiffStringSlice` and `Test_DiffStringSlice` both test `DiffStringSlice` with different cases; consider merging into one table-driven test or renaming (e.g. `TestDiffStringSlice_SymmetricDifference`) to avoid duplication and clarify scope.
- [ ] **Test coverage**: `TestRemoveString` does not cover multiple occurrences (e.g. remove "a" from `["a","a","b"]` should yield `["b"]`); add a case to assert all occurrences are removed.

---

## ./pkg/util/subnet.go

- [ ] **DRY**: `IsOvnProvider` and `GetNadBySubnetProvider` both call `strings.Split(provider, ".")` and check `len(fields) == 3 && fields[2] == OvnProvider`. Extract a small helper e.g. `parseProvider(provider string) (nadName, nadNamespace string, isOvn bool)` and implement both functions in terms of it to avoid duplicated parsing and format assumptions.
- [ ] **Readability**: Replace magic field counts 2 and 3 with named constants (e.g. `providerFormatNADParts = 2`, `providerFormatWithProviderPart = 3`) to document the expected provider format (e.g. `"nadName.nadNamespace"` or `"nadName.nadNamespace.ovn"`).

---

## ./pkg/util/subnet_test.go

- [ ] **Test coverage**: Add tests for `GetNadBySubnetProvider` covering: three-part format (`"ns.name.ovn"` => (name, ns, true)), two-part format (`"ns.name"` => (name, ns, true)), single part (`"a"` => ("", "", false)), empty string => ("", "", false), and optionally malformed (e.g. four parts) to lock in behavior.

---

## ./pkg/util/strings.go

- [ ] **Readability**: Add godoc for `DoubleQuotedFields` describing that fields are split by spaces outside double quotes and that double-quoted segments form a single field (spaces inside quotes preserved).
- [ ] **Documentation / Edge cases**: Document or add tests for: unclosed quote (e.g. `a "b c`), consecutive spaces (e.g. `a  b` yielding an empty field between), and empty string input; clarify intended behavior for these cases.

---

## ./pkg/util/strings_test.go

- [ ] **Test coverage**: Add cases for empty string, consecutive spaces (`a  b`), unclosed quote (`a "b c`), and input that is only a quoted string (`"x y"`), to document and lock in behavior.

---

## ./pkg/util/validator.go

- [ ] **DRY**: The pattern "if ContainsUppercase(field) { err := fmt.Errorf(...); klog.Error(err); return err }" repeats many times in `ValidateSubnet` (gateway, excludeIps, CIDRBlock, allowSubnets, externalEgressGateway, vips, U2OInterconnectionIP). Extract a helper e.g. `validateNoUppercase(fieldName, value string) error` to reduce duplication.
- [ ] **Naming**: Fix typo `validateNatOutGoingPolicyRuleIPs` → `validateNatOutgoingPolicyRuleIPs` (and "OutGoing" in error messages) for consistent Go naming.
- [ ] **Readability / Shadowing**: In `ValidatePodNetwork`, variable `errors` shadows the imported package `errors`; rename to `errs` or `errList` to avoid confusion and allow use of `errors.New` etc. if needed.
- [ ] **Structure / Readability**: `ValidateSubnet` is long (~195 lines) and mixes gateway, CIDR, excludeIps, allowSubnets, gatewayType, protocol, VPC, externalEgressGateway, vips, nat rules, U2O checks. Consider extracting helpers e.g. `validateSubnetGateway`, `validateSubnetExcludeIps`, `validateSubnetCIDRBlocks` to shorten and improve testability.
- [ ] **Error handling**: In `ValidateNetworkBroadcast`, `_, network, _ := net.ParseCIDR(cidrBlock)` ignores the error; if CIDR is invalid, `network` may be nil and `AddressCount(network)` could panic. Check error and return or skip invalid CIDR.

---

## ./pkg/util/validator_test.go

- [ ] **Naming / Readability**: Fix typos in test case names: `CICDblockFormalErr` → `CIDRBlockFormatErr` (CICD → CIDR, Formal → Format); `ExgressGWErr1` etc. → `EgressGWErr1` (Exgress → Egress); `CIDRformErr` → `CIDRFormatErr`; `corretV4` / `corretDual` → `correctV4` / `correctDual`; `EgRatErr` → `EgressRateErr`; `ingRaErr` → `IngressRateErr`; `LogicalGatewayU2OInterconnectionSametimeTrueErr` → `SameTime` (Sametime → SameTime) for consistency and discoverability.
- [ ] **Structure**: `ErrorContains` is shared with net_test.go; consider moving to a shared test helper (see net.go refactor) so it is defined once.

---

## ./pkg/util/version.go

- [ ] **Readability / Shadowing**: In the loop `for i := range 4`, variables `version1` and `version2` shadow the function parameters and are reused as ints from `strconv.Atoi`. Rename to e.g. `v1, v2` or `num1, num2` to avoid confusion and make the loop body clearer.
- [ ] **Error handling**: `strconv.Atoi(versionA[i])` and `strconv.Atoi(versionB[i])` ignore errors; invalid segments (e.g. "1.8.x") are treated as 0. Document this behavior or validate and return an error for malformed versions.
- [ ] **Readability**: Magic number `4` (version segment count) appears in three places; extract a named constant e.g. `versionSegmentCount` for maintainability.
- [ ] **DRY**: The two loops that pad `versionA` and `versionB` to 4 segments are identical; extract a helper e.g. `normalizeVersionSegments(segments []string, length int) []string` to reduce duplication.

---

## ./pkg/util/version_test.go

- [ ] **Readability**: Several test cases have empty `name`; give each case a descriptive name (e.g. "v1_gt_v2", "v1_lt_v2", "v1_eq_v2") so `go test -v` output is clearer.

---

## ./pkg/util/vlan.go

- [ ] **Maintainability / DRY**: The prefix `"br-"` is hardcoded here; `pkg/daemon/ovs_linux.go` and `pkg/util/vlan_interfaces.go` use the literal `"br-"` when parsing. Extract a package constant (e.g. `externalBridgePrefix`) and use it in `ExternalBridgeName`; consider exporting it so callers (ovs_linux, vlan_interfaces) can use the same constant and avoid drift.

---

## ./pkg/util/vlan_interfaces.go

- [ ] **DRY**: Line 119 `strings.HasPrefix(parts[0], "br-")` duplicates the bridge prefix; use the same constant as in vlan.go (externalBridgePrefix) once it exists.
- [ ] **Error handling / API**: `DetectVlanInterfaces` returns an empty slice when `netlink.LinkList()` fails; callers cannot distinguish "no VLAN interfaces" from "error listing links". Consider returning `([]int, error)` so callers can handle or retry on error (aligned with `FindKubeOVNAutoCreatedInterfaces` which returns error).
- [ ] **Readability**: In `IsVlanInternalPort`, returning `(false, 0)` for invalid format makes VLAN ID 0 ambiguous (valid VLAN 0 vs invalid). Document this in the function comment or consider a different signature if callers need to distinguish.

---

## ./pkg/util/vpc_nat_gateway.go

- [ ] **Structure / Testability**: Package-level mutable var `VpcNatGwNamePrefix` makes behavior order-dependent and harder to test (tests rely on t.Cleanup to restore). Consider making it configurable via a function or a small config struct so tests can isolate without mutating globals.
- [ ] **DRY**: `GenNatGwPodName(name)` duplicates the format of `GenNatGwName(name)`; implement as `GenNatGwName(name) + "-0"` to avoid drift when prefix logic changes.
- [ ] **Maintainability**: In `GenNatGwSelectors`, when `len(parts) != 2` the code silently skips the selector. Consider logging a warning or returning an error for malformed selectors so user typos (e.g. missing colon) are visible.
- [ ] **Readability / Fail-fast**: In `GenNatGwPodAnnotations`, provider validation (providerSplit length and OvnProvider check) runs after building part of the result. Validate provider format at the start when `p != OvnProvider` so invalid input fails early.
- [ ] **Readability**: In `GenNatGwBgpSpeakerContainer`, validate all required params (ASN, RemoteASN, Neighbors) at the beginning of the function before appending optional args, for fail-fast and clearer control flow.
- [ ] **Readability**: In `GenNatGwBgpSpeakerContainer`, the loop that partitions neighbors into IPv4/IPv6 and builds arg strings is long; extract a helper e.g. `partitionNeighborsByFamily(neighbors []string) (v4, v6 []string, err error)` and build neighbor args in one place to shorten the main function.

---

## ./pkg/util/vpc_nat_gateway_test.go

- [ ] **Correctness**: In `TestGenNatGwBgpSpeakerContainer`, when `mustError` is true and `err == nil`, the test does not fail; it falls through and checks `result.Name`. Add `if tc.mustError && err == nil { t.Error("expected error"); return }` so invalid input is properly asserted.
- [ ] **DRY**: `TestGenNatGwNameWithCustomPrefix` and `TestGenNatGwPodNameWithCustomPrefix` repeat the pattern of setting `VpcNatGwNamePrefix`, `t.Cleanup` restore, then running cases. Consider a helper e.g. `withNatGwPrefix(t, prefix string, fn func())` to avoid duplication and reduce risk of forgetting cleanup.
- [ ] **Readability**: `TestGenNatGwPodAnnotations` has many inline struct literals with repeated gw/externalNad fields. Consider defining default base values and overriding only what changes per case to shorten the table and make diffs clearer.
- [ ] **Maintainability**: The container name `"vpc-nat-gw-speaker"` is hardcoded in the test; the same string appears in the implementation. Consider a package constant (e.g. in vpc_nat_gateway.go) and use it in both so renames are in one place.

---

## ./pkg/webhook/ip.go

- [ ] **DRY / Structure**: `IPUpdateHook` has 7 nearly identical blocks checking immutable fields (Subnet, Namespace, PodName, PodType, V4IPAddress, V6IPAddress, MacAddress). Extract a table-driven check or helper e.g. `validateImmutableIPFields(old, new *ovnv1.IP) error` to reduce duplication.
- [ ] **Readability**: Fix success message typo "by pass" → "bypass" (lines 29, 76).
- [ ] **Logic / Redundancy**: In `ValidateIP`, the check `if ip.Spec.Subnet == ""` appears twice (lines 80–82 and 123–125); the second is unreachable after the first return. Remove the redundant second check.
- [ ] **Readability**: Simplify `err := fmt.Errorf(...); return err` to `return fmt.Errorf(...)` in multiple places (e.g. lines 48–49, 52–53, 92–93, 106–107).
- [ ] **DRY**: V4 and V6 validation in `ValidateIP` share the same pattern (parse IP, check CIDR); consider a small helper e.g. `validateIPInSubnet(ipStr, subnetName string, cidrBlock string, version string) error` (V6 has extra uppercase check, so either two helpers or one with an option).

---

## ./pkg/webhook/ovn_nat_gateway.go

- [ ] **Readability**: Fix success message typo "by pass" → "bypass" (multiple hooks). Consider a package-level constant e.g. `webhookAllowedReason = "bypass"` for consistency across webhook files.
- [ ] **DRY**: Eip/Dnat/Snat/Fip Update hooks share the same pattern (decode new/old, if spec changed and old.Status.Ready then error else if spec changed validate new). Extract a generic helper or small closure factory to reduce duplication.
- [ ] **DRY**: `ValidateOvnEip` V4/V6 validation duplicates the pattern in `pkg/webhook/ip.go` (parse IP, check CIDR, V6 uppercase). Share a validation helper in the webhook package.
- [ ] **DRY**: In `ValidateOvnDnat`, port validation (Atoi + range 0–65535) is duplicated for ExternalPort and InternalPort. Extract e.g. `validatePort(portStr, fieldName string) error`.
- [ ] **Maintainability**: Fix error message typo: "failed to parse spec internalIP" (line 304) → "internalPort" to match the field name.
- [ ] **Readability**: Simplify `err := errors.New(...); return err` to `return errors.New(...)` in ValidateOvnDnat, ValidateOvnFip and elsewhere.
- [ ] **Consistency**: `ValidateOvnSnat` and `ValidateOvnFip` return `v.cache.Get(ctx, key, eip)` directly; other validators use `if err := v.cache.Get(...); err != nil { return err }`. Use the same pattern for consistency and easier logging.
- [ ] **DRY / Readability**: `isOvnEipInUse` has three identical list+check blocks (dnat, fip, snat). Consider a loop over (list ptr, usageKind string) or a helper to reduce repetition.

---

## ./pkg/webhook/static_ip.go

- [ ] **Readability**: Fix success message typo "by pass" → "bypass" (multiple hooks).
- [ ] **DRY**: DeploymentCreateHook, StatefulSetCreateHook, DaemonSetCreateHook, JobCreateHook are nearly identical (decode, get static IPS from template annotations, log, if empty allowed else validateIP). Extract a helper e.g. `validateWorkloadStaticIP(ctx, req, getAnnotations func() map[string]string, kind, name, namespace string)` or pass template annotations + kind/name/namespace to reduce duplication. CronJob differs only in path (JobTemplate.Spec.Template).
- [ ] **Readability**: `allowLiveMigration` can be simplified to `return annotations[kubevirtv1.MigrationJobNameAnnotation] != ""`.

---

## ./pkg/webhook/subnet.go

- [ ] **Readability**: Fix success message typo "by pass" → "bypass" in all three hooks (lines 55, 90, 101). Prefer a package-level constant e.g. `webhookAllowedReason = "bypass"` for consistency with other webhook files.
- [ ] **DRY**: SubnetCreateHook and SubnetUpdateHook both list subnets and call `util.ValidateCidrConflict(o, subnetList.Items)`. Extract a helper e.g. `validateSubnetCidrNoConflict(ctx context.Context, cache ClientReader, subnet *ovnv1.Subnet) error` to avoid duplication and centralize cache.List + validation.
- [ ] **Consistency**: SubnetCreateHook and SubnetUpdateHook use `ctrlwebhook.Errored` for decode/cache errors but `admission.Errored(http.StatusConflict, err)` for ValidateCidrConflict. Use a single package (e.g. ctrlwebhook) for all admission responses for consistency.
- [ ] **Readability**: In SubnetCreateHook, the VPC loop combines name-collision check and namespace-in-VPC validation. Extract namespace validation into e.g. `validateSubnetNamespacesInVpc(subnet *ovnv1.Subnet, vpc *ovnv1.Vpc) error` to clarify intent and simplify the loop.
- [ ] **Maintainability**: Error message strings ("vpc and subnet cannot have the same name", "namespace '%s' is out of range to custom vpc '%s'") could be named constants or vars for reuse and i18n.

---

## ./pkg/webhook/vip.go

- [ ] **Readability**: Fix success message typo "by pass" → "bypass" (lines 30, 54). Use package-level constant for consistency with other webhook files.
- [ ] **DRY**: ValidateVip V4 and V6 blocks share the same pattern (parse IP, check CIDR; V6 adds uppercase check). Extract a helper e.g. `validateVipIPInSubnet(ipStr, subnetName string, cidrBlock string, isV6 bool) error` to reduce duplication; aligns with suggestions in ip.go and ovn_nat_gateway.go.
- [ ] **Readability**: Simplify `err := fmt.Errorf(...); return err` to `return fmt.Errorf(...)` in ValidateVip (lines 70-71, 75-77, 84-85, 88-89, 93-95).
- [ ] **Maintainability**: Fix error message grammar "not support change" → "does not support change" or "changes are not supported" (line 50).

---

## ./pkg/webhook/vpc.go

- [ ] **Readability**: Fix success message typo "by pass" → "bypass" (lines 37, 50, 61). Use package-level constant for consistency.
- [ ] **DRY**: Error message "vpc and subnet cannot have the same name" is duplicated in subnet.go (VPC vs subnet name collision). Extract to a shared constant in the webhook package (e.g. `errVpcSubnetSameName`) for consistency and i18n.
- [ ] **Readability**: In VpcCreateHook, simplify `err := errors.New(...); return ctrlwebhook.Errored(..., err)` to `return ctrlwebhook.Errored(..., errors.New(...))` (lines 28-29).
- [ ] **Structure / Performance**: In VpcCreateHook, consider calling `util.ValidateVpc(&vpc)` before listing subnets so invalid VPC spec fails fast without a cache.List.

---

## ./pkg/webhook/vpc_nat_gateway.go

- [ ] **Readability**: Fix success message typo "by pass" → "bypass" in all hooks (lines 48, 61, 83, 114, 160, 181, 214, 234, 267, 287, 317). Use package-level constant for consistency.
- [ ] **Maintainability**: Fix error message grammar "not support change" → "does not support change" or "changes are not supported" (lines 99, 197, 250, 303). Add space after comma in "in use,you need" (line 155).
- [ ] **DRY**: Create hooks (VpcNatGwCreateOrUpdateHook, iptablesEIPCreateHook, iptablesDnatCreateHook, iptablesSnatCreateHook, iptablesFipCreateHook) share the same pattern: decode → ValidateVpcNatConfig → ValidateVpcNatGatewayConfig → resource-specific Validate → Allowed. Update hooks (iptablesEIP/Dnat/Snat/FipUpdateHook) share: decode new/old → if spec changed and old ready then error else if spec changed then same three validations + resource Validate → Allowed. Extract helpers e.g. `validateVpcNatContext(ctx) error` and a small closure or table for "create vs update" flow to reduce duplication.
- [ ] **DRY**: ValidateIptablesEIP V4/V6 validation duplicates the pattern in vip.go and ip.go (parse IP, check CIDR, V6 uppercase). Share a validation helper in the webhook package (e.g. validateIPInSubnet).
- [ ] **DRY**: ValidateIptablesDnat: ExternalPort and InternalPort validation duplicate Atoi + range 0–65535. Extract e.g. `validatePort(portStr, fieldName string) error` to reduce duplication.
- [ ] **Correctness**: ValidateIptablesDnat line 468: error message says "internalIP" but the field being parsed is InternalPort; use "internalPort" in the message. Line 471: "internalIP" in "internalIP ... is not a valid port" should be "internalPort".
- [ ] **Readability**: Simplify `err := fmt.Errorf(...); return err` to `return fmt.Errorf(...)` in ValidateVpcNatGW, ValidateIptablesEIP, ValidateIptablesDnat, ValidateIptablesFip (multiple places). Same for `err := errors.New(...); return err` in ValidateIptablesFip (line 506-507).
- [ ] **Readability**: ValidateIptablesDnat line 481: "invalid iptable protocol" → "invalid iptables protocol"; add space after comma in supported params.

---

## ./pkg/webhook/vpc_nat.go

- [ ] **Maintainability**: Replace magic string `"image"` with a named constant. The same key is used in `pkg/controller/init.go`, `pkg/controller/vpc_nat.go`, and e2e tests; consider adding e.g. `VpcNatConfigKeyImage` in `pkg/util/const.go` and using it here and in controller code to avoid typos and document intent.
- [ ] **Readability**: In the empty-image branch, simplify `err := fmt.Errorf(...); return err` to `return fmt.Errorf(...)`.

---

## ./pkg/webhook/webhook.go

- [ ] **Readability / Consistency**: Fix typo "by pass" → "bypass" (line 119) and use a package-level constant for the allowed reason string (e.g. `webhookAllowedReason = "bypass"`) to align with suggestions in static_ip.go, subnet.go, vip.go, vpc.go, vpc_nat_gateway.go.
- [ ] **Structure / DRY**: The switch in Handle() has three symmetric branches (nil check, log, call hook, return). Extract a helper e.g. `dispatch(ctx, req, hooks map[schema.GroupVersionKind]admission.HandlerFunc, op string) (admission.Response, bool)` returning (resp, found) to reduce duplication.
- [ ] **Maintainability / Testability**: Package-level createHooks/updateHooks/deleteHooks are mutated in NewValidatingHook; creating two ValidatingHook instances overwrites the first's handlers. Consider attaching hook maps to ValidatingHook (e.g. v.createHooks) so each instance owns its registry, or document that NewValidatingHook must be called once per process.
- [ ] **Readability**: When no hook is found for the GVK, consider logging at V(4) or V(5) that the request was allowed because no hook was registered, to aid debugging.

---

## ./test/anp/anp_test.go

- [ ] **Naming / Readability**: Variable `client` (line 65) shadows the imported package `client` (sigs.k8s.io/controller-runtime/pkg/client). Use a different name e.g. `ctrlClient` or `runtimeClient` to avoid shadowing and improve clarity.
- [ ] **Correctness**: If "sigs.k8s.io/network-policy-api" is not found in go.mod, `version` remains empty and `manifestsURL` is built with an empty gitRef. After the loop, add a check e.g. `if version == "" { t.Fatalf("sigs.k8s.io/network-policy-api not found in go.mod") }`.
- [ ] **Readability**: Replace magic value `300 * time.Second` (GetTimeout) with a named constant (e.g. at package level) for documentation and tuning.
- [ ] **Maintainability**: Report path `"../../"+anpReportFileName` depends on test working directory; if tests run from a different cwd the file may be written to an unexpected location. Consider a test output dir constant or environment variable.
- [ ] **DRY**: The flow (read go.mod, parse version, build URL, get kube config, create controller-runtime client and clientset, install scheme, create suite, run, report, write file) is largely duplicated in test/cnp/cnp_test.go. Extract shared helpers e.g. `getNetworkPolicyAPIVersion(t)`, `buildConformanceClients(t)`, and optionally a shared conformance runner to reduce duplication between anp and cnp tests.

---

## ./test/cnp/cnp_test.go

- [ ] **Structure / Readability**: `TestClusterNetworkPolicyConformance` is a long function (120 lines) that does multiple things: reads go.mod, parses version, configures clients, runs test suite, generates report. Extract helper functions e.g. `getNetworkPolicyAPIVersion(t)`, `buildConformanceClients(t)`, `runConformanceSuite(t, cfg, client, clientset, manifestsURL)`, `writeReport(t, report)` to improve readability and testability.
- [ ] **Correctness**: If "sigs.k8s.io/network-policy-api" is not found in go.mod, `version` remains empty (line 42) and `manifestsURL` is built with an empty gitRef (line 58). After the loop (line 49), add a check e.g. `if version == "" { t.Fatalf("sigs.k8s.io/network-policy-api not found in go.mod") }`.
- [ ] **Readability**: Variable `r` in the loop (line 43) is not descriptive; use `req` or `require` to clarify it's a module requirement.
- [ ] **Readability**: Replace magic value `300 * time.Second` (line 96, GetTimeout) with a named constant (e.g. `const defaultGetTimeout = 300 * time.Second`) at package level for documentation and tuning.
- [ ] **Maintainability**: Report path `"../../"+cnpReportFileName` (line 116) depends on test working directory; if tests run from a different cwd the file may be written to an unexpected location. Consider a test output dir constant or environment variable, or use `filepath.Join` for path construction.
- [ ] **Maintainability**: File permission `0o600` (line 116) is a magic number; use a named constant e.g. `const reportFilePerm = 0o600` for clarity.
- [ ] **DRY**: The flow (read go.mod, parse version, build URL, get kube config, create controller-runtime client and clientset, install scheme, create suite, run, report, write file) is largely duplicated in test/anp/anp_test.go. Extract shared helpers e.g. `getNetworkPolicyAPIVersion(t)`, `buildConformanceClients(t)`, and optionally a shared conformance runner to reduce duplication between anp and cnp tests.
- [ ] **Extensibility**: Test configuration (profiles, timeout, debug, cleanup) is hardcoded in the test function. Consider making it configurable via test flags or environment variables for different test scenarios.

---

## ./test/e2e/anp-domain/e2e_test.go

- [ ] **DRY / Structure**: Multiple test cases repeat the same pattern: create namespace with labels, create pod with "sleep infinity", create ANP with namespace selector. Extract helper functions e.g. `setupTestNamespaceAndPod(t, f, namespaceName, podName)`, `createANPWithDomainName(t, anpClient, anpName, priority, domainNames, action)` to reduce duplication.
- [ ] **DRY**: ANP structure verification code (checking Egress length, To length, DomainNames length, Priority) is repeated in multiple tests. Extract a helper e.g. `verifyANPStructure(t, anp, expectedEgressCount, expectedPriority, expectedDomainNames)`.
- [ ] **Readability**: Replace magic numbers with named constants: `20` (maxRetries in testNetworkConnectivity), `2*time.Second` (retryInterval), `5` (curl connect-timeout and max-time), priority values (55, 44, 45, 50, 80, 85).
- [ ] **Maintainability**: Hardcoded test URLs/domains (`"https://www.baidu.com"`, `"https://www.google.com"`, `"https://8.8.8.8"`) should be configurable constants or test parameters for easier maintenance and to avoid external dependencies in tests.
- [ ] **Structure / Extensibility**: `testNetworkConnectivityWithRetry` and `testNetworkConnectivity` are closure functions defined inside the test suite. Consider moving them to the framework package or making them package-level helpers for reuse across test files.
- [ ] **Readability**: In `testNetworkConnectivityWithRetry`, the loop `for i := range maxRetries` (line 81) uses the index, but the variable name `i` suggests iteration count. Use `for attempt := 0; attempt < maxRetries; attempt++` for clarity, or rename to `attemptNum`.
- [ ] **Maintainability**: Variable `anpName2` is declared at suite level (line 30) but only used in the second test case. Consider declaring it locally in the test that uses it, or document why it needs to be at suite level (e.g., for cleanup in AfterEach).
- [ ] **Error handling**: In AfterEach (lines 47-73), resource deletion failures are only logged but don't fail the test. Consider accumulating errors and failing if cleanup fails, or at least make it configurable.
- [ ] **DRY**: The namespace selector creation pattern `&metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: namespaceName}}` is repeated in every test. Extract to a helper e.g. `makeNamespaceSelector(namespaceName)`.
- [ ] **Readability**: Port creation `[]netpolv1alpha1.AdminNetworkPolicyPort{framework.MakeAdminNetworkPolicyPort(443, corev1.ProtocolTCP)}` is repeated. Extract to a constant or helper e.g. `httpsPorts()`.
- [ ] **Structure**: The test "should create ANP with domainName and CIDR rules" (line 322) manually constructs `egressRule2` instead of using framework helpers like other rules. Consider adding a framework helper for CIDR-based rules or document why manual construction is needed.

---

## ./test/e2e/cnp-domain/e2e_test.go

- [ ] **DRY / Structure**: This file is nearly identical to `test/e2e/anp-domain/e2e_test.go`, with only API differences (CNP vs ANP). Extract shared test helpers and utilities to a common package (e.g. `test/e2e/framework/domain_policy_test.go`) to eliminate duplication and ensure consistency.
- [ ] **DRY**: Multiple test cases repeat the same pattern: create namespace with labels, create pod with "sleep infinity", create CNP with namespace selector. Extract helper functions similar to anp-domain suggestions.
- [ ] **Naming / Consistency**: Variable naming is inconsistent: `createdcnp1` (line 203), `createdCNP2` (line 212), `updatedcnp2` (line 300), `updatedcnp3` (line 308), `createdcnp` (line 369), `createdcnp` (line 417). Use consistent camelCase naming (e.g. `createdCNP1`, `updatedCNP2`).
- [ ] **Error handling**: Delete operations (lines 53, 58) ignore errors with `_`, unlike anp-domain which at least logs errors. Consider consistent error handling: either log errors or handle them properly.
- [ ] **Consistency**: Uses hardcoded string `"kubernetes.io/metadata.name"` (lines 121, 136, etc.) instead of `corev1.LabelMetadataName` constant used in anp-domain. Use the constant for consistency and to avoid typos.
- [ ] **Readability**: Same magic numbers as anp-domain: `20` (maxRetries), `2*time.Second` (retryInterval), `5` (curl timeouts), priority values (55, 44, 45, 50, 80, 85). Extract to constants.
- [ ] **Maintainability**: Hardcoded test URLs/domains same as anp-domain. Make configurable.
- [ ] **Structure / Extensibility**: `testNetworkConnectivityWithRetry` and `testNetworkConnectivity` are duplicated from anp-domain. Move to framework package for reuse.
- [ ] **Readability**: Same loop issue as anp-domain: `for i := range maxRetries` uses index, rename or use explicit loop.
- [ ] **DRY**: CNP structure verification code is repeated. Extract helper similar to anp-domain suggestions.
- [ ] **DRY**: Namespace selector and port creation patterns are repeated. Extract helpers.
- [ ] **Error handling**: Create operations (lines 147, 203, 212, 277, 288, 299, 307, 315, 369, 417) ignore the error return value with `_`. At minimum, check for errors and fail the test if creation/update fails.

---

## ./test/e2e/connectivity/e2e_test.go

- [ ] **Correctness / Naming**: Fix typo in ginkgo.By message (line 96): "Creating deplpyment" → "Creating deployment".
- [ ] **DRY**: The pattern of getting DaemonSet ovs-ovn, GetPods, then finding the pod on suiteCtx.Node is repeated in "Recreating ovs-ovn pod", "Stop ovn-controller process", and "Stop ovs-vswitchd process". Extract a helper e.g. `getOvsOvnPodOnNode(cs clientset.Interface, nodeName string) (*corev1.Pod, error)` to reduce duplication.
- [ ] **DRY**: The pattern "get deployment ovn-central" + Get + GetPods is repeated in "Recreating ovn-central pod" and "Stop ovn sb process". Consider a helper e.g. `getOvnCentralPods(cs clientset.Interface) ([]corev1.Pod, error)`.
- [ ] **DRY**: STOP signal → wait 60s → CONT signal is repeated in "Stop ovn sb process", "Stop ovn-controller process", and "Stop ovs-vswitchd process". Extract a helper e.g. `stopAndResumeProcessInPod(pod *corev1.Pod, getPidCmd []string, waitDuration time.Duration)` to centralize pid read, kill -STOP, sleep, kill -CONT and error handling.
- [ ] **Readability**: Replace magic numbers with named constants: `3 * time.Second` (BeforeAll/AfterAll) and `60 * time.Second` (disaster test waits), e.g. `disasterSuiteWait` and `processStopWaitDuration`.
- [ ] **Naming**: Fix log message (line 258): "new created ovs-ovs pod" → "newly created ovs-ovn pod" (typo ovs-ovs and grammar).
- [ ] **Readability**: In the disaster describe block, `var err error` at top level is shadowed in multiple places. Prefer local `err :=` in each block to avoid confusion.
- [ ] **DRY**: In "Stop ovn sb process", the loop that gets pid (cat /run/ovn/ovnsb_db.pid) and sends a signal is duplicated for STOP and CONT. Extract e.g. `getPidFromPod(pod *corev1.Pod, cmd []string) (string, error)` and `sendSignalInPod(pod *corev1.Pod, pid string, sig string) error` for reuse.

---

## ./test/e2e/framework/framework.go

- [ ] **Correctness**: In `NewFrameworkWithContext`, `ginkgo.BeforeEach(f.BeforeEach)` is registered twice (lines 152 and 154), so BeforeEach runs twice per test. Remove the duplicate registration.
- [ ] **DRY / Structure**: In `BeforeEach`, the pattern of calling `framework.LoadConfig()`, setting QPS/Burst, then creating a client is repeated six times (KubeOVN, KubeVirt, Ext, AttachNet, Metallb, Anp). Extract a helper (e.g. `withRestConfig(fn func(*rest.Config) error) error` or cache config once and create all clients from it) to reduce duplication.
- [ ] **Performance**: `framework.LoadConfig()` is invoked up to six times per test in BeforeEach. Load config once, cache on the Framework or in a local variable, and reuse for all client creation to avoid repeated kubeconfig loading.
- [ ] **Readability**: Comment typo "// .e.g. Image" and "// .e.g." (lines 295, 302) should be "// e.g." (no leading dot).
- [ ] **Naming / Maintainability**: `parseEnv()` can call `ginkgo.Fail` on parse error; consider renaming to `parseEnvOrFail` or documenting that it may abort the suite, and/or return error for consistency with other init helpers.
- [ ] **Extensibility**: Adding a new client (e.g. another CRD clientset) requires a new field on Framework and another if-block in BeforeEach. Consider a small registry or slice of client initializers so new clients can be plugged in without editing BeforeEach each time.

---

## ./test/e2e/framework/http/http.go

- [ ] **DRY**: In `runCaseOnce`, the three failure `Report` constructions share the same shape (Timestamp, Success, StartTime, Elapsed, Attachments). Extract a helper e.g. `failureReport(startTime time.Time, attachment string) *Report` to reduce duplication.
- [ ] **Readability**: `Loop` takes `interval` and `requestTimeout` as `int` (milliseconds). Consider `time.Duration` for clarity, or add a short comment that units are milliseconds.

---

## ./test/e2e/framework/admin-network-policy.go

- [ ] **Naming**: In MakeClusterNetworkPolicy (line 39), the variable is named `anp` but holds a ClusterNetworkPolicy. Rename to `cnp` for clarity.
- [ ] **Readability**: In waitForDNSNameResolvers, replace magic numbers with named constants: poll interval (1*time.Second) and timeout (30*time.Second), e.g. `dnsResolverPollInterval` and `dnsResolverWaitTimeout`.
- [ ] **DRY**: MakeAdminNetworkPolicy and MakeClusterNetworkPolicy share the same structure (name, priority, namespaceSelector, egress, ingress); Make*EgressRule and Make*Port are parallel across v1alpha1/v1alpha2. Consider a short file-level comment documenting the intentional parallel APIs, or extract shared builder helpers if the API allows.
- [ ] **Structure**: File mixes ANP (v1alpha1) and CNP (v1alpha2) helpers. Consider splitting into admin_network_policy.go and cluster_network_policy.go for single-responsibility, or add a file-level comment explaining co-location.

---

## ./test/e2e/framework/cni.go

- [ ] **DRY**: MakeMacvlanNetworkAttachmentDefinition and MakeOVNNetworkAttachmentDefinition both marshal a config struct with json.MarshalIndent and call MakeNetworkAttachmentDefinition(name, namespace, string(buf)). Extract a helper e.g. `makeNADFromConfig(name, namespace string, config interface{}) *nadv1.NetworkAttachmentDefinition` to centralize marshal and ExpectNoError.
- [ ] **Maintainability**: The socket path "/run/openvswitch/kube-ovn-daemon.sock" is duplicated in both Make* functions. Define a package constant (e.g. `defaultCniDaemonSocket`) for single source of truth.

---

## ./test/e2e/framework/daemonset.go

- [ ] **Correctness**: In Patch (lines 98-101), when json.Marshal fails the code calls Failf but does not return; execution then reaches ExpectNoError(err). Either return (or panic) after Failf, or remove the explicit if block and use only ExpectNoError(err) for marshal errors.
- [ ] **Naming / Readability**: In RolloutStatus (line 136), the variable `unstructured` shadows the imported package name `unstructured`. Rename to e.g. `dsUnstructured` or `unstructuredDS` to avoid confusion.
- [ ] **Readability**: Replace magic poll interval `2*time.Second` in Patch and RolloutStatus with a named constant (e.g. `daemonSetPollInterval`) for consistency with other framework code.

---

## ./test/e2e/framework/deployment.go

- [ ] **Readability / Naming**: In `RolloutStatus`, the variable `unstructured` shadows the imported package `unstructured`. Rename to e.g. `unst` or `unstructuredObj` to avoid shadowing and improve clarity.
- [ ] **Readability**: Magic numbers `2*time.Second` (poll interval) and `2*time.Minute` (WaitToComplete timeout) appear in multiple places. Consider named constants (e.g. `deploymentPollInterval`, `deploymentCompleteTimeout`) for consistency and tuning.
- [ ] **Consistency**: `SetScale` uses `framework.ExpectNoError` while the rest of the file uses local `ExpectNoError`. Use the same helper for consistency.
- [ ] **Dead code**: In `Patch`, the final `return nil` (line 148) is unreachable because both branches call `Failf` which exits. Remove for clarity.
- [ ] **Readability**: In `Restart`, the variable `deploy` is reused for the result of `FromUnstructured`, overwriting the input parameter. Use a distinct variable name (e.g. `updated`) for the converted deployment to avoid confusion.
- [ ] **Maintainability**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts a second parameter (likely poll interval) but ignores it with `_`. Either use it in the implementation for consistency with `WaitToComplete` (which uses explicit 2*time.Second) or document why it is ignored.

---

## ./test/e2e/framework/dns-name-resolver.go

- [ ] **Readability**: In `Create`, the parameter `dnsNameResolver` is shadowed by the return value assignment. Use a distinct variable name for the result (e.g. `created`) to avoid shadowing and clarify intent.
- [ ] **Readability**: Replace magic number `2*time.Second` in `WaitToBeReady` with a named constant (e.g. `dnsResolverPollInterval`) for consistency with other framework clients.
- [ ] **Consistency**: In `Delete`, the code calls `ExpectNoError(err)` so a not-found error fails the test. Consider treating `apierrors.IsNotFound(err)` as success (like `deployment.go` Delete) so tests can call Delete idempotently.

---

## ./test/e2e/framework/docker/container.go

- [ ] **DRY / Performance**: Each function creates a new Docker client with `client.New(client.FromEnv)` and defers `cli.Close()`. For tests that perform multiple operations, this opens and closes the connection repeatedly. Consider accepting a `*client.Client` as an optional parameter or providing a shared client getter so callers can reuse one connection.
- [ ] **Error handling / Resource leak**: In `ContainerCreate`, if `ContainerStart` fails after `ContainerCreate` succeeds, the container is left in "created" state. Consider calling `ContainerRemove` on start failure to avoid leaking containers, or document that callers must clean up.

---

## ./test/e2e/framework/docker/exec.go

- [ ] **DRY / Performance**: Same as `docker/container.go` — creates a new Docker client per call. Consider accepting an optional `*client.Client` so callers can reuse one connection when running multiple execs.

---

## ./test/e2e/framework/docker/network.go

- [ ] **DRY / Performance**: Each function creates a new Docker client. Consider accepting an optional `*client.Client` or providing a shared client for reuse, consistent with `container.go` and `exec.go`.
- [ ] **Readability / Naming**: In `NetworkCreate`, the variable `network` shadows the imported package `network`. Rename to e.g. `netInfo` or `insp` to avoid shadowing.
- [ ] **Error handling**: In `generateULASubnetFromName`, `h.Write` and `binary.Write` errors are discarded. Consider checking and returning errors for robustness.
- [ ] **Readability**: Magic numbers `64` and `128` in `net.CIDRMask(64, 128)` could be named constants (e.g. `ipv6PrefixLen`, `ipv6Bits`) for documentation.

---

## ./test/e2e/framework/endpoints.go

- [ ] **Naming**: Method `EndpointClient()` (singular) returns `*EndpointsClient` (plural); align naming (e.g. rename to `EndpointsClient()` or document the singular as "client for Endpoints resource").
- [ ] **Readability**: `WaitUntil` and `WaitToDisappear` take an unused `_` parameter (poll interval / duration); the implementation hardcodes `2*time.Second`. Either use the parameter or remove it from the signature to avoid confusion.
- [ ] **DRY / Readability**: Magic number `2*time.Second` appears in CreateSync, Patch, PatchSync, WaitUntil, DeleteSync, WaitToDisappear. Extract a package-level constant (e.g. `defaultEndpointsPollInterval`) for consistency and tuning.
- [ ] **Maintainability**: In `WaitUntil` and `Patch`, the two `Failf` branches (timeout vs other error) repeat the same pattern; consider a small helper e.g. `failWaitError(resource, name string, err error)` to reduce duplication.
- [ ] **Readability**: Comment "deletes a endpoints" in `Delete` has grammar error; use "deletes the endpoints" or "deletes an Endpoints resource".
- [ ] **Naming**: In `MakeEndpoints`, parameter `subset` is a slice (`[]corev1.EndpointSubset`); consider renaming to `subsets` to match the struct field name and plural type.

---

## ./test/e2e/framework/event.go

- [ ] **Correctness**: In `WaitToHaveEvent`, `result` is appended to across poll iterations but never reset, so the same events can be appended multiple times when List returns them on successive polls. Reset `result` at the start of each poll iteration (e.g. `result = nil` or `result = result[:0]`) so the returned slice contains no duplicates.
- [ ] **Readability / Extensibility**: `WaitToHaveEvent` has six string parameters; consider an options struct (e.g. `EventWaitOptions` with Kind, Name, EventType, Reason, SourceComponent, SourceHost) to simplify call sites and make adding filters easier.
- [ ] **Readability**: Extract the source-filter condition (component and host) into a small helper e.g. `eventMatchesSource(event *corev1.Event, component, host string) bool` to clarify intent.

---

## ./test/e2e/framework/exec_utils.go

- [ ] **Extensibility**: `execCommandInPod` accepts `ctx context.Context` but only uses it for the pod Get; the actual exec via `ExecCommandInContainer` is not cancellable. If `pkg/util.ExecuteCommandInContainer` gains context support, pass ctx for consistent cancellation.
- [ ] **Readability**: In `execCommandInPod`, the first container (`p.Spec.Containers[0]`) is used without documenting that InitContainers are ignored; consider a short comment or a helper that picks "default" container for clarity.

---

## ./test/e2e/framework/expect.go

- [ ] **Correctness**: In `buildDescription`, when `len(explain)==1` and the value is not `func() string`, execution falls through to `fmt.Sprintf(explain[0].(string), ...)` which panics if `explain[0]` is not a string. Add an explicit case or safe type check to avoid panic.
- [ ] **Readability**: In `ExpectEqual` and `ExpectNotEqual`, the parameter name `extra` denotes the expected value; renaming to `expected` would clarify intent.
- [ ] **Maintainability**: In `ExpectNoErrorWithOffset`, the comment uses a fullwidth comma ("Instead，we take"); use ASCII comma for consistency.
- [ ] **Readability**: Fix typo in `ExpectIPInCIDR` comment: "in within" → "is within".
- [ ] **Readability**: In `buildExplainWithOffset`, magic numbers `3` and `2` (code location depth, caller skip) could get a brief comment explaining stack offset semantics.

---

## ./test/e2e/framework/ip.go

- [ ] **Naming**: Parameter/variable name `iP` (capital P) is unconventional; use `ip` for the resource in Create, CreateSync, Patch and Get return variable to align with idiomatic Go.
- [ ] **Readability**: In `Get`, the variable `IP` shadows the type name; use `ip` or `obj` for the local variable to avoid confusion.
- [ ] **Maintainability**: Fix comment grammar "Delete deletes a IP" → "Delete deletes an IP".
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "IP %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` has an unused middle parameter (interval); consider simplifying to `WaitToDisappear(name string, timeout time.Duration)` or document why the signature is shared with other clients.
- [ ] **Readability**: In `MakeIP`, fix comment grammar "pod ip name should including" → "pod ip name should include"; clarify "node ip name: only node name" for consistency.

---

## ./test/e2e/framework/ippool.go

- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Update/Patch return `.DeepCopy()`. Return `ippool.DeepCopy()` from Get so callers cannot mutate cached objects and API is consistent.
- [ ] **Maintainability**: Fix comment grammar "Delete deletes a ippool" → "Delete deletes an ippool".
- [ ] **Readability**: In `WaitConditionToBe` and `WaitToBeUpdated`, the loop logs on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Readability**: In `WaitUntil`, "Waiting for ippool %s to meet condition %q" is logged every poll; consider logging once or at intervals.
- [ ] **Correctness**: `WaitToBeUpdated` uses `big.Int` for ResourceVersion comparison; K8s ResourceVersion can be non-numeric. Document the assumption or use string comparison if RV format is guaranteed.
- [ ] **Readability**: In `isIPPoolConditionSetAsExpected`, simplify `(wantTrue && cond.Status == corev1.ConditionTrue) || (!wantTrue && cond.Status != corev1.ConditionTrue)` to `(cond.Status == corev1.ConditionTrue) == wantTrue`.
- [ ] **API design**: `PatchSync(original, modified)` has no timeout parameter and uses package-level `timeout`; consider adding an explicit timeout parameter for consistency with Update/UpdateSync.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` has an unused middle parameter; same as ip.go refactor.

---

## ./test/e2e/framework/iproute/iproute.go

- [ ] **DRY**: `NonLinkLocalAddresses()` and `NonLinkLocalIPs()` share the same iteration and filter over `AddrInfo`; extract a helper (e.g. `filterNonLinkLocalAddrs(l *Link) (ips []string, prefixLens []int)`) or have `NonLinkLocalAddresses()` build CIDRs from the result of `NonLinkLocalIPs()` plus prefix lengths to avoid duplication.
- [ ] **Readability / Maintainability**: In `AddressDelCheckExist`, magic numbers `10` (retries) and `time.Second` (interval) should be named package-level constants (e.g. `addressCheckMaxRetries`, `addressCheckRetryInterval`) for tuning and documentation.
- [ ] **Readability**: In `AddressDelCheckExist`, the nested loops with `found` and multiple breaks are hard to follow; extract a helper e.g. `addressExistsOnLinks(links []Link, addr string) bool` to simplify control flow and make the retry logic clearer.
- [ ] **Structure**: In `RouteShow`, `e.ignoredErrors = append(e.ignoredErrors, reflect.TypeFor[docker.ErrNonZeroExitCode]())` mutates the execer after creation; consider passing ignored error types as a parameter (e.g. `RouteShow(table, device string, execFunc ExecFunc, ignoreErrors []reflect.Type)`) or an options struct so execer remains immutable and intent is explicit.
- [ ] **DRY**: `RouteShow` and `RuleShow` both run IPv4 then IPv6 `ip -j` commands and append results; consider a small helper (e.g. `runIPAndIP6(execer, baseCmd string, result *[]Route)`) or document the pattern for consistency when adding new callers.
- [ ] **Consistency**: `LinkShowRaw` calls `execFunc` directly instead of using `execer`; using execer would give consistent error formatting (including stderr in error message) and align with other functions in the package.

---

## ./test/e2e/framework/kind/kind.go

- [ ] **Correctness**: In `ListRoutes`, when `nonLinkLocalUnicast` is true the "default" route is appended twice: once in `if route.Dst == "default"` and again in the next `if` (because `net.ParseIP("default")` returns nil and `!nil.IsLinkLocalUnicast()` is true). Use `else if` or skip the second condition when Dst is "default", and when `ip` is nil (e.g. non-parseable Dst) do not append to avoid relying on nil receiver behavior.
- [ ] **Readability**: In `ListRoutes`, parameter name `nonLinkLocalUnicast` is ambiguous; consider `excludeLinkLocalUnicast` or `onlyNonLinkLocalUnicast` to clarify that when true we filter to routes that are not link-local unicast.
- [ ] **Readability**: In `WaitLinkToDisappear`, the loop logs on every poll ("Waiting for link...", "link still exists"); consider logging only on final failure or at intervals to reduce log noise.
- [ ] **DRY / Readability**: In `ListClusters`, use the already-assigned `cluster` variable when appending: `clusters = append(clusters, cluster)` instead of `node.Labels[labelCluster]` to avoid double lookup and align with the short-if.
- [ ] **DRY**: `NetworkConnect` and `NetworkDisconnect` share the same structure (iterate nodes, call node method, return on first error); extract a helper e.g. `forEachNode(nodes []Node, fn func(Node) error) error` to reduce duplication.

---

## ./test/e2e/framework/kubectl.go

- [ ] **Readability / Maintainability**: Add godoc for `KubectlExec` (e.g. runs the given command in the pod namespace/name and returns stdout, stderr as bytes) and for unexported `ovnExecSvc` (e.g. runs the command in the ovn-nb or ovn-sb service pod).
- [ ] **DRY**: `KubectlExec` and `ovnExecSvc` share the same pattern (join cmd, RunHostCmdWithFullOutput, convert output to []byte, format error with stderr). Consider a small internal helper that accepts namespace, target, cmd and an error formatter to reduce duplication.

---

## ./test/e2e/framework/kube-ovn.go

- [ ] **Readability / Maintainability**: Add godoc for `GetKubeOvnImage` (e.g. returns the container image used by the kube-ovn DaemonSet).
- [ ] **Robustness**: The code assumes `ds.Spec.Template.Spec.Containers` has at least one element; otherwise `Containers[0]` panics. Consider checking `len(ds.Spec.Template.Spec.Containers) > 0` and failing with a clear message, or document the assumption.

---

## ./test/e2e/framework/log.go

- [ ] **Maintainability**: Fix comment typo in PrunedStack: "caller of PruneStack" → "caller of PrunedStack".
- [ ] **DRY**: Failf and Fail share the same pattern (log "FAIL" with PrunedStack, then ginkgo.Fail). Extract a helper e.g. `failWithStack(msg string, skip int)` to reduce duplication.
- [ ] **Readability**: Consider renaming the unexported `log` to e.g. `writeLog` or `logToGinkgo` to avoid confusion with the standard log package.
- [ ] **Robustness**: PrunedStack assumes stack has an even number of entries after trimming; the loop accesses `stack[i*2+1]`. If len(stack) is odd, the last iteration could panic. Add a guard or document the assumption that debug.Stack() returns paired lines.

---

## ./test/e2e/framework/metallb.go

- [ ] **Maintainability**: Extract hardcoded "metallb-system" to a package-level constant (e.g. MetallbNamespace) and use it in CreateIPAddressPool, CreateL2Advertisement, DeleteIPAddressPool, DeleteL2Advertisement, and ListServiceL2Statuses.
- [ ] **Readability / Maintainability**: Add godoc for MetallbClientSet, NewMetallbClientSet, and the public methods (CreateIPAddressPool, CreateL2Advertisement, MakeL2Advertisement, MakeIPAddressPool, DeleteIPAddressPool, DeleteL2Advertisement, ListServiceL2Statuses).
- [ ] **Extensibility**: All methods use context.TODO(); consider adding ctx context.Context as first parameter to Create/Delete/List methods for cancellation and timeout support.
- [ ] **Structure**: NewMetallbClientSet calls metallbv1beta1.AddToScheme(scheme.Scheme) on every invocation. Consider calling AddToScheme once in init() to avoid repeated scheme registration, or document that AddToScheme is idempotent.

---

## ./test/e2e/framework/namespace.go

- [ ] **Maintainability**: Fix comment double space "namespace  client" → "namespace client"; consider "a client for Namespace operations".
- [ ] **Consistency**: Get returns the object without .DeepCopy(); return np.DeepCopy() from Get so callers cannot mutate cached objects, consistent with Create and Patch.
- [ ] **Readability**: In WaitToDisappear, the variable is named `policy` but holds *corev1.Namespace; rename to `ns` or `namespace`.
- [ ] **DRY / Readability**: Magic number 2*time.Second in Patch and DeleteSync; extract a package-level constant (e.g. defaultNamespacePollInterval) for consistency with other framework clients.
- [ ] **API design**: WaitToDisappear(name string, _, timeout time.Duration) has an unused middle parameter; consider removing it or using it for poll interval (same as other framework clients).

---

## ./test/e2e/framework/iptables/iptables.go

- [ ] **Correctness**: When `chain` is non-empty, `cmd += chain` produces an invalid iptables command (no space between `-S` and chain name). Use `cmd += " " + chain` or `fmt.Sprintf(" %s", chain)` so the command is e.g. `iptables -t nat -S INPUT` not `iptables -t nat -SINPUT`.
- [ ] **Readability**: In the rule-check loop, use a more descriptive variable name than `r` (e.g. `rule`) for clarity.
- [ ] **Readability / Maintainability**: Magic durations `2*time.Second` and `time.Minute` in `CheckIptablesRulesOnNode` could be named package-level constants (e.g. `iptablesCheckInterval`, `iptablesCheckTimeout`) for consistency with other e2e framework code and easier tuning.
- [ ] **Structure**: Consider adding a one-line doc comment for `CheckIptablesRulesOnNode` and `getOvsPodOnNode` describing purpose, for consistency with other framework helpers.

---

## ./test/e2e/framework/iptables-dnat.go

- [ ] **Maintainability**: Fix comment grammar "Delete deletes a iptables dnat" → "Delete deletes an iptables DNAT rule" (or "the iptables dnat").
- [ ] **DRY**: Magic number `2*time.Second` in Patch (poll interval) and DeleteSync; extract a package-level constant (e.g. `defaultPollInterval`) for consistency with other framework clients.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "dnat %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(dnat.ResourceVersion, 10)` ignores the second return value; if ResourceVersion is non-numeric (K8s can use opaque strings), SetString returns false and rv stays 0. Document the numeric-RV assumption or use string comparison for ResourceVersion.
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout time.Duration)` has an unused `_ []string` parameter; remove or document purpose (e.g. for future field selector).
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `dnat.DeepCopy()` from Get so callers cannot mutate cached objects and API is consistent with other clients.
- [ ] **DRY**: The Patch retry pattern (wait.PollUntilContextTimeout, handleWaitingAPIError, then Failf on timeout vs other error) is repeated in other framework clients (e.g. iptables-eip, iptables-fip); consider a shared helper e.g. `patchResourceWithRetry(ctx, pollInterval, timeout, patchFn, resourceName string)` to reduce duplication.

---

## ./test/e2e/framework/iptables-eip.go

- [ ] **Maintainability**: Fix comment grammar "Delete deletes a iptables eip" → "Delete deletes an iptables EIP".
- [ ] **DRY**: Magic number `2*time.Second` in Patch and DeleteSync; extract package-level constant for consistency with iptables-dnat and other framework clients.
- [ ] **Readability**: In `WaitToBeReady` and `WaitToQoSReady`, the loop logs "not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(eip.ResourceVersion, 10)` ignores the second return value; document the numeric ResourceVersion assumption or use string comparison (same as iptables-dnat refactor).
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout time.Duration)` has an unused `_ []string` parameter; remove or document.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `eip.DeepCopy()` from Get for consistency.
- [ ] **Maintainability**: Comment on `PatchQoSPolicySync` says "patches the vpc nat gw" but the method is on IptablesEIPClient; fix to "patches the iptables EIP (QoS policy)".
- [ ] **DRY**: Same Patch retry pattern as iptables-dnat.go; consider shared helper (see iptables-dnat refactor).

---

## ./test/e2e/framework/iptables-fip.go

- [ ] **Maintainability**: Fix comment grammar "Delete deletes a iptables fip" → "Delete deletes an iptables FIP rule".
- [ ] **DRY**: Magic number `2*time.Second` in Patch and DeleteSync; extract package-level constant for consistency with iptables-dnat, iptables-eip and other framework clients.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "fip %s is not ready" on every poll; consider logging only on final failure or at intervals.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(fip.ResourceVersion, 10)` ignores the second return value; document the numeric ResourceVersion assumption or use string comparison (same as iptables-dnat refactor).
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout time.Duration)` has an unused `_ []string` parameter; remove or document.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `fip.DeepCopy()` from Get for consistency.
- [ ] **DRY**: Same Patch retry pattern as iptables-dnat.go and iptables-eip.go; consider shared helper (see iptables-dnat refactor).

---

## ./test/e2e/framework/iptables-snat.go

- [ ] **Maintainability**: Fix comment grammar "Delete deletes a iptables snat" → "Delete deletes an iptables SNAT rule".
- [ ] **DRY**: Magic number `2*time.Second` in Patch (poll interval) and DeleteSync; extract package-level constant (e.g. `defaultPollInterval`) for consistency with iptables-dnat, iptables-eip, iptables-fip.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "snat %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(snat.ResourceVersion, 10)` ignores the second return value; document the numeric ResourceVersion assumption or use string comparison (same as iptables-dnat refactor).
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout time.Duration)` has an unused `_ []string` parameter; remove or document.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch/CreateSync/PatchSync return `.DeepCopy()`; return `snat.DeepCopy()` from Get for consistency.
- [ ] **API design**: `WaitToDisappear(name string, _ time.Duration, timeout time.Duration)` accepts a second parameter (poll interval) but ignores it; either use it for Eventually polling or remove from signature to avoid confusion.
- [ ] **DRY**: Same Patch retry pattern as iptables-dnat, iptables-eip, iptables-fip; consider shared helper (see iptables-dnat refactor).

---

## ./test/e2e/framework/network-attachment-definition.go

- [ ] **Performance**: In `Create`, the code calls `c.Get(nad.Name)` after a successful Create, causing an extra API round trip. The Create response already returns the created object; return it directly instead of refetching.
- [ ] **Readability**: Include resource name in error messages (e.g. "Error creating nad %s", name; "Error deleting nad %s", name) for easier debugging when tests fail.

---

## ./test/e2e/framework/network-policy.go

- [ ] **Maintainability**: Fix double space in comment "network policy  client" → "network policy client".
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while `Create` returns `.DeepCopy()`; return `np.DeepCopy()` from Get for consistency and to avoid callers mutating the returned object.
- [ ] **Readability**: In `Create`, include resource name in error message (e.g. "Error creating network policy %s", netpol.Name) for easier debugging.
- [ ] **API design**: `WaitToDisappear(name string, _ time.Duration, timeout time.Duration)` accepts a second parameter (poll interval) but ignores it; either use it for Eventually polling or remove from signature.
- [ ] **DRY**: Magic number `2*time.Second` in DeleteSync; extract package-level constant (e.g. `defaultPollInterval`) for consistency with other framework clients.

---

## ./test/e2e/framework/ovn_address_set.go

- [ ] **Maintainability**: Remove unused constant `ovnClientMaxRetry` or use it when creating the OVN client if retry is configurable.
- [ ] **DRY**: The reflection loop that iterates address set rows, filters by ippool external ID, and extracts Name/Addresses is duplicated in `WaitForAddressSetIPs` and `WaitForAddressSetDeletion`. Extract a helper e.g. `findAddressSetsForIPPool(rows any, ippoolName string) (map[string][]string, error)` to reduce duplication and simplify both functions.
- [ ] **Structure**: `resolveOVNNbConnection` is long; extract `parseControllerEnv(deploy)` (enableSSL, dbIPs) and `buildOVNNbTargets(client, namespace, enableSSL, dbIPs)` to shorten the function and improve testability.
- [ ] **Readability**: Consider named constants for deployment/container name "kube-ovn-controller" and service name "ovn-nb" to avoid typos and document intent.

---

## ./test/e2e/framework/ovn-dnat.go

- [ ] **Maintainability**: Fix comment grammar "a ovn dnat" → "an OVN DNAT rule" in Delete and related comments.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `dnat.DeepCopy()` from Get for consistency.
- [ ] **Readability**: Include resource name in Create error message (e.g. "Error creating ovn dnat %s", dnat.Name).
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout time.Duration)` has an unused `_ []string` parameter; remove or document.
- [ ] **API design**: `WaitToDisappear(name string, _ time.Duration, timeout time.Duration)` accepts but ignores the second parameter; either use it for polling or remove from signature.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(dnat.ResourceVersion, 10)` ignores the parse error; document numeric ResourceVersion assumption or use string comparison.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "ovn dnat %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **DRY**: Magic number `2*time.Second` in Patch, DeleteSync; extract package-level constant for consistency with other framework clients.

---

## ./test/e2e/framework/ovn-eip.go

- [ ] **Readability**: In `Create`, the parameter `eip` is shadowed by the return value `eip, err := c.OvnEipInterface.Create(...)`; use a distinct name for the result (e.g. `created`) to avoid shadowing.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(eip.ResourceVersion, 10)` and `current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10)` ignore parse errors; invalid or empty ResourceVersion can make comparison wrong or panic. Validate parse result or use string comparison for ResourceVersion.
- [ ] **Readability**: `CreateSync` and `Patch` use package-level `timeout` without it being defined in this file; document or pass timeout explicitly for clarity.
- [ ] **DRY**: Magic number `2*time.Second` in Patch (poll interval) and DeleteSync; extract package-level constant for consistency with ovn-dnat.go and other framework clients.
- [ ] **Maintainability**: Fix comment grammar "Delete deletes a ovn eip" → "Delete deletes an OVN EIP" for consistency with other framework resource comments.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `eip.DeepCopy()` from Get so callers cannot mutate cached objects and API is consistent with ovn-dnat.go.
- [ ] **Structure**: The pattern (Get, Create, CreateSync, Patch, PatchSync, Delete, DeleteSync, WaitToBeReady, WaitToBeUpdated, WaitToDisappear) is nearly identical to other framework resource clients (e.g. ovn-dnat.go, ovn-fip.go). Consider a generic resource client base or shared wait helpers to reduce duplication.

---

## ./test/e2e/framework/ovn-fip.go

- [ ] **Readability**: In `Create`, the parameter `fip` is shadowed by the return value `fip, err := c.OvnFipInterface.Create(...)`; use a distinct name for the result (e.g. `created`) to avoid shadowing.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `fip.DeepCopy()` from Get so callers cannot mutate cached objects and API is consistent with ovn-eip.go and ovn-dnat.go.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(fip.ResourceVersion, 10)` and `current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10)` ignore parse errors; invalid or empty ResourceVersion can make comparison wrong or panic. Validate parse result or use string comparison.
- [ ] **API design**: `PatchSync(original, modified *apiv1.OvnFip, _ []string, timeout time.Duration)` has an unused `_ []string` parameter; remove or document purpose.
- [ ] **Maintainability**: Fix comment grammar "Delete deletes a ovn fip" → "Delete deletes an OVN FIP".
- [ ] **DRY**: Magic number `2*time.Second` in Patch; extract package-level constant for consistency with ovn-eip and ovn-dnat.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "ovn fip %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Structure**: Same CRUD + wait pattern as ovn-eip.go and ovn-dnat.go; consider shared wait helpers or generic resource client base to reduce duplication.

---

## ./test/e2e/framework/ovn-snat.go

- [ ] **Readability**: In `Create`, the parameter `snat` is shadowed by the return value `snat, err := c.OvnSnatRuleInterface.Create(...)`; use a distinct name for the result (e.g. `created`) to avoid shadowing.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `snat.DeepCopy()` from Get for consistency with other framework resource clients.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(snat.ResourceVersion, 10)` and `current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10)` ignore parse errors; invalid ResourceVersion can make comparison wrong or panic. Validate parse result or use string comparison.
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout)` has an unused `_ []string` parameter; remove or document. `WaitToDisappear(name string, _ time.Duration, timeout time.Duration)` accepts but ignores the second parameter; either use it for polling interval or remove from signature.
- [ ] **Maintainability**: Fix comment grammar "Delete deletes a ovn snat" → "Delete deletes an OVN SNAT rule".
- [ ] **DRY**: Magic number `2*time.Second` in Patch and DeleteSync; extract package-level constant for consistency with ovn-eip, ovn-fip, ovn-dnat.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "ovn snat %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Structure**: Same CRUD + wait pattern as ovn-eip, ovn-fip, ovn-dnat; consider shared wait helpers or generic resource client base to reduce duplication.

---

## ./test/e2e/framework/pod.go

- [ ] **Correctness**: In `CheckPodEgressRoutes`, if `output` is empty or contains no newlines, `lines := strings.Split(strings.TrimSpace(output), "\n")` may yield a single empty string or empty slice; `lines[len(lines)-1]` and `strings.Fields(...)` are then safe only when len(lines) > 0. Add a guard (e.g. `if len(lines) == 0 { return false, nil }`) to avoid indexing empty slice or parsing empty last line.
- [ ] **Maintainability**: In `Patch`, the final `return nil` after the second `Failf` is unreachable (Failf typically exits). Remove dead code for clarity.
- [ ] **Readability**: Magic numbers `2*time.Second`, `3*time.Second`, `30*time.Second` in `Patch` and `CheckPodEgressRoutes`; extract package-level constants (e.g. `podPatchPollInterval`, `egressCheckPollInterval`, `egressCheckTimeout`) for consistency with other framework files.
- [ ] **Structure / Readability**: In `CheckPodEgressRoutes`, `afs` and `dst` are built in parallel slices and coupled by index; consider a small struct (e.g. `type egressCheckTarget struct{ af int; dst string }`) or slice of `{af, dst}` to keep address family and destination in one place and avoid index drift.
- [ ] **Consistency**: Mix of `context.Background()` and `context.TODO()` across Create/CreateSync/Delete/DeleteSync vs GetPod/WaitForRunning/WaitForNotFound; standardize on one (e.g. Background for long-running, TODO only where legacy API requires) for consistency.
- [ ] **Extensibility**: `makePod` has many positional parameters; consider a `PodOptions` struct (Namespace, Name, Labels, Annotations, Image, Command, Args, SecurityLevel) for future options and readability.

---

## ./test/e2e/framework/provider-network.go

- [ ] **Naming**: In `Create`, the parameter `pn` is shadowed by the return value `pn, err := c.ProviderNetworkInterface.Create(...)`; use a distinct name for the result (e.g. `created`) to avoid shadowing.
- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `pn.DeepCopy()` from Get for consistency with other framework resource clients.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(pn.ResourceVersion, 10)` and `current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10)` ignore parse errors; invalid ResourceVersion can make comparison wrong or panic. Validate parse result or use string comparison.
- [ ] **API design**: `PatchSync(original, modified, _ []string, timeout)` has an unused `_ []string` parameter; remove or document. `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter; either use it for polling interval or remove from signature.
- [ ] **Maintainability**: In `Patch`, the final `return nil` after the second `Failf` is unreachable; remove dead code.
- [ ] **DRY**: Magic number `2*time.Second` in Patch and DeleteSync; extract package-level constant for consistency with other framework files.
- [ ] **Readability**: In `WaitToBeReady`, no log on timeout (unlike `WaitToBeUpdated`); add a final log e.g. "ProviderNetwork %s did not become ready within %v" for consistency and debugging.
- [ ] **Structure**: Same CRUD + wait pattern as ovn-eip, ovn-snat, subnet, etc.; consider shared wait helpers or generic resource client base to reduce duplication.

---

## ./test/e2e/framework/qos-policy.go

- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch/Update return `.DeepCopy()`; return `.DeepCopy()` from Get for consistency with other framework resource clients.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(...)` and `current, _ := big.NewInt(0).SetString(...)` ignore parse errors; invalid ResourceVersion can make comparison wrong or panic. Validate parse result or use string comparison.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter; either use it for polling interval or remove from signature.
- [ ] **Maintainability**: In `Update`, `Patch`, and `WaitUntil`, the final `return nil` after `Failf` is unreachable; remove dead code.
- [ ] **DRY**: Magic number `2*time.Second` in Update, Patch, DeleteSync; extract package-level constant for consistency with other framework files.
- [ ] **Readability**: In `WaitConditionToBe` and `WaitToBeUpdated`, the loop logs on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Correctness**: In `WaitToQoSReady`, the loop sorts `qos.Spec.BandwidthLimitRules` and `qos.Status.BandwidthLimitRules` in place; mutating the object returned from Get may affect caching or shared references. Sort copies (e.g. slices.Clone then sort) or compare without mutating.
- [ ] **Readability**: The "ready" check in `WaitToQoSReady` (equalCount == len(spec)) could be replaced with a single comparison of sorted copies (e.g. after sorting, use reflect.DeepEqual or a small helper) to simplify the loop.
- [ ] **Maintainability**: Fix comment grammar "Create creates a new qosPolicy" → "Create creates a new QoSPolicy" (and similar for Update, Patch, Delete).
- [ ] **Structure**: Same CRUD + wait pattern as other framework resource clients; consider shared wait helpers or generic resource client base to reduce duplication.

---

## ./test/e2e/framework/rpc/client.go

- [ ] **Performance/Documentation**: `Call(addr, method, args, reply)` creates a new TCP connection per invocation; document that for multiple calls to the same address, callers should use `NewClient` and reuse the client to avoid repeated connection setup.

---

## ./test/e2e/framework/rpc/server.go

- [ ] **Correctness/Race**: `NewServer` starts `ListenAndServe` in a goroutine; callers may use the server address before the server has bound. Consider exposing a readiness channel or sync point so tests can wait until the server is listening.
- [ ] **Maintainability**: `rpc.HandleHTTP()` registers on the default HTTP ServeMux; creating a second RPC server in the same process would overwrite handlers. Document this limitation or use a dedicated `http.ServeMux` per server if multi-server is ever needed.

---

## ./test/e2e/framework/security-group.go

- [ ] **Naming**: In `Create`, the parameter `sg` is shadowed by the return value `sg, err := c.SecurityGroupInterface.Create(...)`; use a distinct name for the result (e.g. `created`) to avoid shadowing.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter; either use it for polling interval or remove from signature.
- [ ] **Maintainability**: In `Patch`, the final `return nil` after the two `Failf` calls is unreachable; remove dead code.
- [ ] **DRY**: Magic number `2*time.Second` in Patch and DeleteSync; extract package-level constant for consistency with other framework files.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "security group %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Structure**: Same CRUD + wait pattern as other framework resource clients; consider shared wait helpers or generic resource client base to reduce duplication.

---

## ./test/e2e/framework/service.go

- [ ] **Consistency**: `Get` returns the object without `.DeepCopy()` while Create/Patch return `.DeepCopy()`; return `service.DeepCopy()` from Get for consistency with other framework resource clients and to avoid callers mutating cached objects.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter (interval); either use it for polling or remove from signature.
- [ ] **Maintainability**: In `Patch` and `WaitUntil`, the final `return nil` after `Failf` is unreachable; remove dead code.
- [ ] **DRY**: Magic number `2*time.Second` in CreateSync, Patch, PatchSync, DeleteSync; extract package-level constant for consistency with other framework files.
- [ ] **Readability**: In `WaitUntil`, the loop logs on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Extensibility**: `MakeService` has many positional parameters; consider a `ServiceOptions` struct for future options and readability.

---

## ./test/e2e/framework/statefulset.go

- [ ] **Naming / Readability**: In `RolloutStatus`, the local variable `unstructured` shadows the imported package `unstructured`; rename to e.g. `u` or `obj` to avoid confusion and improve readability.
- [ ] **Readability**: Replace magic `2*time.Second` (used in Patch, RolloutStatus, DeleteSync, WaitToDisappear) with a package-level constant (e.g. `defaultPollInterval`) for consistency with deployment.go, daemonset.go and other framework clients.
- [ ] **Readability**: In `WaitForRunningAndReady`, variable `n` represents the max ordinal; consider naming it `maxOrdinal` and/or adding a one-line comment for clarity.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter (poll interval); either use it for the Eventually poll interval or document/simplify the signature for consistency with other framework WaitToDisappear methods.
- [ ] **Maintainability**: In `Patch`, the final `return nil` after the second `Failf` is unreachable (Failf typically exits); remove dead code for clarity.

---

## ./test/e2e/framework/subnet.go

- [ ] **DRY**: `Update` and `Patch` share the same retry-and-fail pattern: `PollUntilContextTimeout` with `2*time.Second`, store result, if err==nil return DeepCopy(), else check DeadlineExceeded and Failf. Extract a helper e.g. `retrySubnetOp(timeout time.Duration, opDesc string, name string, fn func(ctx context.Context) (*apiv1.Subnet, error)) *apiv1.Subnet` to reduce duplication.
- [ ] **Correctness**: In `WaitToBeUpdated`, `rv, _ := big.NewInt(0).SetString(subnet.ResourceVersion, 10)` and `current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10)` ignore the ok return; if ResourceVersion is not numeric, behavior is undefined. Check ok and handle invalid ResourceVersion (e.g. treat as not updated or log).
- [ ] **Readability**: In `isSubnetConditionSetAsExpected`, the condition `(wantTrue && (cond.Status == corev1.ConditionTrue)) || (!wantTrue && (cond.Status != corev1.ConditionTrue))` can be simplified to `(cond.Status == corev1.ConditionTrue) == wantTrue`.
- [ ] **Consistency**: `WaitConditionToBe` and `WaitToBeUpdated` use a manual for-loop with `time.Sleep(poll)`, while `Update`, `Patch`, and `WaitUntil` use `wait.PollUntilContextTimeout`. Consider using `PollUntilContextTimeout` in WaitConditionToBe and WaitToBeUpdated for consistency and proper context support.
- [ ] **Maintainability**: In `Update`, `Patch`, and `WaitUntil`, the final `return nil` after `Failf` is unreachable; remove dead code.
- [ ] **Comment**: Delete comment says "deletes a subnet if the subnet exists"; more precise: "Delete deletes a subnet; it is a no-op if the subnet does not exist."

---

## ./test/e2e/framework/switch-lb-rule.go

- [ ] **Correctness**: In `WaitToDisappear`, the error message says "expected endpoints %s to not be found" but this client is for switch-lb-rule; fix to "expected switch-lb-rule %s to not be found".
- [ ] **Naming**: In `Patch`, variable `patchedService` holds *apiv1.SwitchLBRule; rename to `patchedRule` for consistency.
- [ ] **API design**: `WaitUntil(name, cond, condDesc, _, timeout)` accepts a fourth parameter that is ignored; implementation uses hardcoded `2*time.Second`. Either use the parameter as poll interval or remove from signature.
- [ ] **Correctness**: In `WaitUntil`, when err is DeadlineExceeded, Failf says "timed out while retrying to patch switch-lb-rule"; this is a generic wait, not patch. Use "timed out while waiting for switch-lb-rule %s to meet condition %q".
- [ ] **Maintainability**: In `Patch` and `WaitUntil`, the final `return nil` after `Failf` is unreachable; remove dead code.
- [ ] **Correctness**: In `Patch`, the poll callback uses `context.TODO()` instead of the ctx passed to the callback; use the callback's ctx for proper cancellation when timeout is reached.
- [ ] **Comment**: Delete comment says "deletes a switch-lb-rule if the switch-lb-rule exists"; more precise: "Delete deletes a switch-lb-rule; it is a no-op if it does not exist."

---

## ./test/e2e/framework/util.go

- [ ] **Correctness**: In `randomCIDR6`, line 76, the ExpectNotNil message says "failed to generate random ipv4 CIDR seed"; should be "failed to generate random ipv6 CIDR seed".
- [ ] **Robustness**: In `randomCIDR4` and `randomCIDR6`, when all seeds are exhausted (usedRandCIDRSeeds4.Len() == 256 or usedRandCIDRSeeds6.Len() == 65536), the inner loop never breaks and the code can spin forever. Consider failing the test or returning an error when seed space is exhausted.
- [ ] **Readability**: Magic numbers 0xff+1, 0xffff+1, 24, 32, 96, 128 in randomCIDR4/randomCIDR6; consider named constants (e.g. ipv4SeedMax, ipv4PrefixLen) for documentation and tuning.
- [ ] **Robustness**: In `randomPool`, the loop `for s.Len() != count/4` has no max iteration; if random subnets keep overlapping it may never reach the target. Consider a max attempt count or fail after N iterations.
- [ ] **Readability**: In `RandomExcludeIPs`, the logic that picks between range vs single IP (rangeLeft, rand.IntN(count-i) < rangeLeft) is subtle; add a one-line comment explaining the intended distribution.

---

## ./test/e2e/framework/vip.go

- [ ] **Performance**: In `WaitToBeReady`, line 65 calls `c.Get(name)` twice in the condition (`c.Get(name).Status.V4ip != "" || c.Get(name).Status.V6ip != ""`); cache the result in a variable to avoid duplicate API calls per poll.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter (poll interval); either use it for the Eventually poll interval or remove from signature for consistency with other framework clients.
- [ ] **Maintainability**: In `Patch`, the final `return nil` at line 101 after the two `Failf` calls is unreachable; remove dead code.
- [ ] **Consistency**: `CreateSync` uses package-level `timeout`; consider accepting an optional timeout parameter for consistency with `Patch` and `DeleteSync` which take timeout explicitly.
- [ ] **Readability**: In `WaitToBeReady`, the loop logs "ovn vip %s is not ready" on every poll; consider logging only on final failure or at intervals to reduce log noise.

---

## ./test/e2e/framework/vlan.go

- [ ] **Naming**: In `Create`, the parameter `pn *apiv1.Vlan` uses a name that suggests "provider network"; use `vlan` for clarity. In `Patch`, the inner variable `pn` (line 63) holds the patched vlan—rename to `vlan` or `patched` for consistency.
- [ ] **Readability**: Comment "Create creates a new vlan according to the framework specifications" is vague; simplify to "Create creates a new vlan."
- [ ] **Maintainability**: In `Patch`, the final `return nil` (line 80) after the two `Failf` calls is unreachable; remove dead code for clarity.
- [ ] **DRY**: The Patch pattern (GenerateMergePatchPayload, wait.PollUntilContextTimeout, Patch API, handleWaitingAPIError, timeout/DeadlineExceeded, DeepCopy) is duplicated across many framework clients (vlan, vip, subnet, provider-network, vpc-nat-gateway, etc.). Consider a generic helper in the framework package (e.g. `patchResourceWithRetry`) to reduce duplication and centralize timeout/error handling.

---

## ./test/e2e/framework/virtual-machine.go

- [ ] **Correctness**: Comment above `WaitToBeReady` (line 126) says "WaitToDisappear" and "to be ready"; fix to "WaitToBeReady waits the given timeout duration for the specified vm to be ready."
- [ ] **Correctness**: Comment above `WaitToBeStopped` (line 146) says "WaitToDisappear" and "to be stopped"; fix to "WaitToBeStopped waits the given timeout duration for the specified vm to be stopped."
- [ ] **Correctness**: Comment above `StartSync` (line 78) says "StartSync stops the vm"; fix to "StartSync starts the vm and waits for it to be ready."
- [ ] **Consistency**: `Get` returns the VM without `.DeepCopy()`; return `vm.DeepCopy()` for consistency with other framework clients and to avoid callers mutating cached objects.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter; either use it for poll interval or remove from signature.
- [ ] **DRY**: Magic numbers `2*time.Second` (DeleteSync), `2*time.Minute` (StartSync, StopSync); extract package-level constants for consistency with other framework files.

---

## ./test/e2e/framework/vpc.go

- [ ] **Correctness**: In `WaitToBeUpdated`, `big.Int.SetString(vpc.ResourceVersion, 10)` and `big.Int.SetString(s.ResourceVersion, 10)` ignore the boolean return; invalid or empty ResourceVersion can make the comparison unreliable. Check the ok return and handle invalid RV (e.g. treat as not updated or fail explicitly).
- [ ] **DRY / Consistency**: `WaitToBeReady` and `WaitToBeUpdated` use manual for+time.Sleep loops; consider using `wait.PollUntilContextTimeout` (as in `Patch`) for consistency and context cancellation support.
- [ ] **Readability**: `WaitToBeReady` treats `Status.Standby` as "ready"; add a short comment or helper (e.g. `isVpcReady(vpc)`) to document that standby implies ready for this controller.
- [ ] **Maintainability**: In `Patch`, the final `return nil` (line 87) is unreachable after the two `Failf` calls; remove dead code for clarity.
- [ ] **Consistency**: `DeleteSync` passes `2*time.Second` as poll interval to `WaitToDisappear`; consider using the framework's `poll` constant for consistency with `WaitToBeReady` / `WaitToBeUpdated`.

---

## ./test/e2e/framework/vpc-egress-gateway.go

- [ ] **Naming**: Fix typo `namespapce` → `namespace` in `NewVpcEgressGatewayClient(cs clientset.Interface, namespapce string)` and `VpcEgressGatewayClientNS(namespapce string)` (lines 31, 45); also fix the parameter name in both function signatures.
- [ ] **Readability**: Comments "Create creates a new vpc-egress-gateway according to the framework specifications" and "CreateSync creates a new vpc-egress-gateway according to the framework specifications, and waits for it to be ready." — simplify to "Create creates a new vpc-egress-gateway" and "CreateSync creates a new vpc-egress-gateway and waits for it to be ready."
- [ ] **Maintainability**: In `Patch`, the final `return nil` (line 105) after the two `Failf` calls is unreachable; remove dead code.
- [ ] **Consistency**: `Get` returns the gateway without `.DeepCopy()`; return `gateway.DeepCopy()` for consistency with other framework clients and to avoid callers mutating cached objects.
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter; either use it for poll interval or remove from signature for consistency with other framework clients.
- [ ] **Readability**: In `WaitUntil`, the loop logs on every poll ("Waiting for...", "not met condition"); consider logging only on final failure or at intervals to reduce log noise.
- [ ] **Naming**: In `WaitToDisappear`, variable `svc` holds a VpcEgressGateway, not a service; rename to `gateway` or `gw`.
- [ ] **DRY**: Magic numbers `2*time.Second` (CreateSync, PatchSync, DeleteSync); consider package-level constants. Same Patch pattern duplication as vlan.go (see framework DRY note).

---

## ./test/e2e/framework/vpc-nat-gateway.go

- [ ] **Correctness**: In `WaitToBeUpdated`, `big.Int.SetString(vpcNatGw.ResourceVersion, 10)` and `big.Int.SetString(s.ResourceVersion, 10)` ignore the boolean return; invalid or empty ResourceVersion can make the comparison unreliable. Check the ok return and handle invalid RV.
- [ ] **DRY / Consistency**: `WaitToBeReady`, `WaitToBeUpdated`, `WaitGwPodReady`, and `WaitToQoSReady` use manual for+time.Sleep loops; consider using `wait.PollUntilContextTimeout` for consistency and context cancellation support.
- [ ] **Maintainability**: In `Patch`, the final `return nil` (line 90) is unreachable after the two `Failf` calls; remove dead code for clarity.
- [ ] **Consistency**: `Get` returns the gateway without `.DeepCopy()`; return `.DeepCopy()` for consistency with other framework clients and to avoid callers mutating cached objects.
- [ ] **Readability**: Comments "Create creates a new vpc nat gw according to the framework specifications" and similar; simplify to "Create creates a new vpc nat gw" and "CreateSync creates a new vpc nat gw and waits for it to be ready."
- [ ] **API design**: `WaitToDisappear(name string, _, timeout time.Duration)` accepts but ignores the second parameter; either use it for poll interval or remove from signature for consistency with other framework clients.
- [ ] **Readability**: In `WaitGwPodReady` and `WaitToQoSReady`, the loops log on every poll; consider logging only on final failure or at intervals to reduce log noise.
- [ ] **DRY**: Magic numbers `4*time.Minute` (CreateSync), `2*time.Second` (Patch, DeleteSync); consider package-level constants. Same Patch pattern duplication as vlan.go (see framework DRY note).

---

## ./test/e2e/framework/wait.go

- [ ] **Correctness / API**: `WaitUntil(_, timeout time.Duration, ...)` ignores the first parameter (interval); callers pass different intervals (e.g. 2*time.Second, 1*time.Second, 5*time.Second, 0) but the implementation hardcodes 2*time.Second in PollUntilContextTimeout. Use the first parameter as the poll interval (with a default when 0, e.g. 2*time.Second) so caller intent is respected.
- [ ] **Maintainability / Debuggability**: When err != context.DeadlineExceeded, Failf does not include the actual error. Include err in the message, e.g. Failf("... : %v", condDesc, err), so failures are debuggable.
- [ ] **Extensibility**: WaitUntil uses context.Background(), so the wait cannot be cancelled from outside. Consider accepting context.Context as parameter so tests can cancel long waits (e.g. when test timeout is reached).

---

## ./test/e2e/ha/ha_test.go

- [ ] **Maintainability / Debuggability**: WaitUntil at line 218 is called with condDesc "". On timeout the failure message is "timed out while waiting for the condition to be met: " which is not helpful. Pass a non-empty condDesc (e.g. "ovn-central pod count to become replicas-1") so failure messages are debuggable.
- [ ] **Robustness**: In getDbSids line 183, `strings.TrimSpace(stdout)[:4]` can panic if stdout has fewer than 4 characters. Add a length check or use a safe slice (e.g. take first min(4, len(s)) characters).
- [ ] **Readability / Structure**: corruptAndRecover is long (~100 lines). Consider extracting steps into helpers (e.g. scaleDownAndGetRemovedNode, corruptDbOnNode, scaleUpAndVerifyHealth) so the main flow is easier to follow.
- [ ] **DRY**: getDbSids and getDbSidsFromClusterStatus share the same pattern: get pods, expect length, loop over nb/sb and pods. The difference is how server IDs are obtained. Consider extracting a helper that iterates over db and pods and collects results via a callback to reduce duplication.
- [ ] **Readability**: Magic numbers 30*time.Second (WaitUntil), 4 (server ID prefix length in getDbSids). Consider named constants for documentation and tuning.
- [ ] **Naming**: In getDbSidsFromClusterStatus and getDbSids, the loop variable `db` holds "nb" or "sb" (short form). Consider naming it dbKey or dbType for clarity when reading dbFilePath(db) and dbNames[db].

---

## ./test/e2e/ipsec/e2e_test.go

- [ ] **Correctness**: In both test cases, when iterating `for _, addr := range node.Status.Addresses` and `addr.Type == corev1.NodeInternalIP`, the code appends `node.Status.Addresses[0].Address`. It should append `addr.Address` so the correct InternalIP is used when the first address in the list is not InternalIP (e.g. Hostname).
- [ ] **DRY**: The block that builds `nodeIPs` from `nodeList` (get nodes, loop to collect InternalIP per node, ExpectHaveLen) is duplicated in both ConformanceIt blocks. Extract a helper e.g. `getNodeInternalIPs(nodeList *corev1.NodeList) []string` and reuse.
- [ ] **Readability**: Magic timeouts `time.Second*120` (xfrm check) and `time.Second*30` (CA sync) should be named constants (e.g. `xfrmCheckTimeout`, `caSyncTimeout`) for tuning and documentation.
- [ ] **Maintainability**: At line 341 the secret name is hardcoded as `"ovn-ipsec-ca"` while elsewhere `util.DefaultOVNIPSecCA` is used. Use the constant consistently to avoid drift.
- [ ] **Maintainability**: Comment typo at lines 289–290: "stroke up" / "stroke down" should be "spin up" / "spin down".
- [ ] **Structure**: Helper functions `splitCerts`, `getValueFromSecret`, `getPodCert`, `checkPodCACert`, `checkXfrmState` are mixed (some above, some below the test block). Consider grouping all helpers in one place (e.g. above the Describe block) for consistent readability.

---

## ./test/e2e/iptables-eip-qos/e2e_test.go

- [ ] **Naming / Package**: File lives in `test/e2e/iptables-eip-qos/` but package is `ovn_eip`; consider renaming package to match directory (e.g. `iptables_eip_qos`) for consistency unless shared with another suite.
- [ ] **Readability**: The const block `eipLimit = iota*5 + 10`, `updatedEIPLimit`, etc. (lines 48–54) produces 10, 15, 20, 25, 30; the expression is clever but hard to read. Consider explicit values or a short comment (e.g. "Mbps limits: 10, 15, 20, 25, 30").
- [ ] **DRY**: Magic timeouts are repeated: `2*time.Second`, `6*time.Second`, `2*time.Minute`, `60*time.Second`, `30*time.Second`, `500*time.Millisecond`. Extract named package-level constants (e.g. `eipReadyPollInterval`, `iperfRetrySleep`, `gwPodReadyTimeout`) for tuning and documentation.
- [ ] **Maintainability**: In `eipQoSCases`, log messages at lines 405 and 410 say "from natgw" but the code patches the EIP (`eipClient.PatchQoSPolicySync(eipName, ...)`). Use "from eip" in the message to match behavior.
- [ ] **Maintainability**: Comment at line 356 says "eipQoSCases test default qos policy"; should say "eip qos policy". Same at line 412 for `specifyingIPQoSCases`: "test default qos policy" → "specifying IP qos policy".
- [ ] **Structure**: Test case helpers (`defaultQoSCases`, `eipQoSCases`, etc.) each repeat "Derive clients from framework" (vpcNatGwClient, qosPolicyClient, ...). Consider a small helper that returns a struct of shared clients to reduce repetition.

---

## ./test/e2e/iptables-vpc-nat-gw/e2e_test.go

- [ ] **Naming / Shadowing**: In `setupNetworkAttachmentDefinition`, the local variable `nad` (line 86) shadows the imported package `nad` (network-attachment-definition client). Rename to e.g. `nadObj` or `netAttachDef` to avoid shadowing and improve readability.
- [ ] **Readability**: Replace magic timeouts and intervals with named constants: e.g. `eipReadyPollInterval` (2s), `nodeLinksEventuallyTimeout` (30s), `cleanupEIPWaitTimeout` (2m), `subnetStatusWait` (5s), `finalizerWaitIterations` (60), `deleteWaitIterations` (30). Improves tuning and documentation.
- [ ] **Structure / DRY**: `setupVpcNatGwTestEnvironment` is called with repeated overlay config (CIDR, Gw, LanIP) in each test. Consider an overlay config struct e.g. `overlayNetConfig{Cidr, Gw, LanIP string}` and pass it to reduce repetition and typo risk.
- [ ] **Structure / DRY**: Multiple tests use the same "poll in a loop then assert" pattern (e.g. wait for finalizer, wait for EIP/FIP deleted). Prefer `gomega.Eventually(...).Should(...)` or extract a helper e.g. `waitUntil(timeout, interval, cond func() bool, failMsg string)` for consistency and shorter tests.
- [ ] **Readability / Correctness**: In ConformanceIt "[2]", after creating `shareVip`, the code does `fipVip = vipClient.Get(fipVipName)`; the shared FIP/DNAT/SNAT rules all use `fipVip.Status.V4ip`. If the intent was to use the shared VIP's IP for the shared-EIP case, use `shareVip`; otherwise add a short comment clarifying that the shared scenario shares the EIP but still uses the same VIP (fipVip) for assertions.
- [ ] **Maintainability**: The three-branch switch in test "[3]" (IPv4, IPv6, dual) for subnet status deltas is repeated for "after create" and "after delete". Extract a helper e.g. `expectSubnetIPCountsDelta(initial, after *apiv1.Subnet, protocol, createOrDelete string, deltaAvailable, deltaUsing int)` to reduce duplication.
- [ ] **Maintainability**: In `verifySubnetStatusAfterEIPOperation`, the protocol switch could be table-driven (e.g. slice of {protocol, availableField, usingField, rangeField}) to make adding a new protocol easier and reduce branch duplication.
- [ ] **Naming**: Inconsistent casing: `natgwQoS` vs parameter `natGwQosPolicy` (QoS vs Qos). Use consistent `QoS` in identifiers.
- [ ] **DRY**: Test "[5]" defines a local `nadConfigNoIPAM` struct; the file already has `nadConfig` with IPAM. Consider a shared base struct or move both to a common test helper to avoid config drift.
- [ ] **Readability**: ConformanceIt "[1]" uses a hardcoded image `docker.io/kubeovn/vpc-nat-gateway:v1.14.25`; consider a constant or test framework default so version changes are in one place.

---

## ./test/e2e/kube-ovn/ipam/ipam.go

- [ ] **DRY**: The block that asserts pod IPAM annotations (AllocatedAnnotation, CidrAnnotation, GatewayAnnotation, LogicalSwitchAnnotation, MacAddressAnnotation, RoutedAnnotation, etc.) is repeated in many tests (static pod, comma-separated ippool pod, deployment pods, statefulset pods). Extract a helper e.g. `expectPodIPAMAnnotations(pod *corev1.Pod, subnet *apiv1.Subnet, opts expectPodIPAMOpts)` to reduce duplication and keep assertions consistent.
- [ ] **DRY**: The pattern "get pods, ExpectHaveLen, for range pods assert annotations" appears in deployment and statefulset tests repeatedly. Consider a helper that takes (pods, subnet, optional ippool/expectedIPs) and runs the common annotation checks.
- [ ] **DRY**: The ippool separator selection (`if f.VersionPriorTo(1,11) { ippoolSep = "," } else { ippoolSep = ";" }`) appears in two tests. Extract to e.g. `getIPPoolSeparator(f *framework.Framework) string` for reuse and clarity.
- [ ] **Readability**: Replace magic timeouts in WaitUntil and WaitForPodsRunningReady with named constants (e.g. `ippoolStatusPollInterval`, `ippoolStatusTimeout`, `podsRunningReadyTimeout`) for tuning and documentation.
- [ ] **Structure / DRY**: In "should support IPPool feature", the two WaitUntil callbacks that validate ippool status (initial validation and inside checkFn) share the same structure (V4/V6 Using/Available IPs and Ranges). Extract a helper e.g. `expectIPPoolStatusMatches(ippool *apiv1.IPPool, v4Available, v6Available, v4Using, v6Using, ...) (bool, error)` to avoid duplication and simplify adding new checks.
- [ ] **Maintainability**: The test "should allocate static ip for statefulset with ippool separated by comma" largely duplicates "should allocate static ip for statefulset with ippool" (same flow, different separator and skip condition). Consider one parameterized test or a shared helper that accepts separator and skip condition to reduce drift.
- [ ] **Cleanup / Robustness**: In "should allocate static ip for statefulset with ippool", the loop creates one STS per iteration (stsName) and deletes it; stsName2 is set in BeforeEach but never created in this test, and AfterEach calls DeleteSync(stsName2). Relying on DeleteSync to no-op for non-existent resources is fine but could be clarified or the test could avoid touching stsName2.

---

## ./test/e2e/kube-ovn/kubectl-ko/kubectl-ko.go

- [ ] **DRY**: Four tests repeat the same pattern: get nodes via `e2enode.GetReadySchedulableNodes`, then `for _, node := range nodeList.Items { execOrDie(fmt.Sprintf("ko <cmd> %s <args>", node.Name)) }` (vsctl, ofctl, dpctl, appctl). Extract a helper e.g. `execKoOnEachNode(cs clientset.Interface, cmdFormat string, args ...any)` to remove duplication and clarify intent.
- [ ] **DRY**: The trace test logic (for each IP: set target/testARP/targetMAC/prefix, optional arp reply, then for targetMACs run arp/arp request/icmp/tcp/udp via execOrDie) is duplicated across three tests: "kubectl ko trace <pod>", "trace <pod> for pod with host network", and "trace <node>". Differences are prefix format and supportDstMAC/supportARP. Extract a shared helper e.g. `runTraceExecChecks(prefix string, supportARP, supportDstMAC bool, targetMAC string)` that runs the execOrDie calls so adding new trace variants does not copy-paste.
- [ ] **Readability**: In "trace ... should work with network policy", the closure `checkFunc` captures traceService, subCmd, matchPod, matchLocalnet, f and embeds branching logic. Consider extracting to a named function e.g. `makeTraceOutputChecker(traceService bool, subCmd string, f *framework.Framework) func(string)` so the test reads more linearly.
- [ ] **Readability**: Magic port range `8000 + rand.Int32N(1000)` for tcpPort/udpPort in the network policy trace test could be a helper e.g. `randomPortInRange(8000, 9000)` or a package constant for the range to document intent.
- [ ] **Maintainability**: BeforeEach sets `k8sframework.TestContext.KubeConfig = ""` (and restores in AfterEach). Consider a short comment explaining that this forces kubectl-ko to use in-cluster config for the test.

---

## ./test/e2e/kube-ovn/network-policy/network-policy.go

- [ ] **Robustness**: In the first ConformanceIt, `podSameNode` is set inside the loop when `hostPod.Spec.NodeName == pod.Spec.NodeName` and used after the loop (lines 121-125). If no kube-ovn-cni pod matches (e.g. scheduling edge case), `podSameNode` can be nil and cause a panic. Add `framework.ExpectNotNil(podSameNode)` or guard before use.
- [ ] **DRY**: The pattern "get nodes, get daemonset kube-ovn-cni, get kube-ovn-cni pod per node" (lines 84-98) is useful for node-based e2e; extract e.g. `getKubeOvnCniPodsOnNodes(cs clientset.Interface, daemonSetClient *framework.DaemonSetClient) ([]corev1.Pod, error)` for reuse and clarity.
- [ ] **DRY**: The first test has two loops over "hostPods on other nodes": one waits until connection fails (WaitUntil + err != nil), the other runs once and expects error. Extract a helper e.g. `assertConnectionFromOtherNodesBlocked(pods []corev1.Pod, pod *corev1.Pod, cmd string, poll bool)` to reduce duplication.
- [ ] **Readability**: Replace magic timeouts with named constants: `2*time.Second` (poll interval), `time.Minute` / `3*time.Minute` (wait timeouts) for tuning and documentation.
- [ ] **Readability**: Port selection `8000 + rand.IntN(1000)`; consider a small helper e.g. `randomPort(8000, 9000)` or constant for the range (consistent with kubectl-ko and other e2e files).
- [ ] **Structure**: The first ConformanceIt is long (~80 lines). Extract helpers e.g. `createPodWithNetexecInSubnet(...)`, `assertConnectionBlockedFromOtherNodes(...)`, `assertConnectionAllowedFromSameNode(...)` to shorten the test and improve readability.
- [ ] **Naming**: Variable `podSameNode` could be renamed to `cniPodOnSameNode` to clarify it is the kube-ovn-cni daemon pod on the same node as the test pod.

---

## ./test/e2e/kube-ovn/node/node.go

- [ ] **Robustness**: In the first ConformanceIt, per-node pods are deleted with `podClient.Delete(podName)` (async). Use `podClient.DeleteSync(podName)` in the loop so each pod is fully removed before the next iteration or AfterEach, avoiding flakiness.
- [ ] **DRY**: "Validating node annotations" appears twice: once in the first ConformanceIt (full set: AllocatedAnnotation, ChassisAnnotation, CidrAnnotation, GatewayAnnotation, IPAddressAnnotation, LogicalSwitchAnnotation, MacAddressAnnotation, PortNameAnnotation) and once in the SerialDescribe test (subset). Extract a helper e.g. `expectJoinSubnetNodeAnnotations(node *corev1.Node, join *apiv1.Subnet, full bool)` to reduce duplication.
- [ ] **DRY**: The route-validation logic (build `found` slice from `routes` and `cidrs`, check each cidr has a route) is duplicated before deleting routes and inside the WaitUntil loop. Extract e.g. `foundRoutesForCIDRs(routes []iproute.Route, cidrs []string) []bool` or `expectRoutesForCIDRs(...)` to avoid duplication.
- [ ] **Readability**: In "should access overlay services using node ip", variable `cmd` is reused: first for pod command `[]string{"sleep", "infinity"}`, then for the curl command string. Use distinct names (e.g. `curlCmd` for the curl command) to avoid confusion.
- [ ] **Readability**: Magic timeouts `2*time.Second`, `10*time.Second` in WaitUntil could be named constants (e.g. `routeCheckInterval`, `routeRestoreTimeout`) for tuning and documentation.
- [ ] **Structure**: The pattern "create host network pod with sleep infinity" (MakePod/MakePrivilegedPod, HostNetwork=true, cmd sleep infinity) appears multiple times. Extract a helper e.g. `createHostNetworkPod(podClient *framework.PodClient, namespaceName, podName, nodeName string, image string) *corev1.Pod` to shorten tests and keep behavior consistent.

---

## ./test/e2e/kube-ovn/node/node_network.go

- [ ] **Correctness / Readability**: In several tests, the comment says "Getting node internal IP as test encap IP" but the code uses a hardcoded string `storageNetworkIP := "10.10.10.10"`. Either (a) actually get the node internal IP from `node.Status.Addresses` for realism, or (b) use a named constant (e.g. `testStorageNetworkIP`) and fix the comment to "Using test encap IP". The call `framework.ExpectNotEmpty(storageNetworkIP, "Node should have internal IP")` is pointless when the value is a literal and the message is misleading.
- [ ] **Naming / Shadowing**: Line 226: `for _, cs := range p.Status.ContainerStatuses` — the variable `cs` shadows the package-level `cs clientset.Interface`. Rename to e.g. `containerStatus` or `cStatus` to avoid confusion.
- [ ] **DRY**: The block that patches the node with node_networks annotation (build nodeNetworks map, json.Marshal, build patchPayload, cs.CoreV1().Nodes().Patch) is repeated in many tests. Extract a helper e.g. `patchNodeNetworksAnnotation(cs clientset.Interface, nodeName string, nodeNetworks map[string]string) error` to reduce duplication.
- [ ] **DRY**: The pattern "run ovs-vsctl get open . external-ids:ovn-encap-ip (or ovn-encap-ip-default), Trim output, Logf, check Contains" is repeated. Extract e.g. `getOvsEncapIP(ovsPod *corev1.Pod, key string) (string, error)` and reuse in WaitUntil callbacks and assertions.
- [ ] **DRY**: The WaitUntil callback that runs "ovs-vsctl get open . external-ids:ovn-encap-ip", trims output, and checks Contains(output, expectedIP) appears in multiple tests. Extract a helper e.g. `ovsEncapIPContains(ovsPod *corev1.Pod, expectedIPs ...string) (bool, error)` so tests can call it inside WaitUntil.
- [ ] **DRY**: The "Verifying interface encap-ip" WaitUntil (ovs-vsctl find interface external-ids:iface-id=..., check encap-ip=expectedIP) is repeated in several tests. Extract e.g. `waitForInterfaceEncapIP(ovsPod *corev1.Pod, ifaceID, expectedIP string, pollInterval, timeout time.Duration) error`.
- [ ] **Readability**: In "should use default encap IP when subnet nodeNetwork is empty", the variable `cmd` is reused: first for "ovs-vsctl ... ovn-encap-ip-default", then reassigned to the interface external_ids query. Use distinct names (e.g. `defaultEncapIPCmd`, `ifaceEncapCmd`) to avoid confusion.
- [ ] **Readability**: Magic timeouts `2*time.Second`, `30*time.Second`, `60*time.Second`, `2*time.Minute` are repeated. Use named package-level constants (e.g. `nodeNetworkPollInterval`, `ovsSyncTimeout`, `podFailureWaitTimeout`, `subnetPatchTimeout`) for tuning and documentation.
- [ ] **Maintainability**: In AfterEach, when restoring the node annotation fails the code only logs ("Failed to restore node annotation") and does not fail the test. Consider failing the test with framework.ExpectNoError so dirty state is visible, or document that restore is best-effort and tests may leave the node modified.
- [ ] **Structure**: Creating a subnet (MakeSubnet, optional NodeNetwork, CreateSync, append to createdSubnets) is repeated in many tests. Consider a helper e.g. `createSubnetAndTrack(subnetClient *framework.SubnetClient, name, cidr, nodeNetwork string, namespaces []string, created *[]string) *apiv1.Subnet` to shorten tests.

---

## ./test/e2e/kube-ovn/pod/pod_recreation.go

- [ ] **Naming / Readability**: In the loops at lines 65–67 and 74–76, the range variable `pod` shadows the outer test pod; inside the loop it refers to controller pod items (or pod names). Rename to e.g. `controllerPod` / `p` in the first loop and `controllerPodName` in the second to avoid confusion.
- [ ] **DRY**: The "Validating pod annotations" block (ExpectHaveKeyWithValue AllocatedAnnotation, ExpectMAC, ExpectHaveKeyWithValue RoutedAnnotation) appears twice (lines 37–41 and 94–97). Extract a helper e.g. `expectPodAnnotations(pod *corev1.Pod, expectedMAC string)` where expectedMAC is optional (empty = any MAC) to reduce duplication.
- [ ] **DRY**: The "Getting ips" + "Validating ips" pattern (ipClient.Get(portName), ExpectEqual MacAddress/IPAddress) appears twice (lines 44–51 and 101–106). Extract e.g. `expectIPMatchesPod(ipClient *framework.IPClient, portName string, pod *corev1.Pod)` for reuse.
- [ ] **Redundancy**: Line 60 and 61 both have `framework.ExpectNotNil(deploy.Spec.Replicas)`; remove the duplicate.
- [ ] **Readability**: The fallback `1` in `cmp.Or(*deploy.Spec.Replicas, 1)` could be a named constant (e.g. `defaultControllerReplicas`) for documentation.
- [ ] **Error handling**: Lines 84 and 90 ignore return values (`_ = podClient.Create(pod)`, `_ = deployClient.RolloutStatus(deploy.Name)`). In e2e, consider checking errors and failing with a clear message, or add a short comment why ignoring is acceptable (e.g. Create may fail while controller is down; we only need the pod to exist for later reconciliation).
- [ ] **Structure**: The test is one long closure. Extract helpers e.g. `scaleDownController(deployClient, deploy)`, `scaleUpController(deployClient, deploy)`, `waitForControllerPodsGone(kubePodClient, podNames)` to improve readability and potential reuse.

---

## ./test/e2e/kube-ovn/pod/statefulset.go

- [ ] **Readability**: Remove or shorten the PR comment "add this case for pr https://..." (unnecessary per project guidelines).
- [ ] **Readability**: Rename pod2Name, pod2, pod2IP to recreatedPodName, recreatedPod, expectedIP to clarify intent (testing IP preservation after pod recreation).
- [ ] **Maintainability**: Extract magic numbers 3, 1, 2 (replicas and loop range) to named constants (e.g. initialReplicas, scaledDownReplicas, podsDeletedWhenScalingDown) for documentation and tuning.
- [ ] **Readability**: The loop `for index := 1; index <= 2` could use a named constant for the upper bound to document that we're waiting for exactly two pods to be deleted after scale-down.

---

## ./test/e2e/kube-ovn/pod/vpc_pod_probe.go

- [ ] **Correctness**: Lines 77–81: modifying slice with `slices.Delete` while ranging over it; after first delete, indices shift and iteration is over the original slice. Use e.g. `if i := slices.Index(newArgs, "--enable-tproxy=false"); i >= 0 { newArgs = slices.Delete(newArgs, i, i+1) }` once instead of loop.
- [ ] **Maintainability**: vpcName is assigned inside the test (line 88) but used in AfterEach; if test fails before that, DeleteSync(vpcName) runs with empty string. Consider initializing vpcName in BeforeEach or guarding AfterEach when vpcName is non-empty.
- [ ] **DRY**: The "Waiting for pod readiness probe failure" block (lines 125–135 and 171–181) is duplicated. Extract helper e.g. `expectReadinessProbeFailure(eventClient *framework.EventClient, podName string)`.
- [ ] **DRY**: Creating pod with readiness probe (MakePod + set ReadinessProbe + CreateSync/Create) is repeated 4 times with small variations; consider a helper e.g. `createPodWithReadinessProbe(...)` to reduce duplication.
- [ ] **Structure**: checkTProxyRules has duplicated block for IPv4 and IPv6 (build expectedRules + CheckIptablesRulesOnNode for OVN-OUTPUT and OVN-PREROUTING). Extract inner helper or loop over protocol to reduce duplication.
- [ ] **Readability**: Magic numbers 8000 and 1000 for probe port range could be named constants (e.g. probePortBase, probePortRange) for documentation.

---

## ./test/e2e/kube-ovn/pod/pod_routes.go

- [ ] **Error handling**: Line 108: `err = f.KubeOVNClientSet.KubeovnV1().IPs().Delete(...)` — the error is assigned but never checked. Add `framework.ExpectNoError(err)` or document why ignoring is acceptable (e.g. IP may already be deleted).
- [ ] **Naming**: ginkgo.By "remove legacy lsp" (line 104) — "legacy" is ambiguous (old system vs leftover). Prefer "remove leftover lsp" or "remove remaining lsp" for clarity.
- [ ] **DRY**: Magic numbers `2*time.Second` and `2*time.Minute` appear multiple times (lines 73, 96, 102). Extract named constants (e.g. `policyGCPollInterval`, `policyGCTimeout`) for tuning and documentation.
- [ ] **DRY**: The WaitUntil callback that runs NBExec(nbCmd) and checks `strings.TrimSpace(string(out)) == ""` is duplicated (lines 73–79 and 96–102). Extract a helper e.g. `policyRouteGone(nbCmd string) (bool, error)` or pass nbCmd into a shared closure to reduce duplication.
- [ ] **DRY**: The controller restart block (SetScale(0), RolloutStatus, DeleteSync pod, SetScale(1), RolloutStatus) in the first ConformanceIt could be extracted to e.g. `restartKubeOvnController(deployClient *framework.DeploymentClient, deployName string)` for readability and potential reuse with pod_recreation.go.
- [ ] **Readability**: First ConformanceIt is long (~65 lines). Consider extracting helpers e.g. `expectNorthGatewayPolicy(podIP, northGateway, ipSuffix string)`, `waitForPolicyRouteGC(nbCmd string, timeout time.Duration)` to shorten the test.
- [ ] **Readability**: Hardcoded gateways "100.64.0.100" and "fd00:100:64::100" could be named constants (e.g. `defaultNorthGatewayV4`, `defaultNorthGatewayV6`) for documentation.
- [ ] **Consistency**: Line 72 uses `f.PodClientNS(namespaceName).DeleteSync(podName)` while elsewhere the file uses `podClient.DeleteSync(podName)`. Use `podClient.DeleteSync(podName)` for consistency (podClient is already for the test namespace).

---

## ./test/e2e/kube-ovn/qos/qos.go

- [ ] **DRY**: The ovs-vsctl command string is identical in `getOvsQosForPod` and `waitOvsQosForPod`; extract e.g. `ovsVsctlQosCmd(table string, pod *corev1.Pod) string` to avoid duplication.
- [ ] **DRY**: The two netem ConformanceIt blocks share repeated validation (pod annotations: AllocatedAnnotation, RoutedAnnotation, netem fields; OVS qos: latency, jitter, limit, loss). Consider helpers e.g. `expectNetemPodAnnotations(...)` and `expectNetemOvsQos(...)` to reduce duplication.
- [ ] **Readability**: Magic numbers `2*time.Second` and `2*time.Minute` in `waitOvsQosForPod` could be named constants (e.g. `qosPollInterval`, `qosWaitTimeout`) for tuning and documentation.
- [ ] **Readability**: In `waitOvsQosForPod`, when `expected` is nil the function effectively waits for any non-empty config; add a short comment to clarify behavior when `expected == nil`.

---

## ./test/e2e/kube-ovn/service/service.go

- [ ] **Correctness / Robustness**: In `checkContainsClusterIP`, after `output = bytes.TrimSpace(output)` the code does `if output[0] == '"'`; when output is empty this panics. Guard with `if len(output) > 0 && output[0] == '"'` (or handle empty output explicitly).
- [ ] **DRY**: Magic numbers `2*time.Second` and `30*time.Second` appear in both ConformanceIt blocks (lines 109, 156); extract named constants (e.g. `servicePollInterval`, `serviceWaitTimeout`).
- [ ] **DRY**: Port selection `8000 + rand.Int32N(1000)` is duplicated (lines 70, 128); consider a helper or package constants (e.g. `servicePortBase`, `servicePortRange`) for consistency.
- [ ] **Readability**: First ConformanceIt is long (~60 lines); consider extracting helpers e.g. `createSubnetPodAndNodePortService(...)` and `createHostNetworkPod(...)` to shorten and clarify flow.
- [ ] **Naming**: Skip message line 125: "This case is support in v1.11" → "This case is supported in v1.11" (grammar).
- [ ] **Readability**: ginkgo.By messages mix capitalization ("Creating pod" vs "check service from dual stack..."); consider consistent style for test steps.

---

## ./test/e2e/kube-ovn/subnet/subnet.go

- [ ] **Naming**: Fix typo `expectetIPsets` → `expectedIPsets` (lines 64, 71) in `checkIPSetOnNode`.
- [ ] **DRY**: The "Validating subnet spec fields" and "Validating subnet status fields" blocks (ExpectFalse/ExpectEqual/ExpectEmpty for Default, Protocol, ExcludeIps, Gateway, etc.) are repeated in many ConformanceIt cases. Extract helpers e.g. `validateSubnetSpecFields(subnet, cidr, gateways, ...)` and `validateSubnetStatusFields(subnet, cidrV4, cidrV6, ...)` or a single `validateSubnetAfterCreate(subnet, ...)` to reduce duplication.
- [ ] **DRY**: The dual-stack block "if cidrV4 == \"\" { ... firstIPv4/lastIPv4 } else { ... }" and same for cidrV6 is repeated in BeforeEach and conceptually in smallCIDR setup. Consider a small struct or helper that holds parsed subnet bounds (firstV4, lastV4, firstV6, lastV6, gateways) for reuse.
- [ ] **DRY**: In "should support distributed external egress gateway" and "should support centralized external egress gateway", the logic to get docker network and determine gatewayV4/gatewayV6 from `network.IPAM.Config` is duplicated. Extract e.g. `getKindEgressGateways(network, cidrV4, cidrV6) ([]string, error)`.
- [ ] **Readability**: Magic numbers `2*time.Second`, `10*time.Second`, `3*time.Second`, `time.Minute`, `5*time.Second`, `30*time.Second` appear throughout. Consider named constants at the top of the describe block (e.g. `subnetWaitInterval`, `subnetWaitTimeout`) for tuning and documentation.
- [ ] **Structure**: The describe block is very long with many ConformanceIt cases. Consider splitting by feature (e.g. subnet_basic_test.go, subnet_gateway_test.go, subnet_nat_test.go) or grouping related tests with nested Describe for maintainability.
- [ ] **Naming**: Fix typo `calcuIPRangeListStr` → `calcIPRangeListStr` or `calculateIPRangeListStr` (line 1040).
- [ ] **Error handling**: In `checkAccessExternal`, `exec.Command("bash", "-c", cmd).CombinedOutput()` ignores error; consider checking err and failing with a clear message when exec fails.
- [ ] **DRY**: The pattern "if cidrV4 != \"\" { framework.ExpectEqual(subnet.Status.V4AvailableIPs, ...) } else { framework.ExpectZero(...) }" (and same for V6) is repeated in many tests. Extract e.g. `expectSubnetAvailableIPs(subnet, cidrV4, cidrV6)`.
- [ ] **Maintainability**: In "should fail to create pod with static MAC that conflicts with gateway MAC", the test uses `exec.Command("kubectl", "ko", "nbctl", ...)` directly instead of a framework helper (e.g. `framework.NBExec`). Align with other tests for consistency and to avoid dependency on host kubectl.

---

## ./test/e2e/kube-ovn/subnet/subnet-selectors.go

- [ ] **Correctness**: In `BeforeEach`, `namespaceSelectors = append(namespaceSelectors, ns1Selector)` reuses the package-level slice; it is never reinitialized, so across multiple `It()` runs the slice grows (e.g. second test gets `[ns1Selector, ns1Selector]`). Initialize at start of BeforeEach: `namespaceSelectors = []metav1.LabelSelector{ns1Selector}` (or `namespaceSelectors = nil` then append).
- [ ] **Naming**: Fix typo "matched witch" → "matched with" in ginkgo.By messages (lines 123, 140).
- [ ] **DRY**: The `framework.WaitUntil(time.Second, 30*time.Second, func(...) { ns = nsClient.Get(nsName); if ns.Annotations[util.LogicalSwitchAnnotation] ==/!= subnet.Name return true, nil; return false, nil }, "failed to update annotation...")` pattern repeats many times. Extract a helper e.g. `waitForNamespaceSubnetAnnotation(nsClient, nsName, subnetName string, expectPresent bool)` to reduce duplication.
- [ ] **Readability**: Magic number `30*time.Second` appears repeatedly; use a named constant (e.g. `subnetAnnotationWaitTimeout`) at the top of the describe block.
- [ ] **Readability**: Third ConformanceIt title "update namespace with labelSelector, and set subnet spec namespaces with selected namespace" is misleading — the test mainly updates subnet spec (Namespaces and NamespaceSelectors), not namespace labels. Consider a clearer title e.g. "subnet spec namespaces and namespaceSelector interaction".

---

## ./test/e2e/kube-ovn/switch_lb_rule/switch_lb_rule.go

- [ ] **Naming**: Fix typo `slrSlector` → `slrSelector` (lines 221, 234).
- [ ] **Comment**: Line 70 `// TODO:// slr support dual-stack` has double slash and grammar; use `// TODO: SLR supports dual-stack` or similar.
- [ ] **DRY**: The pattern `framework.WaitUntil(2*time.Second, time.Minute, func(...) { _, err = X.Get(...); if err == nil return true,nil; if k8serrors.IsNotFound(err) return false,nil; return false,err }, fmt.Sprintf("..."))` is repeated many times for service/switch-lb-rule/endpoints. Extract a helper e.g. `waitForResource(getter func() (interface{}, error), resourceDesc string)` to reduce duplication.
- [ ] **DRY**: The endpoint validation loop (build ips/tps/protocols from subset, ExpectContainElement for pod IP and for ports) is duplicated for selector SLR and endpoint SLR (lines 278–301 and 358–378). Extract e.g. `validateSlrEndpoints(eps *corev1.Endpoints, pods []corev1.Pod, expectedPorts []kubeovnv1.SwitchLBRulePort)`.
- [ ] **Readability**: Magic numbers `2*time.Second`, `time.Minute`, and port literals (8090, 8091, 8092, 80) appear repeatedly; use named constants at the top of the describe block (e.g. `slrPollInterval`, `slrWaitTimeout`, `slrFrontPort*`).
- [ ] **Structure**: The DescribeTable callback is very long (~250 lines). Extract phases into helpers e.g. `createOverlaySubnetAndClientPod`, `createStsAndService`, `createSelectorSlrAndValidate`, `createEndpointSlrAndValidate` to improve readability and testability.
- [ ] **Consistency**: AfterEach uses `nadClient.Delete(nadName)` while other resources use `DeleteSync`. Consider `DeleteSync` for NAD or document why async Delete is used.
- [ ] **Variable scope**: In the WaitUntil at line 322, `_, err :=` shadows the outer `err`; use `_, err =` for consistency with other WaitUntil blocks in the same function so the same `err` is used after the loop if needed.

---

## ./test/e2e/kube-ovn/underlay/underlay.go

- [ ] **DRY**: The block that extracts cidrV4/cidrV6/gatewayV4/gatewayV6 from `dockerNetwork.IPAM.Config`, builds cidr/gateway slices, and builds excludeIPs from `network.Containers`, is repeated in many ConformanceIt blocks (e.g. ~lines 303–334, 368–399, 430–455, 569–602, 632–657, etc.). Extract a helper e.g. `underlaySubnetFromDockerNetwork(dockerNetwork *dockernetwork.Inspect, f *framework.Framework) (cidr, gateway, excludeIPs []string)`.
- [ ] **DRY**: The long "should support underlay to overlay subnet interconnection" test repeats the pattern: delete underlay pod, patch subnet (U2O on/off), create underlay pod, waitSubnetStatusUpdate, waitSubnetU2OStatus, checkU2OItems. Extract helpers e.g. `toggleU2OAndCheck(subnetName string, enable bool, expectedUsingIPs float64)` to shorten and clarify.
- [ ] **Readability**: Magic numbers `2*time.Second`, `30*time.Second`, `1*time.Second`, `3*time.Second`, `time.Minute`, `10*time.Second`, `5*time.Second` appear repeatedly; use named constants at package or describe level.
- [ ] **Readability**: In `waitSubnetU2OStatus`, when `enableU2O` is false the "Keep waiting" message (line 98) still says "current enable U2O subnet status"; use "current disable U2O subnet status" in the else branch for clarity.
- [ ] **Style**: Line 40 `customInterfaces := make(map[string][]string, 0)` — capacity 0 is redundant for maps; use `make(map[string][]string)`.
- [ ] **Structure**: The file is long (~860 lines) with several large ConformanceIt blocks; consider splitting by feature (e.g. underlay_provider.go, underlay_u2o.go) or extracting shared setup/helpers into a separate _test_helper.go for maintainability.

---

## ./test/e2e/kube-ovn/underlay/vlan_subinterfaces.go

- [ ] **Structure**: BeforeEach is long (~60 lines) with docker network creation, kind node listing, and node interface resolution. Extract helpers e.g. `setupDockerNetworkForVlanSubif()`, `buildKindNodeMapAndInterfaces()` to shorten and improve readability.
- [ ] **DRY**: The pattern "create provider network (createVlanSubinterfaceTestProviderNetwork + CreateSync + WaitToBeReady)" appears in every test. Extract a helper e.g. `createAndWaitVlanSubinterfaceProviderNetwork(name, defaultInterface, autoCreate, customInterfaces) *v1.ProviderNetwork`.
- [ ] **DRY**: The loop "for _, node := range readyKindNodes { nodeIface := nodeInterfaceNameFor(...); framework.ExpectTrue(vlanSubinterfaceExists(...)); framework.ExpectTrue(isKubeOVNAutoCreatedInterface(...)) }" is repeated in many tests. Extract e.g. `assertVlanSubinterfaceCreatedOnAllNodes(kindNodeMap, readyKindNodes, nodeInterfaces, pnDefaultInterface)`.
- [ ] **DRY**: The loop "for _, node := range readyKindNodes { waitForInterfaceState(kindNodeMap, node.Name(), nodeInterfaceNameFor(...), false, 2*time.Minute) }" appears multiple times. Extract `waitForInterfaceGoneOnAllNodes(kindNodeMap, readyKindNodes, nodeInterfaces, pnDefaultInterface, timeout)`.
- [ ] **Readability**: Replace magic durations with named constants: `time.Minute`, `2*time.Minute`, `5*time.Second`, `2*time.Second`, `30*time.Second` (e.g. `providerNetworkReadyTimeout`, `interfaceCleanupTimeout`, `interfaceStatePollInterval`).
- [ ] **Maintainability**: In "should not cleanup existing subinterfaces when autoCreateVlanSubinterfaces set to false", the bare `time.Sleep(5 * time.Second)` is brittle. Use `gomega.Eventually` with a condition or a named constant with a comment explaining the stabilization wait.
- [ ] **Structure**: The test "should isolate subinterfaces across multiple provider networks" creates pn1, pn2, pn3 with similar inline setup. Consider a helper that takes a slice of {name, interface, autoCreate} and returns created ProviderNetworks, or extract a table-driven sub-test to reduce repetition.
- [ ] **Readability**: In `waitForInterfaceState`, the polling interval `5*time.Second` could be a named constant (e.g. `interfaceStatePollInterval`) for consistency with other timeouts.

---

## ./test/e2e/kubevirt/e2e_test.go

- [ ] **DRY**: The pattern "Getting pod of vm", building `labelSelector` with `fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)`, listing pods, `ExpectHaveLen(podList.Items, 1)`, then "Validating pod annotations" with the same three `ExpectHaveKeyWithValue` (AllocatedAnnotation, RoutedAnnotation, VMAnnotation) is repeated in almost every test. Extract a helper e.g. `getVMPodAndValidateAnnotations(podClient, vmName) (*corev1.Pod, []corev1.PodIP)` or split into `getVMPod` + `validateVMPodAnnotations` to reduce duplication.
- [ ] **DRY**: The ovn-nbctl command to check LSP external_ids (`ovn-nbctl --format=list --data=bare --no-heading --columns=external_ids list Logical_Switch_Port ` + portName) and the assertion `ExpectContainElement(strings.Fields(string(output)), "ls="+subnetName)` appear in three tests. Extract e.g. `expectLSPExternalIDsContainSubnet(portName, subnetName)` in the test file or a framework helper.
- [ ] **Readability**: Magic numbers `2*time.Minute` (WaitToBeReady), `2*time.Second, 2*time.Minute` (WaitToDisappear) could be named constants at the top of the Describe block (e.g. `vmReadyTimeout`, `ipDisappearPollInterval`, `ipDisappearTimeout`) for tuning and documentation.
- [ ] **DRY**: Creating a subnet with `framework.MakeSubnet(subnetName, "", framework.RandomCIDR(f.ClusterIPFamily), "", "", "", nil, nil, []string{namespaceName})` and `subnetClient.CreateSync(subnet)` appears multiple times. Extract a helper e.g. `createSubnetForNamespace(f, subnetClient, subnetName, namespaceName)` to reduce repetition.
- [ ] **Maintainability**: The long comment block in "should be able to handle vm restart when subnet changes before the vm is stopped" (lines 163–166) duplicates the test steps; shorten to a single line or move key scenario into the ConformanceIt title for clarity.
- [ ] **Readability**: In the last ConformanceIt, the inline comment "// delete old ip to create the same name ip in other subnet" could be expressed as a ginkgo.By step for consistency with other steps.

---

## ./test/e2e/lb-svc/e2e_test.go

- [ ] **DRY**: The two ConformanceIt tests share most of the flow: create server pod, create service, wait for LB deployment, check LB pod annotations, check service status, create client pod, check connectivity, (v1.13+) delete LB pod and verify reconnect, delete service and wait deployment disappear. Extract helpers e.g. `waitForLBDeploymentReady(deploymentClient, deploymentName)`, `assertLBPodAnnotations(pod, provider, expectedIP string)`, `createClientPodAndCurlLB(clientPodName, lbIP, port, ...)` to reduce duplication.
- [ ] **DRY**: The block "Waiting for LB deployment to be ready" (WaitUntil for Get deployment, then Get + WaitToComplete, GetPods, ExpectHaveLen 1) is identical in both tests. Extract `waitForLBDeploymentReady(deploymentClient, deploymentName) (*appsv1.Deployment, *corev1.Pod)`.
- [ ] **Structure / Extensibility**: Consider DescribeTable with entries "dynamic" and "static" external IP, with a shared setup and only service creation (with or without LoadBalancerIP) differing, to make adding new LB allocation variants easier.
- [ ] **Readability**: Magic numbers `8000 + rand.Int32N(1000)`, `2*time.Second`, `time.Minute`, `30*time.Second`, `2*time.Minute` could be named constants at the top of the Describe block (e.g. `lbSvcPortBase`, `lbDeploymentPollInterval`, `lbDeploymentTimeout`, `lbConnectivityWaitTimeout`, `lbDeploymentDisappearTimeout`).
- [ ] **Readability**: In the static-IP test, `base := util.IP2BigInt(gateway)` then `base.Add(base, big.NewInt(...))` mutates `base`. Use `util.BigInt2Ip(new(big.Int).Add(base, big.NewInt(50+rand.Int64N(50))))` or a copy so the original big.Int is not modified for clarity.
- [ ] **Consistency**: AfterEach uses `nadClient.Delete(nadName)` while other resources use `DeleteSync`. Document why async Delete for NAD or switch to DeleteSync if the framework supports it.
- [ ] **DRY**: The BeforeEach block that extracts cidr/gateway from `dockerNetwork.IPAM.Config` and excludeIPs from `dockerNetwork.Containers` is a common underlay pattern; consider a shared helper e.g. `kind.UnderlaySubnetFromDockerNetwork(dockerNetwork *dockernetwork.Inspect) (cidr, gateway string, excludeIPs []string)` if reused elsewhere.

---

## ./test/e2e/metallb/e2e_test.go

- [ ] **Correctness / Naming**: The package is declared as `package kubevirt` but the file lives under `test/e2e/metallb/`. Change to `package metallb` to match the directory and avoid confusion.
- [ ] **Naming**: Variable `ContainerInspect` (line 177) uses PascalCase; use `containerInspect` for a local variable to follow Go conventions.
- [ ] **Readability**: In `makeProviderNetwork`, `customInterfaces := make(map[string][]string, 0)` — capacity 0 is redundant for maps; use `make(map[string][]string)`.
- [ ] **DRY**: The block that extracts cidrV4/cidrV6, gatewayV4/gatewayV6 from dockerNetwork.IPAM.Config, builds underlayCidr/gateway, metallbVIPv4s/metallbVIPv6s, and excludeIPs is long and similar to other underlay tests. Extract helpers e.g. `underlayCIDRsAndGatewaysFromDocker(dockerNetwork, hasIPv4, hasIPv6)` and `metallbIPRangeFromCIDR(cidr string, start, count int) []string` to shorten the test.
- [ ] **DRY**: Creating the two LB services with the same pattern (MakeService, IPFamilyPolicy from IsDual, ExternalTrafficPolicy Local, CreateSync with LoadBalancer.Ingress condition) is duplicated. Extract e.g. `createUnderlayLBService(serviceClient, name, labels, ports) *corev1.Service` and call twice.
- [ ] **Readability**: Magic numbers `15*time.Second` (flow restoration), `5*time.Second`, `30*time.Second` (WaitUntil for U2O MAC), `30*time.Second` (UpdateSync) could be named constants at the top of the Describe block (e.g. `flowRestoreTimeout`, `u2oMacPollInterval`, `u2oMacTimeout`, `subnetUpdateTimeout`).
- [ ] **Error handling**: In the ConformanceIt, `ip, _ := ipam.NewIP(startIP)` ignores the error (lines 284, 298). Validate or handle the error to avoid wrong IPs if startIP is malformed.
- [ ] **Structure**: The single ConformanceIt is very long (~170 lines). Extract phases into helpers e.g. `setupMetallbUnderlaySubnet(...)`, `createDeployAndLBServices(...)`, `restartCNIAndAssertFlowRestored(...)`, `toggleU2OAndAssertReachable(...)` to improve readability and testability.
- [ ] **Robustness**: In `getVIPNode`, parsing MAC from `fields[4]` assumes a fixed "ip neigh show" output format. Consider more robust parsing (e.g. match MAC pattern) or document the assumed format.
- [ ] **Consistency**: AfterEach uses `vlanClient.Delete(vlanName)` (no Sync) while other resources use DeleteSync. Document why or use DeleteSync if available.

---

## ./test/e2e/multus/e2e_test.go

- [ ] **Correctness / Readability**: In several ConformanceIt blocks, `cidr := pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, provider)]` shadows the Describe-level variable `cidr` (subnet CIDR). Use a different name e.g. `podCIDR` to avoid shadowing and clarify meaning.
- [ ] **DRY**: The block "Validating pod annotations" (ExpectHaveKey NetworkStatusAnnot, get cidr/ip/gateway/mac from provider templates, ExpectIPInCIDR, ExpectMAC) and "Validating IP resource" (ipName, ipCR Get, ExpectEqual for Spec and Labels) are repeated across 6 tests with small variations. Extract helpers e.g. `validatePodNetworkAnnotations(pod, provider) (podCIDR, ip, gateway, mac string)` and `validateIPCR(ipClient, ipName, pod, subnetName, namespaceName, provider, ip, mac string, expectMAC bool)` to reduce duplication.
- [ ] **DRY**: The block "Retrieving pod routes" (RouteShow via KubectlExec), building actualRoutes from podRoutes (filter r.Gateway != "" || r.Dst != ""), then "Validating pod routes" with ExpectContainElement for CIDR/default gateway/nad CIDR (and custom route expectations) is repeated in 4 tests. Extract e.g. `getPodRoutesAsRequestRoutes(namespaceName, podName string) []request.Route` and a parameterized route validator to reduce duplication.
- [ ] **DRY**: The "Constructing network attachment definition config" block (for range 3 to get routeDst != subnet.Spec.CIDRBlock, routeGw from RandomIPs, build routes from nad gateways and routeDst/routeGw) is duplicated in "custom routes" and "custom routes for macvlan" tests. Extract e.g. `randomRouteDstAndGw(f *framework.Framework, subnet *apiv1.Subnet) (routeDst, routeGw string, routes []request.Route)`.
- [ ] **DRY**: The loop that finds ifaceName from `nadutils.GetNetworkStatus(pod)` by matching `status.Name == nadKey` is repeated in 4 tests. Extract e.g. `getNetworkInterfaceName(pod *corev1.Pod, nad *nadv1.NetworkAttachmentDefinition) string`.
- [ ] **Consistency**: AfterEach uses `nadClient.Delete(nadName)` while other resources use DeleteSync. Document why or use DeleteSync if available.
- [ ] **Readability**: The loop `for range 3 { routeDst = framework.RandomCIDR(...); if routeDst != subnet.Spec.CIDRBlock { break } }` could be a helper e.g. `framework.RandomCIDRExcluding(f.ClusterIPFamily, subnet.Spec.CIDRBlock)` to clarify intent and avoid magic retry count.

---

## ./test/e2e/non-primary-cni/e2e_test.go

- [ ] **Readability / Typo**: Fix "at lease" → "at least" in framework.ExpectNotEmpty messages (lines 371, 434, 437).
- [ ] **Readability**: Fix comment on line 341: "Get the EIP associated with this SNAT rule" should say "DNAT rule" in the DNAT verification block.
- [ ] **DRY**: The apply-stages pattern (BeforeEach: for stage 0..N, kubectl apply -f yamlFile --prune -l config-stage=N, optional sleep) is repeated in VPC Simple, VPC NAT Gateway, and Logical Network Simple with different stage counts and sleep durations. Extract a helper e.g. `applyConfigStages(yamlFile string, stages []struct{ Stage int; Sleep time.Duration })` to reduce duplication.
- [ ] **DRY**: The cleanup pattern (AfterEach: for stage N down to 0, removeFinalizers(strconv.Itoa(stage)), kubectl delete -f yamlFile -l config-stage=stage) is repeated in all three contexts. Extract e.g. `cleanupConfigStages(yamlFile string, maxStage int)`.
- [ ] **Readability**: In `waitForResourceReady`, replace magic numbers `60*time.Second` and `2*time.Second` with named constants (e.g. `resourceReadyTimeout`, `resourceReadyInterval`) at package level.
- [ ] **Maintainability**: `removeFinalizers` discards errors from `runBashCommand` (e.g. `output, _ := runBashCommand(cmd)`). Consider logging or asserting on failure when patch/delete fails to ease debugging.
- [ ] **Structure**: File is ~464 lines with three major SerialDescribe blocks. Consider splitting into separate test files per feature (e.g. non_primary_cni_vpc_simple_test.go, non_primary_cni_vpc_nat_gw_test.go, non_primary_cni_lnet_simple_test.go) for better maintainability and parallel review, or add a short package comment documenting the three test groups.

---

## ./test/e2e/ovn-ic/e2e_test.go

- [ ] **Correctness / Naming**: Line 51: Fix typo "no enough" → "not enough".
- [ ] **DRY**: The pattern of getting ConfigMap `util.InterconnectionConfig` and reading `az-name` or `gw-nodes` is repeated in multiple tests (e.g. "should create logical switch ts", "should be able to update az name", ECMP test). Extract helpers e.g. `getAZNames(clientSets []clientset.Interface) []string` and `getGwNodes(clientSets []clientset.Interface) []string` to reduce duplication.
- [ ] **DRY**: Switching kubectl context via `exec.Command("bash", "-c", "kubectl config use-context "+context).CombinedOutput()` and `framework.ExpectNoError(err)` is repeated many times in the ECMP test and in `checkECMPCount`. Extract a helper e.g. `switchKubeContextOrDie(context string)` (or use `execOrDie` with "config use-context" for consistency) to avoid repetition and shell injection.
- [ ] **Error handling**: Lines 245–246, 261–262, 279–280: `json.Marshal` and `Patch` return values are ignored with `_`. Use `framework.ExpectNoError(err, ...)` for Marshal and Patch to fail tests on error.
- [ ] **Error handling**: Lines 278, 289, 307–308, 319–320, etc.: `_, _ = exec.Command(...).CombinedOutput()` discards both output and error. At least check error with `framework.ExpectNoError(err, ...)` for critical commands (e.g. patch deployment, taint).
- [ ] **DRY**: The "disable gateway" (iptables DROP + taint) and "enable gateway" (iptables DROP remove + untaint) pattern is repeated for worker and worker2 in cases 6–9. Extract helpers e.g. `disableGatewayNodeOrDie(nodeName string)` and `enableGatewayNodeOrDie(nodeName string)` to reduce duplication.
- [ ] **Readability**: Replace magic numbers with named constants: `10*time.Second` (line 203), `2*time.Second` (line 211), `30` (maxRetryTimes in checkECMPCount), `3*time.Second` (sleep in checkECMPCount), port range `8000 + rand.Int32N(1000)` (e.g. `azNameApplyWait`, `ovnRecomputeWait`, `ecmpCheckMaxRetries`, `ecmpCheckPollInterval`, `httpPortBase`).
- [ ] **Structure**: The ConformanceIt "Should Support ECMP OVN Interconnection" is very long (~120 lines) with 9 cases. Consider splitting into separate ConformanceIt specs (e.g. one per case) or extract case logic into helper functions (e.g. `reduceGatewaysToOne`, `recoverGatewaysToThree`, `scaleECMPPath`, `disableGatewayAndVerify`, `enableGatewayAndVerify`) for readability and maintainability.
- [ ] **Maintainability**: Hardcoded cluster and node names ("kind-kube-ovn", "kind-kube-ovn1", "kube-ovn1-worker", "kube-ovn1-worker2") appear in the ECMP test. Consider deriving from `clusters` or framework config so tests are less environment-specific.
- [ ] **Readability**: In "should be able to update az name", the same assertion `framework.ExpectTrue(strings.Contains(output, lsp), ...)` is duplicated (lines 217–218). Remove the duplicate.
- [ ] **Consistency**: Prefer a single way to run kubectl: this file uses both `execOrDie`/`execPodOrDie` (which use framework kubectl) and raw `exec.Command("bash", "-c", ...)`. Use a helper like `switchKubeContextOrDie(kubeContext)` that calls `e2ekubectl.NewKubectlCommand("", "config", "use-context", kubeContext).ExecOrDie("")` for consistency and to avoid shell injection.
- [ ] **Error handling / Cleanup**: In "should be able to communicate between clusters", the backup/restore configmap uses a temp file "temp-ovn-ic-config.yaml" in the current directory; on failure before the rm command, the file is left behind. Consider defer cleanup or use a framework temp dir and ensure rm runs in a defer.
- [ ] **Naming**: `fnCheckPodHTTP` — the "fn" prefix is unusual in Go; consider renaming to `checkPodHTTP` for clarity (it is a function variable used as a helper).

---

## ./test/e2e/ovn-vpc-nat-gw/e2e_test.go

- [ ] **Correctness / Naming**: Package is `ovn_eip` but the file lives in `ovn-vpc-nat-gw`; package name should match directory (e.g. `ovn_vpc_nat_gw`) for consistency and to avoid confusion.
- [ ] **Correctness / Bug**: In AfterEach, line 372: `ovnFipClient.DeleteSync(ipFipEipName)` — `ipFipEipName` is an EIP, not a FIP; use `ovnEipClient.DeleteSync(ipFipEipName)`. Also `ipFipEipName` is deleted twice (lines 371–372 and 375–376); remove the duplicate.
- [ ] **Naming**: Fix typos: `sharedEipFipShoudOkName` / `sharedEipFipShoudFailName` / `shareFipShouldOk` → "Should" (not "Shoud"). Fix comment "traditonal" → "traditional" (line 509).
- [ ] **Structure / Readability**: The single ConformanceIt is ~500 lines with many sequential steps. Split into smaller ConformanceIt specs (e.g. "centralized external gw", "extra external subnet", "bfd route", "distributed case") or extract step helpers (e.g. `setupUnderlaySubnet`, `createPodsOnNodes`, `testPingConnectivity`) to improve readability and maintainability.
- [ ] **Structure**: BeforeEach is ~230 lines and mixes "once per suite" setup (cluster, docker networks, kind nodes) with "per test" setup. Consider using ginkgo.BeforeSuite for cluster/network setup and keeping BeforeEach minimal so test order and reuse are clearer.
- [ ] **DRY**: Docker network CIDR/gateway/excludeIPs extraction is duplicated for main and extra network (lines 358–384 and 458–483). Extract e.g. `extractSubnetFromDockerNetwork(network *dockernetwork.Inspect, hasV4, hasV6 bool) (cidr, gateway []string, excludeIPs []string)`.
- [ ] **DRY**: Pod creation loop "for _, node := range nodeNames" with same annotations/cmd pattern is repeated 4+ times (no-bfd pods, no-bfd-extra pods, bfd pods). Extract e.g. `createPodsOnNodes(namespace, subnetName, prefix string, nodeNames []string)`.
- [ ] **DRY**: Ping/connectivity test blocks (1.2 and 1.4) are nearly identical (ping lrp eip, ping pod fip, ping provider bridge ips). Extract e.g. `assertPodPingTargets(pod *corev1.Pod, targets []string, bridgeIps []string)` to reduce duplication.
- [ ] **DRY**: AfterEach has many similar "Deleting X" + DeleteSync calls; consider grouping by resource type or a small helper to reduce repetition (e.g. `deleteOvnEips(client, names...)`).
- [ ] **Readability**: Replace magic numbers with named constants: `gwNodeNum := 2`, `time.Sleep(1*time.Second)`, `3*time.Second`, `5*time.Second`, deadline `time.Minute`, `2*time.Second` in WaitLinkToDisappear (e.g. `gwNodeCount`, `subnetDeleteDelay`, `eipStatusWait`, `configUpdateWait`, `cleanupDeadline`, `linkCheckInterval`).
- [ ] **Maintainability**: ~40+ variables at Describe scope (skip, itFn, cs, dockerNetwork, nodeNames, gwNodeNames, providerBridgeIps, etc.) make the test fragile and hard to follow. Consider a test context struct (e.g. `ovnVpcNatGwContext`) holding cluster/client/network state and pass it to helpers to reduce global state.
- [ ] **Readability**: Variable shadowing in loop at line 451: `command := "ping ..."` inside the loop shadows the outer `command`; use a different name (e.g. `pingCmd`) or avoid redeclaration to avoid confusion.
- [ ] **Extensibility**: Several TODOs in code (ipv6/dual stack, vip type allowed address pair, external-gw-nodes auto managed). Track as tech-debt or extract to refactor tasks for future work.

---

## ./test/e2e/security/e2e_test.go

- [ ] **Readability / Typo**: Line 49: "failed to to get deployment" → "failed to get deployment" (double "to").
- [ ] **Readability**: The condition in checkPods (lines 77-83) is complex with multiple AND/OR and version checks. Extract to a helper e.g. `shouldListenOnPodIP(pods []corev1.Pod, process string, f *framework.Framework) bool` for clarity and testability.
- [ ] **DRY / Structure**: The kube-ovn-cni test (lines 146-165) manually gets nodes, daemonset, and pods then calls checkPods; this duplicates the pattern of checkDeployment (get resource → get pods → checkPods). Consider a helper e.g. `checkDaemonSet(f, name, process string, ports ...string)` to align with checkDeployment and reduce duplication.
- [ ] **Readability**: Port numbers "10660", "10661", "10665" are magic numbers. Consider named constants (e.g. `controllerMetricsPort`, `monitorMetricsPort`, `cniDaemonPort`) at package or test level for consistency and documentation.
- [ ] **Maintainability**: The ss/grep/awk command (line 85) is long and shell-dependent. Consider extracting to a helper e.g. `getListenAddressesInContainer(containerID, process string, portFilter []string) ([]string, error)` for readability and potential reuse.
- [ ] **Structure**: In checkPods, the two branches (len(ports) != 0 vs else) both validate listenAddresses differently. Consider extracting to `validateListenAddresses` or split into `validateListenPorts` and `validateListenPrefix` to shorten checkPods.
- [ ] **Error handling**: When docker.Exec fails (line 95), include process and pod name in the error message for easier debugging (e.g. "failed to get listen addresses for process %s in pod %s/%s", process, pod.Namespace, pod.Name).

---

## ./test/e2e/vip/e2e_test.go

- [ ] **Readability / Typo**: Line 410: comment "virtual parents should be set correctlly" → "correctly".
- [ ] **DRY**: The first ConformanceIt has three nearly identical switch blocks (IPv4, IPv6, dual stack) for "verify subnet status after VIP creation" and again for "after VIP deletion". Extract helpers e.g. `verifySubnetStatusAfterVipCreate(initial, afterCreate *apiv1.Subnet, testVip *apiv1.Vip)` and `verifySubnetStatusAfterVipDelete(afterCreate, afterDelete, initial *apiv1.Subnet)` to reduce duplication.
- [ ] **DRY**: In "Test vip", the subnet status wait/assert blocks after VIP create and after VIP delete (lines 334-348, 354-368) repeat the same pattern. Extract e.g. `expectSubnetStatusChangedAfterVipOp(oldSubnet, newSubnet *apiv1.Subnet, afterCreate bool)`.
- [ ] **Readability**: Replace magic numbers with named constants: `5*time.Second` (subnet status wait), `10*time.Second` (upper case VIP wait), `for range 10` (finalizer poll attempts), e.g. `subnetStatusUpdateWait`, `vipFinalizerPollAttempts`.
- [ ] **Structure**: The second ConformanceIt "Test vip" is ~100 lines and mixes subnet counting, IPv6 VIP, allowed address pair, security group, and switch lb vip. Consider splitting into smaller ConformanceIt specs for readability and easier debugging.
- [ ] **DRY**: testConnectivity has two symmetric blocks (srcPod→dstPod and dstPod→srcPod). Extract e.g. `pingViaVipAndThenRemove(f, namespaceName, addIP, delIP, command, pingFromPod, addRemoveOnPod string)` to reduce duplication.
- [ ] **DRY**: testVipWithSG and testConnectivity both switch on `util.CheckProtocol(ip)` for ping command and conditions. Consider helpers e.g. `pingCommandForIP(ip string) string` and `addressSetConditionsForIP(ip, sgName string) string`.
- [ ] **Readability**: In testVipWithSG, `strings.Split(strings.ReplaceAll(string(output), "\n", ""), " ")` appears twice. Extract e.g. `parseOvnNbctlListOutput(output []byte) []string`.
- [ ] **Consistency**: Step numbering in ginkgo.By: "1. Test allowed address pair vip" then "3. Test vip with security group" then "3. Test switch lb vip". Renumber to 1, 2, 3 for consistency.
- [ ] **Error handling**: Line 381 `_ = vipClient.Create(upperCaseStaticIpv6Vip)` ignores error. Either use ExpectNoError and then assert status is empty, or document that Create may succeed but status stays empty.

---

## ./test/e2e/vpc-egress-gateway/e2e_test.go

- [ ] **Correctness / Naming**: Package is `multus` but the directory is `vpc-egress-gateway`. Rename package to match directory (e.g. `vpc_egress_gateway`) for consistency.
- [ ] **Correctness**: Line 223: `framework.ExpectNotEmpty(nodes)` is in a block that just loaded `kindNodes`; should be `framework.ExpectNotEmpty(kindNodes)` to assert the kind node list is not empty.
- [ ] **Correctness**: Line 433: `for ips := range maps.Values(intIPs)` — range with one variable over a slice yields the **index** (int), not the value. Use `for _, ips := range maps.Values(intIPs)` so `hops = append(hops, ips...)` appends the IP slices, not indices.
- [ ] **DRY**: The three ConformanceIt tests share repeated setup (create NAD, internal subnet, get docker network, generate external subnet, create external subnet, call vegTest). Consider extracting helpers e.g. `setupMacvlanVeg(f, nadName, externalSubnetName string) (provider, internalSubnetName string)` to reduce duplication.
- [ ] **Readability**: Replace magic numbers with named constants: `2` (hop count in CheckPodEgressRoutes), `10*time.Second` (patch timeout), port `8000 + rand.IntN(1000)` (e.g. `egressRouteHopCount`, `vpcPatchTimeout`, `netexecPortBase`).
- [ ] **Structure**: `vegTest` is ~180 lines. Consider splitting into smaller helpers: e.g. createSubnetsAndVeg, validateVegStatus, validateWorkloadPods, createServerPodAndCheckEgress to improve readability.
- [ ] **Maintainability**: In the underlay test, the linkMap loop (lines 213-223) uses `node` from `kindNodes` but the ExpectHaveKey uses `node.Name()` — ensure kind node name matches k8s node name for key consistency, or document the mapping.

---

## ./test/e2e/webhook/e2e_test.go

- [ ] **Documentation**: Add a one-line package comment describing that this package runs e2e tests for kube-ovn webhook validation, for discoverability.
- [ ] **DRY**: The init + TestE2E pattern (klog.SetOutput, CopyFlags, RegisterCommonFlags, RegisterClusterFlags, AfterReadingAllFlags, RunE2ETests) is duplicated across many e2e entrypoints (e.g. test/e2e/kube-ovn/e2e_test.go). Consider a shared helper in test/e2e/framework to reduce duplication.

---

## ./test/e2e/webhook/pod/pod.go

- [ ] **Readability**: Fix inconsistent alignment in annotations map (line 99): `util.IPAddressAnnotation:     staticIP` has extra spaces; align with other keys.
- [ ] **Error handling**: `cidrV4, _ := util.SplitStringIP(cidr)` and `firstIPv4, _ = util.FirstIP(cidrV4)` ignore errors; consider validating or skipping test with framework.Skipf when CIDR is invalid to avoid silent wrong behavior in dual-stack.
- [ ] **Readability**: Replace magic numbers `2*time.Second` and `time.Minute` in framework.WaitUntil with named constants (e.g. `podRoutedPollInterval`, `podRoutedTimeout`) for consistency with other e2e tests.
- [ ] **Structure**: The single ConformanceIt contains five validation steps (invalid IP, IP not in subnet, ippool not in subnet, static IP success, IP conflict). Split into separate ConformanceIt specs for better failure isolation and readability.
- [ ] **DRY**: The pattern "build annotations, set pod.Annotations, Create, ExpectError" is repeated four times. Extract a helper e.g. `expectPodCreateError(f *framework.Framework, podClient *framework.PodClient, namespace, name string, basePod *corev1.Pod, annotations map[string]string, expectedErrSubstr string)` to reduce duplication.
- [ ] **Maintainability**: Reusing and mutating the same `pod` variable across steps (pod.Annotations = ...) is fragile; consider building a fresh pod per step (e.g. framework.MakePod(...)) for clarity and to avoid accidental reuse of previous annotations.
- [ ] **Naming**: `ipCR` could be renamed to `podIPCRName` or `ipCRName` to clarify it is the IP CR resource name, not the CR object.

---

## ./test/e2e/webhook/subnet/subnet.go

- [ ] **Error handling**: `firstIPv4, _ = util.FirstIP(cidrV4)` and `firstIPv6, _ = util.FirstIP(cidrV6)` ignore errors; consider validating or skipping when CIDR is invalid (e.g. dual-stack edge cases).
- [ ] **DRY**: The first ConformanceIt repeatedly mutates the same subnet (Spec.Gateway, Spec.CIDRBlock, Spec.ExcludeIps, etc.) and calls Create + ExpectError. Extract a helper e.g. `expectSubnetCreateError(client *framework.SubnetClient, subnet *apiv1.Subnet, expectedErrSubstr string)` or build a fresh subnet per step to avoid mutation bugs.
- [ ] **Structure**: The first ConformanceIt contains seven validation steps (gateway, cidr conflict, excludeIPs, gateway type, protocol, allowSubnets, cidr). Consider splitting into separate ConformanceIt specs for better failure isolation.
- [ ] **Readability**: Magic strings "100.16.0.1", "10.1.1.302/24", "10.1.1.11..10.1.1.30..10.1.1.50" are test data; consider named constants (e.g. `invalidGateway`, `invalidAllowSubnetCIDR`, `invalidExcludeIPRange`) for documentation.
- [ ] **Naming**: Variable `ipr` could be renamed to `invalidExcludeIPRange` or `excludeIPsInvalidExample` to clarify intent.
- [ ] **Maintainability**: Reusing and mutating the same `subnet` across steps risks accidental reuse; consider building a new subnet (framework.MakeSubnet(...)) per validation step for clarity.

---

## ./test/e2e/webhook/vip/vip.go

- [ ] **Error handling**: `cidrV4, _ := util.SplitStringIP(cidr)` and `lastIPv4, _ = util.LastIP(cidrV4)` ignore errors; consider validating or skipping when CIDR is invalid.
- [ ] **DRY**: The pattern "mutate vip, Create, ExpectError" is repeated five times. Extract a helper e.g. `expectVipCreateError(client *framework.VipClient, vip *apiv1.Vip, expectedErrSubstr string)` to reduce duplication.
- [ ] **Structure**: The single ConformanceIt has five validation steps; split into separate ConformanceIt specs (empty subnet, wrong subnet, invalid v4ip, invalid v6ip, ip not in cidr) for better failure isolation.
- [ ] **Redundancy**: In BeforeEach, `subnetClient = f.SubnetClient()` is assigned twice (lines 24 and 35); remove the duplicate.
- [ ] **Consistency**: ginkgo.By mixes "validating" (lines 56, 61) and "Validating" (lines 66, 73, 78); use consistent capitalization (e.g. "Validating") for all steps.
- [ ] **Maintainability**: Reusing and mutating the same `vip` variable across steps is fragile; consider building a fresh vip per step (framework.MakeVip(...)) for clarity.

---

## ./test/server/server.go

- [ ] **Correctness**: In ReadSnmp, when index is len(snmpLine)-1, snmpLine[index+1] is out of bounds. Add guard: `if index+1 >= len(snmpLine) { continue }` before using snmpLine[index+1].
- [ ] **Correctness**: In main, preSnmp["Icmp"] and preSnmp["Tcp"] (and curSnmp) may be nil or missing if /proc/net/snmp format differs; direct key access can panic. Check key existence and handle missing sections (e.g. return error or skip metrics).
- [ ] **Error handling**: In ReadSnmp, `v, _ := strconv.Atoi(values[i])` ignores Atoi error; malformed values become 0. Consider logging or propagating error for debugging.
- [ ] **Robustness**: In ReadSnmp, if len(values) != len(keys), values[i] may panic. Use min(len(keys), len(values)) or bounds check in the inner loop.
- [ ] **Readability**: Replace magic numbers with named constants: ping count multiplier 100 (line 100), 100*time.Millisecond (curl loop interval), iperf3 "-t" "60" and "-b" "10M", "-l" "1K". Use config.DurationSeconds for iperf3 -t for consistency with duration-seconds flag.
- [ ] **Naming**: totalConnection in the curl goroutine is incremented every 100ms tick; it represents number of attempts, not necessarily "connections". Consider totalCurlAttempts or totalAttempts for clarity.
- [ ] **Error handling**: Line 164 `output, _ := json.MarshalIndent(result, "", "  ")` ignores Marshal error; handle or log for robustness.
- [ ] **Readability**: Result struct literal (lines 146-156) has inconsistent alignment; align fields for consistency.

---

## ./test/unittest/unit_suite_test.go

- [ ] **Naming**: TestE2e is misleading (this is the unit test suite, not e2e). Rename to TestUnit or TestSuite (e.g. TestUnitSuite) to avoid confusion.

---

## ./test/unittest/ipam/ipam.go

- [ ] **DRY**: IPv4, IPv6, and DualStack "normal subnet" It blocks are nearly identical (~80 lines each); same for "change cidr", "reuse released address", "do not reuse released address", "do not count excludedIps". Extract shared helpers (e.g. runNormalSubnetTest(im *ipam.IPAM, subnetName, cidr, gw string, excludeIPs []string, getFreeIP func() (v4, v6 string), isV6 bool)) or use table-driven tests to reduce duplication.
- [ ] **Maintainability**: Tests rely on internal struct access (im.Subnets[subnetName].V4Free.At(0).Start().String(), im.Subnets[subnetName].IPPools[""].V4Released). Consider exposing a minimal test helper in pkg/ipam (e.g. NextFreeV4(subnetName) string) or document that tests depend on internals.
- [ ] **Structure**: Each "normal subnet" It is ~80 lines with many ginkgo.By steps; consider splitting into smaller It specs (e.g. "static ip", "multiple nics", "conflict", "release and reuse", "invalid subnet") for failure isolation.
- [ ] **Tech debt**: TODO line 37: "test case use random ip and ip cidr, and input test data should separate from test case" — track or implement.
- [ ] **Readability**: In DualStack GetStaticAddress the return order is (ip4, ip6, ...); in IPv4-only the test uses first return, in IPv6 the second. Add a short comment or helper to clarify return order and avoid mix-up.

---

## ./test/unittest/ipam/ip.go

- [ ] **Readability**: In IPv6 It, ip1Str and ip2Str are reassigned (lines 57-58) and ip1, ip2 are recreated (62-66), shadowing the earlier ip1, ip2. Use distinct names after re-parse (e.g. ip1Norm, ip2Norm or ip1Parsed, ip2Parsed) to avoid confusion.
- [ ] **Correctness**: IPv6 n2 := [4]uint32{..., n1[3] + 1}; if n1[3] were 0xffffffff (not already decremented), n1[3]+1 would overflow. The guard above only decrements when all four are 0xffffffff; consider also guarding n1[3] == 0xffffffff when computing n2 for clarity.

---

## ./versions/version.go

- [ ] **Naming**: COMMIT, VERSION, BUILDDATE use SCREAMING_CASE (Go idiom reserves for constants). These are vars set by ldflags; consider exported mixed case (Commit, Version, BuildDate) for consistency with Go style.
- [ ] **Readability**: Function String() is generic; consider VersionString() or Info() to clarify it returns version info and avoid confusion with types implementing Stringer.

---

## ./test/unittest/ipam/ip_range.go

- [ ] **DRY**: IPv4 and IPv6 It blocks are structurally similar (NewIPRange, Contains, Clone, Count, NewIPRangeFromCIDR, Random). Consider extracting a shared helper (e.g. runIPRangeTest(startStr, endStr, ipStr string, isV6 bool)) or table-driven test to reduce duplication.
- [ ] **Readability**: Replace magic number 100 in "for i := uint32(0); i < 100 && i <= n2-n1" and "for range 100" with a named constant (e.g. randomSamplesCount).
- [ ] **Readability**: In IPv6 block, variable `n` is redefined (line 102) after being used in the comparison loop; the inner `n` is the random point. Consider naming the inner one `nMid` or `nRand` to avoid confusion with the loop logic above.

---

## ./test/unittest/ipam/ip_range_list.go

- [ ] **Correctness**: In "IPv4 NewIPRangeListFrom", the loop `for i := 0; i < len(ints); i++` can use `ints[i+1]` when `cidrSet.Has(ints[i])` (lines 364-371) or in the rand branch (lines 375-384). When `i == len(ints)-1`, `ints[i+1]` is out of bounds. Add guard `i+1 < len(ints)` before accessing `ints[i+1]`.
- [ ] **DRY**: IPv4 Contains / IPv6 Contains (and Add, Remove, Separate) are nearly identical except IP string literals. Consider table-driven test with (name string, newList func() (ipam.IPRangeList, error), ipCases []string) or a helper to reduce duplication.
- [ ] **Tech debt**: "IPv6 NewIPRangeListFrom" It is empty with only "// TODO"; implement or track.
- [ ] **Structure**: "IPv4 NewIPRangeListFrom" It is ~100 lines and complex; consider extracting helpers (e.g. buildCIDRList, buildIPList, assertListEquals) for readability.

---

## ./test/unittest/util/net.go

- [ ] **Readability**: In AddressCount and CountIPNums, args and wants are parallel slices; consider a single slice of struct { arg T; want float64 } for table-driven clarity and to make the relation explicit.
- [ ] **Readability**: ExpandExcludeIPs test has many inline arg/want pairs; consider extracting test cases to a named variable or sub-tests for easier addition of new cases.

---

## ./test/unittest/ipam_bench/ipam_test.go

- [ ] **Correctness**: delIPAMSubnet uses subnetName := fmt.Sprintf("test%d", index) but addIPAMSubnet uses fmt.Sprintf("subnet%d", index). So Add benchmarks create "subnet0", "subnet1", ... while Del benchmarks delete "test0", "test1", ... — subnets are never actually deleted. Fix delIPAMSubnet to use "subnet%d" so it deletes the same subnets that were added.
- [ ] **Naming / Typo**: addSerailAddrCapacity should be addSerialAddrCapacity (and all call sites).
- [ ] **Readability**: Replace magic numbers with named constants: 10000 (time trace step), 1000 (subnet count in parallel bench), 3000 (pod count in parallel bench), e.g. timeTraceStep, parallelSubnetCount, parallelPodCount.
- [ ] **DRY**: BenchmarkIPAMSerial* and BenchmarkIPAMRandom* follow the same pattern (im := ipam.NewIPAM(), add*Capacity, optionally ResetTimer(), del*Capacity). Consider a generator or shared setup helper to reduce repetition; lower priority for benchmark code.

---

## ./tools/modernize/modernize.go

- [ ] **Correctness (Go 1.23+)**: `for f := range slices.Values(pass.Files)` yields the **index** (int), not the element. Use `for _, f := range pass.Files` (or `for _, f := range slices.Values(pass.Files)`) so `f` is *ast.File. Same for `for comment := range slices.Values(f.Comments)` — use `for _, comment := range f.Comments` so comment is the actual comment group.
- [ ] **Readability**: Variable name `f` in the outer loop shadows the common use of `f` for *os.File; using `file` or `astFile` would avoid confusion.

---

## ./Makefile

- [ ] **DRY / Structure**: `install-chart` and `upgrade-chart` share nearly identical long lists of `--set` and `--set-json` (lines 184–206 vs 210–232). Extract a shared variable (e.g. `HELM_CHART_SET_ARGS`) or include a fragment so both targets use the same options and stay in sync.
- [ ] **DRY**: `install-chart-v2` and `upgrade-chart-v2` share the same helm set options; use a variable for the common part to avoid drift.
- [ ] **Readability**: The `define docker_config_bridge` block (108–141) is long and nested; the vlan_filtering branch has complex awk/sed. Consider splitting into smaller defines or moving logic to a script (e.g. hack/ or dist/images/) for readability and testability.
- [ ] **Readability**: Magic numbers 60 (retries in `kubectl_wait_exist`), 60s (rollout/timeout in several targets). Use named variables (e.g. `KUBECTL_WAIT_RETRIES = 60`, `KUBECTL_ROLLOUT_TIMEOUT = 60s`) for consistency and tuning.
- [ ] **Maintainability**: Group all `*_VERSION` and related `*_IMAGE`/URLs in one section with a short comment so dependency bumps stay in one place (renovate comments are already helpful).
- [ ] **Structure**: Heavy shell in `docker_network_info` and `docker_config_bridge` could be moved to shell scripts (e.g. hack/) and invoked from the Makefile for easier debugging and reuse.
- [ ] **Readability**: In `docker_ensure_image_exists`, the grep pattern `"^$(1)$$"` is easy to misread; a short comment or variable (e.g. `IMAGE_REF = $(1)`) could clarify intent.
- [ ] **Performance / Maintainability**: `install-chart` and `upgrade-chart` run `kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw"` inside the helm command; on large clusters this runs every time. Consider caching in a variable or documenting cost.

---

## ./dist/images/01-kube-ovn.conflist

(No refactoring suggestions — minimal CNI conflist JSON.)

---

## ./dist/images/bfdd-prestart.sh

(No refactoring suggestions — short BFD session config script.)

---

## ./dist/images/clean-ic-az-db.sh

- [ ] **Correctness**: For `filter_type` "node", `availability_zone_uuid` is echoed on line 23 before it is set (it is set only in the "node" branch on lines 25–27). The script therefore echoes an empty value for the node case. Move the `echo` after both branches (e.g. after line 27) or remove it if it was only for debugging.
- [ ] **Readability**: Fix usage message "use method" → "Usage" (or "usage:") for standard help wording.
- [ ] **Structure**: Set `availability_zone_uuid` in both branches (az and node), then run the single validation block `if ! ovn-ic-sbctl get availability_zone ...` once. This clarifies control flow and avoids the echo-before-set bug.
- [ ] **Maintainability**: Add a short comment at the top describing the script purpose (clean OVN IC DB resources for a given AZ or node).

---

## ./dist/images/cleanup.sh

- [ ] **Typo**: Line 80 comment "provier-networks" → "provider-networks".
- [ ] **DRY**: Many loops follow the same pattern (`for x in $(kubectl get <resource> -o name); do kubectl delete --ignore-not-found $x; done`). Consider a list of resource types (e.g. `vpc-nat-gw vpc-dns vip snat dnat ...`) and a single loop or helper function to reduce duplication and ease adding new resources.
- [ ] **DRY**: The two wait loops (pinger pods, cni pods) are similar; extract e.g. `wait_until_pods_gone namespace label` to avoid duplication.
- [ ] **Structure**: The long list of CRD names (lines 115–136) could be a single array and iterated; same for node/ns annotation removals to keep the script maintainable when new annotations are added.
- [ ] **Maintainability**: Add a short comment at the top describing the script purpose (full teardown of Kube-OVN and related resources).

---

## ./dist/images/db_autocheck_script.sh

- [ ] **Correctness**: In the else branch (line 26), `nodeIps=\`kubectl get node ... | awk '{print $6}'\`` assigns a space-separated string; later `${nodeIps[0]}` and `${nodeIps[@]}` may not behave as an array in all shells. Use `nodeIps=( $(kubectl get node ... | awk '{print $6}') )` to make it a proper array.
- [ ] **Naming**: Function name `DBabnormal` is unclear; consider `handle_db_abnormal` or `on_db_abnormal`. Align style with `restoreNB` (e.g. snake_case for both).
- [ ] **DRY**: The NB and SB db status check loops (lines 84–92 and 94–102) are nearly identical; extract e.g. `check_db_storage_status pod ctl_path db_name` and call for both NB and SB.
- [ ] **DRY**: The NB and SB raft check blocks (lines 105–118 and 120–133) are nearly identical; extract e.g. `check_raft_leader service_name endpoint_label` to reduce duplication.
- [ ] **Readability**: Magic string `"ok"` for DBSTATUS and `1` for expected match count; use named constants or variables.
- [ ] **Maintainability**: Remove or gate the commented block (lines 75–82) behind a debug flag instead of leaving dead code.

---

## ./dist/images/del-redundant-ips.sh

- [ ] **Typo**: Line 78 "exist" → "exit".
- [ ] **Correctness**: Line 63 `grep $ip` can misbehave if `$ip` contains regex metacharacters (e.g. dots). Use `grep -F "$ip"` or anchor the pattern.
- [ ] **Readability**: Variable names `DIP`, `IN` are terse; consider `redundant_ips` and `found` (or `ip_in_use`) for clarity. Add a short comment block describing the algorithm (collect IP CRs, collect pod IPs, find IPs not in use, prompt, delete).
- [ ] **Error handling**: `read -p` requires a TTY; when run non-interactively (e.g. cron or CI) the script may hang or read from stdin. Consider a `--yes` or `-y` flag for non-interactive mode.
- [ ] **Performance**: The nested loop (for each ip in IPS, for each pod in PODS) is O(n*m). For large clusters, consider using an associative array or `sort | uniq`/`comm` for set difference.

---

## ./dist/images/Dockerfile

- [ ] **Readability**: Add a short top comment explaining the two-stage build: setcap stage applies capabilities to binaries, final stage produces the runtime image with logrotate and scripts.
- [ ] **Maintainability**: The single RUN (ln -s and setcap) is long; add inline comments to separate logical groups (symlinks for multi-binary components vs setcap for capabilities) for easier future edits.
- [ ] **Extensibility**: Symlinks (kube-ovn-monitor, kube-ovn-speaker, webhook, leader-checker, ovn_ic_controller from cmd; kube-ovn-pinger from controller) are listed explicitly. If more components are added, consider a small script or loop with a mapping (name -> base binary) to reduce repetition.
- [ ] **Documentation**: Document ARG BASE_TAG=$VERSION (e.g. BASE_TAG defaults to VERSION for the base image tag) so builders know the relationship.
- [ ] **Maintainability**: Add a brief comment for `iptables-wrapper-installer.sh --no-sanity-check` (e.g. skip sanity check because container environment may not have full network stack) to avoid accidental removal.

---

## ./dist/images/Dockerfile.base

- [ ] **Consistency**: Lines 125-127 use tab characters for comment indentation; the rest of the file uses spaces. Use spaces throughout for consistency.
- [ ] **Correctness**: Line 127 has trailing whitespace after the backslash; remove it to avoid potential parse or copy-paste issues.
- [ ] **Maintainability**: The patch list is duplicated: ADD patches/... (lines 39-66) and git apply $SRC_DIR/... in RUN blocks (71-133). Adding or reordering a patch requires editing both. Add a short comment above each block reminding to keep them in sync, or consider a build-time script that generates the RUN from a single list.
- [ ] **Readability**: In openssl-builder stage, `cd openssl-*` (line 17) is fragile if multiple directories match. Use an explicit path or `cd $(ls -d openssl-* | head -1)` for robustness.
- [ ] **Structure**: The final-stage RUN (lines 221-234) repeats the same setcap pattern many times. Consider a shell loop over a list of (capabilities,binary) pairs to reduce repetition and make adding new binaries easier.

---

## ./dist/images/Dockerfile.base-dpdk

- [ ] **Maintainability**: Patch list is duplicated: ADD patches (lines 11-22) and git apply in RUN blocks (28-58). Adding or reordering a patch requires editing both. Consider a build-time script that generates RUN from a single list, or add a comment reminding to keep them in sync (same as Dockerfile.base).
- [ ] **Readability**: Fix comment grammar on line 27: "judge it support the avx512" → "to check if the build machine supports AVX-512" (or similar).
- [ ] **DRY**: CONFIGURE_OPTS with ARCH check is duplicated in ovs build (lines 86-88) and ovn build (94-96). Extract to a shared ARG or small script to avoid divergence when adding new ARCHs.
- [ ] **Extensibility**: Hardcoded OVS branch (branch-3.5), OVN branch (branch-25.03), and DPDK_VERSION (24.11.2). Consider ARGs for OVS_BRANCH, OVN_BRANCH so upgrades do not require editing multiple lines.
- [ ] **Readability**: Long apt install list (lines 109-117) could be split into logical groups with inline comments (e.g. base, networking, ipsec, build deps).
- [ ] **Consistency**: Second stage redefines ARG DPDK_VERSION=24.11.2 (line 125); ensure build is invoked with same DPDK_VERSION for both stages or document that ARG must be passed consistently.

---

## ./dist/images/Dockerfile.test

- [ ] **Reproducibility**: `alpine:edge` is a moving target; consider pinning to a specific version or digest (e.g. `alpine:3.19`) for reproducible e2e test image builds.
- [ ] **Readability**: In the single RUN, package list could be grouped with inline comments (e.g. network tools, services, benchmarking) to make additions easier.

---

## ./dist/images/env-check.sh

- [ ] **Correctness**: `for file in $(ls "/etc/cni/net.d")` (line 9) is fragile with filenames containing spaces; use `for file in /etc/cni/net.d/*` or iterate with `find`.
- [ ] **Correctness**: Unquoted variables in tests (e.g. `[ $probe_mtu == 0 ]`, `[ $recycle == 1 ]`) can fail when empty; use `[ "$probe_mtu" = 0 ]` and quote variables; prefer `=` for POSIX.
- [ ] **Correctness**: `[[ $result > 1 ]]` (lines 46, 51, etc.) does string comparison; for numeric use `[ "$result" -gt 1 ]` or `(( result > 1 ))`.
- [ ] **Naming**: Fix typo "mannully" → "manually" (lines 30, 75).
- [ ] **DRY**: Sections 5 (firewall/security checks) repeat the same pattern (ps + grep + wc, then if > 1); extract a helper e.g. `warn_if_running "pattern" "message"` to reduce duplication.
- [ ] **Structure**: Consider extracting each numbered check into a function (e.g. check_cni_config, check_ipv4_config) for readability and easier testing.

---

## ./dist/images/generate-ssl-docker.sh

- [ ] **Correctness**: Quote `$PWD` in volume mount: `-v "$PWD":/etc/ovn` to handle paths with spaces.
- [ ] **Maintainability**: Hardcoded image tag `kubeovn/kube-ovn:v1.7.1`; consider env var (e.g. KUBE_OVN_IMAGE) or script argument for version flexibility.
- [ ] **Readability**: `rm -rf` on single .pem files; `-r` is unnecessary, use `rm -f` for clarity.

---

## ./dist/images/generate-ssl.sh

- [ ] **Security**: `chmod 666` makes cert and private key world-readable/writable; consider 644 for certs and 600 for ovn-privkey.pem (or at least 640) to limit exposure.
- [ ] **Readability**: Paths `/etc/ovn` and `/var/lib/openvswitch/pki` could be assigned to variables (e.g. OVN_SSL_DIR, OVS_PKI_DIR) for single place to change and clarity.

---

## ./dist/images/go-deps/download-go-deps.sh

- [ ] **Correctness**: Quote `$f` in `name=$(basename $f)` (line 33) to handle paths with spaces.
- [ ] **Robustness**: Use `while IFS= read -r f` instead of `while read f` to avoid stripping leading/trailing whitespace and to handle lines without trailing newline; `read -r` avoids backslash interpretation.
- [ ] **Readability**: Consider extracting the trivy + targets-file block (lines 25-50) into a function or adding a brief comment to separate "download deps" from "scan and record versions".

---

## ./dist/images/go-deps/rebuild-go-deps.sh

- [ ] **Correctness**: Line 59 references `$f` but the loop variable is `$t`; use `$name` or `$t` in the error message (typo).
- [ ] **Correctness**: Line 65 `for f in $(ls "$TRIVY_DIR")` is fragile (spaces in filenames); use `for f in "$TRIVY_DIR"/*`; quote in `f=$(basename "$f")` (line 66).
- [ ] **Correctness**: Quote variables in tests: `[ "$type" = "commit" ]` (line 41), and in echo/cut (lines 49-50) use `"$KUBE_GIT_VERSION"`.
- [ ] **Readability**: Variable `type` shadows bash builtin; rename to e.g. `obj_type` to avoid confusion.
- [ ] **DRY**: build_flags for loopback|macvlan and portmap are identical except plugin path (main vs meta); consider a single var for ldflags and only vary the module path.
- [ ] **Security**: `eval $GO_INSTALL $build_flags ...` (lines 25, 29) expands variables in eval; prefer building the command without eval (e.g. use an array and "${arr[@]}") to avoid injection if versions are ever externalized.
- [ ] **Structure**: The kubectl case (lines 31-54) is long; consider extracting to a function e.g. build_kubectl "$version" for readability.

---

## ./dist/images/grace_stop_ovn_controller

- [ ] **Correctness**: In the `*)` branch (line 20) `dir0=./` is set but `ovsdir` is not set; when the script is invoked without a path (e.g. `grace_stop_ovn_controller`), `ovsdir` may be empty and `. "$ovsdir/ovs-lib"` can fail. Set `ovsdir` for the no-path case (e.g. derive from PATH or a default).
- [ ] **Readability**: The case $0 block (lines 15-21) that derives dir0 and ovsdir is dense; a brief comment explaining "resolve script directory and OVS scripts directory" would help.

---

## ./dist/images/install-cilium-cli.sh

- [ ] **Extensibility**: Consider allowing CILIUM_CLI_ARCH to be overridden via env (e.g. `CILIUM_CLI_ARCH=${CILIUM_CLI_ARCH:-$(...)}`) so callers can force arch without editing the script.
- [ ] **Readability**: If more architectures are added later, use a small mapping (e.g. case or associative array) from uname -m to arch name for clarity.

---

## ./dist/images/install-cni.sh

- [ ] **Correctness**: Quote `"$@"` in line 40 (./kube-ovn-daemon ... $@) to preserve arguments with spaces.
- [ ] **Correctness**: Iterating over POD_IPS with `$(echo "${POD_IPS}" | tr ',' ' ')` can be fragile with spaces in IPs; consider `IFS=',' read -ra ips <<< "$POD_IPS"` and iterating over `"${ips[@]}"` (or handle empty POD_IPS explicitly).
- [ ] **DRY**: The four cp lines (36-39) share the same pattern; consider a loop over (SRC DST) pairs or a helper e.g. copy_bin src dst to reduce repetition.
- [ ] **Readability**: `yes | cp -f` is redundant (cp -f overwrites without prompting); remove `yes |` unless a specific cp implementation is known to prompt.
- [ ] **Structure**: Define exit_with_error at the top of the script so it is available and visible before first use.

---

## ./dist/images/install-ic-server.sh

- [ ] **Correctness**: In the generated YAML, `value: $addresses` (line 97) is unquoted; if addresses contained spaces or special characters the YAML could be invalid. Use quoted output e.g. `value: "$addresses"` (or ensure the value is YAML-escaped).
- [ ] **Correctness**: When no nodes match the selector, `count` is 0 and replicas would be 0; consider checking count > 0 and exiting with a clear message if no master nodes are found.
- [ ] **Maintainability**: REGISTRY and VERSION at top are good; add a brief comment that these are the main knobs for image, or document in a header comment.
- [ ] **Structure**: The embedded YAML (cat <<EOF) is long; consider extracting to a separate template file and using envsubst (or similar) for REGISTRY, VERSION, count, addresses, etc., to improve readability and reuse.

---

## ./dist/images/install.sh

- [ ] **Naming**: Fix typo "diffierent" → "different" in comment (line 42, DPDK_TUNNEL_IFACE).
- [ ] **Structure**: The script is ~6200+ lines and monolithic; consider splitting into sourced modules (e.g. config.sh, step-ssl.sh, step-labels.sh, step-ovn.sh, step-cni.sh, step-controller.sh, step-pinger.sh) or at least extracting each "[Step N/6]" block into a function for readability and testability.
- [ ] **DRY**: The pattern for getting master node addresses and count (kubectl get no -lkube-ovn/role=master ...) appears in install.sh and install-ic-server.sh; consider a shared helper or small script.
- [ ] **Readability**: Large embedded YAML heredocs (cat <<EOF) for each component; consider moving to separate template files and using envsubst/sed for variable substitution to improve maintainability and reuse.
- [ ] **Consistency**: Prefer quoting in kubectl label selectors (e.g. -l"$LABEL" instead of -l$LABEL) when LABEL could contain special characters; and use consistent test style ([ ] vs [[ ]]).
- [ ] **Maintainability**: Add a short header comment describing the script purpose, main env vars, and the six steps at a high level so new contributors can navigate the file.

---

## ./dist/images/iptables-wrapper-installer.sh

- [ ] **Portability**: Replace deprecated `[ -d /usr/sbin -a -e /usr/sbin/iptables ]` (lines 38, 40) with separate tests: `[ -d /usr/sbin ] && [ -e /usr/sbin/iptables ]` for POSIX compliance (-a is deprecated).

---

## ./dist/images/init-vpc-egress-gateway.sh

- [ ] **DRY**: The IPv4 block (lines 21–57) and IPv6 block (59–96) are nearly identical (ip rules, routes, iptables/ip6tables). Extract a function parameterized by address family (e.g. `setup_egress_for_family 4` and `setup_egress_for_family 6`) to reduce duplication and keep both stacks in sync.
- [ ] **Readability**: Magic numbers for routing table and rule priorities (1000, 1001–1004) should be named constants at the top (e.g. `ROUTE_TABLE_EGRESS=1000`, `PRIORITY_INTERNAL=1001`, etc.) for documentation and tuning.
- [ ] **Readability**: The pattern `ip -o route get "${ADDR}" | grep -o 'src [^ ]*' | awk '{print $2}'` (and the `dev` variant) is repeated multiple times; extract a small helper (e.g. `get_src_ip 4 "${ADDR}"`, `get_dev_for 4 "${ADDR}"`) or add a short comment explaining the pipeline.
- [ ] **Correctness / Maintainability**: Line 99 uses `sysctl net/ipv4/conf/${internal_iface}/rp_filter=0` after both blocks; when both IPv4 and IPv6 are set, `internal_iface` is from the last block (IPv6). If IPv4 and IPv6 use different interfaces, rp_filter is only applied to one. Consider applying rp_filter for both internal interfaces when both families are configured, or document that a single internal interface is assumed.
- [ ] **Robustness**: No validation that INTERNAL_GATEWAY_IPV4 and EXTERNAL_GATEWAY_IPV4 are both set or both unset (same for IPv6). Consider an early check and clear error message to avoid partial or confusing state.
- [ ] **DRY**: The loop `for priority in 1001 1002 1003 1004` and the ip rule del/add logic is duplicated between IPv4 and IPv6; moving into the proposed family-scoped function would remove this duplication.

---

## ./dist/images/kubectl-ko

- [ ] **Structure / Maintainability**: Script is monolithic (~1100 lines). Consider splitting into sourced modules (e.g. helpers.sh for ipv4_to_hex, expand_ipv6, ipIsInCidr; trace.sh; diagnose.sh; perf.sh; dbtool.sh; log.sh) for readability and testability.
- [ ] **Naming**: Fix typo "Prepareing" → "Preparing" in addHeaderDecoration argument (line 908).
- [ ] **Consistency**: getOvnCentralPod and reload() use hardcoded `kube-system` in several places (e.g. NORTHD_POD, image lookup, reload delete/rollout); use KUBE_OVN_NS throughout so the script respects the namespace variable.
- [ ] **DRY**: The pattern to get a pod on a specific node (`kubectl get pod -n $KUBE_OVN_NS -l app=... -o 'jsonpath={.items[?(@.spec.nodeName=="'$node'")].metadata.name}'`) is repeated many times; extract a helper e.g. get_pod_on_node(namespace, label_selector, node_name).
- [ ] **Readability / Structure**: trace() is ~250 lines with deep nesting; extract sub-functions (e.g. resolve_lsp_and_namespace, get_dst_mac, build_ovn_trace_cmd) to shorten and clarify.
- [ ] **Correctness**: In trace(), error message "Error: no ovs-ovn Pod running on node $nodeName" uses $nodeName which is unset when the target is node//nodename (only $node is set); use $node for the message.
- [ ] **Correctness**: quitPerfTest uses `if [ ! $? -eq 0 ]` to decide whether to print failure log; $? at that point is from the previous command in the script, not the perf test result. Capture exit status when the test fails (e.g. via a trap that saves $?) and use it in quitPerfTest.
- [ ] **Portability**: In ipIsInCidr (IPv4), replace deprecated `[ ... -a ... ]` with separate tests and `&&`: `[ $ip_dec -gt $network_dec ] && [ $ip_dec -lt $broadcast_dec ]`.
- [ ] **Readability**: Magic numbers (PERF_TIMES=5, 30s rollout timeout, 29 retries in checkDeployment, 300 in perf wait loop, 10256 kube-proxy port, 8100/8101 conn-check ports) could be named constants at the top for tuning and documentation.
- [ ] **Naming**: Fix typo "availabelNum" → "availableNum" in dbtool restore (lines 663–666).
- [ ] **Readability**: In trace(), prefer `[ -z "$namespace" ]` over `[ ! -n "$namespace" ]` for idiomatic shell.

---

## ./dist/images/logrotate/kubeovn

- [ ] **Maintainability**: The empty `postrotate`/`endscript` block may confuse future maintainers. Add a one-line comment (e.g. `# no post-rotate command needed`) to document that it is intentional.

---

## ./dist/images/logrotate/openvswitch

- [ ] **Robustness**: When no `.ctl` files exist, `for ctl in /var/run/openvswitch/*.ctl` may run once with literal `*.ctl` (depending on shell), causing a no-op failure. Consider a guard (e.g. `[ -e /var/run/openvswitch ] && for ctl in ...`) or document that `2>/dev/null || :` is sufficient.
- [ ] **DRY / Maintainability**: The logrotate pattern (daily, rotate 7, size 100M, compress, sharedscripts, missingok, postrotate loop over ctl with appctl vlog/reopen) is nearly identical to `dist/images/logrotate/ovn`. Consider documenting the intentional similarity or extracting a small shared helper script parameterized by log dir, run dir, and appctl binary to keep behavior in sync.

---

## ./dist/images/logrotate/ovn

- [ ] **Robustness**: When no `.ctl` files exist, `for ctl in /var/run/ovn/*.ctl` may run once with literal `*.ctl` (depending on shell). Consider a guard (e.g. `[ -e /var/run/ovn ] && for ctl in ...`) or document that `2>/dev/null || :` is sufficient.
- [ ] **DRY**: Same logrotate/postrotate pattern as `dist/images/logrotate/openvswitch`; see openvswitch refactor for shared helper or documentation.

---

## ./dist/images/ovn-healthcheck.sh

- [ ] **DRY**: NB and SB checks are duplicated (get status/role, check "failed", check "disconnected" + "candidate"). Extract a function e.g. `check_db_health(db_name, ctl_path)` that runs `ovn-appctl -t "$ctl_path" cluster/status`, parses Status and Role, and exits with message when "failed" or when "disconnected" and "candidate".
- [ ] **Readability**: The condition `if ! echo ${nb_status} | grep -v "failed"` is obscure (we exit when "failed" is in nb_status). Prefer explicit: `if echo "${nb_status}" | grep -q "failed"; then echo "nb health check failed"; exit 1; fi`. Same for sb_status and for the disconnected+candidate checks.
- [ ] **Readability / Maintainability**: Hardcoded ctl paths `/var/run/ovn/ovnnb_db.ctl` and `ovnsb_db.ctl` could be named variables at the top (e.g. `NB_CTL`, `SB_CTL`) for documentation and future configurability.

---

## ./dist/images/ovn-ic-db-docker.sh

- [ ] **Maintainability**: Image tag `v1.13.0` is hardcoded. Consider using an env var (e.g. `KUBE_OVN_IMAGE_TAG`) or add a comment that this is an example and should be updated for releases.

---

## ./dist/images/ovn-ic-healthcheck.sh

- [ ] **DRY**: NB/SB checks duplicated (same pattern as `ovn-healthcheck.sh`). Extract a function e.g. `check_db_health(db_name, ctl_path, cluster_name)` and reuse; or source a shared helper from both ovn-healthcheck.sh and ovn-ic-healthcheck.sh to avoid divergence.
- [ ] **Readability**: Same obscure `if ! echo ${var} | grep -v "failed"` as ovn-healthcheck.sh; prefer explicit `if echo "${var}" | grep -q "failed"; then ... fi`. Same for disconnected+candidate checks.
- [ ] **Readability**: Hardcoded ctl paths (`ovn_ic_nb_db.ctl`, `ovn_ic_sb_db.ctl`) could be named variables at the top.

---

## ./dist/images/ovn-is-leader.sh

- [ ] **DRY**: The SSL vs non-SSL `ovsdb-client query` block is duplicated for nb_leader (lines 16-19), sb_leader (38-41), and the steal block (55-58). Extract a helper e.g. `ovsdb_client_cmd(port, subcmd, args...)` that picks tcp vs ssl and key/cert/cacert based on ENABLE_SSL.
- [ ] **DRY**: The pattern "if leader then kubectl label ... ovn-X-leader=true else kubectl label ... ovn-X-leader-" repeats for nb, northd, sb. Extract e.g. `set_leader_label(component, is_leader)` to reduce duplication.
- [ ] **Readability**: Magic ports 6641, 6642 could be named constants at the top (e.g. `NB_PORT=6641`, `SB_PORT=6642`) for documentation and tuning.
- [ ] **Structure**: The northd_leader / steal block (lines 47-61) is nested and complex. Extract to a function e.g. `maybe_release_northd_lock()` to shorten the main flow and clarify intent.
- [ ] **Maintainability**: Hardcoded paths `/var/run/ovn/ovnnb_db.ctl`, `ovnsb_db.ctl`, `/var/run/ovn/ovn-northd.*.ctl` could be variables or documented as fixed OVN layout.

---

## ./dist/images/ovsdb-inspect.sh

- [ ] **Naming**: Fix typo "chould" → "could" in comment (line 28).
- [ ] **DRY**: The pattern `$(kubectl -n kube-system get pods -o wide | grep ovs-ovn | awk '{print $1}')` appears in init-ovs-ctr and in the main loop. Extract e.g. `get_ovs_ovn_pod_names` to output pod names and reuse.
- [ ] **Robustness**: In `ovs-exec`, use quoted `"$1"` (and `"$2"` if appropriate) so pod names or commands with spaces do not break.
- [ ] **Consistency**: Hardcoded `kube-system` in multiple places; consider a variable at the top (e.g. `KUBE_OVN_NS`) for consistency with other scripts.

---

## ./dist/images/ovs-dpdk-config

- [ ] **Maintainability**: Add a one-line header comment describing that this file is the OVS-DPDK config and ENCAP_IP/DPDK_DEV should be set per node.

---

## ./dist/images/ovs-dpdk-healthcheck.sh

- [ ] **Readability**: Magic timeout `3` in ovn-sbctl could be a named variable at the top (e.g. `OVN_SBCTL_TIMEOUT=3`) for tuning and documentation.

---

## ./dist/images/ovs-healthcheck.sh

- [ ] **DRY**: The ENABLE_SSL branch for `ovsdb-client` (tcp vs ssl with key/cert/cacert) is duplicated across scripts; consider sourcing a shared helper for ovsdb-client invocation.
- [ ] **Readability**: In `gen_conn_str`, the parameter `$1` is the port; add a short comment or rename to e.g. `gen_sb_conn_str(port)`. Magic port 6642 could be `SB_PORT=6642` at top.
- [ ] **Robustness**: Use `if [ -e "$file" ]` instead of `if [ -e $file ]` to avoid word splitting when `file` contains spaces.
- [ ] **Structure**: The log check block (tail -6, match "stale data", run sb-cluster-state-reset) could be extracted to a function e.g. `check_ovn_controller_stale_and_reset()` for readability and reuse.

---

## ./dist/images/remove-finalizer.sh

- [ ] **Robustness**: When `kubectl get` returns no resources, the loop runs with empty input; consider skipping or exiting cleanly when there is nothing to patch (e.g. check `[[ -z "$(kubectl get ...)" ]]` or use `--no-headers` and test for empty).
- [ ] **Maintainability**: Add a short comment at the top describing purpose (e.g. remove finalizers from subnet/vpc/ip resources for cleanup or recovery).
- [ ] **Error handling**: With `set -e`, the first failed `kubectl patch` aborts the whole script; consider continuing on patch failure and reporting at the end, or document that partial failure is intentional.

---

## ./dist/images/restore-ovn-nb-db.sh

- [ ] **Consistency**: Use `$KUBE_OVN_NS` instead of hardcoded `kube-system` on lines 14–18 so a single variable controls the namespace.
- [ ] **Robustness**: Quote variables in tests and commands (e.g. `[ "$nodeIp" = "$hostip" ]`, `kubectl get deployment -n "$KUBE_OVN_NS"`) to avoid word splitting and empty arguments.
- [ ] **Maintainability**: Prefer `$(...)` over backticks for command substitution (e.g. line 20).
- [ ] **Robustness**: Script does not use `set -e`; failed `kubectl` or `ovsdb-tool` could leave the cluster in a bad state. Consider `set -e` and explicit error messages for critical steps.
- [ ] **Compatibility**: `kubectl exec -it` allocates a TTY; in non-interactive contexts this can cause issues. Prefer `kubectl exec -i` when script is run from cron or CI.
- [ ] **Readability**: Add a short comment that `ovsdb-tool cluster-to-standalone` and `mv /etc/ovn/...` are intended to run on the host where the DB file is available (or document the expected run environment).

---

## ./dist/images/start-cniserver.sh

- [ ] **Robustness**: In `quit` and the `rm` call, quote `$CNI_SOCK` (e.g. `rm -rf "$CNI_SOCK"`) to handle paths with spaces.
- [ ] **Readability**: Consider a one-line comment that `ovs-appctl -T 1` uses a 1-second timeout for the readiness check.

---

## ./dist/images/start-controller.sh

- [ ] **Robustness**: Use `"$@"` instead of `$@` in the exec line so arguments with spaces are preserved.
- [ ] **Readability**: Document or name the port parameter in `gen_conn_str` (e.g. 6641/6642); consider `NB_PORT=6641` and `SB_PORT=6642` at the top and pass by name.

---

## ./dist/images/start-db.sh

- [ ] **Correctness**: Line 13 echoes `OVN_LEADER_PROBE_INTERVAL` but it is not set in this script; ensure it is set before use or default it (e.g. `OVN_LEADER_PROBE_INTERVAL=${OVN_LEADER_PROBE_INTERVAL:-...}`).
- [ ] **Naming / Correctness**: `get_leader_ip` ignores its argument (nb/sb) and always returns the first entry of `NODE_IPS`. Call sites pass `nb` and `sb`; either implement per-database leader resolution in `get_leader_ip` or rename to e.g. `get_first_node_ip` and drop the unused argument.
- [ ] **Maintainability**: Fix typo in comment: "corrputed" → "corrupted" (line 155).
- [ ] **DRY**: `ovn_ctl_args` is built in four large blocks (non-SSL leader/follower, SSL leader/follower) with repeated options. Consider a function e.g. `build_ovn_ctl_args { is_leader, use_ssl }` to reduce duplication.
- [ ] **Readability**: Replace magic numbers with named variables at the top (e.g. 120s join timeout, 10s ovsdb-client timeout, 6641/6642 ports).
- [ ] **Portability**: In `ovn_db_pre_start`, prefer `[ ... ] && [ ... ]` over `[ ... -a ... ]` for test conditions (e.g. line 201).

---

## ./dist/images/start-ic-controller.sh

- [ ] **Robustness**: Use `"$@"` instead of `$@` in the exec line so arguments with spaces are preserved.
- [ ] **Robustness**: Quote variable in command substitution: use `"${OVN_NB_DAEMON}"` in `$(echo "${OVN_NB_DAEMON}" | wc -c)` and the cut pipeline (lines 44–45) to avoid word splitting.
- [ ] **DRY**: `gen_conn_str` is duplicated with `start-controller.sh`; consider sourcing a shared snippet or documenting the duplication if both run in the same image.

---

## ./dist/images/start-ovs-dpdk-v2.sh

- [ ] **Robustness**: Use `source "$OVS_DPDK_CONFIG_FILE"` (or `source "${OVS_DPDK_CONFIG_FILE}"`) instead of unquoted `source $OVS_DPDK_CONFIG_FILE` to avoid word splitting.
- [ ] **Readability**: Magic number `12` for nproc threshold is duplicated with start-ovs.sh; use a named variable at top (e.g. `OVS_NPROC_THRESHOLD=12`) for consistency and tuning.
- [ ] **Portability**: Use `$(nproc)` instead of backticks `` `nproc` `` for command substitution (line 55).
- [ ] **Robustness**: In the config file read loop, quote `$config_line` when passing to ovs-vsctl if it can contain spaces; and quote `$CONFIG_FILE` in `done < "$CONFIG_FILE"` (already correct).
- [ ] **Structure**: The block that sets ovn-remote, ovn-remote-probe-interval, ovn-openflow-probe-interval, ovn-encap-type is similar to start-ovs.sh; consider a shared function or sourced snippet for OVN controller env setup.
- [ ] **Readability**: Magic timeout `10` in ovs-vsctl add-port/add-bond could be a variable at top (e.g. `OVS_VSCTL_TIMEOUT=10`).

---

## ./dist/images/start-ovs.sh

- [ ] **Naming**: Inconsistent OVS DB name: line 81 uses `Open_vSwitch`, line 86 uses `open_vswitch`; OVS accepts both but use one consistently (e.g. `Open_vSwitch`) for readability.
- [ ] **Readability**: Magic number `12` for nproc threshold; use a named variable (e.g. `OVS_NPROC_THRESHOLD=12`) and consider sharing with start-ovs-dpdk-v2.sh.
- [ ] **Robustness**: In `handle_underlay_bridges`, `$(ip link show $br type openvswitch 2>/dev/null || true)` should quote `$br` (e.g. `"$br"`) to avoid word splitting.
- [ ] **Robustness**: In `gen_conn_str`, the loop variable `i` and `$1` (port) are used; ensure all expansions are quoted (e.g. `"$i"`, `"$1"`) where used in command substitution.
- [ ] **Maintainability**: The `quit` function is long and does multiple kubectl/ovn-ctl/ovs-ctl calls; consider extracting e.g. `stop_ovn_controller_if_same_cgroup` and `stop_ovs_if_same_cgroup` for readability and reuse.
- [ ] **Readability**: Magic port 6642 in `gen_conn_str 6642` could be a named constant at top (e.g. `OVN_SB_PORT=6642`).

---

## ./dist/images/test-server.sh

- [ ] **Robustness**: No `set -e`; if `nginx` fails the script still runs `iperf3 -s`. Add `set -e` to fail fast on first command failure, or document that both services are best-effort.
- [ ] **Clarity**: Add a one-line comment describing the script purpose (e.g. test server for e2e that runs nginx and iperf3).

---

## ./dist/images/uninstall.sh

- [ ] **DRY**: The iptables block (lines 9–38) and ip6tables block (49–79) are nearly identical except for binary (iptables vs ip6tables) and prefix (ovn40 vs ovn60). Extract a function e.g. `clean_iptables_rules iptables_cmd prefix` (ovn40/ovn60) to reduce duplication and keep IPv4/IPv6 in sync.
- [ ] **DRY**: The ipset destroy blocks (42–48 and 84–89) follow the same pattern; loop over prefix (ovn40, ovn60) and destroy the same set names with different prefix to avoid duplication.
- [ ] **Robustness**: No error handling — if a rule/chain does not exist (e.g. already removed), iptables/ip6tables -D or -F may fail. Consider appending `|| true` for idempotent uninstall or document that script assumes a prior install.
- [ ] **Readability**: The script is a flat sequence of commands; break into functions (e.g. clean_ovs, clean_iptables_v4, clean_iptables_v6, clean_ipsets, clean_dirs) for readability and reuse.

---

## ./dist/images/upgrade-ovs.sh

- [ ] **Robustness**: Quote variables in command substitution and tests: use `"$POD_NAMESPACE"` in line 9 (`$(kubectl -n "$POD_NAMESPACE" get ds ...)`), and `[ "$UPDATE_STRATEGY" = "OnDelete" ]` on line 49 to avoid word splitting and empty arguments.
- [ ] **Readability**: Replace backtick command substitution (e.g. line 36 `x\`ovn-nbctl ... | grep -o ...\``) with `$(...)` for clarity and avoid nesting; use a descriptive variable name (e.g. `conn_str` or `nb_conn`) instead of `x` in `gen_conn_str`.
- [ ] **DRY**: `gen_conn_str` is duplicated with start-controller.sh and start-ic-controller.sh; consider sourcing a shared snippet or documenting the duplication if all run in the same image.
- [ ] **Readability**: Magic numbers 6641 (NB port), 120s (rollout timeout), 3 and 1 (sleep seconds); use named variables at top (e.g. OVN_NB_PORT=6641, ROLLOUT_TIMEOUT=120s) for tuning and documentation.
- [ ] **Correctness**: Line 37 — when using `value` in the comparison, ensure it is quoted (`"$value"`) in case it contains spaces or special characters from NB_Global options.

---

## ./dist/images/vpcnatgateway/Dockerfile

- [ ] **Reproducibility**: `alpine:edge` is a moving target; consider pinning to a specific version (e.g. `alpine:3.19`) for reproducible builds, consistent with Dockerfile.test suggestion.
- [ ] **Readability**: Add a short top comment describing the image purpose (e.g. VPC NAT gateway runtime with nat-gateway.sh and lb-svc.sh).

---

## ./dist/images/vpcnatgateway/lb-svc.sh

- [ ] **Correctness**: In `exec_cmd`, `cmd=${@:1:${#}}` then `$cmd` does not preserve quoting; arguments with spaces get split. Use `"$@"` and run the command directly (e.g. `exec_cmd() { "$@"; ret=$?; ... }`) so callers pass arguments properly.
- [ ] **Correctness**: `grep "\-d $eip"` and similar use unquoted variables; if `$eip` contains regex metacharacters matching can be wrong. Use `grep -F "$eip"` for literal match where appropriate.
- [ ] **Correctness**: Line 81 — `iptables -t nat -C PREROUTING $checkRule` splits `checkRule` by spaces; for rules with multiple words (e.g. `--to-destination ip:port`) this can fail. Pass rule components as separate arguments or build the -C invocation from parts.
- [ ] **Maintainability**: `add_eip` uses `ipcalc -n` and `ipcalc -p`; the vpcnatgateway Dockerfile does not install ipcalc. Ensure ipcalc is added to the image or replace with another method (e.g. awk/sed or ip route parsing) so the script runs in the built image.
- [ ] **Typo**: Line 105 echo "dnat-del rules" should be "dnat-del $rules" for consistency with other commands.
- [ ] **DRY**: `exec_cmd` is duplicated with nat-gateway.sh; consider sourcing a common snippet or document duplication.

---

## ./dist/images/vpcnatgateway/nat-gateway.sh

- [ ] **Typo**: Line 257 "2>/dev/nul" → "2>/dev/null".
- [ ] **Correctness**: In `exec_cmd`, `cmd=${@:1:${#}}` then `$cmd` does not preserve quoting; use `"$@"` and run the command directly so arguments with spaces are preserved.
- [ ] **Robustness**: Quote variables in command substitution: e.g. line 140 `ip -4 addr show dev "$EXTERNAL_INTERFACE"`, and line 222 use `grep -F "$eip"` and `$(...)` instead of backticks.
- [ ] **Correctness**: Line 284–289 — `ruleMatch=$(...); if [ "$?" -eq 0 ]` checks the exit code of the assignment, not grep. Use `if ruleMatch=$(...); [ -n "$ruleMatch" ]` or test grep exit code before assignment.
- [ ] **DRY**: `exec_cmd` and the pattern "for rule in $@; do arr=(${rule//,/ }); ..." repeated in many functions; consider a helper to parse rule strings and reduce duplication.
- [ ] **Robustness**: `for rule in $@` — unquoted $@ can break on spaces in rules; use `for rule in "$@"` or handle multi-word rules explicitly.

---

## ./dist/monitoring/cni-grafana.json

(No refactoring suggestions — Grafana dashboard export; structure is dictated by Grafana.)

---

## ./dist/monitoring/cni-monitor.yaml

- [ ] **DRY**: cni-monitor.yaml, controller-monitor.yaml, ovn-monitor.yaml, and pinger-monitor.yaml share the same ServiceMonitor structure (only name and app label differ). Consider a Helm list or single parameterized template to reduce duplication and ease adding new components.

---

## ./dist/monitoring/controller-grafana.json

(No refactoring suggestions — Grafana dashboard export.)

---

## ./dist/monitoring/controller-monitor.yaml

- [ ] **DRY**: Same as cni-monitor.yaml; consider a single parameterized ServiceMonitor template for all kube-ovn components.

---

## ./dist/monitoring/ovn-grafana.json

(No refactoring suggestions — Grafana dashboard export.)

---

## ./dist/monitoring/ovn-monitor.yaml

- [ ] **DRY**: Same as cni-monitor.yaml; consider a single parameterized ServiceMonitor template.

---

## ./dist/monitoring/ovs-grafana.json

(No refactoring suggestions — Grafana dashboard export.)

---

## ./dist/monitoring/pinger-grafana.json

(No refactoring suggestions — Grafana dashboard export.)

---

## ./dist/monitoring/pinger-monitor.yaml

- [ ] **DRY**: Same as cni-monitor.yaml; consider a single parameterized ServiceMonitor template.

---

## ./hack/audit-to-json.sh

- [ ] **Robustness**: Quote `$1` in `basename $1 .log` (line 10) so paths with spaces are handled correctly.
- [ ] **Portability**: `sed -i '1 i\['` — the leading backslash and `[` may be interpreted differently by BSD vs GNU sed. Prefer a more portable form (e.g. `sed -i '1s/^/[\n/'` or document that GNU sed is required).

---

## ./hack/backup.sh

- [ ] **Robustness**: In `main`, use `[ "$#" -ne 1 ]` instead of `[ $# -ne 1 ]` for consistency with quoted numeric tests. In `validate_version`, quote `$version` in the regex test: `[[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]` to handle empty or odd input.

---

## ./hack/boilerplate.go.txt

(No refactoring suggestions — license header only.)

---

## ./hack/changelog.sh

- [ ] **Readability**: Variable names `tag` and `last` are confusing: when printing a section, `tag` is the newer tag and `last` is the older (range is `$last..$tag`). Consider renaming to e.g. `newer_tag` and `older_tag` (or `curr_tag` and `prev_tag`) for clarity.
- [ ] **Maintainability**: The excluded contributor "oilbeater" is hardcoded in `grep -vi oilbeater`. Consider a variable at the top (e.g. `EXCLUDED_CONTRIBUTORS`) for easier customization.
- [ ] **Structure**: Extract "print one release section" (tag header, git log, contributors) into a small function so the main loop is easier to follow and logic is reusable.
- [ ] **Portability**: `sort -rV` is GNU-specific (version sort). Document that GNU sort is required, or add a comment in the script header.

---

## ./hack/cherry-pick.sh

- [ ] **Extensibility**: Source branch is hardcoded as `origin/master`; some repos use `main`. Add a variable (e.g. `SOURCE_BRANCH`) or detect default branch so the script works across repos.
- [ ] **Structure**: The per-branch logic (checkout, cherry-pick, push, rollback on failure) is a long block in the loop. Extract to a function e.g. `cherry_pick_to_branch(branch, commit)` returning success/failure to shorten the main loop and improve readability.
- [ ] **Readability**: Add a short comment above `git reset --hard HEAD~1` clarifying that the only new commit is the cherry-pick, so rollback is safe.
- [ ] **Portability**: `git branch --show-current` requires Git 2.22+. Document in script header or use fallback e.g. `git rev-parse --abbrev-ref HEAD` for older Git.
- [ ] **Maintainability**: When `git checkout -B "$BRANCH" "origin/$BRANCH"` runs, local commits on that branch are lost. Consider a warning when local branch exists and diverges from origin, or document this in the usage/comment.

---

## ./hack/ci-check-crash.sh

- [ ] **Correctness**: With `set -e`, when the ignorable-pod selector loop runs and `grep -q "^$pod$"` finds no match, grep returns 1 and the script exits. Use `grep -q ... || true` or run the match in a context that does not trigger set -e so the script only exits with 1 when a real crash is found.
- [ ] **Correctness**: Container name is fetched once per container type with jsonpath `[*].name` (space-separated list), but the loop is over restartCounts by index. For multiple containers, `name` is all names concatenated, so `kubectl logs -c $name` is wrong. Use index in jsonpath e.g. `{.status.${containerType}Statuses[$i].name}` to get the i-th container name.
- [ ] **Readability**: Variable `name` is the container name; rename to e.g. `containerName` to avoid confusion with pod name.
- [ ] **Structure**: The Talos + "network not ready" ignorable check is nested deep; extract to a helper e.g. `is_ignorable_talos_restart(namespace, pod, container_name)` to simplify the main loop.
- [ ] **DRY**: Multiple kubectl get calls per pod/container type; consider a single get with jsonpath or jq to obtain names and restart counts together to reduce invocations.

---

## ./hack/go-list.sh

- [ ] **Correctness**: If `path` is empty or not provided, `./$path/...` becomes `./...` and lists all packages. Add a check for at least one argument and print usage.
- [ ] **Readability**: Variables `d` and `f` are cryptic; rename to e.g. `pkgPath` and `fileName` for clarity.
- [ ] **Performance**: The inner loop runs `go list` once per package; for large trees this is many invocations. Consider a single `go list` with a template that outputs package path and compiled files together to reduce calls.

---

## ./hack/modelgen.sh

- [ ] **Robustness**: Script does not `cd` to project root; paths like `pkg/ovsdb/ovnnb` and `git add pkg/ovsdb/ovnnb` assume current directory is repo root. Add `ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"` and `cd "${ROOT_DIR}"` at start so the script works when run from any directory.
- [ ] **Maintainability**: `OVN_VERSION="25.03"` is hardcoded; consider reading from an env var (e.g. `OVN_VERSION="${OVN_VERSION:-25.03}"`) or a single place (e.g. VERSION file or Makefile) for easier upgrades.
- [ ] **DRY**: The two `go tool ... modelgen` invocations differ only by package name, output dir, and schema file. Consider a loop over `(ovnnb ovn-nb.ovsschema pkg/ovsdb/ovnnb)` and `(ovnsb ovn-sb.ovsschema pkg/ovsdb/ovnsb)` to reduce duplication.

---

## ./hack/release.sh

- [ ] **Naming**: Fix typo "successed" → "succeeded" in echo messages (lines 11 and 13).
- [ ] **Correctness**: Comment says "run from project root" but `DOCS_DIR="../docs"` is relative to current directory; if run from project root, `../docs` leaves the repo. Use script-relative path e.g. `DOCS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/docs"` so docs path is correct regardless of cwd.
- [ ] **DRY**: `NEXT_VERSION=$(cat VERSION | awk -F '.' ...)` appears multiple times with two variants (patch: `$3+1` vs minor: `$2+1"."$3`). Extract e.g. `next_version() { local kind=$1; ... }` to avoid duplication and mistakes.
- [ ] **Structure**: Docker manifest create/push and pull sequences are repeated for kube-ovn, vpc-nat-gateway, kube-ovn-base; consider arrays of image names and a loop to reduce repetition and ease adding new images.
- [ ] **Maintainability**: The master vs non-master branch duplicates the kube-ovn-base manifest create/push/tag block; extract a function (e.g. `push_base_manifests`) to keep a single implementation.

---

## ./hack/update-codegen-docker.sh

- [ ] **Naming**: Fix typo "useage" → "usage" in comment (line 2).
- [ ] **Robustness**: Script uses `-v ${PWD}:/app`; if run from a subdirectory, generated code and `go mod tidy` run in the wrong tree. Add `cd "$(dirname "${BASH_SOURCE[0]}")/.."` at start so it always runs in project root.

---

## ./hack/update-codegen.sh

- [ ] **Robustness**: `KUBE_CODEGEN_ROOT` is the script dir (hack/); paths like `pkg/apis` and `pkg/client` are relative to current directory. If the script is run from elsewhere, generation writes to the wrong place. Add `cd "$(dirname "${BASH_SOURCE[0]}")/.."` at the top and use that as the project root for all paths.
- [ ] **Readability**: The final block (gen_helpers, gen_register, gen_client) repeats `--boilerplate ${KUBE_CODEGEN_ROOT}/../hack/boilerplate.go.txt`; consider a variable e.g. `BOILERPLATE="${KUBE_CODEGEN_ROOT}/../hack/boilerplate.go.txt"` for clarity and single place to change.

---
