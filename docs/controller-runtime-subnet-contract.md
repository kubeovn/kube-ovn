# Controller-runtime Subnet contract

This document is the boundary for the incremental migration after the VLAN and
ProviderNetwork pilot. It describes the current legacy ownership and the
contract enforced by the opt-in Subnet writer. Default
`CONTROLLER_RUNTIME_MODE` values leave the Subnet writer in the legacy
controller; `subnet-writer` is the explicit opt-in runtime writer mode.
The explicit `subnet-shadow` mode now wires only the read-only
`SubnetShadowReconciler`; it observes Subnet dependencies without owning status,
finalizers, OVN, or IPAM. All other modes leave that shadow unwired, and no mode
enables the Subnet writer by default. The shadow exposes
`kube_ovn_subnet_shadow_reconciliations_total{result}` and
`kube_ovn_subnet_shadow_reconcile_duration_seconds` for reconcile outcome and
latency observability; these metrics do not change its read-only ownership. The
mode also registers a readiness runnable that waits for the Subnet, Vlan,
ProviderNetwork, and IP caches before shadow workers can run. It exposes
`kube_ovn_subnet_shadow_cache_ready{resource}` and
`kube_ovn_subnet_shadow_cache_sync_duration_seconds`; cancellation or a failed
sync keeps readiness closed and fails manager startup.

The opt-in `subnet-status` mode installs a read-only status observer beside
legacy producers. IP, Pod, VIP, EIP, and NAT enqueues continue to reach the
legacy `updateSubnetStatusQueue` and worker. A callback fan-out mirrors them to
an explicit `SubnetStatusEventSource`, coalesces pending notifications by Subnet
name while the manager starts, and preserves delayed notifications. Other
runtime modes leave this bridge disabled. Non-status spec/dependency events
are rejected before entering the status queue.

The explicit `subnet-writer` mode is opt-in production wiring: it acquires a
dedicated Kubernetes Lease for durable fencing, disables legacy Subnet workers
and startup Subnet syncs, and routes one runtime reconcile through the
Controller's status, finalizer, IPAM, and OVN domain path. Its manager-owned
Subnet queue also watches ProviderNetwork changes and fans them out through the
VLAN-to-Subnet dependency map so readiness recovery causes an immediate retry.

The event source returns an idempotent bridge handle. A controller-runtime
source start reuses one active legacy subscription for the same context, while
a different active context must close the old bridge before starting again.
Source cancellation or explicit shutdown closes the bridge before a later
start may subscribe again. This keeps event delivery tied to the manager
lifecycle and avoids silently redirecting an active legacy watcher to a new
queue; it does not transfer ownership of the legacy status queue.

`SubnetStatusReconciler` reads the Subnet through the manager's uncached API
reader and invokes the legacy read-only IP/status calculator. In observer mode
it never persists the projection; an optional writer is only used by an
explicitly gated mode. The returned
`SubnetStatusProjection` must carry the original identity, spec, generation,
resource version, and deletion state; a projector cannot substitute another
snapshot. Before comparing the projection, a second uncached GET confirms that
the active Subnet revision is unchanged. Changed snapshots return a conflict
for queue retry and fresh calculation. Missing or terminating Subnets converge
without reporting a status difference, and already terminating objects never
reach the calculator. API errors remain retryable. Both Subnet reads have a
five-second deadline.

The comparison covers only the IP calculator's eight IPv4/IPv6 used/available
counter and range fields. Counters are compared as arbitrary-precision integers,
so large IPv6 values retain their precision. Status and projection deep copies
own independent integer backing storage, so later counter mutations cannot
change a captured snapshot. Conditions, gateway MAC, U2O state,
and other status fields are excluded. The observer exposes
`kube_ovn_subnet_status_observations_total{result}` with the bounded results
`match`, `drift`, `conflict`, `not_found`, `terminating`, and `error`, plus
`kube_ovn_subnet_status_observation_duration_seconds`. These metrics have no
Subnet name, address, or source-object labels. Observations never write status,
finalizers, OVN, or IPAM state, and drift alone does not trigger a retry.

A stable Subnet revision is not an atomic snapshot of all informer and IPAM
inputs. The observer reuses the legacy calculator, so a drift observation
records a difference at that moment rather than independent proof of calculator
correctness or persistent divergence. It does not add a full-cache scan or a
periodic resync. Legacy status persistence remains the only production writer
in the default modes; `subnet-writer` is the explicit exception.

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
queue, call OVN, or mutate IPAM. The default modes retain the legacy Subnet
writer; `subnet-writer` replaces its workers behind the durable fence and the
same snapshot/conflict gates.

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
must be present in `ProviderNetwork.status.vlans` before the opt-in runtime
writer may enter the combined status, finalizer, IPAM, and OVN path. The
runtime adapter re-checks this gate immediately before admission and the
Controller facade rejects an active reconcile without the grant; terminating
cleanup and startup recovery retain their separate semantics. The default
modes continue to use the legacy Subnet writer.

`pkg/controller/runtime/subnet_writer_gate.go` and
`pkg/controller/runtime/subnet_writer_ownership.go` now make the writer gates
executable for the opt-in `subnet-writer` mode. Every side effect must
carry an immutable Subnet identity/spec/resource-version snapshot. Status and
finalizer-add operations require an active object; finalizer removal, OVN
cleanup, and IPAM release require a terminating object; OVN/IPAM allocation
requires `AllocationAllowed`. UID, generation, spec, resource-version, and
deletion-state changes return a Kubernetes conflict. A missing recovery callback
fails closed, so a restart cannot allocate or mutate OVN state before durable
recovery completes. Side-effect plans now propagate a `SubnetMutationIdentity`
containing the Subnet UID, generation, resource version, operation, and (when
the durable fence exposes it) writer epoch into the backend context. Concrete
receipts can require and verify that identity; the production Controller facade
still returns no per-mutation receipt, so backend atomic fencing remains a
follow-up step. `subnet_status_writer.go` is the explicit status writer
installed by `subnet-writer`. It performs an uncached snapshot check, uses an
optimistic-lock status patch, preserves foreign condition types, and confirms a
write that may have committed before its response was lost. The same
implementation remains an isolated seam for a future split backend. The
opt-in `subnet-writer` mode reaches status through this writer while the legacy
Subnet workers are disabled. `subnet_finalizer_writer.go` provides the matching
metadata writer and is installed at the finalizer patch boundary. It migrates
the deprecated finalizer on active objects, removes both owned finalizers during
termination, preserves foreign finalizers, and confirms metadata writes after
an uncertain response. `subnet_side_effect_writer.go` is installed around the
OVN/IPAM domain phases and gives them the same recovery, identity, and durable
Lease gate. The production callback still delegates the complete phase to the
Controller facade through an explicit OVN mutation writer seam. The seam
preserves the legacy domain callback while isolating operation-specific OVN
admission. In strict `subnet-writer` mode, that seam acquires a deterministic
OVN `Address_Set` owner marker before the domain callback and conditionally
releases it only after the callback succeeds. Apply or release errors retain the
marker for recovery; they never speculate that an uncertain OVN mutation did
not commit. Recovery rebinds a marker for the current Subnet revision to the
new writer epoch while preserving its pending token, so the next retry takes
over the same fence. Active recovery also enumerates markers for the same
Subnet UID and fails closed on an older generation or operation marker. Delete
recovery enumerates every marker for the UID, rebinds markers from the current
generation to the new writer epoch, retains older-generation markers under
their original CAS owner, and releases all of them only after cleanup
succeeds. This lets a previously uncertain allocation marker converge during
deletion without releasing a fence before the data-plane cleanup. Legacy mode
keeps the existing callback without this marker. IPAM allocation and release
now have concrete per-mutation receipts and owner fencing. OVN mutations also
persist an Active/Releasing receipt in a ConfigMap around the OVSDB marker saga:
uncertain apply/release outcomes retain both records for recovery, while a
confirmed marker release deletes the receipt. Recovery must use the configured
backend-aware `SubnetOVNMutationResolver` to read the concrete OVN mutation
before changing either durable record; marker presence alone is not evidence
that the mutation committed. An active allocation receipt is releasable only
after the resolver reports `Committed`; a confirmed `Absent` allocation
reacquires the marker for a retry while the Subnet is live. During deletion, an
allocation receipt confirmed `Absent` is a tombstone terminal state and is
removed with its marker. An active cleanup receipt is releasable only after
`Absent` is confirmed. A receipt already in `Releasing` follows the same
operation-specific terminal state before its marker and record are removed.
Resolver errors and `Unknown` or pending states retain the receipt and any
existing marker and fail closed, so a restart cannot issue a duplicate mutation
or release ownership prematurely. The ConfigMap and OVSDB steps remain a
retryable saga and are not a two-phase commit. A memory OVN owner-marker CAS
seam is also available for tests. The queued virtual-port parent worker
is also wired through a runtime hook: it reads the Subnet with the uncached API reader,
revalidates UID, generation, spec, resource version, and deletion state, and
only then invokes the OVN backend. Legacy mode keeps the informer-backed
worker.

The runtime writer also exposes concrete IPAM mutation seams for Subnet
registration, address allocation/release, and deletion. `AddOrUpdateSubnet`,
the Subnet-owned U2O/multicast address operations, and `DeleteSubnet` now pass
through operation-specific identity and recovery gates instead of relying only
on the enclosing domain-phase wrapper. Random and static address allocation,
release by NIC, and release by Pod use a typed address request; runtime
installation fails closed unless the backend exposes that typed writer and its
initialized address backend. The typed adapter preserves the legacy release by
Pod semantics while conditionalizing release by NIC on the expected address
tuple. These seams delegate to the existing in-memory IPAM implementation;
allocation receipts are persisted by the configured memory or ConfigMap store.
Release first claims a receipt with a durable CAS (active to releasing)
over the complete allocation, token, Subnet UID, identity, and address, so an
old epoch or rebound receipt cannot invoke the backend.

Each isolated invocation waits for recovery and performs a fresh Subnet GET
with a five-second deadline before checking the immutable snapshot and
operation gate. The writer requires an uncached API reader (the manager's
`GetAPIReader()`); a missing reader, API error, timeout, cancellation, or
changed identity/spec/resource-version/deletion state prevents the action from
running. A terminating tombstone is the one missing-object exception: after
the reconciler confirms that no same-name replacement exists, cleanup may use
the immutable delete snapshot and continue. Callers cannot supply a previously
read `current` object as authorization. State changed during recovery or
between invocations is therefore checked against the API again.

The ownership lease is process-local and epoch-bound: legacy and runtime claims
are mutually exclusive, stale or wrong-owner releases fail closed, and an
active reconcile or manager registration holds an operation guard so release
cannot switch owners during side effects. `subnet-writer` binds this lease to a
dedicated Kubernetes Lease that is acquired, renewed, and checked before each
operation; losing the holder or lease expiry fails closed. A runtime
`SubnetReconciler` cannot be registered without both leases. OVN and IPAM
uncertain commits still require backend ownership checks.

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
OVN marker-backed plans retain their marker on any apply or release error,
because releasing a fence before readback could allow a later writer to race
the uncertain mutation. A receipt must bind the Subnet UID,
mutation revision, and writer ownership epoch, and atomically check them in
the backend before conditionally reverting only its own change. An idempotent
delete by name alone is insufficient. Cancellation never authorizes rollback,
even after a successful apply; the recovered owner resolves pending work.
Compensation inherits writer cancellation and has a five-second deadline;
adapters must honor that context. Compensation errors and deadline expiry are
joined with the apply error. There is no deduplication across plan executions.

`subnet_ipam_adapter.go` remains the isolated receipt seam for future direct
IPAM operations. The opt-in `subnet-writer` currently reaches IPAM through the
Controller domain facade; this adapter documents the stronger receipt contract
needed for a split backend.
It stages allocation and release through `IPAM.WithTransactionContext`, whose
atomic commit is provided by `IPAM.WithTransaction`, so a failed allocation
leaves no committed address and a retry can allocate again. An
existing pod/NIC/subnet allocation is an explicit no-op and returns no receipt;
transaction failures likewise return no receipt. Successful allocations carry
an owner token, Subnet UID, writer epoch, and the allocated address tuple. The
memory store preserves these checks only within a process; the production
ConfigMap store persists them with create-if-absent records and
resource-version guarded compare-and-swap operations. During startup recovery,
a receipt whose Subnet UID, address tuple, and complete old identity still
match is atomically rebound to a new token and the current writer epoch. The
old receipt therefore cannot compensate after a restart; failure to compare
and update the durable record fails closed. A `releasing` receipt is pending
work: recovery removes it when the backend allocation is gone, or rebinds it
to the current epoch so release can be retried when the allocation remains.
An active receipt whose backend allocation is missing fails closed.
Allocations discovered directly from IPAM without a durable receipt are legacy
ownership and are published with `Remember` before further mutation is admitted.
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
receipt remains in releasing for recovery to retry; the adapter does not discard
ownership after an unsuccessful release. A successful backend release is
followed by a claimed-record CAS delete; if that delete is lost, retrying the
pending receipt confirms the uncached backend state before cleanup. When the
backend reports ErrAddressOwnershipChanged, cleanup is allowed only after an
uncached read confirms no address remains; a replacement tuple keeps the
receipt and returns the ownership error.
The same rule applies when the release context is canceled while the backend
operation is in progress: recovery must retry with a fresh context and resolve
the resulting ownership state.

The adapter now consumes a narrow backend interface for exact allocation
lookup, transactional allocation, and conditional release. Lookup receives
the writer context and returns errors separately from the no-op result, so a
durable backend read failure fails closed without attempting allocation or
compensation. Receipt release passes the claimed complete allocation into the
backend, which validates ownership and releases it in one IPAM transaction.
The owner-aware transaction backend fences the opt-in path by the complete
allocation owner (token, Subnet UID, generation, resource version, and writer
epoch) in addition to the address tuple. Legacy tuple APIs remain available for
legacy mode. This interface is testable in isolation; the opt-in production
mode invokes the Controller's owner-aware IPAM facade and keeps this receipt
adapter available for a later split.

## Readiness and deletion gates

The following cases remain legacy-owned in the default modes and must be
covered during the `subnet-writer` rollout and further hardening:

- missing VLAN or ProviderNetwork, a ProviderNetwork that is not Ready, a VLAN
  conflict, and recovery from each condition;
- status-only IP/Pod/VIP events without a spec reconcile or allocation side
  effect;
- API resource-version conflicts and retry convergence without a status
  hot-loop;
- deletion with occupied addresses, U2O interconnection state, finalizer
  removal, OVN errors, and API errors;
- controller restart, cache resync, leader handoff and OVN database restart;
- rollback from `subnet-shadow` or `subnet-writer` to legacy without
  duplicate cleanup or lost IPAM ownership.

The IPAM transaction seam is now available, and the opt-in `subnet-writer`
mode wires the Controller's status, finalizer, OVN, and IPAM path behind the
durable Lease. The opt-in `subnet-shadow` mode still provides read-only
dependency observation without changing ownership. The next
production increment is opt-in
[IPPool scheduling](controller-runtime-ippool.md), which reuses the recovered
legacy backend and its count-pool activation protocol. Further Subnet business
handler extraction and conflict-safe finalizer/OVN/IPAM receipts remain
separate steps. IP is last because Pod, VM, LSP and `syncIPCR` paths depend on
it. In `subnet-writer`, the Controller facade is the single Subnet domain
writer while legacy Subnet workers and startup syncs are disabled; all other
modes remain legacy-owned. The OVN split adapter now has concrete durable
receipts, owner markers, and a backend-aware resolver. The current resolver
confirms the Subnet logical switch as the coarse OVN mutation boundary; a
future increment should bind readback to mutation intent so CIDR, gateway,
ACL, DHCP, and related side-effects can be confirmed individually.
The plan compensation contract is also unwired and does not itself provide
durable backend rollback or a fencing mechanism. Any future split adapter
must provide the same atomic receipt checks, idempotency, uncertain-commit
confirmation, and ownership fencing before its path can be used in
production.
