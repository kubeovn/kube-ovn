# Kube-OVN RoadMap

## v1.11.0 -- July 2022

### DataCenter Network
- DNS in custom VPC
- L2 LB in custom VPC

### Application Network
- Windows overlay/underlay network support
- Multi cluster Service
- Load balancer type Service

### Performance
- Use libovsdb to replace ovn-nbctl daemon
- Optimize OVN logical flows

## Planned features

### DataCenter Network
- Namespaced VPC and Subnet
- Integrate DPU to support bare metal
- Integrate SDN switches to support bare metal

### Application Network
- Traffic visualization and application level analyzing
- Multi cluster Service/DNS/Networkpolicy
- Traffic encryption
- Fine-grained ACL

### Performance Enhancement
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
