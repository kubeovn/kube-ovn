# kubectl-ko

`kubectl-ko` is a standalone Go kubectl plugin for operating and diagnosing
Kube-OVN. It talks to the Kubernetes API and routes node-sensitive operations
through the independent `kubectl-ko-node-agent` DaemonSet. The agent is not a
`kube-ovn-cni`, `ovs`, `ovs-ovn`, or `ovn-central` container; it accesses the
node's sockets and namespaces directly. No local Bash, OVN, OVS, tar, or
kubectl subprocess is required.
`kubectl` itself is only needed to invoke the plugin as `kubectl ko`.

## Installation

Kube-OVN images and the CNI installer install the binary at the existing
`/usr/local/bin/kubectl-ko` path. For a separate workstation, download the
`kubectl-ko-<os>-<arch>.tar.gz` (Linux/macOS) or
`kubectl-ko-windows-<arch>.zip` asset and `kubectl-ko-checksums.txt` from the
matching Kube-OVN GitHub release, verify its SHA256, and put the extracted
`kubectl-ko` on PATH. Linux, macOS and Windows, each on amd64 and arm64, are
built independently. Do not copy a Linux pod binary to a macOS or Windows
workstation. The Kubernetes nodes and remote OVN/OVS tools remain Linux-based.

The cluster installation script fetches its CLI from the independent agent
after the agent DaemonSet rollout completes, selecting the `agent` container
explicitly. It does not copy the CLI from a CNI or OVS/OVN component Pod.

On Windows, use the ZIP matching the workstation architecture. Verify its
SHA256 with `Get-FileHash`, extract it with `Expand-Archive`, and place
`kubectl-ko.exe` in a directory on PATH alongside the existing `kubectl.exe`
installation. PowerShell can also run it directly:

```powershell
Get-FileHash .\kubectl-ko-windows-arm64.zip -Algorithm SHA256
Expand-Archive .\kubectl-ko-windows-arm64.zip .\kubectl-ko-windows-arm64
.\kubectl-ko-windows-arm64\kubectl-ko.exe version
.\kubectl-ko-windows-arm64\kubectl-ko.exe --context staging exec nbctl -- show
```

Use the amd64 archive instead on x64 Windows. `kubectl ko version` and
`kubectl plugin list` confirm discovery once its directory is on PATH. WSL,
Bash and local OVN/OVS installations are not required. Use PowerShell 7.4 or
later (or cmd.exe) when redirecting binary stdout such as `tcpdump -w -`;
older PowerShell versions can decode and corrupt native command output.

For a source checkout, run `make build-kubectl-ko`; the results are
`dist/images/kubectl-ko` and the Linux `dist/images/kubectl-ko-node-agent`.
Install the chart's `kubectl-ko-node-agent` DaemonSet before using commands
that need node access. The module uses replacements, so installing release
binaries is preferred to `go install ...@version`.
Both Helm charts deploy the independent agent. The v2 chart uses its global
image and pull policy, `ovsOvn.ovnDirectory`, `logging.directory` and
`cni.configDirectory` to mount the same host data as the running components.
The agent is scheduled only on Linux nodes; Windows client support covers the
kubectl plugin, while node-side OVN/OVS operations remain Linux-only.
On Windows, build from the checkout with
`go build -o kubectl-ko.exe ./cmd/kubectl-ko`. Cross-builds via
`GOOS=windows GOARCH=amd64 make build-kubectl-ko` produce
`dist/images/kubectl-ko.exe`; substitute `arm64` for Windows on ARM.

Use `kubectl plugin list` to detect older copies shadowing the new binary.
`kubectl ko version` prints the local build without accessing a cluster.

## Command structure and complete feature inventory

The Go CLI uses the following command tree. This is an intentional breaking
change: old command names and positional forms are not aliases.

```text
kubectl ko
  exec
    nbctl | sbctl | ic-nbctl | ic-sbctl -- [TOOL_ARGS...]
    vsctl | ofctl | dpctl | appctl --node NODE -- [TOOL_ARGS...]
  db
    health
    nb
      status | backup | kick SERVER_ID | restore
    sb
      status | backup | kick SERVER_ID
  trace --pod POD|--node NODE --dst-ip IP [OPTIONS]
  capture --pod POD -- [TCPDUMP_ARGS...]
  network
    inspect --pod POD [--output table|json]
  diagnose
    cluster
    node NODE
    subnet SUBNET
    connectivity --target PROTOCOL://IP:PORT [--target ...]
    environment
  logs [OPTIONS]
  restart
  perf
    run [OPTIONS]
    recovery --yes
  acl
    decode COOKIE
    listen --node NODE
  version
  completion bash|zsh|fish|powershell
```

| New command | Complete capability and effects |
| --- | --- |
| `exec nbctl`, `exec sbctl` | Find the requested database leader independently and execute its OVN CLI through the node agent's mounted OVN socket. All CLI operations, including writes and multi-command transactions, remain available. |
| `exec ic-nbctl`, `exec ic-sbctl` | Find the corresponding interconnection leader and execute its CLI. |
| `exec vsctl/ofctl/dpctl/appctl --node NODE` | Execute the selected OVS tool through the node agent and its mounted OVS sockets. Remote commands may change live state. |
| `db nb status`, `db sb status` | Show RAFT cluster and storage status for the selected leader. |
| `db health` | Check both NB and SB storage on every running central container, without requiring a healthy leader. Report unhealthy storage as failure. |
| `db nb backup`, `db sb backup` | Convert and download a standalone database, verify DB name and SHA256, and publish without overwriting an existing file. Write a JSON provenance sidecar; remove the temporary remote backup. |
| `db nb kick SERVER_ID`, `db sb kick SERVER_ID` | Remove a stale cluster member. `--dry-run` prints the database leader Pod and exact command without requiring an agent or applying the change; actual removal runs through the agent on that leader's node. |
| `db nb restore` | Reconstruct the central NB/SB cluster from the NB database already present on the explicit bootstrap node. Verify the shared hostPath, use temporary per-node helpers when OVS does not mount the databases, stop central, preserve originals and RAFT headers, rebuild, verify storage and restart OVS. Helpers use the central image and security context and are cleaned up by UID. This is not local-file import; SB-only restore is not supported. |
| `trace` | Resolve Pod/Node addresses, MACs and logical ports, trace through OVN, then OVS. Supports IPv4/IPv6, ICMP/TCP/UDP, IPv4 ARP request/reply, explicit destination MAC, hostNetwork, Underlay/U2O and VM logical ports. `--engine ovn` runs only OVN trace. |
| `capture` | Execute tcpdump in a Pod's network namespace, including hostNetwork and internal-port paths. A remote `-w PATH` stays remote; `-w -` streams the original pcap bytes locally. |
| `network inspect` | Show the Pod network namespace path, every interface's index, kind, MAC, MTU, state and addresses, plus the host-side veth peer when one exists. For `macvlan`/`ipvlan`, also show the parent host NIC with its link details. `--output=json` emits machine-readable data. Host-network Pods use the host namespace and expose peer indexes for host links. |
| `diagnose cluster` | Check cluster configuration, component rollout and leaders; create a unique temporary NodePort Service and run active checks from independent temporary probe Pods in the original pinger network namespaces. |
| `diagnose node NODE` | Perform configuration checks and restrict the independent active probes to one node. |
| `diagnose subnet SUBNET` | Check the subnet, create an isolated temporary DaemonSet and NodePort Service, and check peer TCP/UDP, node ICMP and NodePort connectivity from those subnet Pods. Does not require optional CNI node TCP/UDP listeners. |
| `diagnose connectivity` | Check configuration and probe explicit TCP/UDP IP endpoints using independent temporary Pods in the existing pinger network namespaces. Does not create NodePort Services or subnet DaemonSets. |
| `diagnose environment` | Discover Linux nodes from the Kubernetes API and run the environment checker through one independent agent per node, including nodes without CNI Pods. |
| `logs` | Collect component files, container logs and Linux node state in parallel; limit each item and record partial failures in a manifest. |
| `restart` | Restart and wait for central, OVS, controller, CNI, pinger and monitor in dependency order. |
| `perf run` | Create isolated probe Pods/Service; measure Pod, host, Service and multicast performance. Temporarily configure an OVN LB and multicast membership, then clean up owned changes. Does not delete central leaders. |
| `perf recovery --yes` | Deliberately delete NB, SB and northd leader Pods in sequence and measure their recovery. Does not run the traffic benchmark. |
| `acl decode COOKIE` | Decode a sample cookie using the existing NB helper. |
| `acl listen --node NODE` | Stream sampled cookies from that node, decode with bounded backpressure, and emit YAML documents until cancellation. |
| `version`, `help`, `completion` | Show the client build, discover commands or generate shell completion without accessing a cluster. |

## Configuration and argument boundaries

Temporary NodePort probes request `PreferDualStack` and target only the IP
families actually allocated to the Service. Single-stack clusters use their
available family; dual-stack clusters probe both families. Missing node
addresses for an allocated family are reported as errors.

Standard Kubernetes connection flags are accepted before or after subcommands,
up to the explicit `--` remote-argument separator. The kubeconfig loader supports
`--kubeconfig`, `KUBECONFIG`, context, TLS, authentication plugins and impersonation.
Workload namespace selection is `namespace/pod`, then `--namespace/-n`, then
kubeconfig namespace, then `default`. The deployment namespace is independently
selected by `--kube-ovn-namespace`, `KUBE_OVN_NS`, then `kube-system`.

| Scope | Parameters and defaults |
| --- | --- |
| Global | `--timeout=0` bounds the whole invocation; zero allows long streams. `--discovery-timeout=10s` bounds target selection. Kubernetes `--request-timeout` does not truncate an established exec stream. |
| Raw tools / capture | `exec` OVS tools require `--node`; `capture` requires `--pod`. All remote arguments must follow `--`. |
| Network inspection | `network inspect` requires `--pod`; `--output=table` is the default and `--output=json` is intended for automation. |
| Trace | Exactly one of `--pod` / `--node`; required `--dst-ip`; `--protocol=icmp`, `--engine=all`, optional `--dst-mac`. TCP/UDP require `--dst-port=1..65535`. ARP uses `--arp-op=request|reply` and IPv4. |
| Database backup | `--output FILE`; otherwise a unique DB-specific filename. |
| Database recovery | Required `--source-node NODE` and either `--yes` or `--dry-run`. |
| Diagnostics | Cluster/node/subnet accept `--read-only` to skip active probes and temporary resources. All configuration/probe modes accept `--skip-kube-proxy`, defaulting from `WITHOUT_KUBE_PROXY=true`. Subnet accepts `--tcp-port` / `--udp-port`, defaulting from `TCP_CONN_CHECK_PORT` / `UDP_CONN_CHECK_PORT`, then 8100 / 8101. Explicit flags override environment values. Connectivity requires one or more `--target tcp://IP:PORT` / `udp://IP:PORT`; IPv6 uses brackets. |
| Logs | `--component=all` (`all`, `kube-ovn`, `ovn`, `ovs`, `linux`), `--output-dir=kubectl-ko-log`, `--concurrency=4`, `--item-timeout=30s`, `--max-bytes=268435456` per item, optional `--strict`. |
| Performance | `perf run --image=docker.io/kubeovn/test:v1.13.0 --duration=5s --bandwidth=1G`. Duration is per measurement and must be whole seconds from 1s to 5m. Use `--image` for an internal mirror. Leader disruption is a separate `perf recovery --yes` operation. |
| ACL | `acl decode COOKIE`; `acl listen --node NODE`. |

```console
kubectl ko exec nbctl --context staging -- --format=json show
kubectl ko exec nbctl -- -- ls-add example -- lsp-add example example-port
kubectl ko exec vsctl --node worker-a -- --timeout=5 show
kubectl ko trace --pod app/web --dst-ip 10.0.0.8 --protocol tcp --dst-port 443
kubectl ko trace --node worker-a --dst-ip 2001:db8::8 --engine ovn
kubectl ko capture --namespace app --pod web -- -w - > capture.pcap
kubectl ko network inspect --pod app/web
kubectl ko network inspect --pod app/web --output json
kubectl ko diagnose subnet ovn-default --tcp-port 8100 --udp-port 8101
kubectl ko diagnose connectivity --target tcp://10.0.0.8:8100 --target 'udp://[2001:db8::8]:8101'
kubectl ko db nb backup --output northbound.backup
kubectl ko logs --component ovn --output-dir ./support --strict
kubectl ko perf run --image registry.example/test:v1.13.0 --duration 2s
```

The separator belongs to the plugin and is removed exactly once. Every argument
after it, including another `--`, `--help`, `--timeout`, `-n`, `-c`, whitespace or
quotes within a single argument, belongs to the remote tool and is preserved.
`exec nbctl --help` shows local help; `exec nbctl -- --help` shows remote help.
`exec nbctl show` is rejected instead of guessing the argument boundary.

UDP endpoint probes send a `health check` datagram and require a response.
Use a responding health/echo service for UDP; this is not a DNS query or a
generic UDP port scan. Subnet probes supply their own matching listeners.

Cluster/node/subnet diagnostics check internal connectivity by default. To
also check external ICMP connectivity, repeat `--external-address IP`, for
example `diagnose cluster --external-address 1.1.1.1 --external-address
2606:4700:4700::1111`. These explicit probes fail the command when unreachable
or when a probe Pod has no matching IP family.
No public Internet endpoint is required for the default cluster health check.

Each node operation uses Kubernetes `pods/exec` to start an isolated helper
process in the independent agent. A versioned gRPC stream runs over that
process's stdin/stdout. Concurrent operations have separate streams and do not
restart the agent. It does not execute in Kube-OVN component Pods.
`network inspect` resolves the Pod netns from OVSDB through the agent, with a
Pod-UID lookup in host process cgroups when no OVS interface exists, then
reads Pod and host links with `ip -s -j -d addr show`; it reports the host-side veth peer by
ifindex and resolves `macvlan`/`ipvlan` parent NICs in the Pod or host netns
using the link namespace identity, name and ifindex,
without entering `kube-ovn-cni` or `ovs-ovn`.
Both JSON and text output include RX/TX statistics for Pod interfaces and
resolved host peers or parent NICs. Counter names match iproute2; 64-bit values
are preserved, including zero and driver-specific counters. Older `stats`
output is accepted when `stats64` is unavailable.
Probe Pods may use the normal Kubernetes streaming path for their own
short-lived test process. Exec uses WebSocket with SPDY fallback only for
supported handshake failures.
Completed requests close helper stdin and wait for the outer exec process
status before returning; a successful tool result does not hide a helper
process failure. Remote exit codes propagate; failed commands are never
replayed. Streams have no TTY transformations or stdout banners. Invalid arguments fail before client
creation. Cancelling an agent request terminates its Linux command process
group, including children such as those started by `nsenter`, and returns exit
130. Processes that deliberately detach into a different session are outside
that group.

## Logs and recovery

`logs` keeps the `kubectl-ko-log/<node>/{kube-ovn,ovn,openvswitch,linux}` layout.
Central files on the same node occupy an `ovn/central-<pod>` child directory.
`manifest.json` records every item and failure. `--concurrency`, `--item-timeout`
and `--max-bytes` limit each run; the byte limit is per item. Use `--strict` to
make any failed item fail the command. Archives cannot write outside their
collection root. On Unix, extracted files use private permissions. On Windows,
use a destination directory protected by your user ACL; Unix permission bits
do not configure Windows ACLs. Backup destinations must support hard links
(for example, NTFS) for atomic publication without overwriting existing files.
Linux state collection and environment checks discover nonterminating Linux
nodes independently of CNI Pods. A missing, unready or duplicate agent is an
explicit discovery failure; other nodes continue to be checked or collected.
XFRM state is collected with
`nokeys`. Treat database and network diagnostics as sensitive local artifacts.
IPsec collection locates the CNI Pod's live charon process by Pod UID, pins its
root directory and uses the independent agent's strongSwan client to query its
control socket. It collects the active `ipsec.conf`, certificate metadata and
status, without executing a tool in the CNI container or exporting keys.
Missing or ambiguous source Pods, disabled IPsec and unavailable daemons remain
individual IPsec failures in the manifest; other Linux state is still collected.

`db nb restore` preserves the old operation meaning: reconstruct from a database
already on a node, not import an arbitrary local backup. It requires an explicit
source node and confirmation; the source must be the first `NODE_IPS` member,
which the image startup script uses to bootstrap the cluster. Recovery supports only the standard central Deployment
with literal `NODE_IPS` membership and a writable central hostPath mount. If
OVS does not mount the database directory, temporary hostNetwork helpers use
the same hostPath without depending on the OVN Pod network. All helper and
database-file checks finish before central is stopped. An existing OVS database
mount must match central's hostPath. Run `--dry-run` first. All central pods must stop before file
changes begin. Original files remain in a unique directory on every member;
a private local JSON record tracks the last recovery stage. An error stops the
procedure and reports this record, the remote directory and original replica
count. It does not guess whether it is safe to undo a partially completed
recovery. Retain these files for a deliberate recovery or rollback. Remote
backup directories use `/etc/ovn/.kubectl-ko-recovery-<id>` so central startup's
file-permission glob cannot remove their directory traversal permissions.

Probe resources have unique names and a run identity. Cleanup uses saved UIDs,
not broad label deletion. A cleanup failure reports the exact remaining object
without hiding the original error. Diagnostic probe Pods mount a private temporary
log directory. Cluster/node/connectivity probes reuse the existing pinger image,
service account, tool mounts and network namespace, while executing in a separate
short-lived Pod. They retain the original pinger name/IP environment for its
Kubernetes checks, revalidate its UID and readiness before execution, and use
host PID access plus `SYS_ADMIN` to enter its network namespace. The independent
node agent does not mount a service-account token. No component Pod is an exec
target. If the subnet probe DaemonSet fails to become ready, the command collects
status and limited container logs from at most eight Pods owned by that
DaemonSet before cleanup. Log collection has a five-second deadline and retains
the original readiness error. `restart`, raw OVN/OVS commands, recovery and
explicit performance disruption can change live cluster state.

Before Service performance measurements, the temporary IP-wide OVN load
balancer must synchronize to all chassis within 30 seconds. This makes qperf's
dynamic data ports available before the benchmark starts. A synchronization
failure stops measurements and still cleans up the owned load balancer.

## Compatibility and migration

The supported starting point is a matching CLI and Kube-OVN release. Remote
image tools determine feature availability. The independent node agent is
required for node sockets, namespaces, captures and host diagnostics; it is
deployed by the chart and does not share a component Pod. Existing kubeconfig
authorization applies:
resource discovery requires get/list, node-agent access needs `pods/exec`,
logs need pods/log, probes need create/delete, and
rollout/recovery need workload patch/scale permissions. Exec access is not a
read-only database permission.

The matching image also fixes pinger control-socket discovery across PID
namespaces. Daemon status checks connect to the socket named by the recorded
PID, avoiding appctl's namespace-dependent PID-file lock lookup. Service and
port-binding checks remain enabled, and failed probes return a nonzero status.

This rewrite replaces most of the old command interface. The Go executable
retains `log COMPONENT` for existing log collectors, including CI Valgrind
checks; it collects the same files as `logs --component COMPONENT`. Consecutive
component collections preserve files already collected in the output directory.
Migrate other commands using this table:

| Previous form | New form |
| --- | --- |
| `nbctl ARGS`, `sbctl ARGS` | `exec nbctl -- ARGS`, `exec sbctl -- ARGS` |
| `icnbctl ARGS`, `icsbctl ARGS` | `exec ic-nbctl -- ARGS`, `exec ic-sbctl -- ARGS` |
| `vsctl/ofctl/dpctl/appctl NODE ARGS` | `exec TOOL --node NODE -- ARGS` |
| `nb/sb status`, `nb/sb backup`, `nb/sb kick ID` | `db nb/sb status`, `db nb/sb backup`, `db nb/sb kick ID` |
| `nb dbstatus`, `sb dbstatus` | `db health` |
| `nb restore` | `db nb restore --source-node NODE --yes` |
| `trace POD IP [MAC] PROTOCOL [PORT/OP]` | `trace --pod POD --dst-ip IP [--dst-mac MAC] --protocol PROTOCOL [--dst-port PORT/--arp-op OP]` |
| `trace node//NODE ...` | `trace --node NODE ...` |
| `ovn-trace ...` | `trace --engine ovn ...` |
| `tcpdump POD ARGS` | `capture --pod POD -- ARGS` |
| `diagnose [all]` | `diagnose cluster` |
| `diagnose node NODE`, `diagnose subnet SUBNET` | Same spelling, now real subcommands with their own help/validation |
| `diagnose IPPorts tcp-IP-PORT,udp-IP-PORT` | `diagnose connectivity --target tcp://IP:PORT --target udp://IP:PORT` |
| `env-check` | `diagnose environment` |
| `log COMPONENT --output DIR` | `logs --component COMPONENT --output-dir DIR` |
| `reload` | `restart` |
| `perf IMAGE --duration 5` | `perf run --image IMAGE --duration 5s` |
| Implicit leader deletion or `perf --include-disruption` | Separate `perf recovery --yes` |
| `acl-sample decode/listen ...` | `acl decode/listen ...` |

Bare pod names honor kubeconfig namespace; multiple ready leaders fail instead
of selecting arbitrarily; failures retain nonzero exit codes; binary streams
do not use a TTY. UDP offered bandwidth defaults to 1G instead of 1000G.

The original script remains a separate `kubectl-ko-legacy` executable for an
explicit rollback; the Go executable never invokes it. Legacy syntax exists
only in that script and version-specific E2E fixtures for older releases.
Build scripts and CI invoke the Go binary directly, never through Bash.

## Development checks

```console
go test -race ./pkg/ko/... ./cmd/kubectl-ko/...
make build-kubectl-ko
make lint
```

The dedicated workflow tests real exec streams, builds all six workstation
platforms and runs file/streaming tests natively on Windows amd64 and arm64.
Chart CI overlays the current Go client, node agent and environment checker
onto the published component image, loads it into Kind and installs the v2
chart with that image. It checks credential-free agent readiness on every
Linux node, environment output and actual Pod interface statistics. It also
executes the installer's CLI bootstrap phase with a temporary local destination,
verifies the copy uses the `agent` container, and compares the copied binary
with the current build.
Existing `[group:kubectl-ko]` E2E exercises trace, capture, logs,
diagnostics and backup against a cluster. Environment coverage also installs
credential-free agents in a namespace without CNI Pods and verifies every Linux
node's environment and interface logs, plus explicit IPsec source failures.
The Kind-only serial `[group:ha]`
suite checks restart and leader recovery against NB data and internal
connectivity, and database reconstruction against its recovery record,
retained originals, NB data and connectivity. It repeats the traffic benchmark
and checks probe/LB/multicast cleanup and unchanged central Pod UIDs. These
specs use the matching binary from the test cluster image. New CLI paths
select the existing full E2E matrix.
