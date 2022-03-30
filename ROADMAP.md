# Kube-OVN RoadMap

## v1.10.0 -- April 2022

### Subnet Enhancement
- Support to add multiple subnets for one namespace
- Use lr-policy to optimize ovn flows
- Support for DHCP

### VPC Enhancement
- VPC peering

### Virtualization Enhancement
- OVS-DPDK support
- Static IP for VM lifecycle

### Monitoring and Operation
- Command for restore ovn db
- Metrics for db storage status

## Planned features

### DataCenter Network
- Namespaced VPC and Subnet
- Break down vpc-nat-gateway CRD to eip/nat/lb CRDs 
- Integrate DPU to support bare metal
- Integrate SDN switches to support bare metal

### Application Network
- Traffic visualization and application level analyzing
- Windows overlay/underlay network support
- Multi cluster Service/DNS/Networkpolicy
- Traffic encryption
- Fine-grained ACL
- Load balancer type Service

### Performance Enhancement
- Use libovsdb to replace ovn-nbctl daemon
- eBPF to accelerate intra-node communication
- Tools for automatically profile
- Repos for fastpath and optimized ovs modules
- OVN/OVS tailor
- SR-IOV and OVS-DPDK integration

### User Experience Enhancement
- New document website and optimized for new beginners
- Helm/Operator to automate daily operations
- More organized metrics and grafana dashboard
- Troubleshooting tools that can automatically find known issues
- Integrated with other projects like kubeaz, kubekey, sealos etc.
