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
| Unknown identity, unexpected database name, unknown storage format, or invalid clustered log | Preserve the original database and header. For an invalid clustered log, create a clean member from a valid matching Raft header and a reachable quorum; if the header or quorum is unavailable, keep startup blocked for retry. |

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
