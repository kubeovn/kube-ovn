# Kube-OVN Non-Primary CNI Mode

This document describes how to use Kube-OVN as a non-primary (secondary) CNI alongside other primary CNI plugins like Cilium, Calico, or Flannel.

## Overview

In non-primary CNI mode, Kube-OVN works as a secondary CNI plugin that provides additional network interfaces to pods while another CNI handles the primary network interface (eth0). This allows you to:

- Use Kube-OVN's advanced networking features (VPC, subnets, security groups) alongside your existing CNI
- Implement network segmentation and multi-tenancy
- Provide multiple network interfaces to pods for different purposes
- Leverage Kube-OVN's VPC NAT Gateway functionality for secondary networks

## Prerequisites

### Required Components

1. **Primary CNI**: Any Kubernetes-compatible CNI (Cilium, Calico, Flannel, etc.)
2. **Multus CNI**: Required for multi-network support
3. **Kube-OVN**: Configured in non-primary mode

### Installation Order

1. Install and configure your primary CNI
2. Install Multus CNI
3. Install Kube-OVN in non-primary mode

## Installation

### Using Helm Chart v2

```yaml
# values.yaml
cni:
  nonPrimaryCNI: true
```

Install with Helm:

```bash
helm install kube-ovn ./charts/kube-ovn-v2 \
  --namespace kube-system \
  --set cni.nonPrimaryCNI=true
```

### Using Helm Chart v1

```yaml
# values.yaml
cni_conf:
  NON_PRIMARY_CNI: true
```

Install with Helm:

```bash
helm install kube-ovn ./charts/kube-ovn \
  --namespace kube-system \
  --set cni_conf.NON_PRIMARY_CNI=true
```

### Manual Installation

Add the following flag to the kube-ovn-controller deployment:

```yaml
containers:
- name: kube-ovn-controller
  args:
  - --non-primary-cni-mode=true
```

## Configuration

### Network Attachment Definitions (NADs)

Create NADs to define additional network interfaces:

```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: kube-ovn-vpc-network
  namespace: default
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "kube-ovn",
      "server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
      "provider": "kube-ovn-vpc-network.default.ovn"
    }
```

### VPC and Subnet Configuration

Create VPC and subnet resources for secondary networks:

```yaml
apiVersion: kubeovn.io/v1
kind: Vpc
metadata:
  name: vpc-secondary
spec:
  namespaces:
  - default
---
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: subnet-secondary
spec:
  vpc: vpc-secondary
  cidr: "10.100.0.0/16"
  gateway: "10.100.0.1"
  provider: kube-ovn-vpc-network.default.ovn
```

### Pod Configuration

Annotate pods to request additional network interfaces:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: multi-network-pod
  annotations:
    k8s.v1.cni.cncf.io/networks: default/kube-ovn-vpc-network
spec:
  containers:
  - name: app
    image: nginx
```

## Network Interface Behavior

### Primary Interface (eth0)
- Managed by the primary CNI (Cilium, Calico, etc.)
- Used for cluster networking, service discovery, and default routing
- Provides connectivity to Kubernetes services and external networks

### Secondary Interfaces (net1, net2, ...)
- Managed by Kube-OVN
- Provide additional network connectivity
- Can be connected to different VPCs and subnets
- Support Kube-OVN features like security groups, QoS, and NAT

## Use Cases

### 1. Network Segmentation

Separate different types of traffic onto different network interfaces:

```yaml
# Frontend pods with access to both public and internal networks
apiVersion: v1
kind: Pod
metadata:
  name: frontend
  annotations:
    k8s.v1.cni.cncf.io/networks: |
      [
        {"name": "default/public-network"},
        {"name": "default/internal-network"}
      ]
```

### 2. Multi-Tenancy

Provide tenant-specific networks:

```yaml
# Tenant A pods
apiVersion: v1
kind: Pod
metadata:
  name: tenant-a-app
  annotations:
    k8s.v1.cni.cncf.io/networks: default/tenant-a-network

---
# Tenant B pods  
apiVersion: v1
kind: Pod
metadata:
  name: tenant-b-app
  annotations:
    k8s.v1.cni.cncf.io/networks: default/tenant-b-network
```

### 3. VPC NAT Gateway

Enable external connectivity for secondary networks:

```yaml
apiVersion: kubeovn.io/v1
kind: VpcNatGateway
metadata:
  name: vpc-nat-gw
spec:
  vpc: vpc-secondary
  subnet: subnet-secondary
  lanIp: "10.100.0.254"
```

## Advanced Features

### Multiple Network Providers

Pods can have interfaces from multiple providers:

```yaml
metadata:
  annotations:
    k8s.v1.cni.cncf.io/networks: |
      [
        {"name": "default/kube-ovn-network-1", "interface": "net1"},
        {"name": "default/kube-ovn-network-2", "interface": "net2"}
      ]
```

### IP Address Assignment

Control IP assignment for secondary interfaces:

```yaml
# Static IP assignment
metadata:
  annotations:
    k8s.v1.cni.cncf.io/networks: |
      [{
        "name": "default/kube-ovn-network",
        "ips": ["10.100.0.100/16"]
      }]
```

### Quality of Service (QoS)

Apply QoS policies to secondary interfaces:

```yaml
metadata:
  annotations:
    ovn.kubernetes.io/ingress_rate: "1000"
    ovn.kubernetes.io/egress_rate: "1000"
```

## Troubleshooting

### Common Issues

1. **Pod fails to get secondary interface**
   - Verify NAD is created in the correct namespace
   - Check kube-ovn-cni logs for errors
   - Ensure Multus is properly installed

2. **Secondary interface has no connectivity**
   - Verify subnet and VPC configuration
   - Check routing table in the pod
   - Ensure security group rules allow traffic

3. **IP address conflicts**
   - Verify subnet CIDR ranges don't overlap
   - Check for static IP conflicts
   - Review IPAM configuration

### Debug Commands

```bash
# Check pod network status
kubectl get pod <pod-name> -o yaml | grep -A 10 "networks-status"

# View network interfaces in pod
kubectl exec <pod-name> -- ip addr show

# Check routing table
kubectl exec <pod-name> -- ip route show

# Debug kube-ovn-cni
kubectl logs -n kube-system daemonset/kube-ovn-cni
```

## Limitations

1. **Service Discovery**: Secondary networks don't participate in Kubernetes service discovery
2. **Network Policies**: Kubernetes NetworkPolicies only apply to primary interfaces
3. **Load Balancing**: Service load balancing typically only works on primary interfaces
4. **DNS**: Pod DNS resolution happens through the primary interface

## Best Practices

1. **Network Planning**: Plan your network topology carefully to avoid IP conflicts
2. **Resource Management**: Monitor resource usage as each interface consumes additional resources
3. **Security**: Apply appropriate security group rules to secondary networks
4. **Documentation**: Document your network architecture and interface purposes
5. **Testing**: Thoroughly test connectivity between different network segments

## Migration Guide

### From Primary to Non-Primary Mode

1. **Backup**: Backup your current configuration
2. **Plan**: Design your multi-network architecture
3. **Install Primary CNI**: Install and configure your chosen primary CNI
4. **Install Multus**: Deploy Multus CNI
5. **Reconfigure Kube-OVN**: Switch to non-primary mode
6. **Create NADs**: Define network attachment definitions
7. **Update Workloads**: Add network annotations to pods
8. **Validate**: Test connectivity and functionality

### Rollback Procedure

1. Remove network annotations from pods
2. Delete NADs
3. Reconfigure Kube-OVN to primary mode
4. Remove Multus CNI
5. Remove primary CNI (if returning to Kube-OVN only)

## Examples

See the [test files](../test/) for complete examples of:
- [Controller tests](../pkg/controller/controller_test.go) - Network attachment handling
- [Endpoint slice tests](../pkg/controller/endpoint_slice_test.go) - Secondary IP management
- [Pod tests](../pkg/controller/pod_test.go) - VPC NAT Gateway integration
- [Network attachment tests](../pkg/util/network_attachment_test.go) - Interface extraction utilities

## References

- [Multus CNI Documentation](https://github.com/k8snetworkplumbingwg/multus-cni)
- [Network Attachment Definition Specification](https://github.com/k8snetworkplumbingwg/multi-net-spec)
- [Kube-OVN Architecture](https://kubeovn.github.io/docs/stable/en/reference/architecture/)
