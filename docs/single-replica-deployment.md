# Single-replica ovn-central deployment

Kube-OVN can run `ovn-central` as a single-replica Deployment with the OVN DB
stored on a PersistentVolume, instead of the default multi-replica raft cluster.
On host failure the pod is rescheduled to another node and reattaches to the
PV, recovering the DB state.

## When to use it

| | Cluster (raft) mode | Single-replica mode |
|---|---|---|
| `ovn-central` replicas | one per master | exactly 1 |
| DB consistency | raft quorum | single ovsdb-server, no consensus |
| Storage | hostPath per master | one PVC, must be attachable on the new node after a failover |
| Failover time | seconds (leader election) | PV detach/attach time, typically minutes |
| Operational complexity | higher (quorum, raft repair) | lower |
| Suitable cluster size | medium / large | small to medium, or constrained environments |

Pick single-replica when you do not have three master nodes available, or when
you prefer a simpler failure model and can tolerate a multi-minute recovery
window during host loss.

## StorageClass requirements

The PVC is `ReadWriteOnce`. For the pod to actually drift between nodes the
StorageClass must support **detach from the failed node and attach on the new
node**. This is true for most network block / file CSIs:

- NFS (`csi-driver-nfs`) — easy to set up, well-tested for failover
- Cloud block storage (EBS, GCE PD, Azure Disk) — works, but detach from a
  dead node can take several minutes
- Ceph RBD, Longhorn, Portworx, etc. — work, see vendor docs for fencing
- `hostPath` provisioners (e.g. local-path-provisioner) **will not drift** —
  the PV is bound to the original node and the new pod will stay `Pending`

`volumeBindingMode: WaitForFirstConsumer` is recommended so the PV topology is
chosen at first pod schedule rather than at PVC creation time.

## Install

### Helm

```yaml
# values.yaml
OVN_CENTRAL_MODE: single

ovn-central:
  storage:
    enabled: true
    storageClassName: my-csi          # leave empty to use the cluster default
    size: 10Gi
    accessModes:
      - ReadWriteOnce
```

```bash
helm install kube-ovn ./charts/kube-ovn -f values.yaml
```

### install.sh

```bash
ENABLE_SINGLE_REPLICA_OVN=true \
  OVN_CENTRAL_STORAGE_CLASS=my-csi \
  OVN_CENTRAL_PVC_SIZE=10Gi \
  bash dist/images/install.sh
```

After install, verify:

```bash
kubectl get pod  -n kube-system -l app=ovn-central          # should be 1 pod
kubectl get pvc  -n kube-system ovn-central-data
kubectl get svc  -n kube-system ovn-nb -o jsonpath='{.spec.selector}'
# Expected: {"app":"ovn-central"}  — no ovn-nb-leader selector
```

## Failover drill

```bash
node=$(kubectl get pod -n kube-system -l app=ovn-central \
  -o jsonpath='{.items[0].spec.nodeName}')

# Simulate node loss
kubectl cordon "$node"
kubectl delete pod -n kube-system -l app=ovn-central

# Wait for the new pod to come up on a different node and the DB to respond
kubectl wait pod -n kube-system -l app=ovn-central \
  --for=condition=Ready --timeout=5m
kubectl exec -n kube-system -l app=ovn-central -- ovn-nbctl show
```

For a real host-loss test (rather than `cordon` + delete), the CSI driver must
support **fencing** — otherwise the volume will stay attached to the dead node
and the new pod will hang in `ContainerCreating` until Kubernetes force-detaches
(typically 6+ minutes).

## Exposing the OVN DB to data planes outside the cluster

In a Kamaji-style topology the kube-ovn control plane (apiserver controllers,
`ovn-central`) runs in a *management* cluster while `ovn-controller`,
`kube-ovn-cni` and `kube-ovn-controller` run in one or more *tenant* clusters.
The tenant components cannot reach a `ClusterIP` Service of the management
cluster, so the `ovn-nb` / `ovn-sb` / `ovn-northd` Services need to be exposed
via `LoadBalancer` (or `NodePort`).

Single-replica mode is a prerequisite: with a stable, non-elected single backend
the LoadBalancer always has exactly one healthy endpoint and there is no leader
flapping when the LB does its own health checks.

### Helm

```yaml
# values.yaml on the management cluster
OVN_CENTRAL_MODE: single

ovn-central:
  storage:
    enabled: true
    storageClassName: my-csi
    size: 10Gi
  service:
    type: LoadBalancer
    loadBalancerIP: 10.99.99.99       # provider-dependent; omit to let LB pick
    externalTrafficPolicy: Local      # optional; preserves source IPs
```

### install.sh

```bash
ENABLE_SINGLE_REPLICA_OVN=true \
  OVN_CENTRAL_STORAGE_CLASS=my-csi \
  OVN_CENTRAL_SERVICE_TYPE=LoadBalancer \
  OVN_CENTRAL_LB_IP=10.99.99.99 \
  OVN_CENTRAL_EXTERNAL_TRAFFIC_POLICY=Local \
  bash dist/images/install.sh
```

### Wiring tenant clusters

The tenant data plane deploys only `ovs-ovn` / `kube-ovn-cni` /
`kube-ovn-controller` (not `ovn-central`) and is told where to connect via
`OVN_DB_IPS`:

```yaml
# data-plane values (or chart fork) — pseudo-config
OVN_DB_IPS: 10.99.99.99      # the LoadBalancer VIP from above
```

The startup scripts already handle a single-entry `OVN_DB_IPS` correctly,
producing `tcp:[10.99.99.99]:6641` / `:6642` connection strings.

> **Security note.** Exposing OVN DB over plain TCP outside the cluster is
> usually unacceptable. Enable SSL (`networking.ENABLE_SSL=true`) and
> distribute the OVN certs to the tenant clusters before pointing them at
> the LB; the existing kube-ovn SSL machinery covers everything once the
> certs are in place.

A full Kamaji integration also needs a "data-plane only" rendering of the
chart that skips `ovn-central` and its PVC — that's tracked separately and
not part of this change.

## Backup and restore

Take a hot backup of the standalone DB at any time:

```bash
pod=$(kubectl get pod -n kube-system -l app=ovn-central -o name | head -n1)
kubectl exec -n kube-system "$pod" -- \
  ovsdb-client backup unix:/var/run/ovn/ovnnb_db.sock > ovnnb_backup.db
```

Restore using the bundled script:

```bash
bash dist/images/restore-ovn-nb-db.sh single ovnnb_backup.db
```

The script scales `ovn-central` to 0, runs a helper pod that mounts the same
PVC to copy the backup into place, then scales back to 1.

## Migrating between modes

**Cluster → single**: scale the existing cluster `ovn-central` to 0, run
`ovsdb-tool cluster-to-standalone` against one of the raft member DB files to
produce a standalone NB DB, reinstall in single mode pointing at an empty PVC,
then use `restore-ovn-nb-db.sh single` to load the standalone DB.

**Single → cluster**: take a backup, reinstall in cluster mode on a fresh
hostPath, and load the backup into the leader using `ovsdb-client`. Verify the
NB DB content matches the backup before deleting the PVC.

## Limitations

- The `kube-ovn-leader-checker` raft queries, `ovn_northd` lock stealing and
  raft header backup are all skipped in single-replica mode. Compaction and DB
  storage-status checks still run.
- `ovn-healthcheck.sh` uses `ovsdb-server/get-db-storage-status` instead of
  `cluster/status`, so the readiness probe will not detect raft-specific issues
  (there are none in this mode).
- During failover all kube-ovn-controller / ovs-ovn / ovn-controller clients
  will see Service connect errors until the new pod is `Ready`. They reconnect
  automatically; in-flight ovsdb transactions during the outage may be retried.
