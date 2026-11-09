# MetalLB underlay localport interfaces

`Subnet.spec.externalLBInterface` opts an IPv4 underlay subnet into an
addressless L2 speaker interface. An empty value retains the existing MetalLB
integration and its per-Service provider bridge flows.

```text
MetalLB speaker -> mlb0 (ARP only) -> shared localport -> localnet -> physical LAN
physical LAN -> localnet -> OVN load balancer -> underlay backend
```

The shared logical switch port is `external-lb.<subnet>` with type `localport`,
addresses `unknown`, no port security, and option `localnet-arp-only=true`.
Every eligible node binds its own `br-int` internal interface to that port.
Its locally administered unicast MAC is derived from the node name and Subnet
UID, so advertisements from different nodes do not move the same source MAC
between physical switch ports. In OVN 26.03 localports do not learn logical FDB
entries; the per-node MACs therefore do not compete for one logical FDB entry.
The explicit option also makes the logical switch local to a node that has only
the speaker port, allowing Cluster-policy election without a local backend.

## Requirements

- Rebuild the Kube-OVN base image with the included OVN controller patch, then
  deploy that image on **all** participating nodes before enabling this field.
  An older OVN controller ignores the option and continues blocking localport
  ARP to localnet. A Kubernetes readiness label does not verify OVN patch support.
- Enable OVN load balancing and the existing `ls_dnat_mod_dl_dst` NB option.
  Use the default VPC, `enableExternalLBAddress: true`, and an IPv4-only VLAN
  subnet without `logicalGateway`, `u2oInterconnection`, or routed mode.
- Use a unique Linux interface name (1–15 letters, digits, underscores or
  hyphens) per opted-in subnet. The daemon refuses to take over unrelated Linux
  interfaces, OVS interfaces or OVS ports. Reserved names are rejected.
- Keep `net.ipv4.conf.all.proxy_arp` and `proxy_arp_pvlan` at zero. The daemon
  refuses to bind an interface when host-wide proxy ARP is enabled.
- Do not assign any IPv4 or IPv6 address to this interface or let a host network
  manager configure it. The daemon removes addresses during reconciliation and
  disables IPv6, including automatic link-local addressing. IPv6 MetalLB NDP
  needs link-local addressing and is outside this mode.

## Configuration

Add the following to an existing eligible Subnet:

```yaml
spec:
  enableExternalLBAddress: true
  externalLBInterface: mlb0
```

Wait for `ovn.kubernetes.io/external-lb-mlb0=true` on participating nodes, then
configure the relevant MetalLB advertisement:

```yaml
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: underlay
  namespace: metallb-system
spec:
  ipAddressPools:
    - underlay
  interfaces:
    - mlb0
  nodeSelectors:
    - matchLabels:
        ovn.kubernetes.io/external-lb-mlb0: "true"
```

MetalLB interface selectors do not themselves limit leader election to nodes
with that interface. Keep the readiness selector and ensure other advertisements
for the same pool do not broaden the eligible nodes or interfaces. Existing
provider network exclusions and readiness also determine which nodes get mlb0.

The internal interface has `arp_ignore=8`, proxy ARP disabled and IPv4 forwarding
disabled. It keeps ARP enabled at the link level because MetalLB excludes NOARP
interfaces. These settings suppress kernel replies for addresses on other host
interfaces, including IPVS VIPs, while allowing MetalLB's raw socket ARP replies.
The patched OVN controller admits only ARP in either direction at this port and
keeps its local-only tunnel behavior. Ordinary localports retain the original
localnet restriction.

Service IP packets enter through localnet and run OVN DNAT and the existing
backend destination MAC rewrite. They do not traverse the speaker interface or
the Linux IP stack. Provider bridge Service flows are omitted in this mode;
cached rules from the previous mode are removed. Keep the existing Service annotation
`ovn.kubernetes.io/service_external_ip_from_subnet` and address-pool integration
for the target Subnet as described in the standard MetalLB underlay guide.

## Lifecycle and rollback

The daemon reconciles every three seconds. It creates missing owned interfaces,
repairs their configuration, removes accidentally assigned addresses, and removes
stale interfaces and readiness labels after rename, disable, Subnet deletion or
provider exclusion. New interfaces remain unbound until host isolation succeeds;
failed host configuration unbinds an existing interface. Changes are eventually
consistent with MetalLB election and OVN programming.

To return to the existing integration, update the MetalLB interface and node
selectors for that integration, then clear `externalLBInterface`. The controller
removes the shared localport; the daemon removes owned internal interfaces and
restores legacy per-Service flows through Service reconciliation. Coordinate the
advertisement change with this switch because it can briefly interrupt access.

## Validation

Unit tests cover scope validation, stable distinct MACs, kernel isolation errors,
address removal, provider eligibility, foreign interface ownership, LSP drift
repair and deletion, and removal of cached flows after Subnet deletion or mode
changes. The OVN patch extends the existing localport gARP regression with option
enablement and removal. The base-image CI runs a dummy-datapath packet test
against the packaged OVN/OVS binaries before uploading normal and DPDK images.
It verifies default ARP suppression, bidirectional ARP with the option enabled,
bidirectional IP drops, no localport FDB learning, backend destination MAC
rewrite, suppression after removing the option, and ARP advertisement from a
switch with no local backend or router port. The same test runs in the
scheduled base-image workflows, including normal multi-architecture builds.

The MetalLB E2E suite runs the IPv4 localport case using the rebuilt image from
the PR build. It checks unique addressless interfaces and kernel isolation,
external access without source NAT, a local backend on the advertising node,
absence of provider Service flows, VIP migration between two backend nodes,
accidental address removal, internal port recreation, CNI daemon restart,
provider exclusion and restoration, Cluster-policy access through a node without
a local backend, and cleanup and connectivity after returning
to legacy mode. Leader checks use ServiceL2Status and the speaker MAC rather than
the physical NIC MAC used by the legacy integration. IPv6 and dual-stack jobs
continue to exercise the legacy cases and skip this IPv4-only feature.

A passing real-cluster E2E run with the rebuilt base image is required before
graduating this implementation from draft. Building the suite alone does not
validate MetalLB election or the lifecycle scenarios above.
