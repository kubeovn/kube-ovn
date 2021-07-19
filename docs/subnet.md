# Subnets

From v0.6.0 Kube-OVN will use Subnet crd to manage subnets. If you still use a version prior to v0.6.0 please update to this version to use new subnet.

## Example

```bash
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

- `protocol`: The ip protocol ,can be IPv4 or IPv6. *Note*: Through kube-ovn support both protocol subnets coexist in a cluster, kubernetes control plan now only support one protocol. So you will lost some ability like probe and  service discovery if you use a protocol other than the kubernetes control plan.
- `default`: If set true, all namespaces that not bind to any subnets will use this subnet to allocate pod ip and share other network configuration. Note: Kube-OVN will create a default subnet and set this field to true. There can only be one default subnet in a cluster.
- `namespaces`: List of namespaces that bind to this subnet. If you want to bind a namespace to this subnet, edit and add the namespace name to this field.
- `cidrBlock`: The cidr of this subnet.
- `gateway`: The gateway address of this subnet.
- `excludeIps`: List of ips that you do not want to be allocated. The format `192.168.10.20..192.168.10.30` can be used to exclude a range of ips.

## Isolation

Besides standard NetworkPolicy，Kube-OVN also supports network isolation and access control at the Subnet level to simplify the use of access control.

*Note*: NetworkPolicy take a higher priority than subnet isolation rules.

- `private`: Boolean, controls whether to deny traffic from IP addresses outside of this Subnet. Default: false.
- `allow`: Strings of CIDRs separated by commas, controls which addresses can access this Subnet, if `private=true`.

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
- `underlayGateway`: if enable vlan network, use this field to use underlay network gateway directly, instead of ovs virtual gateway
- `externalEgressGateway`: External egress gateway address. When set, egress traffic is redirected to the external gateway through gateway node(s) by policy-based routing. Conflict with `natOutgoing`.
- `policyRoutingPriority`/`policyRoutingTableID`: Priority & table ID used in policy-based routing. Required when `externalEgressGateway` is set. NOTICE: `policyRoutingTableID` MUST be unique.
- `disableInterConnection`: if enable cluster-interconnection, use this field to disable auto route.

## Bind Pod to Subnet

By default, Pod will automatically inherit subnet from Namespace, From 1.5.1 users can bind Pod to another Subnet by manually setup the `logical_switch` annotation for a Pod.
```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: another-subnet
  namespace: default
  name: another-subnet-pod
```
