
Kube-OVN has supported the implementation of VPC. The detailed configuration can be referred to 
[Vpc](https://github.com/kubeovn/kube-ovn/blob/master/docs/vpc.md).

A customized VPC can connect the Internet through the vpc-nat-gw pod. The default vpc can also be configured with a gateway, so that pod in customized vpc can visit the pod in default vpc.

The precondition should be matched in the environment:
1. The multus-cni and macvlan cni should be installed, which is used for creating an attachment network for the gateway pod.
2. The configmap of `ovn-vpc-nat-gw-config` in the kube-system namespace should have existed. It should be created if the configmap does not exist, which name is stable.

One detailed configuration for the configmap is as follows:

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

# Configuration for Default Vpc Gateway
## Create Subnet
After the installation of Kube-OVN, the default vpc 
`ovn-cluster` and the default subnet `ovn-default` had been created. The gateway pod for default vpc can be assigned an ip address from the default subnet, and also can be assigned an ip address from a new subnet.

It's an example to assign IP from a new subnet for the gateway pod.

Create subnet with yaml as follows

```
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: test
spec:
  cidrBlock: 192.100.0.0/16
  default: false
  disableGatewayCheck: false
  disableInterConnection: true
  gatewayNode: ""
  gatewayType: distributed
  natOutgoing: false
  private: false
  protocol: IPv4
  provider: ovn
  vpc: ovn-cluster
```

## Create Gateway Pod
Create a vpc-nat-gw crd with yaml as follows, which will create a gateway pod for the default vpc.

```
apiVersion: kubeovn.io/v1
kind: VpcNatGateway
metadata:
  name: default
spec:
  vpc: ovn-cluster                            # default vpc
  subnet: test                                # assign ip from the subnet for gateway pod
  lanIp: 192.100.10.10                        # ip address for gateway pod

  eips:
  - eipCIDR: 172.18.0.12/16                   # eip address for pod in the default vpc
    gateway: 172.18.0.1
  - eipCIDR: 172.18.0.22/16
    gateway: 172.18.0.1
```

After the vpc-nat-gw crd is created, a gateway pod will be created automatically in the kube-system namespace.

```
apple@appledeMacBook-Pro ovn-test % kubectl get pod -n kube-system
NAME                                             READY   STATUS    RESTARTS   AGE
coredns-f9fd979d6-dcppf                          1/1     Running   0          4d18h
coredns-f9fd979d6-fg7rw                          1/1     Running   0          4d18h
etcd-kube-ovn-control-plane                      1/1     Running   0          4d18h
kube-apiserver-kube-ovn-control-plane            1/1     Running   0          4d18h
kube-controller-manager-kube-ovn-control-plane   1/1     Running   0          4d18h
kube-multus-ds-g782g                             1/1     Running   0          22h
kube-multus-ds-knj7m                             1/1     Running   0          22h
kube-ovn-cni-2q6b9                               1/1     Running   0          4d18h
kube-ovn-cni-6x7jl                               1/1     Running   0          4d18h
kube-ovn-controller-7658c87bd-kdwd8              1/1     Running   0          4d18h
kube-ovn-monitor-5dc58b495c-xv5vz                1/1     Running   0          4d18h
kube-ovn-pinger-9mc6l                            1/1     Running   0          4d18h
kube-ovn-pinger-xckxs                            1/1     Running   0          4d18h
kube-proxy-7xk9j                                 1/1     Running   0          4d18h
kube-proxy-h9r6x                                 1/1     Running   0          4d18h
kube-scheduler-kube-ovn-control-plane            1/1     Running   0          4d18h
ovn-central-6b87fcd545-pt8hr                     1/1     Running   0          4d18h
ovs-ovn-8nvj8                                    1/1     Running   0          4d18h
ovs-ovn-wffd2                                    1/1     Running   0          4d18h
vpc-nat-gw-default-cb7b9677f-q6sbg               1/1     Running   0          17h
apple@appledeMacBook-Pro ovn-test %
```

There's no need to add a custom route to the gateway pod in the default vpc. The subnet in the default vpc has added route when created.

To visit the pod in the default vpc, it's visiting the eip address configured in gateway pod in fact, which is the same as visit pod in customized vpc.