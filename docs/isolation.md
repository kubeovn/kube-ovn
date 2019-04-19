# Subnet Isolation

Kube-OVN supports network isolation and access control at the Subnet level.

Use following annotations to specify the isolation policy:
- `ovn.kubernetes.io/private`: boolean, controls whether to deny traffic from IP addresses outside of this Subnet. Default: false.
- `ovn.kubernetes.io/allow`: strings of CIDR separated by commas, controls which addresses can access this Subnet, if `private=true`.

Example:

```bash
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    ovn.kubernetes.io/cidr: 10.17.0.0/16
    ovn.kubernetes.io/gateway: 10.17.0.1
    ovn.kubernetes.io/logical_switch: ovn-subnet
    ovn.kubernetes.io/exclude_ips: 10.17.0.0..10.17.0.10
    ovn.kubernetes.io/private: "true"
    ovn.kubernetes.io/allow: 10.17.0.0/16,10.18.0.0/16
  name: ovn-subnet
``` 