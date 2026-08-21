# Routed subnet mode

Set `spec.routed: true` on a Subnet so pods get host routes (`/32` IPv4 or `/128` IPv6) and send all traffic through the subnet gateway instead of sharing an L2 broadcast domain.

## Behavior

With CIDR `10.0.0.0/16` and gateway `10.0.0.1`, each pod is configured like:

```text
ip addr add 10.0.0.5/32 dev eth0
ip route add 10.0.0.1/32 dev eth0
ip route add default via 10.0.0.1
```

Same-subnet east-west traffic is hairpinned through the OVN logical router. OVN ACLs allow ARP/ND only for the gateway and IP frames only to/from the logical router port MAC.

## Requirements

- OVN provider subnet
- Overlay, or underlay with `logicalGateway` / `u2oInterconnection` (a logical router port is required)
- Not compatible with `enableDHCP`, `enableIPv6RA`, `enableMulticastSnoop`, or mac-only / BYO-DHCP subnets

## Notes

- IPAM still allocates from `spec.cidrBlock`; only the pod interface mask and routes change.
- Enabling or disabling `routed` does not reconfigure existing pods; recreate pods to apply the new addressing.
- Distinct from the pod annotation `ovn.kubernetes.io/routed`, which means OVN routes are ready for the pod.
