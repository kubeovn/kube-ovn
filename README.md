# Kube-OVN

Kube-OVN is an advanced Kubernetes network fabric designed for Enterprise container network management.

## Primary Features
- **Namespaced Subnet Allocation**: Each namespace can has a unique subnet(backend by a vswitch) to allocated pod ip. Multiple namespaces can also share a same subnet.
- **Subnet Isolation**: Control which address can visit a specific subnet.
- **Static IP Address for Workload**: Allocate random or static IP addresses to workloads just as you wish.
- **Dynamic QoS**: Modify pod ingress/egress traffic rate on the fly.
- **Embedded Loadbalancer**: Replace kube-proxy by ovn embedded distributed L2 Loadbalancer.
- **Distributed Gateway**: Every node can act as a gateway to provide external network connectivity.

## Features on The Way
- **Namespaced Gateway**
- **Direct External Connectivity**
- **ACL Based Network Policy**
- **Policy based QoS**
- **More Metrics and Traffic Graph**
- **More Diagnose and Tracing Tools**

## Quick Start
Kube-OVN is easy to use and has a quick out of box installation. Please refer to [Installation](docs/install.md).

## Documents
- [Namespaced Subnet](docs/subnet.md)
- [Subnet Isolation](docs/isolation.md)
- [Static IP](docs/static-ip.md)
- [Dynamic QoS](docs/qos.md)
- [Policy Routing Gateway](docs/policy-gateway.md)
- [Direct External Connectivity](docs/direct-connect.md)

## !!Cautions!!
Kube-OVN is still in early stage and heavy development. Please *DO NOT* use it in production!!

## Contact
Mail: mengxin#alauda.io

WeChat: liumengxinfly