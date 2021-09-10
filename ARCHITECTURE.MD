# Architecture

This document describes the high-level architecture of Kube-OVN.
If you want to understand the components CRDs and workflows, you are in the right place.

On the high-level, Kube-OVN acts as a bridge between Kubernetes and OVN.
That means Kube-OVN will translate Kubernetes network concepts like CNI, Service, NetworkPolicy into OVN network.
Also, Kube-OVN brings OVN advance SDN functions like L2/L3 network, VPC, Gateway, QoS back to the Kubernetes network.
At the same time, Kube-OVN embeds monitor and diagnose tools to enhance operational productivity.

## Components

The components in Kube-OVN can be divided into three categories:
* The upstream OVN components
* The core controller components
* Monitor, diagnose, and extension tools

### OVN components

These components are from OVN/OVS community which is a system that supports logical network abstraction in virtual machines and container environments.
We highly recommend you to read [ovn-architecture(7)](https://www.mankier.com/7/ovn-architecture) first to get an overview of what OVN is and its architecture.
In Kube-OVN we treat OVN as the SDN implementation and try to use its API to integrate it into Kubernetes.

All the OVN components in Kube-OVN are packed into the docker image and can run as Kubernetes workloads.

#### ovn-central

It's a deployment that runs OVN management components including `ovn-nb`, `ovn-sb`, and `ovn-northd`.
`ovn-nb` stores the logical network and provides API for controllers to manage the logical network.
It's the main components that the Kube-OVN controller will interact with.

`ovn-sb` stores the logical flows that are generated from the logical network in `ovn-nb` and the physical network in each node.

`ovn-northd` do the translation from `ovn-nb` to `ovn-sb`

Multiple `ovn-central` pods will run in a raft-cluster mode that provides high availability.

### ovs-ovn

It's a daemonset that runs openvswitch, ovsdb, and ovn-controller in every node.
They are agents of `ovn-central` that translate the logical network into the real network.

### Core Controllers

These are the core components in Kube-OVN that act as the bridge between Kubernetes and OVN and implement all the advanced network functions.

The code entrypoint of the components below can be found in `/cmd`.

#### kube-ovn-controller
It's a deployment that runs all the logic that translates the Kubernetes network concept into OVN.
You can treat it as the control plane of Kube-OVN.

It watches all network-related events in Kubernetes APIServer like Pod creation/deletion, Service/Endpoint modification, Networkpolicy changes, and so on.
Then the controller translates them into OVN logical network changes.
It also watches and updates CRDs that belong to Kube-OVN like VPC/Subnet/Vlan/IP to implement advanced network functions.

The basic function of kube-ovn-controller is watching pod creation events.
When the event comes the controller uses the embedded in-memory IPAM to allocate an address and call ovn-central to update the logical network,
in the pod creation case, it will create a logical switch port, add static routes and update ACL rules.
Then the controller writes the allocated address and other options like cidr, gateway, routes into Pod's annotations for the `kube-ovn-daemon` to use.
As the controller has a global ipam, it can allocate addresses in a global view.

#### kube-ovn-cni
It's a binary that acts as a thin shim between kubelet and `kube-ovn-daemon`.
It implements the CNI specification and passes the argument from kubelet to `kube-ovn-daemon` to do the real node-level network configuration works.

The binary is contained in the kube-ovn-cni daemonset and will be placed into `/opt/cni/bin` by `kube-ovn-daemon`.

#### kube-ovn-daemon
It's a daemonset run in every node and does all the stuff that really touches the network.

The main works include:
1. Bootstrap ovn-controller and ovs-vswitchd on every node
2. Handle CNI actions like add/del
   1. Create/Delete veth pair and plug them into Pod and OVS
   2. Configure the OVS port
   3. Update iptables/ipset/routes rules on the host network
3. Dynamically update the Pod bandwidth
4. Create `ovn0` for Pod-Host connectivity
5. Configure host network interface for Vlan/Underlay/EIP functions
6. Dynamically update the inter-cluster network gateway

### Other Operation and maintenance tools

These components are extensions of Kube-OVN main functions.
They provide monitoring, diagnosis, productive tools for Kube-OVN maintenance.

#### kube-ovn-speaker
It's a BGP speaker that can announce container networks to external BGP routers or switches so that workloads outside the Kubernetes cluster can visit the container network directly.

For more usage you can read [BGP support](docs/bgp.md).

#### kube-ovn-pinger
It's a daemonset that runs in every node and collects various network metrics like ovs condition, network quality, network latency, and so on.

#### kube-ovn-monitor
It's a sidecar run with `ovn-central` to collect all the OVN metrics.

All the metrics are listed in [Kube-OVN Monitor Metrics](docs/ovn-ovs-monitor.md)

#### kubectl-ko
It's a kubectl plugin that can run diagnose commands with comfort.

For more usage you can read [kubectl-ko](docs/kubectl-plugin.md).

## CRDs
TODO

## Workflows
TODO
