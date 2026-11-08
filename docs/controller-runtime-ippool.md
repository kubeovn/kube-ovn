# Controller-runtime IPPool scheduling

The experimental `--controller-runtime-mode=ippool` moves IPPool scheduling and
retries to a controller-runtime reconciler. The existing controller remains the
shared business backend: allocation, address-set preparation, membership status,
statistics, finalizers and namespace notifications retain their existing handlers.
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
  -> start runtime with that same Controller backend
  -> start legacy workers for resources that have not migrated

IPPool informer events + Subnet membership/statistics notifications
  -> existing unbounded ingress queues
  -> runtime source
  -> one typed runtime queue / retry loop
  -> shared per-IPPool lock
  -> live IPPool API read
  -> membership, statistics-only, or deletion handler
```

The three legacy IPPool business workers are disabled only in `ippool` mode.
Their queues remain ingress so informer events received before recovery and
explicit notifications from Subnet handlers are retained. Statistics notifications
must remain explicit: changes between pools can leave total Subnet usage
unchanged, producing no Subnet API event. They never trigger membership activation,
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

## Remaining migration work

This stage intentionally retains legacy informer ingress, startup restoration
and business handlers. Extracting an IPPool domain adapter, migrating its writes
to conflict-safe runtime clients, and replacing ingress with native watches are
later increments. The scheduling migration alone does not satisfy the Subnet
writer's OVN, conflict, deletion and restart gates.
