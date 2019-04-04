# Subnet Isolation

Kube-OVN supports subnet isolation and access control by modifying annotation of namespace.

Use following keys to modify isolation policy:
- `ovn.kubernetes.io/private`: boolean, control whether ip outside this subnet can access this subnet. Default: false.
- `ovn.kubernetes.io/allow`: strings of cidr separated by comma, control which address can visit this subnet, if private=true.

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