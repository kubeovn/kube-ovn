# Subnets

From v0.6.0 Kube-OVN will use Subnet crd to manage subnets. If you still use a version prior to v0.6.0 please update to this version to use new subnet.

## Example

```yaml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: subnet-gateway
spec:
  protocol: IPv4
  default: false
  namespaces:
  - ns1
  - ns2
  cidrBlock: 10.10.0.0/16
  gateway: 10.10.0.1
  excludeIps:
  - 10.10.0.1
  private: true
  allowSubnets:
  - 10.16.0.0/16
  - 10.18.0.0/16
  gatewayType: centralized
  gatewayNode: node1
  natOutgoing: true
```

## Basic Configuration

- `protocol`: The ip protocol ,can be IPv4 or IPv6 or Dual.
- `default`: If set true, all namespaces that not bind to any subnets will use this subnet to allocate pod ip and share other network configuration. Note: Kube-OVN will create a default subnet and set this field to true. There can only be one default subnet in a cluster.
- `namespaces`: List of namespaces that bind to this subnet. If you want to bind a namespace to this subnet, edit and add the namespace name to this field.
- `cidrBlock`: The cidr of this subnet.
- `gateway`: The gateway address of this subnet.
- `excludeIps`: List of ips that you do not want to be allocated. The format `192.168.10.20..192.168.10.30` can be used to exclude a range of ips.

## Isolation

Besides standard NetworkPolicy，Kube-OVN also supports network isolation and access control at the Subnet level to simplify the use of access control.

*Note*: NetworkPolicy take a higher priority than subnet isolation rules.

- `private`: Boolean, controls whether to deny traffic from IP addresses outside of this Subnet. Default: false.
- `allowSubnets`: List of CIDRs, controls which addresses can access this Subnet, if `private=true`.

After Kube-OVN v1.10.0, we provide support for fine-grained traffic control in subnet. The detailed implementation can be referenced in [Subnet-ACL](https://github.com/kubeovn/kube-ovn/blob/master/docs/subnet-acl.md).

## Gateway

Gateway is used to enable external network connectivity for Pods within the OVN Virtual Network.

Kube-OVN supports two kinds of Gateways: the distributed Gateway and the centralized Gateway. Also user can expose pod ip directly to external network.

For a distributed Gateway, outgoing traffic from Pods within the OVN network to external destinations will go through the Node where the Pod is hosted.

For a centralized gateway, outgoing traffic from Pods within the OVN network to external destinations will go through Gateway Node for the Namespace.

- `gatewayType`: `distributed` or `centralized`, default is `distributed`.
- `gatewayNode`: when `gatewayType` is `centralized` used this field to specify which node act as the namespace gateway. This field can be a comma separated string, like `node1,node2`.
Before kube-ovn v1.6.3, kube-ovn will automatically apply an active-backup failover strategy.
Since kube-ovn v1.7.0, kube-ovn support ecmp routes, and outgoing traffic can go through multiple gateway specified.
Since kube-ovn v1.8.0, kube-ovn support using designative egress ip on node, the format of gatewayNode can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3'.
- `natOutgoing`: `true` or `false`, whether pod ip need to be masqueraded when go through gateway. When `false`, pod ip will be exposed to external network directly, default `false`.

## Advance Options

- `vlan`: if enable vlan network, use this field to specific which vlan the subnet should bind to.
- `logicalGateway`: Create a logical gateway for the subnet instead of using underlay gateway. Take effect only when the subnet is in underlay mode. Default: `false`.
- `externalEgressGateway`: External egress gateway address. When set, egress traffic is redirected to the external gateway through gateway node(s) by policy-based routing. Conflict with `natOutgoing`.
- `policyRoutingPriority`/`policyRoutingTableID`: Priority & table ID used in policy-based routing. Required when `externalEgressGateway` is set. NOTICE: `policyRoutingTableID` MUST be unique.
- `disableGatewayCheck`: By default Kube-OVN checks Pod's network by sending ICMP request to the subnet's gateway. Set it to `true` if the subnet is in underlay mode and the physical gateway does not respond to ICMP requests.
- `disableInterConnection`: if enable cluster-interconnection, use this field to disable auto route.

## DHCP Options

> This function mainly works with KubeVirt SR-IOV or OVS-DPDK type network, where the embedded dhcp in KubeVirt can not work.

OVN implements native DHCPv4 and DHCPv6 support which provides stateless replies to DHCPv4 and DHCPv6 requests. 

Now kube-ovn support [DHCP feature](https://github.com/kubeovn/kube-ovn/pull/1320) too, you can enable it in the spec of subnet. It will create DHCPv4 options or DHCPv6 options, and patch the UUIDs into the status of subnet.

When a pod created, the logical switch port will associate with the DHCP options to use DHCP feature. 

If you want to use DHCPv6, you may need ipv6 router advertisement too. It will send the prefix, default gateway and other infos to the DHCPv6 client.

- `enableDHCP`: Boolean, set true to enable DHCP feature for the subnet. If it's a `Dual` subnet, both DHCPv4 and DHCPv6 will be enabled. Default: false.
- `dhcpV4Options`: String, the DHCP options setting of IPv4, it works only when `enableDHCP` is true. If not set, the default configuration is: `"lease_time=3600, router=$ipv4_gateway, server_id=169.254.0.254, server_mac=$random_mac1"`.
- `dhcpV6Options`: String, the DHCP options setting of IPv6, it works only when `enableDHCP` is true. If not set, the default configuration is: `"server_id=$random_mac1"`.
- `enableIPv6RA`: Boolean, set true to enable IPv6 router advertisement. Default: false.
- `ipv6RAConfigs`: String, the ipv6_ra_configs of the logical_router_port, it works only when `enableIPv6RA` is true. If not set, the default configuration is: `"address_mode=dhcpv6_stateful, max_interval=30, min_interval=5, send_periodic=true"`.

For more information about configuration of DHCP options, please see [docs](https://www.ovn.org/support/dist-docs/ovn-nb.5.html) and [example](https://blog.oddbit.com/post/2019-12-19-ovn-and-dhcp/).

> Tips: DHCP options is very useful for the pod which implement VirtualMachines to get an ip address by DHCP, such as [KubeVirt](https://github.com/kubevirt/kubevirt) scheme will manage VM in the pod.


## Bind Pod to Subnet

By default, Pod will automatically inherit subnet from Namespace, From 1.5.1 users can bind Pod to another Subnet by manually setup the `logical_switch` annotation for a Pod.

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: another-subnet
  namespace: default
  name: another-subnet-pod
```
