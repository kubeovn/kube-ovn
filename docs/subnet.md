# Subnets

From v0.6.0 Kube-OVN will use Subnet crd to manage subnets. If you still use a version prior to v0.6.0 please update to this version to use new subnet.

## Example

```bash
apiVersion: kubeovn.io/v1
kind: Subnet
  name: subnet-gateway
spec:
  protocol: IPv4
  default: false
  namespaces:
  - ns1
  - ns2
  cidrBlock: 100.64.0.0/16
  gateway: 100.64.0.1
  excludeIps:
  - 100.64.0.1
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
- `excludeIps`: List of ips that you do not want to be allocated.

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
- `gatewayNode`: when `gatewayType` is `centralized` used this field to specify which node act as the namespace gateway.
- `natOutgoing`: `true` or `false`, whether pod ip need to be masqueraded when go through gateway. When `false`, pod ip will be exposed to external network directly, default `false`.
