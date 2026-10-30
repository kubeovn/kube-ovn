# What's Next

This document lists the features merged into the master branch for the next minor release.

## Post-v1.16.0

- Upgrade `OVN` to 26.03 and `OVS` to 3.7. [#7357](https://github.com/kubeovn/kube-ovn/pull/7357)
- Add an option to cap the MTU for default and custom subnets. [#7442](https://github.com/kubeovn/kube-ovn/pull/7442)
- Load BGP speaker parameters from a YAML configuration file. [#6933](https://github.com/kubeovn/kube-ovn/pull/6933)
- Add Kubernetes warning and lifecycle Events for IP, VPC, VPC NAT Gateway, and VPC DNS resources. [#7200](https://github.com/kubeovn/kube-ovn/pull/7200) [#7202](https://github.com/kubeovn/kube-ovn/pull/7202)
- VPC Egress Gateway now supports dynamic NAT gateway LAN IPs, image pull secrets, an optional `priorityClassName`, and an internal tenant network by default. [#7237](https://github.com/kubeovn/kube-ovn/pull/7237) [#7326](https://github.com/kubeovn/kube-ovn/pull/7326) [#7635](https://github.com/kubeovn/kube-ovn/pull/7635) [#7425](https://github.com/kubeovn/kube-ovn/pull/7425)
- VPC NAT Gateway can back `LoadBalancer` Services through shared nftables DNAT, and nftables load balancing is enabled by default. [#7047](https://github.com/kubeovn/kube-ovn/pull/7047) [#7403](https://github.com/kubeovn/kube-ovn/pull/7403)
- Add service-scoped OVN load balancers for `ClusterIP` and `LoadBalancer` Services, including ClientIP session affinity, `internalTrafficPolicy`, and traffic distribution. `PreferSameNode` honors EndpointSlice node hints. [#7378](https://github.com/kubeovn/kube-ovn/pull/7378) [#7457](https://github.com/kubeovn/kube-ovn/pull/7457)
- Upgrade `ClusterNetworkPolicy` to `network-policy-api` v0.2.0. [#7585](https://github.com/kubeovn/kube-ovn/pull/7585)
- Resolve NetworkPolicy named ports per destination Pod and fail closed when a named port is ambiguous. [#7795](https://github.com/kubeovn/kube-ovn/pull/7795) [#7796](https://github.com/kubeovn/kube-ovn/pull/7796)
- Clarify IP and VIP ownership: `IP` handles Pod address allocation, while `Vip` handles allowed address pairs and host VM VIPs through `kube_host_vm_vip`. [#7721](https://github.com/kubeovn/kube-ovn/pull/7721)
- Rewrite `kubectl-ko` as a cross-platform Go CLI with an independent node agent. It now supports workload-style `exec`, VM/VMI packet capture across live migration, and native packet diagnostics. [#7606](https://github.com/kubeovn/kube-ovn/pull/7606) [#7621](https://github.com/kubeovn/kube-ovn/pull/7636) [#7639](https://github.com/kubeovn/kube-ovn/pull/7639)
- Build ACL sampling as a standalone binary. [#7602](https://github.com/kubeovn/kube-ovn/pull/7602)
- The CNI installer can skip bundled standard CNI plugins, and periodic CNI recomputation is optional. TProxy namespace dialing can run in an optional CNI container. [#7694](https://github.com/kubeovn/kube-ovn/pull/7694) [#7714](https://github.com/kubeovn/kube-ovn/pull/7714) [#7594](https://github.com/kubeovn/kube-ovn/pull/7594)
- Allow Helm installations to use externally managed webhook certificates. [#7727](https://github.com/kubeovn/kube-ovn/pull/7727)
- Initialize single-instance OVN central deployments with standalone NB and SB databases. [#7725](https://github.com/kubeovn/kube-ovn/pull/7725)

- VPC NAT Gateway
  - A `vpc-nat-gw` can now serve Kubernetes Services through nftables share DNAT: a `LoadBalancer`
    Service is published on its EIP, and a Service ClusterIP can be served from the gateway. See
    [docs/vpc-nat-gateway-service.md](docs/vpc-nat-gateway-service.md). [#7451](https://github.com/kubeovn/kube-ovn/pull/7451)
  - **Breaking**: the flag `--enable-nftable-lb-svc` (default `true`) is replaced by
    `--enable-gw-nftable-lb-svc` and `--enable-gw-nftable-svc-cluster-ip`, both default `false`.
    There is no alias: a controller started with the old flag fails on an unknown flag. Update the
    Deployment arguments, the `install.sh` variables (`ENABLE_GW_NFTABLE_LB_SVC`,
    `ENABLE_GW_NFTABLE_SVC_CLUSTER_IP`) or the Helm values before upgrading.
  - **Breaking**: `IptablesDnatRule` objects of `type: share` are now controller-owned accounting
    records of such a Service. Creating one by hand is rejected by the webhook, and the controller
    no longer programs or removes NAT state from them. [#7451](https://github.com/kubeovn/kube-ovn/pull/7451)

## v1.16.0

- NetworkPolicy now supports provider-scoped policies for multi-network pods using the `ovn.kubernetes.io/policy-for` annotation. [#6223](https://github.com/kubeovn/kube-ovn/pull/6223)
- Support static IP/MAC for multiple interfaces on the same logical switch. [#6060](https://github.com/kubeovn/kube-ovn/pull/6060)
- MetalLB underlay integration now supports IPv6 and dual-stack. [#6159](https://github.com/kubeovn/kube-ovn/pull/6159)
- KubeVirt live-migration multi-chassis options now apply to all VM NICs, not just the primary one. [#6241](https://github.com/kubeovn/kube-ovn/pull/6241)
- Add human-readable descriptions to all Kube-OVN CRD fields for better `kubectl explain` output. [#6133](https://github.com/kubeovn/kube-ovn/pull/6133) [#6147](https://github.com/kubeovn/kube-ovn/pull/6147)
- Support BGP and EVPN (L3VPN) for VPC Egress Gateway, running FRR in the egress gateway Pod. [#6224](https://github.com/kubeovn/kube-ovn/pull/6224)
- Support per-pod NIC granular DHCP control via annotations, overriding subnet-level DHCP settings. [#6475](https://github.com/kubeovn/kube-ovn/pull/6475)
- Extend SecurityGroup API with tiers, larger priority range (1-16384), localAddress and port matches. [#6330](https://github.com/kubeovn/kube-ovn/pull/6330)
- Use BigInt for SubnetStatus IP count fields to support large address ranges. [#6403](https://github.com/kubeovn/kube-ovn/pull/6403)
- Improve hairpin logic to support FIP, SNAT from any CIDR within the VPC and LoadBalancers. [#6445](https://github.com/kubeovn/kube-ovn/pull/6445)
- VPC NAT Gateway
  - Support user-defined annotations on NAT gateway Pod template. [#6256](https://github.com/kubeovn/kube-ovn/pull/6256)
  - Allow SNAT EIP to FIP EIP traffic. [#6329](https://github.com/kubeovn/kube-ovn/pull/6329)
  - Allow any EIP to share external subnet gateway in one native VLAN. [#6426](https://github.com/kubeovn/kube-ovn/pull/6426)
- VPC Egress Gateway
  - Support custom resources and bandwidth limits. [#6407](https://github.com/kubeovn/kube-ovn/pull/6407)
- Interconnection
  - Add vendor ID to transit switches to avoid conflicts with other OVN controllers. [#6186](https://github.com/kubeovn/kube-ovn/pull/6186)
- Reliability
  - OpenFlow synchronization: detect and recover from stale or missing OVS flows automatically. [#6117](https://github.com/kubeovn/kube-ovn/pull/6117)
  - OVN DB: back up Raft header before rejoining cluster to improve recovery. [#6106](https://github.com/kubeovn/kube-ovn/pull/6106)
- Performance
  - Strip `managedFields` from informer cache to reduce memory usage. [#6119](https://github.com/kubeovn/kube-ovn/pull/6119)
  - Add field selectors to informer factory to reduce API server load. [#6091](https://github.com/kubeovn/kube-ovn/pull/6091)
- Security
  - Replace wildcard RBAC verbs with explicit verb lists. [#6233](https://github.com/kubeovn/kube-ovn/pull/6233)
  - Specify ephemeral storage limits for containers. [#6259](https://github.com/kubeovn/kube-ovn/pull/6259)
- Helm Chart
  - Make DaemonSet update strategy configurable via `values.yaml`. [#6136](https://github.com/kubeovn/kube-ovn/pull/6136)
  - Introduce `extraEnv` variable for all components. [#6142](https://github.com/kubeovn/kube-ovn/pull/6142)
  - Add `affinity` and `nodeSelector` support for ovs-ovn and ovs-ovn-dpdk DaemonSets. [#6308](https://github.com/kubeovn/kube-ovn/pull/6308)
  - Add `external-gateway-config-ns` option for controller. [#6211](https://github.com/kubeovn/kube-ovn/pull/6211)
  - Introduce ServiceMonitor and labels to services. [#6340](https://github.com/kubeovn/kube-ovn/pull/6340)

## v1.15.0

- ACL log supports ratelimiting. [#5938](https://github.com/kubeovn/kube-ovn/pull/5938)
- Subnet with centralized gateway now supports nodeSelectors. [#5956](https://github.com/kubeovn/kube-ovn/pull/5956)
- Overlay encapsulation NIC selection. [#5946](https://github.com/kubeovn/kube-ovn/pull/5946)
- Performace: skip conntrack for specific dst CIDRs. [#5821](https://github.com/kubeovn/kube-ovn/pull/5821)
- NetworkPolicy supports `lax` mode which only deny traffic type of TCP, UDP and SCTP. That means ARP, ICMP and DHCP traffic are alaways allowed. [#5745](https://github.com/kubeovn/kube-ovn/pull/5745)
- Remove internal-port type interface code. [#5794](https://github.com/kubeovn/kube-ovn/pull/5794)
- IPPool
  - Multiple IPPools now can bind to the same Namespace. [#5731](https://github.com/kubeovn/kube-ovn/pull/5731)
  - Pods in a bound namespace will only get IPs from the bound pool(s), not other ranges in the subnet. [#5731](https://github.com/kubeovn/kube-ovn/pull/5731)
  - IPPool will create an AddressSet that can be work with VPC Policy Route and ACL. [#5920](https://github.com/kubeovn/kube-ovn/pull/5920)
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
  - Auto create VLAN sub-interfaces. [#5966](https://github.com/kubeovn/kube-ovn/pull/5966)
  - Auto move VLAN sub-interfaces to OVS bridges. [#5949](https://github.com/kubeovn/kube-ovn/pull/5949)
- Adding `pod_name` and `pod_namespace` labels to interface metrics. [#5463](https://github.com/kubeovn/kube-ovn/pull/5463)
- IPSec
  - Support `cert-manager` to issue certificates. [#5365](https://github.com/kubeovn/kube-ovn/pull/5365)
  - Request new certificate if current certificate is not trusted. [#5710](https://github.com/kubeovn/kube-ovn/pull/5710)
- kubectl-ko
  - Collect IPSec and xFRM information. [#5472](https://github.com/kubeovn/kube-ovn/pull/5472)
  - Replace `Endpoint` with `EndpointSlice`. [#5425](https://github.com/kubeovn/kube-ovn/pull/5425)
- NetworkAttachment caching: reduce APIServer load in large-scale deployments with Multus. [#5386](https://github.com/kubeovn/kube-ovn/pull/5386)
- Upgrade `OVS` to 3.5 and `OVN` to 25.03. [#5537](https://github.com/kubeovn/kube-ovn/pull/5537)
