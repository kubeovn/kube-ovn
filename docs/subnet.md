# Subnet

Kube-OVN use annotations on namespace to create and share subnets. If a namespace has no related annotations, it will used the default subnet(10.16.0.0/16)

Use following keys to define a subnet:

- `ovn.kubernetes.io/cidr`: The cidr of the subnet.
- `ovn.kubernetes.io/gateway`: The gateway address for the subnet.
- `ovn.kubernetes.io/logical_switch`: The logical switch name in OVN.
- `ovn.kubernetes.io/exclude_ips`: Addresses that should not be allocated to Pod.


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
  name: ovn-subnet
```

This yaml will create a logical switch called ovn-subnet in OVN, cidr is 10.17.0.0/16,gateway is 10.17.0.1 and ip between 10.17.0.0 and 10.17.0.10 will not be allocated to pod.

**NOTE**: Now we only support creating subnet by creating a new namespace. Modify annotation after namespace creation will not trigger subnet creation in OVN. Subnet dynamical configuration will be a future feature.

To share a subnet among namespaces, create namespace with `ovn.kubernetes.io/logical_switch` point to an exist logical switch. Such as:

```bash
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: ovn-subnet
  name: ovn-share
```

This yaml will create a namespace ovn-share that use same subnet with previous ovn-subnet.