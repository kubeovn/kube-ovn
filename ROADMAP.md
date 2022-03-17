# Kube-OVN RoadMap

## v1.10.0 -- April 2022

### Subnet Enhancement
- Support to add multiple subnets for one namespace
- Use lr-policy to optimize ovn flows

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
- Integrate DPU to support bare metal
- Integrate SDN switches to support bare metal

### Application Network
- Traffic visualization and application level analyzing
- Windows network support
- Multi cluster network
- Fine-grained ACL
- Load balancer type Service

### Performance Enhancement
- eBPF to accelerate intra-node communication
- Tools for automatically profile
- OVN/OVS tailor
- SR-IOV and OVS-DPDK integration

### User Experience Enhancement
- New document website and optimized for new beginners
- Helm/Operator to automate daily operations
- Troubleshooting tools
- Integrated with other projects
