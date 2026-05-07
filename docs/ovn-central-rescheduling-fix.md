# Design: Resilient OVN-Central Scheduling

## Problem

`ovn-central` embeds the IPs of OVN Central DB nodes as the env var `NODE_IPS`,
baked into the Deployment spec at Helm render time. If a pod is rescheduled onto
a node whose IP was not present at the last Helm render, it crashes on startup:

```bash
if ! echo "$NODE_IPS" | tr ',' '\n' | grep '^<host-ip>$'; then
    echo "ERROR! host ip $DB_CLUSTER_ADDR not in env NODE_IPS $NODE_IPS"
    exit 1
fi
```

Common triggers:

- Node replacement / re-imaging (new host gets a new IP)
- Any infra operation that rotates the underlying machines hosting `ovn-central`

There are two separate `NODE_IPS` failure modes:

1. **Multi-node cluster (≥3 nodes), one node replaced.**
   Two surviving pods keep the cluster alive. The new pod startup crashes before
   it can join the existing RAFT cluster (the hard-exit guard fires). Even after
   the pod joins, the dead node's RAFT membership entry remains, which stalls
   quorum if a second node failure occurs.

2. **Single-node cluster, node replaced.**
   No surviving pod. `is_clustered` returns false (no live peer responds). The
   bootstrap condition checks `nb_leader_ip == DB_CLUSTER_ADDR` where
   `nb_leader_ip` is the first IP in (stale) `NODE_IPS`—the old dead IP. The
   condition is false, so the pod falls into the join branch and tries to
   connect to the dead peer, failing indefinitely.

Single-node clusters are used for non-production environments. Since the
kube-ovn controller reconstructs the OVN database from Kubernetes CRDs on
startup, data loss in a single-node scenario is acceptable.

### OVN_DB_IPS: consumer pods lose connectivity after OVN nodes are rescheduled

Consumer pods (`ovs-ovn`, `kube-ovn-controller`, `ovn-ic-controller` and
upgrade/healthcheck hooks) use `OVN_DB_IPS` to build the OVSDB connection
string for the NB/SB databases. Like `NODE_IPS`, this value is baked in at
Helm render time.

**Multi-node (≥3), one node replaced**: Not actually broken. The OVSDB
multi-address connection string (`tcp:[ip1]:6641,tcp:[ip2]:6641,...`) is handled
natively by `ovn-controller`, which tries each address sequentially. Two of
three IPs remaining live is transparent to the consumer.

**Single-node, node replaced**: The only IP in `OVN_DB_IPS` is dead. All
consumer pods lose connectivity to the NB/SB databases immediately. The existing
Service fallback (`OVN_NB_SERVICE_HOST`, `OVN_SB_SERVICE_HOST`) only activates
when `OVN_DB_IPS` is completely empty—which it never is in deployed clusters.

---

## Approaches considered and rejected

### ConfigMap for NODE_IPS

A controller-maintained ConfigMap holding the live member IPs was considered.
ConfigMaps are propagated to kubelets lazily. During a node rotation, two
concurrently-starting `ovn-central` pods could briefly see different snapshots
of `NODE_IPS` and both elect themselves as the RAFT bootstrap leader, causing
split-brain. Refreshing the env var also does not remove the stale RAFT
membership entry (`cluster/kick` is still required). This approach was dropped.

### Runtime IP filtering with Service fallback (`probe_live_ips`)

An earlier revision added a `probe_live_ips` helper to the five consumer
scripts. At startup each script TCP-probed the IPs in `OVN_DB_IPS` and
silently fell back to the Kubernetes `ovn-nb`/`ovn-sb` Service when every
static IP was unreachable. The upstream maintainers rejected this: the Service
discovery code path is legacy compatibility shim that predates `OVN_DB_IPS`,
and relying on it implicitly can hide configuration errors and may produce
incorrect behaviour if Kubernetes control-plane state is inconsistent at the
time of the reconnection. The preferred approach is to keep `OVN_DB_IPS` correct
rather than work around a stale value at runtime.

---

## Solution

One targeted change to an existing file; no new Kubernetes resources.

### Change 1: `OVN_DB_IPS` reconciliation in `kube-ovn-controller`

**Add a master-node reconciler** to `pkg/controller/node.go` that keeps
`OVN_DB_IPS` (and `NODE_IPS` in `ovn-central`) current whenever the set of
master-labelled node IPs changes.

The reconciler hooks into the existing node event handlers
(`handleAddNode` / `handleDeleteNode` / `handleUpdateNode`). When the handler
processes a node that carries the `kube-ovn/role=master` label, it:

1. Lists all nodes labelled `kube-ovn/role=master` via the existing
   `nodesLister`.
2. Collects their `InternalIP` addresses and forms the canonical comma-joined
   string (sorted for stable comparison).
3. If the computed string differs from the value already in the target
   workloads, patches the relevant env vars:

| Workload | Var patched |
|----------|-------------|
| `ovn-central` Deployment | `NODE_IPS` |
| `kube-ovn-controller` Deployment | `OVN_DB_IPS` |
| `ovs-ovn` DaemonSet | `OVN_DB_IPS` |
| `ovs-ovn-dpdk` DaemonSet | `OVN_DB_IPS` |
| `ovn-ic-controller` Deployment (silently skipped if not deployed) | `OVN_DB_IPS` |

The patch is a standard `strategic merge patch` on the container env array.
Patching triggers a rolling restart of each workload with the corrected value.

**Why this is safe:**

- Patching the controller's own Deployment is harmless: the rolling restart
  brings up a new pod with correct `OVN_DB_IPS`. Kubernetes handles the
  overlap window via `RollingUpdate` strategy.
- The reconciler only needs the Kubernetes API (not OVN). Even if the OVN
  connection is broken because `OVN_DB_IPS` is stale, the controller can still
  watch nodes and issue patches.
- `ovn-central`'s `NODE_IPS` patch triggers a rolling restart. This is safe
  because the existing cluster stays alive during it (2 of 3 nodes survive).
  The new pod will start once `NODE_IPS` is correct, so the hard-exit guard
  will pass on the first attempt after the reconciler fires. In the transition
  window before the patch lands, the new pod will crash-loop once on the
  stale `NODE_IPS`; this is a short, self-healing cycle.

**RBAC additions required**: the existing `ClusterRole` grants
`get/list/watch/create/update/delete` on `deployments`; `patch` is also added.
The `daemonsets` rule currently only grants `get`; `patch` and `update` must be
added to allow the reconciler to update `ovs-ovn` and `ovs-ovn-dpdk`.

---

## Sequence diagrams

### Multi-node: one node replaced

```
Old pod (10.0.0.1) terminates  →  New pod (10.0.0.4) starts
                                   NODE_IPS = "10.0.0.1,10.0.0.2,10.0.0.3"

start-db.sh (cycle 1 — reconciler not yet fired):
  hard-exit guard: 10.0.0.4 ∉ NODE_IPS → exit 1  [CrashLoopBackOff]

kube-ovn-controller (running on surviving cluster):
  Processes node-delete(10.0.0.1) + node-add(10.0.0.4) events
  Patches NODE_IPS="10.0.0.2,10.0.0.3,10.0.0.4" + OVN_DB_IPS in ovn-central
  Rolling restart of ovn-central triggered

start-db.sh (cycle 2 — correct NODE_IPS):
  hard-exit guard: 10.0.0.4 ∈ NODE_IPS → passes
  is_clustered: queries 10.0.0.2 (alive, leader) → result=0
  join branch: calls join-cluster against 10.0.0.2 → 10.0.0.4 joins
```

### Single-node: node replaced

```
Old pod (10.0.0.1) terminates  →  New pod (10.0.0.4) starts
                                   NODE_IPS = "10.0.0.1"

start-db.sh (cycle 1 — reconciler not yet fired):
  hard-exit guard: 10.0.0.4 ∉ NODE_IPS → exit 1  [CrashLoopBackOff]

kube-ovn-controller (still running, OVN connection broken but K8s API fine):
  Processes node-delete(10.0.0.1) + node-add(10.0.0.4) events
  Patches NODE_IPS="10.0.0.4" + OVN_DB_IPS="10.0.0.4" in ovn-central
  Rolling restart triggered

start-db.sh (cycle 2 — correct NODE_IPS):
  hard-exit guard: 10.0.0.4 ∈ NODE_IPS → passes
  is_clustered: queries 10.0.0.4 (self, no cluster yet) → result=1
  bootstrap condition: nb_leader_ip==DB_CLUSTER_ADDR==10.0.0.4 → TRUE
  10.0.0.4 bootstraps as new single-member cluster leader
```

### Single-node: node replaced — consumer reconnection

```
Old pod (10.0.0.1) terminates  →  New pod (10.0.0.4) bootstraps (Change 1)

kube-ovn-controller (still running, OVN connection broken):
  Watches node events: old node (10.0.0.1) deleted, new node (10.0.0.4) added
  Lists master-labelled nodes → new IP set = "10.0.0.4"
  Patches in:
    ovn-central Deployment       → NODE_IPS="10.0.0.4"
    kube-ovn-controller Deployment → OVN_DB_IPS="10.0.0.4"
    ovs-ovn DaemonSet            → OVN_DB_IPS="10.0.0.4"
  Rolling restarts triggered for each workload

kube-ovn-controller (restarted with OVN_DB_IPS="10.0.0.4"):
  gen_conn_str → "tcp:[10.0.0.4]:6641" / "tcp:[10.0.0.4]:6642"
  Connects to OVN; rebuilds NB/SB from CRDs

ovs-ovn (restarted with OVN_DB_IPS="10.0.0.4"):
  gen_conn_str → "tcp:[10.0.0.4]:6642"
  ovs-vsctl set ... ovn-remote="tcp:[10.0.0.4]:6642"
```

---

## Files changed

| File | Change |
|------|--------|
| `pkg/controller/node.go` | Add master-node IP reconciler; patch `OVN_DB_IPS`/`NODE_IPS` in affected workloads on node add/delete/update |
| `charts/kube-ovn/templates/ovn-CR.yaml` | Add `update`/`patch` verb to the `daemonsets` rule so the controller can patch `ovs-ovn` |
| `charts/kube-ovn-v2/templates/rbac/ovn-CR.yaml` | Same RBAC change for the v2 chart |
