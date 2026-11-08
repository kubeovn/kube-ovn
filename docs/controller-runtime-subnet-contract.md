# Controller-runtime Subnet contract

This document is the boundary for the next incremental migration after the
VLAN and ProviderNetwork pilot. It describes the current legacy ownership and
the read-only contract that a future Subnet reconciler must satisfy. The
contract is deliberately separate from production wiring: the current
`CONTROLLER_RUNTIME_MODE` leaves the Subnet writer in the legacy controller.

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

The IPAM transaction seam is now available. The next production increment is
opt-in [IPPool scheduling](controller-runtime-ippool.md), which reuses the recovered
legacy backend and its count-pool activation protocol. Further business-handler
extraction and conflict-safe client writes remain separate steps. IP is last
because Pod, VM, LSP and `syncIPCR` paths depend on it. Subnet writers remain
legacy-owned in every currently supported mode.
