# VPC

From v1.6.0, users can create custom VPC. Each VPC has independent address space, users can set overlapped CIDR, Subnet and Routes.

By default, all subnets without VPC options belong to the default VPC. All functions and usages remain unchanged for users who are not intended to use custom VPC.

## Steps

1. Create a custom VPC

```yaml
kind: Vpc
apiVersion: kubeovn.io/v1
metadata:
  name: test-vpc-1
spec:
  namespaces:
  - ns1
---
kind: Vpc
apiVersion: kubeovn.io/v1
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
apiVersion: kubeovn.io/v1
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
For more, VPC provides a more powerful way to configure your Policy-based routing (PBR).you can configure permit/deny and reroute policies with priority on the router by specifying policyRoutes field in Vpc.
```yaml
kind: Vpc
apiVersion: kubeovn.io/v1
metadata:
  name: test-vpc-1
spec:
  policyRoutes:
    - action: drop
      match: ip4.src==10.0.1.0/24 && ip4.dst==10.0.1.250
      priority: 11
    - action: reroute
      match: ip4.src==10.0.1.0/24
      nextHopIP: 10.0.1.252
      priority: 10
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
  image: 'kubeovn/vpc-nat-gateway:v1.9.0'  # Docker image for vpc nat gateway
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
      externalPort: '8888'
      protocol: tcp
      internalIp: 10.0.1.10
      internalPort: '80'
  snatRules:
    - eip: 192.168.0.112
      internalCIDR: 10.0.1.0/24
  selector:                        # NodeSelector for vpc-nat-gw pod, the item of array should be string type with key:value format
    - "kubernetes.io/hostname: kube-ovn-worker"
    - "kubernetes.io/os: linux"
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

## VPC LoadBalancer

Allow external network to access services in custom VPCs.

### Steps to use VPC LoadBalancer

1. Install Multus CNI and macvlan CNI.

2. Pull docker image `kubeovn/vpc-nat-gateway:v1.9.0`.

3. Create an attachment network using macvlan. Replace `eth0` on necessary.

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: ovn-vpc-lb
  namespace: kube-system
spec:
  config: '{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "eth0",
      "mode": "bridge",
      "ipam": {
        "type": "kube-ovn",
        "server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
        "provider": "ovn-vpc-lb.kube-system"
      }
    }'
```

3. Create a subnet used to provide IPAM for the macvlan interfaces.

```yaml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: ovn-vpc-lb
spec:
  protocol: IPv4
  provider: ovn-vpc-lb.kube-system
  cidrBlock: 202.120.68.0/24
  gateway: 202.120.68.1
  excludeIps:
  - 202.120.68.1..202.120.68.100
```

4. Add annotations to the custom VPC.

```bash
kubectl annotate --overwrite vpc <VPC_NAME> ovn.kubernetes.io/vpc_lb=on
```

5. Access services in the custom VPC.

Add static route(s) in your host or router:

```bash
ip route add <SVC_IP> via <VPC_LB_IP>
```

Replace `<VPC_LB_IP>` with the VPC LB Pod's IP address in subnet `ovn-vpc-lb`.

## Custom VPC limitation

- Custom VPC can not access host network
- Not support DNS/Service/Loadbalancer
