# Routed subnet mode

Set `spec.routed: true` on a Subnet so pods get host routes (`/32` IPv4 or `/128` IPv6) and send all traffic through the subnet gateway instead of sharing an L2 broadcast domain.

## Behavior

With CIDR `10.0.0.0/16` and gateway `10.0.0.1`, each pod is configured like:

```text
ip addr add 10.0.0.5/32 dev eth0
ip route add 10.0.0.1/32 dev eth0
ip route add default via 10.0.0.1
```

Same-subnet east-west traffic is hairpinned through the OVN logical router. OVN ACLs use an allow-list / default-deny policy:

- Allow ARP/ND only for the gateway
- Allow IP frames whose Ethernet source or destination is the logical router port MAC (both ACL directions), so pod→router and router→pod hairpin traffic is permitted while direct L2 pod→pod is denied
- Drop all other IP/ARP/ND (both directions)

When `private: true`, ingress via the router is further limited to hairpin (own CIDR), node join CIDR, and `allowSubnets`.

## Pod route annotations

Provider route annotations (e.g. `ovn.kubernetes.io/routes`) are supported on non-DPDK interfaces when every annotated route uses the **subnet gateway** as next hop (or the U2O interconnection IP when that applies). CNI ADD fails if a route cannot be parsed or installed.

Other next hops are rejected: with `/32`/`/128` addressing and gateway-only ACLs, the pod can only ARP the subnet gateway.

## Requirements

- OVN provider subnet
- Overlay, or underlay with `logicalGateway` / `u2oInterconnection` (a logical router port is required)
- Not compatible with `enableDHCP`, `enableIPv6RA`, `enableMulticastSnoop`, mac-only / BYO-DHCP, custom `spec.acls`, or DPDK/vhost-user NICs
- DPDK/vhost-user also rejects pods that carry route annotations (they cannot be applied on that path)

## Notes

- IPAM still allocates from `spec.cidrBlock`; only the pod interface mask and routes change.
- Enabling or disabling `routed` does not reconfigure existing pods; recreate pods to apply the new addressing.
- Distinct from the pod annotation `ovn.kubernetes.io/routed`, which means OVN routes are ready for the pod.
