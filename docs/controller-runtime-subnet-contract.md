# Controller-runtime Subnet contract

This document is the boundary for the next incremental migration after the
VLAN and ProviderNetwork pilot. It describes the current legacy ownership and
the read-only contract that a future Subnet reconciler must satisfy. The
contract is deliberately separate from production wiring: the current
`CONTROLLER_RUNTIME_MODE` leaves the Subnet writer in the legacy controller.
The explicit `subnet-shadow` mode now wires only the read-only
`SubnetShadowReconciler`; it observes Subnet dependencies without owning status,
finalizers, OVN, or IPAM. All other modes leave that shadow unwired, and no mode
enables the Subnet writer yet. The shadow exposes
`kube_ovn_subnet_shadow_reconciliations_total{result}` and
`kube_ovn_subnet_shadow_reconcile_duration_seconds` for reconcile outcome and
latency observability; these metrics do not change its read-only ownership. The
mode also registers a readiness runnable that waits for the Subnet, Vlan,
ProviderNetwork, and IP caches before shadow workers can run. It exposes
`kube_ovn_subnet_shadow_cache_ready{resource}` and
`kube_ovn_subnet_shadow_cache_sync_duration_seconds`; cancellation or a failed
sync keeps readiness closed and fails manager startup.

The status migration starts with a pure projection seam. The legacy controller
continues to collect IP, Pod, VIP, and EIP inputs, calculate the IP-related
status projection, and remain the only status writer. Runtime code can now
carry a snapshot-bound `SubnetStatusProjection` through an injected projector;
it performs no API write and does not suppress legacy status handling. An
explicit `subnet-status` mode will be added only after this projection source
and its conflict/readback tests are complete.

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
returns a receipt for a confirmed mutation owned by this attempt. No-op and
uncommitted outcomes return no receipt. A committed-but-canceled outcome is
resolved by bounded backend readback; when the address is confirmed, the
adapter persists and returns a receipt together with the cancellation error so
the caller can retain the owner for recovery. If readback cannot confirm the
commit, the adapter fails closed and leaves the uncertainty for recovery. A
receipt must bind the Subnet UID,
mutation revision, and writer ownership epoch, and atomically check them in
the backend before conditionally reverting only its own change. An idempotent
delete by name alone is insufficient. Cancellation never authorizes rollback,
even after a successful apply; the recovered owner resolves pending work.
Compensation inherits writer cancellation and has a five-second deadline;
adapters must honor that context. Compensation errors and deadline expiry are
joined with the apply error. There is no deduplication across plan executions.

`subnet_ipam_adapter.go` is the first concrete IPAM seam, but remains unwired.
It stages allocation and release through `IPAM.WithTransactionContext`, whose
atomic commit is provided by `IPAM.WithTransaction`, so a failed allocation
leaves no committed address and a retry can allocate again. An
existing pod/NIC/subnet allocation is an explicit no-op and returns no receipt;
transaction failures likewise return no receipt. Successful allocations carry
a process-local owner token, Subnet UID, and the allocated address tuple. A
receipt compensates only while that token and tuple still match the adapter's
current owner, and refuses replay or a later owner. This check is process-local
and cannot provide durable fencing or cross-process ownership; production wiring
still needs a durable owner/revision protocol and an OVN adapter.
`IPAM.WithTransactionContext` checks cancellation before and after the atomic
transaction, but cannot interrupt the IPAM mutex while a transaction is
running. Cancellation observed after the action can therefore report an
uncertain result even though the transaction has already committed; the adapter
uses a bounded context without the canceled signal to read back and persist the
owner before returning the cancellation error. A cancellation observed before
receipt compensation starts leaves
the allocation untouched; cancellation during the transaction still requires
readback because the atomic release may already have committed.

If conditional release returns a backend error before ownership is cleared, the
receipt remains available for recovery to retry; the adapter does not discard
ownership after an unsuccessful release.
The same rule applies when the release context is canceled while the backend
operation is in progress: recovery must retry with a fresh context and resolve
the resulting ownership state.

The adapter now consumes a narrow backend interface for exact allocation
lookup, transactional allocation, and conditional release. Lookup receives
the writer context and returns errors separately from the no-op result, so a
durable backend read failure fails closed without attempting allocation or
compensation. Receipt release passes the expected address tuple into the
backend, which validates ownership and releases it in one IPAM transaction;
this closes the lookup/release race for the current backend. A future durable
backend must replace this with an atomic fencing check. The current
implementation wraps the in-memory IPAM transaction API; a future durable
backend can implement the same seam only after preserving the receipt,
ownership, and uncertain-commit guarantees. This interface is testable in
isolation and remains unwired from the production manager.

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
is implemented as an unwired contract. The opt-in `subnet-shadow` mode provides
read-only dependency observation without changing ownership. The next
production increment is opt-in
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
