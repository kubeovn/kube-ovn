# Kube-OVN RoadMap

## v1.9.0 -- Jan 2022

### VPC Enhancement
- Namespaced level VPC 
- Security group support
- L4 load balancer for custom VPC
- QoS for NAT gateway

### Performance Optimization
- OVN logical flow simplification
- FastPath for 4.x kernel
- Replace Service and Networkpolicy implement from OVN to eBPF
- Control plan profiling

### Network QoS
- Support traffic priority for different workloads
- Inject network loss and latency for chaos engineering

### Virtualization Enhancement
- Kubevirt live migration with static IP
- Kubevirt/Kata high performance type nic

### Monitoring and operation
- Cilium monitoring and application level tracing integration
- Disaster recovery when all OVN db lost
