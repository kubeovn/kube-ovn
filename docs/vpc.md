# VPC

From v1.6.0, users can create custom VPC. Each VPC has independent address space, users can set overlapped CIDR, Subnet and Routes.

By default, all subnets without VPC options belong to the default VPC. All functions and usages remain unchanged for users who are not intended to use custom VPC.

*To connect custom VPC network with the external network, custom gateway is needed. This part of work is still work in progress.*

## Steps
1. Create a custom VPC
```
kind: Vpc
metadata:
  name: test-vpc-1
spec:
  namespaces:
  - ns1
---
kind: Vpc
metadata:
  name: test-vpc-2
spec: {}
```

The `namespace` list can limit which namespace can bind to the VPC, no limit if the list is empty

2. Create subnet
```
kind: Subnet
apiVersion: kubeovn.io/v1
metadata:
  name: net1
spec:
  vpc: test-vpc-1
  namespaces:
    - ns1
  cidrBlock: 10.0.1.0/24
  default: true
  gatewayType: distributed
  natOutgoing: false
  private: false
  protocol: IPv4
  provider: ovn
  underlayGateway: false
 
---
kind: Subnet
apiVersion: kubeovn.io/v1
metadata:
  name: net2
spec:
  vpc: test-vpc-2
  cidrBlock: 10.0.1.0/24
  default: false
  gatewayType: distributed
  natOutgoing: false
  private: false
  protocol: IPv4
  provider: ovn
  underlayGateway: false
```

In the examples above, two subnet in different VPCs can use same IP space

3. Create Pod 

Pod can inherent VPC from the namespace or explicitly bind to subnet by annotation
```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: ne1
  namespace: default
  name: vpc1-pod
---
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: ne2
  namespace: default
  name: vpc2-pod
```

4. Custom routes

VPC level policy routes to orchestrate traffic.

```
kind: Vpc
metadata:
  name: test-vpc-1
spec:
  staticRoutes:
    - cidr: 0.0.0.0/0
      nextHopIP: 10.0.1.254
      policy: policyDst
    - cidr: 172.31.0.0/24
      nextHopIP: 10.0.1.253
      policy: policySrc
```

## Custom VPC limitation

- Custom VPC can not access host network
- Not support DNS/Service/Loadbalancer
- Not support EIP/SNAT
