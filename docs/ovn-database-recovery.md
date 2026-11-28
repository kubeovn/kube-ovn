# OVN database startup validation

During central startup in Raft mode and interconnection database startup,
preflight checks preserve existing database files when their identity, storage
format, or clustered log cannot be validated. They report the validation error
and exit with a nonzero status.

Central single-instance standalone startup retains its existing mode selection
and startup path, without the database-identity preflight described here.

| Database state | Startup behavior |
| --- | --- |
| Valid clustered database | Start normally and let Raft synchronize the member. |
| Valid standalone database | Preserve the existing standalone startup path. |
| Missing database | Preserve the existing initialization and header recovery paths. |
| Pending cluster join | Resume the join. Central databases retain their existing stale pending-join cleanup. |
| Complete valid Raft history followed only by zero bytes | Validate a private copy of the complete prefix, preserve the original member file and identity, and let `ovsdb-server` recover its physical tail without requiring live peers. |
| Unknown identity, unexpected database name, unknown storage format, or invalid clustered log | Preserve the original database and header. For an invalid clustered log, create a clean member from a valid matching Raft header and a reachable quorum; if the header or quorum is unavailable, keep startup blocked for retry. |

Central and IC startup and health probes share `ovn-db-check.py`. It first runs
normal `check-cluster`. If that fails, it accepts only an all-zero suffix after
complete records with valid lengths and SHA-1 checksums, and requires the
unchanged prefix to pass a separate `check-cluster` on a private temporary copy.
The original file is never trimmed, repaired, or replaced. A concurrent append
or replacement fails validation. Internal corruption, checksum failures,
nonzero garbage, other incomplete records, and Raft semantic errors retain the
existing failure and quorum-based recovery behavior.

This avoids a startup dependency cycle when all members have recoverable zero
tails: requiring an already running quorum to rebuild every member would stop
all of them before any database process starts. Probes use the same validation
so they do not restart a server that has loaded such a recoverable tail.

The Kind HA suite exercises NB and SB recovery with an 80-byte zero tail on
every stopped member, without a live quorum. These disruptive specs require
Kube-OVN v1.15 or later and at least three central replicas. They verify the
committed marker on every member, fresh writes, cluster/server IDs, the original
member files, and new cross-node Pod connections. Failed specs restore their
saved test databases after capturing failure diagnostics.
Backups are removed only after the replica count, rollout, leaders, and database
membership have recovered. A failed restart retains the complete backup set.

`ovsdb-tool fix-cluster` can rewrite a locally valid log prefix while discarding
later records. A successful repair followed by a successful `check-cluster`
does not prove that committed history was preserved. Likewise, a reachable
leader, matching cluster ID, or two Ready Pods do not prove that a peer is a
safe recovery source. Automatically repairing multiple members can allow them
to form a majority with a shortened history, conflicting with another member
that still retains the original committed entries.

For this reason, startup does not run `fix-cluster` on an invalid member. It
requires a valid Raft header whose cluster ID matches a reachable quorum, then
creates a clean `rejoin-cluster` file with the header's server ID and peer
addresses. The original database is moved to a timestamped backup only after
the new file is created successfully. Starting the clean member lets the
healthy majority repopulate it through normal Raft log or snapshot
synchronization. If the disk is still full, the header is invalid, a quorum is
unavailable, or the replacement cannot be installed, startup exits nonzero and
the member remains unavailable for a later retry.

The liveness and readiness checks also run `ovsdb-tool check-cluster` for
clustered databases. A semantic persistence gap therefore removes the member
from service and lets kubelet restart it; a successful process or Pod status
alone does not hide the gap. The recovery path must still be exercised with
only one damaged member at a time. Automatically rebuilding multiple members
without a verified healthy majority remains unsafe.

Before explicit recovery:

1. Preserve the databases, headers, existing backups, and relevant logs from
   every member, using a consistent backup procedure or stopped databases.
2. Compare the cluster and server identities, snapshot/log history, terms,
   committed entries, and application data. Account for log compaction and
   ordinary replication lag; file sizes and commit indices alone do not
   identify an authoritative copy.
3. Determine a recovery source and the application state that must be retained
   before replacing a member. If histories conflict, reconcile them explicitly
   rather than automatically accepting whichever members started first. The
   automatic path is limited to one invalid member with a reachable, consistent
   majority.

This startup protection does not repair already divergent histories or fix the
underlying runtime I/O failure. It limits automatic recovery to a fresh member
that can synchronize from a reachable, consistent majority using the preserved
Raft identity. See [issue
#7870](https://github.com/kubeovn/kube-ovn/issues/7870).

## Rejoining after losing the database directory

In a static `NODE_IPS` cluster, a member whose database directory is erased
cannot recover its old Raft server ID. It may rejoin with a new ID while the
old ID remains in the cluster configuration. The leader checker removes that
old ID only when `cluster/status` shows two stable members with the same Raft
endpoint, the old member has been silent for at least
`OVN_LEADER_STALE_MEMBER_TIMEOUT` (120 seconds by default), and the current
leader still has a quorum. The observation must remain true for three checks.
The replacement must have a positive replicated log index at least as high as
the old member's last replicated index. The old index can remain nonzero after
the original member stops; it is not evidence of a current connection.

A member that is merely powered off or undergoing maintenance has no
replacement member at the same endpoint, so it is not automatically kicked,
regardless of how long it remains offline. `cluster/kick` is never issued for
a normal single stale member. If a replacement is still catching up, a
reconfiguration is in progress, or quorum is unavailable, automatic cleanup
stops and the existing `kubectl ko db nb|sb kick SERVER_ID` procedure remains
the manual recovery path.
