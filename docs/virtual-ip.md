From v1.10, users can create vip. vip looks a bit like port in openstack neutron. users can use vip to keep ip address before using it (in pod).

in some scenarios below, vip should be very useful。

- build k8s cluster based on  kubevirt vms, if you use veth-pair, ipvlan, macvlan as your cni
- ovn lb health-check also need vip

**vip can use in any subnet, vpc and underlay subnet both included.**

## Creating vip

```yaml
# 1. dynamic get vip
apiVersion: kubeovn.io/v1
kind: Vip
metadata:
  name: vip-dynamic-01
spec:
  subnet: my-ovn-vpc-subnet # specify your subnet
---
# 2. static ip
apiVersion: kubeovn.io/v1
kind: Vip
metadata:
  name: static-vip01
spec:
  subnet: my-ovn-vpc-subnet # specify your subnet
  v4Ip: "172.20.10.201"  # and specify your ip
```
