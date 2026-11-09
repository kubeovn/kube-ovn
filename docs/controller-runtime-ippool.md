# Controller-runtime IPPool scheduling

The experimental `--controller-runtime-mode=ippool` moves IPPool scheduling,
retries, native IPPool watches and API writes to controller-runtime. The existing
controller remains the shared business backend for allocation, address-set
preparation and namespace
notifications. Its API persistence calls delegate to a native writer in this mode.
This increment does not create another IPAM or migrate Subnet/IP writers.

## Modes and ownership

| Mode | VLAN / ProviderNetwork scheduling | IPPool scheduling |
| --- | --- | --- |
| `disabled` (default) | Legacy | Legacy |
| `manager` | Legacy | Legacy |
| `vlan` | Runtime | Legacy |
| `ippool` | Runtime | Runtime, using the shared legacy backend |

For the installer, set `CONTROLLER_RUNTIME_MODE=ippool`. Restart the controller
with `vlan` or `disabled` to return IPPool scheduling to legacy. There is no CRD
or durable data-format change. Mode selection is process-wide, not per object.
The outer controller leader lease remains the ownership boundary for both paths.

## Startup and event flow

```text
legacy informer caches sync
  -> InitOVN / syncIPCR / syncFinalizers
  -> InitIPAM (restore durable count-pool address sets and membership)
  -> syncSubnetCR / syncVlanCR
  -> install the native IPPool writer with the leader context
  -> start runtime with that same Controller backend
  -> start legacy workers for resources that have not migrated

manager IPPool initial list + native create/update/delete watch
  -> typed lifecycle requests (deletions retain independent snapshots)
Subnet membership/statistics notifications + legacy deletion snapshots
  -> existing unbounded ingress queues
  -> runtime source
both sources
  -> one typed runtime queue / retry loop
  -> shared per-IPPool lock
  -> live IPPool API read
  -> membership, statistics-only, or deletion handler
```

The three legacy IPPool business workers are disabled only in `ippool` mode.
Legacy IPPool create and membership-update events are disabled in this mode.
The queues retain explicit notifications from Subnet handlers before recovery or
while the manager is busy. The legacy delete handler remains a snapshot backstop:
a pool restored into IPAM can disappear between recovery and the manager's initial
list. A relist UID replacement also retains the old object as a deletion snapshot.
Dropping those events would orphan reservations and OVN state. Native and
legacy deletion snapshots share the same runtime worker, per-pool lock and
idempotent replacement-aware cleanup; duplicate notifications do not add a writer.
The manager waits for its IPPool cache to sync before processing either source.
Native updates reconcile spec or ordered `status.ips` changes and terminating
objects. Statistics, conditions, finalizers and metadata alone do not trigger
membership reconciliation. Initial terminating objects are routed to cleanup.

Statistics notifications must remain explicit: changes between pools can leave
total Subnet usage unchanged, producing no Subnet API event. They never trigger membership activation,
OVN preparation or namespace notifications for a live pool.

The runtime uses one worker and retains the legacy IPPool queues' default retry
limiter: exponential backoff from 5 ms to 1000 s, combined with a 10 QPS token
bucket with a burst of 100. The `--cust-crd-retry-min-delay` and
`--cust-crd-retry-max-delay` flags do not configure IPPool retries.
Errors are retried only by the runtime queue. Ingress forwarding stops with the
leader context; shutdown waits for the runtime before closing shared OVN clients.

## Allocation and deletion boundaries

Count pools continue to use `SelectIPPoolIPs` / `ActivateIPPoolIPs`: selections
remain reserved and unavailable to Pods during OVN preparation and durable
membership persistence. Failed preparation retains reservations for retry. The
ordinary allocation transaction callback is not used here because activation
deliberately releases the allocation lock during external I/O.

Deletion requests retain informer snapshots for force-deleted objects. Under
the shared pool lock, the backend first reads the live API. A snapshot for an old
UID preserves a same-name replacement's state. If the replacement uses a different
Subnet, its live reconciliation or terminating cleanup must succeed before the
old Subnet's orphaned IPAM entry is removed. Preparation or API failures retain
the old reservations and retry the deletion snapshot. A missing object is cleaned
up without attempting a finalizer patch; an existing terminating object uses its
current snapshot. Address-set preparation or cleanup still precedes returning
reservations to the default pool. Terminating objects received by an initial
informer list follow the same cleanup path.

## API write ownership and conflicts

The native writer uses the manager client for writes and its uncached APIReader
for validation and readback. It is installed once, before runtime processing
starts, only in `ippool` mode. Startup finalizer migration remains a legacy
compatibility step, and every other mode keeps its existing persistence path.

| Operation | Fields written |
| --- | --- |
| Membership | `status.ips`, preserving allocation order, or clearing it in explicit mode |
| Statistics notification | The four IP counters and four range fields only |
| Reconciliation status | Statistics and Kube-OVN `Ready` / `Error` conditions; foreign conditions are retained |
| Finalizer | Add/remove the Kube-OVN finalizer names; foreign finalizers are retained |

Before each operation, a live read must match the reconciliation snapshot's UID,
generation, spec and resource version. Membership/status/add-finalizer operations
also reject terminating objects. The subsequent merge patch carries the resource
version as an optimistic lock, so an update or same-name replacement during
external I/O cannot receive an old snapshot's status or finalizer mutation.
Conflicts return to the queue to restart the operation from a fresh snapshot.
Membership reconciliation repeats selection and OVN preparation; a statistics
notification only recomputes statistics. The writer never blindly replays old
membership.

Successful writes advance the working object's metadata for its next write while
retaining pending conditions. A failed membership response is confirmed through a
fresh uncached read only when UID, generation, spec and ordered membership still
match and the object is not terminating. Otherwise activation retains its blocked
reservations for retry. Each write/read uses a separate five-second timeout under
the leader context; cancellation cannot confirm activation. Failed finalizer
addition aborts before address selection, including when the object disappeared.
No-op membership/status/finalizer writes do not patch the API.

## Remaining migration work

This stage retains startup restoration, business handlers, explicit dependency
notifications and the legacy deletion backstop. Extracting an IPPool domain adapter
and making recovery atomic with native watch ownership are later increments.
The legacy IPPool lister remains available to other resource handlers; this stage
adds a manager cache for IPPools and does not claim a cache-memory reduction.
The IPPool migration does not satisfy the Subnet writer's OVN, conflict, deletion
and restart gates.
