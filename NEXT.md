# What's Next

This document lists the features merged into the master branch for the next minor release.

## Post-v1.14.0

- Performace: skip conntrack for specific dst CIDRs. [#5821](https://github.com/kubeovn/kube-ovn/pull/5821)
- NetworkPolicy supports `lax` mode which only deny traffic type of TCP, UDP and SCTP. That means ARP, ICMP and DHCP traffic are alaways allowed. [#5745](https://github.com/kubeovn/kube-ovn/pull/5745)
- Remove internal-port type interface code. [#5794](https://github.com/kubeovn/kube-ovn/pull/5794)
- IPPool
  - Multiple IPPools now can bind to the same Namespace. [#5731](https://github.com/kubeovn/kube-ovn/pull/5731)
  - Pods in a bound namespace will only get IPs from the bound pool(s), not other ranges in the subnet. [#5731](https://github.com/kubeovn/kube-ovn/pull/5731)
- `AdminNetworkPolicy` now supports specify egress peers using FQDNs. [#5703](https://github.com/kubeovn/kube-ovn/pull/5703)
- Using ARP for IPv4 network ready check: now you don't need ACL allow rules for gateway to make Pod running. [#5716](https://github.com/kubeovn/kube-ovn/pull/5716)
- Non-primary CNI mode: you can run Kube-OVN as the secondary only network, without annoying unused annotations and logical switch port allocations. [#5618](https://github.com/kubeovn/kube-ovn/pull/5618)
- VPC NAT Gateway:
  - No default EIP mode: the secondary interface can initialize without a default EIP to avoid the waste. [#5605](https://github.com/kubeovn/kube-ovn/pull/5605)
  - Custom routes: you can control the route rules within the vpc-nat-gateway Pods to control traffic paths. [#5608](https://github.com/kubeovn/kube-ovn/pull/5608)
  - Gratuitous ARP: VPC NAT Gateway automatically sends gratuitous ARP packets during initialization to accelerate network convergence. [#5607](https://github.com/kubeovn/kube-ovn/pull/5607)
- Healthchecks for static endpoints in `SwitchLBRules`: SLR with both selector or endpoints key can support healthchecks. [#5435](https://github.com/kubeovn/kube-ovn/pull/5435)
- Underlay
  - Node Selectors for `ProviderNetwork`: instead of adding/removing nodes to the `ProviderNetwork` one by one, you can use node selectors to simplify the workflow. [#5518](https://github.com/kubeovn/kube-ovn/pull/5518)
  - Different `NetworkProvider`s can now share the same VLAN. [#5471](https://github.com/kubeovn/kube-ovn/pull/5471)
- Adding `pod_name` and `pod_namespace` labels to interface metrics. [#5463](https://github.com/kubeovn/kube-ovn/pull/5463)
- IPSec
  - Support `cert-manager` to issue certificates. [#5365](https://github.com/kubeovn/kube-ovn/pull/5365)
  - Request new certificate if current certificate is not trusted. [#5710](https://github.com/kubeovn/kube-ovn/pull/5710)
- kubectl-ko
  - Collect IPSec and xFRM information. [#5472](https://github.com/kubeovn/kube-ovn/pull/5472)
  - Replace `Endpoint` with `EndpointSlice`. [#5425](https://github.com/kubeovn/kube-ovn/pull/5425)
- NetworkAttachment caching: reduce APIServer load in large-scale deployments with Multus. [#5386](https://github.com/kubeovn/kube-ovn/pull/5386)
- Upgrade `OVS` to 3.5 and `OVN` to 25.03. [#5537](https://github.com/kubeovn/kube-ovn/pull/5537)
