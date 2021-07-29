# IPv4/IPv6 dual-stack
We provide dual-stack support for Kube-OVN from v1.6.0.

It's easy to apply this feature, all things need to do is just configure subnet CIDR as dual-stack format, ```cidr=<IPv4 CIDR>,<IPv6 CIDR>```. The CIDR is ordered, IPv4 CIDR should be placed before IPv6 CIDR.

## Example

```yaml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: ovn-test
spec:
  cidrBlock: 10.16.0.0/16,fd00:10:16::/64
  default: false
  excludeIps:
  - 10.16.0.1
  - fd00:10:16::1
  gateway: 10.16.0.1,fd00:10:16::1
  gatewayNode: ""
  gatewayType: distributed
  natOutgoing: true
  private: false
  protocol: Dual
```

The fields of subnet can be found at [Subnets](https://github.com/kubeovn/kube-ovn/blob/master/docs/subnet.md).

## Test for dual-stack

When a pod applies for ip address from dual-stack subnet, it will be assigned two addresses, one for IPv4 and another for IPv6.

Here is an example for dual-stack pod.

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/allocated: "true"
    ovn.kubernetes.io/cidr: 10.16.0.0/16,fd00:10:16::/64
    ovn.kubernetes.io/gateway: 10.16.0.1,fd00:10:16::1
    ovn.kubernetes.io/ip_address: 10.16.0.9,fd00:10:16::9
    ovn.kubernetes.io/logical_switch: ovn-default
    ovn.kubernetes.io/mac_address: 00:00:00:14:88:09
    ovn.kubernetes.io/network_types: geneve
    ovn.kubernetes.io/routed: "true"
  creationTimestamp: "2020-12-21T07:40:01Z"
...
podIP: 10.16.0.9
  podIPs:
  - ip: 10.16.0.9
  - ip: fd00:10:16::9
```

## Others
The crd resources of IP and Subnet had been adapted for dual-stack.The result is displayed by protocol.

```shell
mac@localhost ~ % kubectl get ips
NAME                                                         V4IP         V6IP             MAC                 NODE                     SUBNET
coredns-f9fd979d6-9448b.kube-system                          10.16.0.8    fd00:10:16::8    00:00:00:D6:16:9A   kube-ovn-control-plane   ovn-default
coredns-f9fd979d6-smgjt.kube-system                          10.16.0.7    fd00:10:16::7    00:00:00:17:E4:14   kube-ovn-worker          ovn-default
kube-ovn-pinger-25bd9.kube-system                            10.16.0.10   fd00:10:16::a    00:00:00:A6:2C:83   kube-ovn-control-plane   ovn-default
kube-ovn-pinger-vk7d6.kube-system                            10.16.0.9    fd00:10:16::9    00:00:00:14:88:09   kube-ovn-worker          ovn-default
local-path-provisioner-78776bfc44-n9klh.local-path-storage   10.16.0.11   fd00:10:16::b    00:00:00:9F:2C:FB   kube-ovn-worker          ovn-default
node-kube-ovn-control-plane                                  100.64.0.2   fd00:100:64::2   00:00:00:6C:96:3B   kube-ovn-control-plane   join
node-kube-ovn-worker                                         100.64.0.3   fd00:100:64::3   00:00:00:47:B8:A6   kube-ovn-worker          join
mac@localhost ~ % kubectl get subnet
NAME          PROVIDER   VPC           PROTOCOL   CIDR                             PRIVATE   NAT     DEFAULT   GATEWAYTYPE   V4USED   V4AVAILABLE   V6USED   V6AVAILABLE
join          ovn        ovn-cluster   Dual       100.64.0.0/16,fd00:100:64::/64   false     false   false                   2        65531         2        1.8446744073709552e+19
ovn-default   ovn        ovn-cluster   Dual       10.16.0.0/16,fd00:10:16::/64     false     true    true      distributed   5        65528         5        1.8446744073709552e+19
```
