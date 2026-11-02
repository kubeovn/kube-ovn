# kubectl-ko

> **Code baseline audited:** GitHub PR #7826, HEAD
> `c01741eeec2e001a408331ebb7b42d9e3d3cef31`. This page describes the command
> tree and validation implemented by that code baseline.

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
Both Helm charts deploy the independent agent and use
`global.registry.imagePullSecrets` for private image pulls. The legacy chart
includes the agent in `full`, `controlPlaneOnly` and `dataPlaneOnly` installations,
so management clusters can execute database commands through the agent too.
The v2 chart uses its global
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

## Command reference

### Command tree

```text
kubectl ko
  exec (POD | TYPE/NAME) [exec flags] -- COMMAND [args...]
  exec -l SELECTOR -- COMMAND [args...]
  exec nbctl|sbctl|ic-nbctl|ic-sbctl -- [TOOL_ARGS...]
  exec vsctl|ofctl|dpctl|appctl --node NODE -- [TOOL_ARGS...]
  db health
  db nb|sb status|backup|kick SERVER_ID|restore
  db sb status|backup|kick SERVER_ID
  trace TARGET --target IP [trace flags]
  capture TARGET [capture flags] -- [CAPTURE_ARGS...]
  network inspect TARGET [inspect flags]
  diagnose cluster|node NODE|subnet SUBNET|connectivity|environment
  logs [flags]
  restart
  perf run [flags] | recovery --yes
  acl decode COOKIE | listen --node NODE
  version
  completion bash|zsh|fish|powershell
```

### Global Kubernetes and plugin flags

The following flags are persistent and can be placed before or after the
subcommand (before a remote `--` separator):

| Flag | Default | Meaning |
| --- | --- | --- |
| `-n, --namespace NAMESPACE` | kubeconfig namespace, then `default` | Namespace for namespaced target resolution. |
| `--kube-ovn-namespace NAMESPACE` | `$KUBE_OVN_NS`, then `kube-system` | Namespace containing Kube-OVN component Pods. The current HEAD does not auto-discover this value from labels. |
| `--kubeconfig FILE` | kubeconfig default | Kubernetes client configuration. |
| `--context NAME` | current context | Kubeconfig context. |
| `--cluster NAME` | context default | Kubeconfig cluster. |
| `--server URL` | kubeconfig default | API server address. |
| `--certificate-authority FILE` | empty | CA bundle. |
| `--client-certificate FILE` / `--client-key FILE` | empty | Client TLS credentials. |
| `--token TOKEN` | empty | Bearer token. |
| `--user NAME` | context default | Kubeconfig user. |
| `--insecure-skip-tls-verify` | `false` | Disable API server certificate verification. |
| `--tls-server-name NAME` | empty | TLS server name override. |
| `--proxy-url URL` | empty | API server proxy. |
| `--request-timeout DURATION` | `0` | Timeout for an individual Kubernetes request. It does not truncate an established stream. |
| `--as USER`, `--as-group GROUP`, `--as-uid UID`, `--as-user-extra KEY=VALUE` | empty | Kubernetes impersonation; repeat array flags as needed. |
| `--discovery-timeout DURATION` | `10s` | Maximum wait for one unique ready target. |
| `--timeout DURATION` | `0` | Whole command timeout; zero permits long-running streams. |

### Resource references

Use one positional `TARGET`. A bare name means `pod/NAME`; namespaced
resources use `KIND/NAME` together with `-n NAMESPACE`. `KIND/NAMESPACE/NAME`
is rejected. Node, LSP, LRP and localnet references are cluster-scoped.

| Canonical kind | Accepted spellings | Namespace | `exec` | `trace` | `capture` | `network inspect` |
| --- | --- | --- | --- | --- | --- | --- |
| Pod | `pod`, `po`, `pods` | yes | yes | yes | yes | yes |
| Node | `node`, `no`, `nodes` | no | no | yes | yes | yes |
| Service | `service`, `svc`, `services` | yes | yes | yes | yes | yes |
| Deployment | `deployment`, `deploy`, `deployments` | yes | yes | yes | yes | yes |
| DaemonSet | `daemonset`, `ds`, `daemonsets` | yes | yes | yes | yes | yes |
| StatefulSet | `statefulset`, `sts`, `statefulsets` | yes | yes | yes | yes | yes |
| ReplicaSet | `replicaset`, `rs`, `replicasets` | yes | yes | yes | yes | yes |
| ReplicationController | `replicationcontroller`, `rc`, `replicationcontrollers` | yes | yes | yes | yes | yes |
| Job | `job`, `jobs` | yes | yes | yes | yes | yes |
| VirtualMachine | `virtualmachine`, `vm`, `virtualmachines` | yes | no | yes | yes | yes |
| VirtualMachineInstance | `virtualmachineinstance`, `vmi`, `virtualmachineinstances` | yes | no | yes | yes | yes |
| Localnet Subnet | `localnet` | cluster-scoped Subnet name | no | yes | yes | no |
| Logical switch port | `logicalswitchport`, `lsp`, `logicalswitchports` | no | no | yes | yes | no |
| Logical router port | `logicalrouterport`, `lrp`, `logicalrouterports` | no | no | yes | yes | no |

`service` and workload targets resolve one running backend Pod. There is no
named-port, NodePort, DNS-name, VIP, EIP or VPC resource selector in this HEAD;
use an IP with `trace --target` and choose a Pod or logical port explicitly.

### `exec`

| Form | Parameters and behavior |
| --- | --- |
| `exec POD -- COMMAND [args...]` | Execute in a named Pod. A bare `POD` uses the selected namespace. |
| `exec KIND/NAME -- COMMAND [args...]` | Resolve one running Pod from a Service or workload selector. Supported kinds are `pod`, `service`, `deployment`, `daemonset`, `statefulset`, `replicaset`, `replicationcontroller` and `job`, including aliases. |
| `exec -l SELECTOR -- COMMAND [args...]` | Select one running Pod by label. Cannot be combined with a resource reference. |
| `exec nbctl|sbctl|ic-nbctl|ic-sbctl -- [TOOL_ARGS...]` | Run the corresponding OVN CLI on its unique leader. |
| `exec vsctl|ofctl|dpctl|appctl --node NODE -- [TOOL_ARGS...]` | Run the corresponding OVS CLI on the selected node. `--node` is required. |

Exec flags: `-c, --container NAME`; `-l, --selector SELECTOR`;
`-i, --stdin`; `-t, --tty`; `-q, --quiet`; and
`--pod-running-timeout DURATION` (default `1m`). Put all remote arguments
after `--`; `exec nbctl show` is invalid, while
`exec nbctl -- show` is valid.
The Kubernetes-style group-qualified form, such as `deploy.apps/web`, is
accepted for `exec`; the group-qualified form is not accepted by `trace`,
`capture` or `network inspect`.

### `trace`

Syntax: `trace TARGET --target IP [flags]`.

| Flag | Default | Supported values and constraints |
| --- | --- | --- |
| `--target IP` | required | Bare IPv4/IPv6 or `addr/IP`; zone identifiers are rejected. |
| `--protocol` | `icmp` | `icmp`, `tcp`, `udp` or `arp`. |
| `--dst-port PORT` | none | Required for TCP/UDP; integer `1..65535`. |
| `--arp-op OP` | `request` | `request` or `reply`; only with IPv4 `--protocol=arp`. |
| `--dst-mac MAC` | auto | Six-byte destination MAC. Required when the destination MAC cannot be inferred. |
| `--engine` | `all` | `all` (OVN then OVS) or `ovn`. LRP forces `ovn`. |
| `--node NODE` | auto | OVS node for `localnet` or a localnet LSP; requires an OVS stage. |
| `--source-ip IP` | inferred | Required for localnet sources; optional override for LRP/LSP cases. |
| `--source-mac MAC` | inferred | Required for localnet sources; optional override for LRP/LSP cases. |

Localnet sources require both `--source-ip` and `--source-mac`. An ordinary
LSP without a bound chassis supports OVN-only trace; localnet LSP follows the
localnet ProviderNetwork path. LRP has no Linux interface and is OVN-only.

### `capture`

Syntax: `capture TARGET [flags] -- [CAPTURE_ARGS...]`.

Plugin flags are `--node NODE` (localnet or OVN-port node selection) and
`--observe-at lsp/NAME` (required for LRP capture). Capture arguments after
`--` are deliberately limited to the following native agent interface:

| Capture argument | Meaning and validation |
| --- | --- |
| `-D` | List capture interfaces; required instead of `-i` for node interface discovery. |
| `-i IFACE` or `-iIFACE` | Select interface. Node capture requires it unless `-D` is used. |
| `-c COUNT` or `-cCOUNT` | Stop after a positive packet count. |
| `-s SNAPLEN` or `-sSNAPLEN` | Snap length from `64` through `1048576`; default `262144`. |
| `-n` / `-nn` | Disable address/name conversion. |
| `-w -` | Stream pcap to stdout. Other output files are rejected. |

The target is resolved before capture. Service/workload targets select one
running Pod; VM/VMI targets select the current valid KubeVirt launcher and
recheck it before capture. LRP capture observes an adjacent bound LSP selected
by `--observe-at`; localnet capture uses the ProviderNetwork interface on the
selected node.

### `network inspect`

Syntax: `network inspect TARGET [-o|--output table|json]`.
The default output is `table`; `json` is intended for automation. Supported
targets are Pod, Node, Service, workload, VM and VMI. OVN ports and localnet
targets are rejected. Pod output includes the network namespace, interfaces,
RX/TX statistics, host-side veth peers and resolved macvlan/ipvlan parents;
Service/workload/VM/VMI output identifies the selected Pod.

### Database commands

| Command | Flags | Effect |
| --- | --- | --- |
| `db health` | none | Check NB/SB storage on every running central Pod without requiring a leader. |
| `db nb status`, `db sb status` | none | Show cluster and storage status from the selected leader. |
| `db nb backup`, `db sb backup` | `--output FILE` | Download a verified standalone backup; default is a unique generated filename. Existing files are never overwritten. |
| `db nb kick SERVER_ID`, `db sb kick SERVER_ID` | `--dry-run` | Remove a stale member; dry-run only resolves and prints the command. |
| `db nb restore` | `--source-node NODE` plus `--yes` or `--dry-run` | Rebuild central NB/SB from the existing database on the explicit source node. This is a destructive operation and is not local-file import. |

### Diagnostics, logs, performance and ACL

| Command | Parameters | Behavior |
| --- | --- | --- |
| `diagnose cluster` | `--read-only`, `--skip-kube-proxy`, repeatable `--external-address IP` | Cluster configuration and active connectivity checks. |
| `diagnose node NODE` | Same flags | Restrict checks to one node. |
| `diagnose subnet SUBNET` | Same flags, `--tcp-port PORT`, `--udp-port PORT` | Subnet probes with temporary DaemonSet/NodePort resources. Port defaults are `$TCP_CONN_CHECK_PORT`/`$UDP_CONN_CHECK_PORT`, then `8100`/`8101`. |
| `diagnose connectivity` | `--skip-kube-proxy`, repeatable `--target tcp://IP:PORT` or `udp://IP:PORT` | Probe explicit endpoints; IPv6 uses brackets. |
| `diagnose environment` | none | Run environment checks on every Linux node through the independent agent. |
| `restart` | none | Restart central, OVS, controller, CNI, pinger and monitor in dependency order. |
| `logs` | `--component all\|kube-ovn\|ovn\|ovs\|linux`; `--output-dir DIR`; `--concurrency N` (4); `--item-timeout DURATION` (30s); `--max-bytes BYTES` (268435456); `--strict` | Collect logs and node state. `--strict` fails if any item fails. |
| `perf run` | `--image IMAGE`; `--duration DURATION` (5s, whole seconds `1s..5m`); `--bandwidth RATE` (`1G`) | Measure Pod, host, Service and multicast performance; temporary resources are cleaned up. |
| `perf recovery` | required `--yes` | Deliberately delete central leaders and measure recovery. |
| `acl decode COOKIE` | none | Decode one ACL sample cookie. |
| `acl listen` | required `--node NODE` | Stream decoded ACL samples from a node until cancelled. |
| `version` | none | Print local client version without contacting the cluster. |
| `completion bash\|zsh\|fish\|powershell` | shell-specific `--no-descriptions` where supported | Generate shell completion. |

## Usage rules and examples

Temporary NodePort probes request `PreferDualStack` and use only the IP
families allocated to the Service. Single-stack clusters use their available
family; missing node addresses for an allocated family are reported as errors.

```console
kubectl ko exec deploy/web -- date
kubectl ko exec svc/web -c app -- sh -c 'echo ready'
kubectl ko exec -n app -l app=web -- curl http://localhost:8080/healthz
kubectl ko exec nbctl --context staging -- --format=json show
kubectl ko exec nbctl -- ls-add example -- lsp-add example example-port
kubectl ko exec ic-nbctl -- --format=json show
kubectl ko exec vsctl --node worker-a -- --timeout=5 show
kubectl ko trace -n app po/web --target 10.0.0.8
kubectl ko trace -n app svc/api --target 10.0.0.8 --protocol tcp --dst-port 443
kubectl ko trace no/worker-a --target 2001:db8::8 --engine ovn
kubectl ko trace localnet/underlay-a --target 10.0.0.8 --source-ip 10.0.0.2 --source-mac 02:00:00:00:00:02 --dst-mac 02:00:00:00:00:08 --node worker-a
kubectl ko trace lsp/web.app --target 10.0.0.8 --engine ovn --dst-mac 02:00:00:00:00:08
kubectl ko trace lrp/router-underlay-a --target 10.0.0.8
kubectl ko capture -n app pod/web -- -i eth0 -c 10
kubectl ko capture -n app vmi/database -- -w - > vm.pcap
kubectl ko capture --namespace app vm/database -- -w - > vm.pcap
kubectl ko capture node/worker-a -- -D
kubectl ko capture no/worker-a -- -i eth0 -c 10
kubectl ko capture localnet/underlay-a --node worker-a -- -c 10
kubectl ko capture lsp/web.app -- -c 10
kubectl ko capture lrp/router-underlay-a --observe-at lsp/web.app -- -c 10
kubectl ko network inspect -n app pod/web
kubectl ko network inspect node/worker-a
kubectl ko network inspect -n app svc/api --output json
kubectl ko db health
kubectl ko db nb status
kubectl ko db nb kick ffffffff --dry-run
kubectl ko db nb restore --source-node worker-a --dry-run
kubectl ko diagnose cluster --read-only
kubectl ko diagnose node worker-a --external-address 1.1.1.1
kubectl ko diagnose subnet ovn-default --tcp-port 8100 --udp-port 8101
kubectl ko diagnose connectivity --target tcp://10.0.0.8:8100 --target 'udp://[2001:db8::8]:8101'
kubectl ko diagnose environment
kubectl ko restart
kubectl ko db nb backup --output northbound.backup
kubectl ko logs --component ovn --output-dir ./support --strict
kubectl ko perf run --image registry.example/test:v1.13.0 --duration 2s
kubectl ko perf recovery --yes
kubectl ko acl decode 0x1234
kubectl ko acl listen --node worker-a
kubectl ko version
kubectl ko completion bash > ~/.local/share/bash-completion/completions/kubectl-ko
```

The separator belongs to the plugin and is removed exactly once. Every argument
after it, including another `--`, `--help`, `--timeout`, `-n`, `-c`, whitespace or
quotes within a single argument, belongs to the remote tool and is preserved.
For `trace`, the positional resource is before the destination flags, for example
`trace -n app po/web --target 10.0.0.8`; a bare name is always a Pod.
`KIND/NAMESPACE/NAME` is rejected; specify the namespace with `-n` instead.
For `capture`, the resource is before the separator; `-n` before `--` selects the
Kubernetes namespace, while `-n` after `--` remains a capture display option.
Kubernetes-style resource execution accepts one Pod or workload reference before
the separator; `-l/--selector` is an alternative that selects one running Pod.
The resource and selector forms never execute against multiple Pods in one
invocation.
`exec nbctl --help` shows local help; `exec nbctl -- --help` shows remote help.
`exec nbctl show` is rejected instead of guessing the argument boundary.

KubeVirt capture resolves a running VMI's current launcher using its controller
UID, node and active Pod status. A `vm/NAME` target additionally verifies that the VMI is
controlled by the requested VM; `vmi/NAME` also supports standalone VMIs. A stopped
VM or unavailable VMI returns an error. During migration, the source launcher
is selected until migration succeeds; a completed failed migration still uses
the source. A successful migration selects the target launcher. The selected
Pod must be Running and not terminating. Missing or ambiguous launchers and a
VMI/Pod change detected before capture fail explicitly; retry after status
settles or use `pod/NAME` to select a launcher yourself. An established capture
stays in that launcher's network namespace and does not follow later migrations.
Target diagnostics go to stderr, so `-w -` preserves pcap stdout. The caller
needs permission to get VMIs, list launcher Pods and, for `vm/NAME`, get VMs.

Supported target kinds are `pod`/`po`/`pods`, `node`/`no`/`nodes`,
`service`/`svc`/`services`, `deployment`/`deploy`/`deployments`,
`daemonset`/`ds`/`daemonsets`, `statefulset`/`sts`/`statefulsets`,
`replicaset`/`rs`/`replicasets`, `replicationcontroller`/`rc`/`replicationcontrollers`,
`job`/`jobs`, `virtualmachine`/`vm`/`virtualmachines`, and
`virtualmachineinstance`/`vmi`/`virtualmachineinstances`, `localnet`,
`logicalswitchport`/`lsp`/`logicalswitchports`, and
`logicalrouterport`/`lrp`/`logicalrouterports`. OVN port names are cluster-scoped;
`-n` applies only to namespaced Kubernetes resources. Localnet uses
`localnet/SUBNET`, where `SUBNET` is the Kube-OVN Subnet name; an LSP whose OVN
type is `localnet` takes the same path. Its OVN port normally has no source IP
or MAC, so both must be supplied. For the OVS stage, the selected node's
ProviderNetwork interface supplies the ingress ofport on `br-PROVIDER`, with
the Subnet VLAN tag. If `exchangeLinkName` is enabled, the actual bridge and
renamed physical NIC are used instead. Without `--node`, exactly one node must have that
ProviderNetwork ready. Capture uses that actual host interface; an explicit
`-i` must match it. An ordinary LSP without a bound chassis supports OVN-only
trace with `--engine ovn`; capture and OVS trace require a bound chassis and its
OVS interface. LRP trace uses its router port IP and MAC for a synthetic packet
by default; `--source-ip` and `--source-mac` override them. An LRP has no Linux
interface, so its trace is OVN-only and capture must select an adjacent, bound
LSP with `--observe-at`. Other VPC and
Kube-OVN CRD kinds require separate adapters.

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
restart the agent. It does not execute in Kube-OVN component Pods. Packet
capture uses the agent's native Linux `AF_PACKET` socket and `setns(2)`;
it does not start `tcpdump`, `nsenter`, or a shell.
`network inspect` resolves Pod, Service, workload, VM and VMI targets to a Pod,
then resolves its netns from OVSDB through the agent, with a
Pod-UID lookup in host process cgroups when no OVS interface exists, then
reads Pod and host links with `ip -s -j -d addr show`; it reports the host-side veth peer by
ifindex and resolves `macvlan`/`ipvlan` parent NICs in the Pod or host netns
using the link namespace identity, name and ifindex,
without entering `kube-ovn-cni` or `ovs-ovn`. Node targets inspect the host
network namespace through the selected node agent.
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

This rewrite replaces the old command interface. Use
`logs --component COMPONENT --output-dir DIR` in CI and support collectors.
Consecutive component collections preserve files already collected in the
output directory. Migrate commands using this table:

| Previous form | New form |
| --- | --- |
| `nbctl ARGS`, `sbctl ARGS` | `exec nbctl -- ARGS`, `exec sbctl -- ARGS` |
| `icnbctl ARGS`, `icsbctl ARGS` | `exec ic-nbctl -- ARGS`, `exec ic-sbctl -- ARGS` |
| `vsctl/ofctl/dpctl/appctl NODE ARGS` | `exec TOOL --node NODE -- ARGS` |
| `nb/sb status`, `nb/sb backup`, `nb/sb kick ID` | `db nb/sb status`, `db nb/sb backup`, `db nb/sb kick ID` |
| `nb dbstatus`, `sb dbstatus` | `db health` |
| `nb restore` | `db nb restore --source-node NODE --yes` |
| `trace POD IP [MAC] PROTOCOL [PORT/OP]` | `trace pod/POD --target IP [--dst-mac MAC] --protocol PROTOCOL [--dst-port PORT/--arp-op OP]` |
| `trace node//NODE ...` | `trace node/NODE --target IP ...` |
| `ovn-trace ...` | `trace --engine ovn ...` |
| `tcpdump POD ARGS` | `capture pod/POD -- ARGS` |
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

Only the Go executable is shipped and installed. Legacy syntax exists only
in version-specific E2E fixtures for older releases.
Build scripts and CI invoke the Go binary directly, never through Bash.

## Development checks

```console
go test -race ./pkg/ko/... ./cmd/kubectl-ko/...
make build-kubectl-ko
make lint
```

The dedicated workflow tests real exec streams, builds all six workstation
platforms and runs file/streaming tests natively on Windows amd64 and arm64.
Chart CI builds the complete image from the current checkout, loads it into
Kind and installs the v2 chart with that image. It checks credential-free agent
readiness on every Linux node, environment output and actual Pod interface
statistics. It also executes the installer's CLI bootstrap phase with a
temporary local destination,
verifies the copy uses the `agent` container, and compares the copied binary
with the current build. It collects OVN and OVS logs with the current CLI's
`logs --component` interface, verifies files on every Linux node, and checks
that OVS collection preserves the earlier OVN files.
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
