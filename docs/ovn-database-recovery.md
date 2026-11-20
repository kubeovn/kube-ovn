# OVN database zero-tail validation

Central and IC startup and health probes share `ovn-db-check.py`. It first runs
`ovsdb-tool check-cluster`. If that fails, it accepts only an all-zero suffix
after complete records with valid lengths and SHA-1 checksums. The complete
prefix must also pass `check-cluster` on a private temporary copy, and the source
file must remain unchanged throughout validation.

For this case, startup preserves the original database, cluster ID and server
ID, and lets `ovsdb-server` load the valid history. It does not require live
peers to repair or rebuild the member. This allows all members to start when
all their journals end in recoverable zero bytes after a simultaneous outage.
Probes use the same validation so they do not reject such a running member.

The helper never trims, repairs or replaces the original file. Internal
corruption, checksum errors, nonzero garbage, other incomplete records and Raft
semantic errors fail validation and continue through this branch's existing
recovery path. That legacy path can invoke `fix-cluster` and rebuild a member;
this backport does not change its behavior for other corruption. A successful
repair or a Ready Pod alone does not prove committed history was retained.
Before explicit recovery, preserve all databases, headers and logs, and compare
member identities, committed history and application data across the ensemble.

The shared Kind HA suite is fetched from master by maintenance-branch CI.
The zero-tail NB/SB cases in #8061 require Kube-OVN >= v1.15 and at least three
central replicas. They stop all members, append 80 zero bytes to every copy,
and verify committed data, new writes, member/file identities and new cross-node
Pod connections. Until #8061 reaches master, maintenance CI cannot run those
new cases through its normal shared E2E checkout.
