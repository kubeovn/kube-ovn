# Kamaji-style split-cluster deployment

In a Kamaji-style topology the tenant's Kubernetes control plane (apiserver,
controller-manager, scheduler, etcd) runs as pods in a **management cluster**
while the tenant's worker nodes run in a separate **data-plane cluster**. Each
tenant gets its own apiserver instance hosted in the management cluster.

Kube-OVN can be split along the same boundary:

```
┌────────────────────────────────────────┐         ┌────────────────────────────────────────┐
│  Management cluster                    │         │  Tenant data-plane cluster             │
│                                        │         │                                        │
│  ┌────────────────┐                    │         │  ┌────────────────┐                    │
│  │ ovn-central    │  (single replica   │         │  │ kube-ovn-      │                    │
│  │  + PVC         │   recommended)     │  TLS    │  │ controller     │                    │
│  └─────────┬──────┘                    │ <─────> │  └────────────────┘                    │
│            │                           │ ovn-nb  │  ┌────────────────┐                    │
│  ┌─────────▼──────┐                    │ ovn-sb  │  │ ovs-ovn (DS)   │                    │
│  │ ovn-nb /       │  (LoadBalancer)    │         │  │ kube-ovn-cni   │                    │
│  │ ovn-sb /       │ ──── 10.99.99.99 ──┼─────────┼─►│ kube-ovn-pinger│                    │
│  │ ovn-northd svc │                    │         │  └────────────────┘                    │
│  └────────────────┘                    │         │  + Subnet / IP / Vpc CRDs in tenant   │
│                                        │         │    apiserver                           │
└────────────────────────────────────────┘         └────────────────────────────────────────┘
```

The same Helm chart is installed twice, with different `installMode` values
targeting different `--kube-context`s.

## Component placement

| Component | Where it runs | Why |
|---|---|---|
| `ovn-central` (Deployment + PVC + nb/sb/northd Services) | Management cluster | Centralised OVN DB; PVC keeps DB durable across pod drift |
| `kube-ovn-controller` | Tenant cluster | Watches tenant Subnet/IP/Vpc CRs — best done with in-cluster auth |
| `ovs-ovn`, `kube-ovn-cni`, `kube-ovn-pinger` (DaemonSets) | Tenant cluster | They program local OVS on every tenant node |
| `kube-ovn-monitor` (Deployment) | **`full` mode only** | Reads ovn-central's local Unix sockets and DB files; would crashloop in a tenant-only install. Tracked as follow-up. |
| Kube-OVN CRDs (`kubeovn.io/v1` …) | Tenant apiserver | Tenants `kubectl create subnet` against their own apiserver |
| `kube-ovn-tls` Secret | **Both clusters** | ovn-central serves SSL listeners (mgmt); controller / ovs-ovn use client certs (tenant) |

## Prerequisites

- Two reachable clusters with separate kubeconfigs / contexts (`mgmt`, `tenant`).
- A LoadBalancer (or NodePort / Ingress) in the management cluster that exposes
  ports 6641 (NB) and 6642 (SB) of the `ovn-nb` / `ovn-sb` Services. The
  tenant cluster's pods must be able to reach that VIP/hostname.
- A StorageClass in the management cluster that supports cross-node attach
  (NFS-CSI, Ceph RBD, cloud block storage) — see
  [single-replica-deployment.md](./single-replica-deployment.md).
- Recommended: enable SSL (`networking.ENABLE_SSL=true`) before exposing OVN DB
  ports outside the cluster. Plain TCP across cluster boundaries should be
  considered insecure.

## Install

### 1. Management cluster — `controlPlaneOnly`

```yaml
# mgmt-values.yaml
namespace: kube-system

installMode: controlPlaneOnly
OVN_CENTRAL_MODE: single

ovn-central:
  storage:
    storageClassName: my-csi
    size: 10Gi
  service:
    type: LoadBalancer
    loadBalancerIP: 10.99.99.99        # provider-dependent; omit for auto
    externalTrafficPolicy: Local       # preserves tenant source IPs (optional)

networking:
  ENABLE_SSL: true                     # strongly recommended
```

```bash
helm install --kube-context=mgmt kube-ovn ./charts/kube-ovn -f mgmt-values.yaml
```

This release renders:

- `ovn-central` Deployment + `ovn-central-data` PVC
- `ovn-nb` / `ovn-sb` / `ovn-northd` Services (`type: LoadBalancer`)
- `kube-ovn-tls` Secret (SSL keying material)
- `ovn-ovs` ServiceAccount + `system:ovn-ovs` ClusterRole + binding

…and nothing else. No CRDs, no agents.

After install, capture the LoadBalancer's ingress IP/hostname and copy the
`kube-ovn-tls` Secret out of the management cluster — the tenant cluster
needs the same TLS material.

```bash
# Pull the assigned VIP (if you let the LB pick)
kubectl --context=mgmt -n kube-system get svc ovn-nb \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

# Export the TLS Secret for syncing to the tenant cluster
kubectl --context=mgmt -n kube-system get secret kube-ovn-tls -o yaml > kube-ovn-tls.yaml
```

### 2. Tenant data-plane cluster — `dataPlaneOnly`

First seed the TLS Secret into the tenant cluster's `kube-system` namespace
(or whatever you set `.Values.namespace` to). In production, use tooling like
`external-secrets`, `sealed-secrets`, or Argo CD's secret syncing rather than
`kubectl apply -f kube-ovn-tls.yaml`.

```bash
kubectl --context=tenant -n kube-system apply -f kube-ovn-tls.yaml
```

Then install the data-plane release pointing at the management cluster's LB:

```yaml
# tenant-values.yaml
namespace: kube-system

installMode: dataPlaneOnly
externalOvnCentral:
  endpoint: 10.99.99.99                # or DNS name of the mgmt cluster LB
  nbPort: 6641
  sbPort: 6642

networking:
  ENABLE_SSL: true                     # must match mgmt cluster
```

```bash
helm install --kube-context=tenant kube-ovn ./charts/kube-ovn -f tenant-values.yaml
```

This release renders:

- Kube-OVN CRDs (Subnet / IP / Vpc / …) — installed into the **tenant apiserver**
- `kube-ovn-controller` Deployment with `OVN_DB_IPS=10.99.99.99` →
  start-controller.sh builds `tcp:[10.99.99.99]:6641` and `tcp:[…]:6642`
- `ovs-ovn`, `kube-ovn-cni`, `kube-ovn-pinger` DaemonSets — also pointing at
  the management cluster's LB via `OVN_DB_IPS`
- All the related ServiceAccounts / ClusterRoles / RoleBindings

…and **does not** render `ovn-central` or its Services.

## Verifying connectivity

After both installs, on the tenant cluster:

```bash
# kube-ovn-controller must be Ready
kubectl --context=tenant -n kube-system rollout status deploy/kube-ovn-controller

# Active TCP connection from ovn-controller (in the ovs-ovn DS) to the LB
kubectl --context=tenant -n kube-system exec ds/ovs-ovn -- \
  ss -tnp | grep ':6642'

# Should show ESTAB ... <node IP>:<port> 10.99.99.99:6642 users:(("ovn-controller",...))

# Sanity: create a tenant Subnet, watch a pod get an IP
kubectl --context=tenant create -f - <<EOF
apiVersion: kubeovn.io/v1
kind: Subnet
metadata: {name: smoke}
spec:
  cidrBlock: 10.50.0.0/16
  default: false
EOF
```

## Version lockstep

The chart enforces that both installs use the same `kube-ovn` image tag (via
`global.images.kubeovn.tag`). Keep them aligned — if you upgrade the
management cluster's chart, upgrade the tenant cluster's chart in the same
window. Cross-version OVN schema drift can wedge `ovn-northd` reconciliation
in ways that are painful to diagnose.

Using one of the cross-cluster GitOps patterns helps:

- **Argo CD `ApplicationSet`** with two `Application`s sharing one Helm values
  fragment (the version) and overriding only `installMode` + the
  `externalOvnCentral.endpoint`.
- **Flux `Kustomization`** per cluster, each pointing at the same Helm chart
  revision.

## Limitations

- The CRD bundle is intentionally rendered **only** in the data-plane release,
  but its content matches the chart version used by the management release.
  Apply the data-plane release before the management release does its first
  reconcile loop, otherwise the controller will spam "CRD not found" until
  the tenant CRDs land.
- ovn-northd's port (6643) does **not** need to be exposed across the cluster
  boundary — only the management cluster components talk to it. The chart
  still defines the Service so internal traffic works.
- Single-cluster IC (interconnect) deployments are still rendered under
  `installMode: full`. Multi-cluster IC topologies need their own design and
  are out of scope here.
- `externalOvnCentral.endpoint` should be an **IP address** (IPv4 or IPv6).
  DNS hostnames work with recent OVN releases but the connection string
  format `tcp:[host]:port` is fragile against older OVN parsers. If you
  expose ovn-central behind a hostname, prefer a static VIP that hostname
  resolves to and put the VIP in `endpoint`.
- The three OVN Services (ovn-nb / ovn-sb / ovn-northd) share the single
  `ovn-central.service.loadBalancerIP`. With MetalLB the chart emits the
  `metallb.universe.tf/allow-shared-ip: kube-ovn-central` annotation so
  the three Services land on one VIP distinguished by port. With cloud
  LoadBalancer providers that reject duplicate `loadBalancerIP`, you have
  two options: (a) use NodePort instead and front the node IPs with an
  external LB you control, or (b) extend the chart to render
  per-Service VIPs and have `externalOvnCentral` accept separate `nbEndpoint`
  / `sbEndpoint` fields. The chart does not currently support (b) — track it
  as follow-up work.
- In `installMode=dataPlaneOnly` with `networking.ENABLE_SSL=true`, you
  **must** pre-seed the `kube-ovn-tls` Secret in the tenant cluster
  before installing the chart. Self-signing locally would produce certs
  the management cluster does not trust. The chart fails with a clear
  message in this case rather than silently generating an incompatible
  CA.
- `kube-ovn-monitor`, DPDK (`HYBRID_DPDK=true`), and the OVS upgrade hooks
  (`pre-upgrade-ovs-ovn` / `upgrade-ovs-ovn`) are currently disabled outside
  `installMode: full` because they reference a local ovn-central. Running
  Kamaji with DPDK or doing in-place OVS upgrades on the tenant cluster
  requires a follow-up to make `start-ovs-dpdk-v2.sh` and `upgrade-ovs.sh`
  honor `OVN_DB_IPS` / `externalOvnCentral`.
