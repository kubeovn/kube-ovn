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
| Unknown identity, unexpected database name, unknown storage format, or invalid clustered log | Preserve the original database and header, refuse automatic repair or rebuilding, and require explicit recovery. |

`ovsdb-tool fix-cluster` can rewrite a locally valid log prefix while discarding
later records. A successful repair followed by a successful `check-cluster`
does not prove that committed history was preserved. Likewise, a reachable
leader, matching cluster ID, or two Ready Pods do not prove that a peer is a
safe recovery source. Automatically repairing multiple members can allow them
to form a majority with a shortened history, conflicting with another member
that still retains the original committed entries.

For this reason, startup does not run `fix-cluster` or replace an invalid
existing database with a new `join-cluster` file. Repeated failed starts do not
create additional full database backups. A member with an invalid database
remains unavailable until explicitly recovered; a healthy majority can
continue serving. If a healthy majority is unavailable, preserving the
remaining history takes precedence over automatically rebuilding it.

Before explicit recovery:

1. Preserve the databases, headers, existing backups, and relevant logs from
   every member, using a consistent backup procedure or stopped databases.
2. Compare the cluster and server identities, snapshot/log history, terms,
   committed entries, and application data. Account for log compaction and
   ordinary replication lag; file sizes and commit indices alone do not
   identify an authoritative copy.
3. Determine a recovery source and the application state that must be retained
   before replacing a member. If histories conflict, reconcile them explicitly
   rather than automatically accepting whichever members started first.

This startup protection does not repair existing divergent histories or fix
the underlying runtime I/O failure. It prevents automatic startup recovery
from silently discarding more history. See [issue #7870](https://github.com/kubeovn/kube-ovn/issues/7870).
