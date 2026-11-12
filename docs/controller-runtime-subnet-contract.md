# Controller-runtime Subnet contract

This document is the boundary for the next incremental migration after the
VLAN and ProviderNetwork pilot. It describes the current legacy ownership and
the read-only contract that a future Subnet reconciler must satisfy. The
contract is deliberately separate from production wiring: the current
`CONTROLLER_RUNTIME_MODE` leaves the Subnet writer in the legacy controller.

## Single-writer matrix

| Surface | Current writer | Future shadow may do | Future writer gate |
| --- | --- | --- | --- |
| `Subnet.spec` defaults and normalization | `formatSubnet` in `pkg/controller/subnet.go` and startup `syncSubnetCR` | Read and compare | Contract tests plus conflict-safe `envtest` |
| `Subnet.status.conditions` and IP counters | `patchSubnetStatus`, `calcSubnetStatusIP`, and status workers in `pkg/controller/subnet_status.go` | Observe and report drift | No status hot-loop under repeated reconcile |
| `Subnet.status.gatewayMAC` | `patchSubnetGatewayMAC` and `recordSubnetGatewayMAC` | Observe | API conflict and retry tests |
| Subnet finalizer | `syncFinalizers`/`syncSubnetFinalizer` and subnet deletion handling | Observe deletion and finalizer state | Occupied IP, U2O, OVN/API error and restart tests |
| OVN logical switch/router, DHCP, ACL, routes and virtual ports | `handleAddOrUpdateSubnet`, `handleDeleteSubnet`, and related workers | No calls | OVN adapter transaction seam and native OVN restart tests |
| IPAM subnet allocation and release | `c.ipam` calls from Subnet, Pod, IP and IPPool handlers | No calls | IPAM transaction seam and allocation consistency tests |
| `IPPool` status/finalizer/address set | Existing handlers, scheduled by runtime only in opt-in `ippool` mode | Observe only | Shared recovered backend; see [IPPool scheduling](controller-runtime-ippool.md) |
| `IP` status/finalizer/address ownership | Existing IP handlers and Pod/VIP paths | Observe only | Migrate after IPPool |

The shadow must not patch a status, add/remove a finalizer, enqueue a legacy
queue, call OVN, or mutate IPAM. The production Subnet writer remains the
single writer until every row has an explicit replacement and rollback gate.

## Dependency and event map

| Source event | Subnet consequence | Path | Ownership constraint |
| --- | --- | --- | --- |
| Subnet spec or deletion event | Normalize, validate, reconcile or delete the Subnet | `spec-reconcile` | Legacy Subnet writer owns spec, finalizer and cleanup |
| Vlan change | Re-check VLAN conflict/readiness and dependent localnet work | `spec-reconcile` | VLAN/ProviderNetwork writer owns VLAN status and localnet tag |
| ProviderNetwork readiness or membership | Re-check a VLAN-backed Subnet gate | `spec-reconcile` | ProviderNetwork writer owns ProviderNetwork status |
| IP, Pod, VIP, EIP or NAT change | Recalculate derived Subnet usage/status | `status-observe` | Status-only path cannot allocate or mutate OVN |
| Node change | Observe indirectly through ProviderNetwork | `dependency-observe` | Node/ProviderNetwork path owns node readiness |
| Subnet change | Enqueue dependent IPPool and VPC status work | `spec-reconcile` plus dependent status observation | IPPool and VPC remain their own writers |

The executable vocabulary for this map is in
`pkg/controller/runtime/subnet_contract.go`. `ObserveSubnet` reads only the
Subnet, its VLAN, and its ProviderNetwork. Missing dependencies are represented
as an unready observation. `SubnetAllocationGate` is a read-only gate: a VLAN
must exist without conflict, its ProviderNetwork must be Ready, and the VLAN
must be present in `ProviderNetwork.status.vlans` before a future writer may
allocate IPAM or create OVN objects. This is stricter than the current legacy
validator and is intentionally not wired into production by this PR.

`pkg/controller/runtime/subnet_writer_gate.go` now makes the writer gates
executable without enabling a second production writer. Every side effect must
carry an immutable Subnet identity/spec/resource-version snapshot. Status and
finalizer-add operations require an active object; finalizer removal, OVN
cleanup, and IPAM release require a terminating object; OVN/IPAM allocation
requires `AllocationAllowed`. UID, generation, spec, resource-version, and
deletion-state changes return a Kubernetes conflict. A missing recovery callback
fails closed, so a restart cannot allocate or mutate OVN state before durable
recovery completes. `subnet_status_writer.go` adds the next unwired status-only
implementation: it performs an uncached snapshot check, uses an optimistic-lock
status patch, preserves foreign condition types, and confirms a write that may
have committed before its response was lost. The gate and writer remain an
unwired contract until the legacy Subnet writer has an explicit replacement and
rollback path. `subnet_finalizer_writer.go` adds the matching unwired metadata
contract: it migrates the deprecated finalizer on active objects, removes both
owned finalizers during termination, preserves foreign finalizers, and confirms
metadata writes after an uncertain response. `subnet_side_effect_writer.go` adds
the unwired OVN/IPAM side-effect boundary: each allocate or cleanup/release
callback is executed only after the matching snapshot operation and recovery
gate pass, and callback or context failures fail closed without retrying the
side effect in the contract. Each invocation waits for recovery and performs
a fresh Subnet GET with a five-second deadline before checking the immutable
snapshot and operation gate. The writer requires an uncached API reader (the
manager's `GetAPIReader()`); a missing reader, missing object, API error,
timeout, cancellation, or changed identity/spec/resource-version/deletion state
prevents the action from running. Callers cannot supply a previously read
`current` object as authorization. State changed during recovery or between
invocations is therefore checked against the API again.

This read does not make the API check and OVN/IPAM mutation atomic. Changes
after the GET still require backend identity/ownership fencing and recovery;
the reader alone does not provide those guarantees.

`SubnetSideEffectPlan` accepts only those four
OVN/IPAM operations. A failed apply can be compensated only when the adapter
returns a receipt for a confirmed mutation owned by this attempt. No-op,
uncommitted, and uncertain outcomes return no receipt; uncertainty must be
resolved by backend readback or recovery. A receipt must bind the Subnet UID,
mutation revision, and writer ownership epoch, and atomically check them in
the backend before conditionally reverting only its own change. An idempotent
delete by name alone is insufficient. Cancellation never authorizes rollback,
even after a successful apply; the recovered owner resolves pending work.
Compensation inherits writer cancellation and has a five-second deadline;
adapters must honor that context. Compensation errors and deadline expiry are
joined with the apply error. There is no deduplication across plan executions.

## Readiness and deletion gates

The following cases remain legacy-owned and must be covered before enabling a
Subnet writer:

- missing VLAN or ProviderNetwork, a ProviderNetwork that is not Ready, a VLAN
  conflict, and recovery from each condition;
- status-only IP/Pod/VIP events without a spec reconcile or allocation side
  effect;
- API resource-version conflicts and retry convergence without a status
  hot-loop;
- deletion with occupied addresses, U2O interconnection state, finalizer
  removal, OVN errors, and API errors;
- controller restart, cache resync, leader handoff and OVN database restart;
- rollback from a future `subnet-shadow`/`subnet-new` mode to legacy without
  duplicate cleanup or lost IPAM ownership.

The IPAM transaction seam is now available, and the status-only Subnet writer
is implemented as an unwired contract. The next production increment is opt-in
[IPPool scheduling](controller-runtime-ippool.md), which reuses the recovered
legacy backend and its count-pool activation protocol. Further Subnet business
handler extraction and conflict-safe finalizer/OVN/IPAM writes remain separate
steps. IP is last because Pod, VM, LSP and `syncIPCR` paths depend on it.
Subnet status, finalizer, and OVN/IPAM side-effect writers remain legacy-owned
in every currently supported mode. Wiring side effects requires concrete OVN
and IPAM adapters, durable recovery, and an explicit rollback/ownership gate.
The plan compensation contract is also unwired and does not itself provide
durable backend rollback or a fencing mechanism. Concrete adapters must
implement atomic receipt checks, idempotency, uncertain-commit confirmation,
and ownership fencing before this contract can be used in production.
