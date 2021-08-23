# VPC

From v1.6.0, users can create custom VPC. Each VPC has independent address space, users can set overlapped CIDR, Subnet and Routes.

By default, all subnets without VPC options belong to the default VPC. All functions and usages remain unchanged for users who are not intended to use custom VPC.

## Steps
1. Create a custom VPC

```yaml
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

```yaml
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
  natOutgoing: false
---
kind: Subnet
apiVersion: kubeovn.io/v1
metadata:
  name: net2
spec:
  vpc: test-vpc-2
  cidrBlock: 10.0.1.0/24
  natOutgoing: false
```

In the examples above, two subnet in different VPCs can use same IP space

3. Create Pod

Pod can inherent VPC from the namespace or explicitly bind to subnet by annotation

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: net1
  namespace: default
  name: vpc1-pod
---
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/logical_switch: net2
  namespace: default
  name: vpc2-pod
```

4. Custom routes

VPC level policy routes to orchestrate traffic.

```yaml
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

## VPC external gateway
To connect custom VPC network with the external network, custom gateway is needed.

### Steps to use VPC external gateway
First, you need to confirm that Multus-CNI and macvlan CNI have been installed. Then we start to config the VPC nat gateway.

1. Config and enable the feature

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: ovn-vpc-nat-gw-config
  namespace: kube-system
data:
  image: kubeovn/vpc-nat-gateway:v1.7.1  # Docker image for vpc nat gateway
  enable-vpc-nat-gw: true                  # 'true' for enable, 'false' for disable
  nic: eth1                                # The nic that connect to underlay network, use as the 'master' for macvlan
```
Controller will check this configmap and create network attachment definition.

2. Create VPC NAT gateway

```yaml
kind: VpcNatGateway
apiVersion: kubeovn.io/v1
metadata:
  name: ngw
spec:
  vpc: test-vpc-1                  # Specifies which VPC the gateway belongs to
  subnet: sn                       # Subnet in VPC
  lanIp: 10.0.1.254                # Internal IP for nat gateway pod, IP should be within the range of the subnet
  eips:                            # Underlay IPs assigned to the gateway
    - eipCIDR: 192.168.0.111/24
      gateway: 192.168.0.254
    - eipCIDR: 192.168.0.112/24
      gateway: 192.168.0.254
  floatingIpRules:
    - eip: 192.168.0.111
      internalIp: 10.0.1.5
  dnatRules:
    - eip: 192.168.0.112
      externalPort: 8888
      protocol: tcp
      internalIp: 10.0.1.10
      internalPort: 80
  snatRules:
    - eip: 192.168.0.112
      internalCIDR: 10.0.1.0/24
```

3. Add static route to VPC

```yaml
kind: Vpc
apiVersion: kubeovn.io/v1
metadata:
  name: test-vpc-1
spec:
  staticRoutes:
    - cidr: 0.0.0.0/0
      nextHopIP: 10.0.1.254     # Should be the same as the 'lanIp' for vpc gateway
      policy: policyDst
```

## Custom VPC limitation

- Custom VPC can not access host network
- Not support DNS/Service/Loadbalancer
