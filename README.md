<img src="https://github.com/cncf/artwork/raw/master/projects/kube-ovn/horizontal/color/kube-ovn-horizontal-color.svg" alt="kube_ovn_logo" width="500"/>

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/kubeovn/kube-ovn/blob/master/LICENSE)
[![Build Tag](https://img.shields.io/github/tag/kubeovn/kube-ovn.svg)](https://github.com/kubeovn/kube-ovn/releases)
[![Docker Tag](https://img.shields.io/docker/pulls/kubeovn/kube-ovn)](https://img.shields.io/docker/pulls/kubeovn/kube-ovn)
![Docker Image Size (latest by date)](https://img.shields.io/docker/image-size/kubeovn/kube-ovn?sort=date)
[![Go Report Card](https://goreportcard.com/badge/github.com/kubeovn/kube-ovn)](https://goreportcard.com/report/github.com/kubeovn/kube-ovn)
[![Slack Card](https://kube-ovn-slackin.herokuapp.com/badge.svg)](https://kube-ovn-slackin.herokuapp.com)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Falauda%2Fkube-ovn.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2Falauda%2Fkube-ovn?ref=badge_shield)

[中文教程](https://github.com/kubeovn/kube-ovn/wiki)

Kube-OVN, a [CNCF Sandbox Level Project](https://www.cncf.io/sandbox-projects/), integrates the OVN-based Network Virtualization with Kubernetes. It offers an advanced Container Network Fabric for Enterprises with the most functions and the easiest operation.

## Community
The Kube-OVN community is waiting for your participation!
- Follow us at [Twitter](https://twitter.com/KubeOvn)
- Chat with us at [Slack](https://kube-ovn-slackin.herokuapp.com/)
- 微信用户扫码加入交流群

  ![Image of wechat](./docs/wechat.png)

## Features
- **Namespaced Subnets**: Each Namespace can have a unique Subnet (backed by a Logical Switch). Pods within the Namespace will have IP addresses allocated from the Subnet. It's also possible for multiple Namespaces to share a Subnet.
- **Vlan/Underlay Support**: In addition to overlay network, Kube-OVN also supports underlay and vlan mode network for better performance and direct connectivity with physical network.
- **VPC Support**: Multi-tenant network with independent address spaces, where each tenant has its own network infrastructure such as eips, nat gateways, security groups and loadbalancers.
- **Static IP Addresses for Workloads**: Allocate random or static IP addresses to workloads.
- **Multi-Cluster Network**: Connect different Kubernetes/Openstack clusters into one L3 network.
- **TroubleShooting Tools**: Handy tools to diagnose, trace, monitor and dump container network traffic to help troubleshoot complicate network issues.
- **Prometheus & Grafana Integration**: Exposing network quality metrics like pod/node/service/dns connectivity/latency in Prometheus format.
- **ARM Support**: Kube-OVN can run on x86_64 and arm64 platforms.
- **Subnet Isolation**: Can configure a Subnet to deny any traffic from source IP addresses not within the same Subnet. Can whitelist specific IP addresses and IP ranges.
- **Network Policy**: Implementing networking.k8s.io/NetworkPolicy API by high performance ovn ACL.
- **DualStack IP Support**: Pod can run in IPv4-Only/IPv6-Only/DualStack mode.
- **Pod NAT and EIP**: Manage the pod external traffic and external ip like tradition VM.
- **IPAM for Multi NIC**: A cluster-wide IPAM for CNI plugins other than Kube-OVN, such as macvlan/vlan/host-device to take advantage of subnet and static ip allocation functions in Kube-OVN.
- **Dynamic QoS**: Configure Pod/Gateway Ingress/Egress traffic rate limits on the fly.
- **Embedded Load Balancers**: Replace kube-proxy with the OVN embedded high performance distributed L2 Load Balancer.
- **Distributed Gateways**: Every Node can act as a Gateway to provide external network connectivity.
- **Namespaced Gateways**: Every Namespace can have a dedicated Gateway for Egress traffic.
- **Direct External Connectivity**：Pod IP can be exposed to external network directly.
- **BGP Support**: Pod/Subnet IP can be exposed to external by BGP router protocol.
- **Traffic Mirror**: Duplicated container network traffic for monitoring, diagnosing and replay.
- **Hardware Offload**: Boost network performance and save CPU resource by offloading OVS flow table to hardware.
- **DPDK Support**: DPDK application now can run in Pod with OVS-DPDK.
- **Cilium Integration**: Cilium can take over the work of Kube-proxy.
- **F5 CES Integration**: F5 can help better manage the outgoing traffic of k8s pod/container.

## Planned Future Work
- Policy-based QoS
- High performance kernel datapath
- Namespaced VPC
- Kubevirt/Kata optimization

## Network Topology

The Switch, Router and Firewall showed in the diagram below are all distributed on all Nodes. There is no single point of failure for in-cluster network.

![topology](docs/ovn-network-topology.png "kube-ovn network topology")

## Monitoring Dashboard

Kube-OVN offers prometheus integration with grafana dashboards to visualize network quality.

![dashboard](docs/pinger-grafana.png)

## Quick Start
Kube-OVN is easy to install with all necessary components/dependencies included. If you already have a Kubernetes cluster without any cni plugin, please refer to the [Installation Guide](docs/install.md).

If you want to install Kubernetes from scratch, you can try [kubespray](https://github.com/kubernetes-sigs/kubespray/blob/master/docs/kube-ovn.md) or for Chinese users try [kubeasz](https://github.com/easzlab/kubeasz/blob/master/docs/setup/network-plugin/kube-ovn.md) to deploy a production ready Kubernetes cluster with Kube-OVN embedded.

## Documents
- [Namespaced Subnets](docs/subnet.md)
- [Subnet Isolation](docs/subnet.md#isolation)
- [Static IP](docs/static-ip.md)
- [Pod NAT and EIP](docs/snat-and-eip.md)
- [Dynamic QoS](docs/qos.md)
- [Subnet Gateway and Direct connect](docs/subnet.md#gateway)
- [Pod Gateway](docs/pod-gw.md)
- [Multi-Cluster Network](docs/cluster-interconnection.md)
- Network interconnection with OpenStack
  - [Separated Kubernetes and OpenStack connected by OVN-IC](docs/OpenStackK8sInterconnection.md)
  - [Kubernetes and OpenStack based on the same OVN deployment](docs/OpenstackOnKubernetes.md)
- [BGP support](docs/bgp.md)
- [Multi NIC Support](docs/multi-nic.md)
- Hardware Offload for [Mellanox](docs/hw-offload-mellanox.md) and [Corigine](docs/hw-offload-corigine.md)
- [Vlan/Underlay Support](docs/vlan.md)
- [DPDK Support](docs/dpdk.md)
- [Traffic Mirror](docs/mirror.md)
- [IPv6](docs/ipv6.md)
- [DualStack](docs/dual-stack.md)
- [VPC](docs/vpc.md)
- [Tracing/Diagnose/Dump Traffic with Kubectl Plugin](docs/kubectl-plugin.md)
- [Prometheus Integration](docs/prometheus.md)
- [Metrics](docs/ovn-ovs-monitor.md)
- [Performance Tuning](docs/performance-tuning.md)
- [Cilium Integration](docs/IntegrateCiliumIntoKubeOVN.md)
- [F5 CES Integration](https://github.com/f5devcentral/container-egress-service/wiki)

## Contribution
We are looking forwards to your PR!

- [Development Guide](docs/development.md)
- [Architecture Guide](ARCHITECTURE.MD)


## FAQ
1. Q: How about the scalability of Kube-OVN?

   A: We have simulated 200 Nodes with 10k Pods by kubemark, and it works fine. Some community users have deployed one cluster with 250+ Nodes and 3k+ Pods in production. It's still not reach the limitation, but we don't have enough resources to find the limitation.

2. Q: What's the Addressing/IPAM? Node-specific or cluster-wide?

   A: Kube-OVN use a cluster-wide IPAM, Pod address can float to any nodes in the cluster.

3. Q: What's the encapsulation?

   A: For overlay mode, Kube-OVN uses Geneve/Vxlan to encapsulate packets between nodes. For Vlan/Underlay mode there is no encapsulation.

## Kube-OVN vs. Other CNI Implementation

Different CNI Implementation has different function scope and network topology. There is no single implementation that can resolve all network problems. In this section, we compare Kube-OVN
to some other options to give users a better understanding to assess which network will fit into your infrastructure.

### Kube-OVN vs. ovn-kubernetes

[ovn-kubernetes](https://github.com/ovn-org/ovn-kubernetes) is developed by the ovn community to integration ovn for Kubernetes. As both projects use OVN/OVS as the data plane, they have some same function sets and architecture. The main differences come from the network topology and gateway implementation.

ovn-kubernetes implements a subnet-per-node network topology.
That means each node will have a fixed cidr range, and the ip allocation is fulfilled by each node when the pod has been invoked by kubelet.

Kube-OVN implements a subnet-per-namespace network topology.
That means a cidr can spread the entire cluster nodes, and the ip allocation is fulfilled by kube-ovn-controller at a central place. And then kube-ovn can apply lots of network configurations at subnet level, like cidr, gw, exclude_ips, nat and so on. This topology also gives Kube-OVN more ability to control how ip should be allocated, on top of this topology, Kube-OVN can allocate static ip for workloads.

We believe the subnet-per-namespace topology will give more flexibility to evolve the network.

On the gateway side, ovn-kubernetes uses native ovn gateway concept to control the traffic. The native ovn gateway relies on a dedicated nic or needs to transfer the nic ip to another device to bind the nic to the ovs bridge. This implementation can reach better performance, however not all environments meet the network requirements especially in the cloud.

Kube-OVN uses policy-route, ipset and iptables to implement the gateway functions that all by software, which can fit more infrastructure and give more flexibility to more function.

### Kube-OVN vs. Calico

[Calico](https://www.projectcalico.org/) is an open-source networking and network security solution for containers, virtual machines, and native host-based workloads. It's known for its good performance and security policy.

The main difference from the design point is the encapsulation method. Calico use no encapsulation or lightweight IPIP encapsulation and Kube-OVN uses geneve to encapsulate packets. No encapsulation can achieve better network performance for both throughput and latency. However, as this method will expose pod network directly to the underlay network with it comes with the burden on deploy and maintain. In some managed network environment where BGP and IPIP is not allowed, encapsulation is a must.

Use encapsulation can lower the requirement on networking, and isolate containers and underlay network from logical. We can use the overlay technology to build a much complex network concept, like router, gateway, and vpc. For performance, ovs can make use of hardware offload and DPDK to enhance throughput and latency.

Kube-OVN can also work in non-encapsulation mode, that take use of underlay switches to switch the packets or use hardware offload to achieve better performance than kernel datapath.

From the function set, Kube-OVN can offer some more abilities like static ip, QoS and traffic mirror. The subnet in Kube-OVN and ippool in Calico share some same function set.

## License
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Falauda%2Fkube-ovn.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Falauda%2Fkube-ovn?ref=badge_large)
