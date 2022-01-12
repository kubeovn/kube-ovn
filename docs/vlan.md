# Vlan/Underlay Support

## Introduction

By default, Kube-OVN use Geneve to encapsulate packets between hosts, which will build an overlay network above your infrastructure.
Kube-OVN also supports underlay Vlan mode networking for better performance and throughput.
In Vlan mode, packets from pods will be sent directly to physical switches with vlan tags.

![topology](vlan-topology.png "vlan network topology")

To enable Vlan mode, a ~~dedicated~~ network interface is required by container network. Mac address, MTU, IP addresses and routes attached to the interface will be copied/transferred to an OVS bridge named `br-PROVIDER` where `PROVIDER` is name of the provider network.
The related switch port must work in trunk mode to accept 802.1q packets. For underlay network with no vlan tag, you need
to set the VLAN ID to 0.

~~By now, Geneve or Vlan network mode is a global install option, all container must work in the same network mode.
We are working at combine two networks in one cluster.~~

~~We introduce a new hybrid mode that allows Geneve and Vlan to exist at the same time.
You can have a subnet A using Geneve encapsulation and subnet B using Vlan tag.~~

From v1.7.1 on, Kube-OVN supports dynamic underlay/VLAN networking management.

## Environment Requirements

In the Vlan/Underlay mode, OVS sends origin Pods packets directly to the physical network and uses physical switch/router to transmit the traffic, so it relies on the capabilities of network infrastructure.

1. For K8s running on VMs provided by OpenStack, `PortSecurity` of the network ports MUST be `disabled`;
2. For K8s running on VMs provided by VMware, the switch security options `MAC Address Changes`, `Forged Transmits` and `Promiscuous Mode Operation` MUST be `allowed`;
3. The Vlan/Underlay mode can not run on public IaaS providers like AWS/GCE/Alibaba Cloud as their network can not provide the capability to transmit this type packets;
4. In versions prior to v1.9.0, Kube-OVN checks the connectivity to the subnet gateway through ICMP, so the gateway MUST respond the ICMP messages if you are using those versions, or you can turn off the check by setting `disableGatewayCheck` to `true` which is introduced in v1.8.0;
5. For in-cluster service traffic, Pods set the dst mac to gateway mac and then Kube-OVN applies DNAT to transfer the dst ip, the packets will first be sent to the gateway, so the gateway MUST be capable of transmitting the packets back to the subnet.

## Comparison with Macvlan

The Kube-OVN underlay mode works much like macvlan with some differences in functions and performance.

1. Macvlan has better throughput and latency performance as it has much shorter kernel path. Kube-OVN still needs to move packets between bridges and do the OVS actions;
2. Kube-OVN provides arp-proxy functions which records all ip-mac pair within the subnet to reduce the impact of arp broadcast;
3. As the Macvlan works at very low end of kernel networks, netfilter can not take effect so the Service and NetworkPolicy functions do not work. Kube-OVN uses OVS to provide Service and NetworkPolicy functions.

## Create Underlay Network

For Kube-OVN with version below v1.7.1, Kube-OVN MUST be deployed with vlan network type; From v1.7.1 on, you can create underlay networks dynamically by creating provider network and vlan using CRD.

### Deploy With Vlan Mode

With default Vlan mode, Kube-OVN creates a default subnet named `ovn-default` which is working in underlay/vlan mode.

1. Get the installation script

`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.9/dist/images/install.sh`

2. Edit the `install.sh`, set `NETWORK_TYPE` to `vlan` and `VLAN_INTERFACE_NAME` to related host interface.

3. Install Kube-OVN

`bash install.sh`

4. Create Vlan CR

For versions below v1.7.1:

```yml
apiVersion: kubeovn.io/v1
kind: Vlan
metadata:
  name: product
spec:
  vlanId: 10
```

For versions above v1.7.1:

```yml
apiVersion: kubeovn.io/v1
kind: Vlan
metadata:
  name: product
spec:
  id: 10
  provider: provider
```

5. Create Subnet

> Multiple Subnets can bind to one Vlan.

```yml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: product
spec:
  cidrBlock: 10.100.0.0/16
  gateway: 10.100.0.1
  vlan: product
```

### Dynamical Management

NOTICE: This feature requires version v1.7.1 and above.

1. Creating Provider Network

```yml
apiVersion: kubeovn.io/v1
kind: ProviderNetwork
metadata:
  name: net1
spec:
  defaultInterface: eth1
  customInterfaces:
  - interface: eth2
    nodes:
      - node1
  excludeNodes:
    - node2
```

Here is explanation about the fields of CRD `ProviderNetwork`:

| CRD Field              | Required | Usage                                                                |
| ---------------------- | -------- | -------------------------------------------------------------------- |
| .spec.defaultInterface | Yes      | Specify the default interface to be used                             |
| .spec.customInterfaces | No       | Specify the custom interfaces to be used                             |
| .spec.excludeNodes     | No       | Specify the nodes on which the provider network will not be deployed |

1. Create Vlan

```yml
apiVersion: kubeovn.io/v1
kind: Vlan
metadata:
  name: vlan1
spec:
  id: 0
  provider: net1
```

> You can specify a non-zero ID to use Vlan.

1. Create Subnet

```yml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: subnet1
spec:
  cidrBlock: 10.100.0.0/16
  gateway: 10.100.0.1
  vlan: vlan1
```

### Install Hybrid mode

NOTICE: From v1.7.1 on, `hybrid` mode will be no longer supported since Kube-OVN has builtin support.

1. Get the installation script

`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.9/dist/images/install.sh`

2. Edit the `install.sh`, modify `NETWORK_TYPE` to `hybrid`, `VLAN_INTERFACE_NAME` to related host interface.
> NOTE: if your nodes have different nic name for vlan device you could use regex for VLAN_INTERFACE_NAME or label those nodes with
   own `ovn.kubernetes.io/host_interface_name`.

3. Install Kube-OVN

### Note

Vlan mode will auto-assign a VLAN to a subnet if the subnet doesn't specify a VLAN. 
The hybrid mode will not do the auto-assign, if your subnet doesn't specify a VLAN then the subnet will treat as Geneve mode.
