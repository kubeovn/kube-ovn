# Subnets

Kube-OVN uses annotations on Namespaces to create and share Subnets. If a Namespace has no related annotations, it will use the default Subnet (10.16.0.0/16)

Use the following annotations to define a Subnet:

- `ovn.kubernetes.io/cidr`: The CIDR of the Subnet.
- `ovn.kubernetes.io/gateway`: The Gateway address for the Subnet.
- `ovn.kubernetes.io/logical_switch`: The Logical Switch name in OVN.
- `ovn.kubernetes.io/exclude_ips`: Addresses that should not be allocated to Pods.


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

This YAML will create a Logical Switch named `ovn-subnet` in OVN, with CIDR 10.17.0.0/16, and Gateway 10.17.0.1. The IP addresses between 10.17.0.0 and 10.17.0.10 will not be allocated to the Pods.

**NOTE**: In the current version, we only support creating a Subnet while creating a new Namespace. Modifying annotations after Namespace creation will not trigger Subnet creation/update in OVN. Dynamic Subnet configuration is planned for a future release.

To share a Subnet across multiple Namespaces, point the annotation `ovn.kubernetes.io/logical_switch` to an existing Logical Switch when creating the Namespace. For example:

```bash
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: ovn-subnet
  name: ovn-share
```

This YAML will create a Namespace ovn-share that uses the same Subnet as the previous Namespace `ovn-subnet`.