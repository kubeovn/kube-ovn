# NEWS

## Post-v1.14.0

- Using ARP for IPv4 network ready check: now you don't need ACL allow rules for gateway to make Pod running. [#5716](https://github.com/kubeovn/kube-ovn/pull/5716)
- Non-primary CNI mode: you can run Kube-OVN as the secondary only network, without annoying unused annotations and logical switch port allocations. [#5618](https://github.com/kubeovn/kube-ovn/pull/5618)
- VPC NAT Gateway:
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
