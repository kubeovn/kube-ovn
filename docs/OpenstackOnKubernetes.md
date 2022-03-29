# Openstack on Kubernetes

## Target

Some Kube-OVN users need to deploy both containers and VMs(Virtual Machines) in one host node to improve resource utilization. This document describes the network part of a solution to achieve this goal.

In this solution, Kubernetes Kube-OVN and Openstack Neutron share the same OVN platform. Also, within a host node, both VMs and containers are connected to the same OvS(Open vSwitch). All OvS are managed by the same shared OVN. Therefore, all VMs and containers are connected to the same virtual network and are scheduled by OVN.

This document is based on Openstack *Victoria* and Kube-OVN *1.8.0*

## Prerequisite

- Openstack neutron should build networking based on OVN.
- Subnets CIDRs in Openstack and Kubernetes connected to the same router MUST NOT be overlapped.

## Kubernetes configuration

Install Kubernetes with Kube-OVN in overlay mode normally.

Record IP of the host nodes where ovn-cni is located as [IP<sub>1</sub>, IP<sub>2</sub>, ..., IP<sub>n</sub>].

## Openstack Neutron

Install Neutron based on ovn as the [document](https://docs.openstack.org/neutron/victoria/install/ovn/manual_install.html).

The configuration of the ovn document on the controller node could be omitted.

The following configuration of Neutron in **/etc/neutron/plugins/ml2/ml2_conf.ini** should be changed

```conf
[ovn]
...
ovn_nb_connection = tcp:[IP1,IP2,...,IPn]:6641
ovn_sb_connection = tcp:[IP1,P2,...,IPn]:6642
ovn_l3_scheduler = OVN_L3_SCHEDULER
```

The following configuration of OvS on each host node should be changed.

```bash
ovs-vsctl set open . external-ids:ovn-remote=tcp:[IP1,IP2,...,IPn]:6642
ovs-vsctl set open . external-ids:ovn-encap-type=geneve
ovs-vsctl set open . external-ids:ovn-encap-ip=IP_ADDRESS   # IP_ADDRESS should be accessible for all nodes
```

## Verification

##### Environment:

First, assume that Openstack has created a VM and the subnet on which the VM is located. Then Openstack also creates a router and lets the subnet connect to the router.

In this example, the information for openstack is as follows:

```shell
# openstack router list
+--------------------------------------+---------+--------+-------+----------------------------------+
| ID                                   | Name    | Status | State | Project                          |
+--------------------------------------+---------+--------+-------+----------------------------------+
| 22040ed5-0598-4f77-bffd-e7fd4db47e93 | router0 | ACTIVE | UP    | 62381a21d569404aa236a5dd8712449c |
+--------------------------------------+---------+--------+-------+----------------------------------+
# openstack network list
+--------------------------------------+----------+--------------------------------------+
| ID                                   | Name     | Subnets                              |
+--------------------------------------+----------+--------------------------------------+
| cd59e36a-37db-4c27-b709-d35379a7920f | provider | 01d73d9f-fdaa-426c-9b60-aa34abbfacae |
+--------------------------------------+----------+--------------------------------------+
# openstack subnet list
+--------------------------------------+-------------+--------------------------------------+----------------+
| ID                                   | Name        | Network                              | Subnet         |
+--------------------------------------+-------------+--------------------------------------+----------------+
| 01d73d9f-fdaa-426c-9b60-aa34abbfacae | provider-v4 | cd59e36a-37db-4c27-b709-d35379a7920f | 192.168.1.0/24 |
+--------------------------------------+-------------+--------------------------------------+----------------+
# openstack server list
+--------------------------------------+-------------------+--------+-----------------------+--------+--------+
| ID                                   | Name              | Status | Networks              | Image  | Flavor |
+--------------------------------------+-------------------+--------+-----------------------+--------+--------+
| 8433d622-a8d6-41a7-8b31-49abfd64f639 | provider-instance | ACTIVE | provider=192.168.1.61 | ubuntu | m1     |
+--------------------------------------+-------------------+--------+-----------------------+--------+--------+
```

in the Kubernetes master node, we can see the corresponding Openstack network structure with the following CRD:

```shell
# kubectl get vpc
NAME                                           STANDBY   SUBNETS
neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93   true      ["neutron-cd59e36a-37db-4c27-b709-d35379a7920f"]
ovn-cluster                                    true      ["join","ovn-default"]
```

Obviously, the network components starting with "neutron" are from Openstack. Where "neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93" is a router and "neutron-cd59e36a-37db-4c27-b709-d35379a7920f" is a subnet.

##### Add subnet in Kubernetes:

Now Kube-OVN supports adding new containers to the Openstack virtual network by modifying Kube-OVN CRD api. In fact, this feature comes from the support for the [vpc](https://github.com/kubeovn/kube-ovn/blob/master/docs/vpc.md).

The following steps describe a way to add a container subnet to the Openstack network.

1. Add a namespace.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: net2
```

2. Add the namespaces to the VPC.

   NOTICE that subnet name should be added in *spec.namespace*.

```yaml
apiVersion: kubeovn.io/v1
kind: Vpc
metadata:
  creationTimestamp: "2021-06-20T13:34:11Z"
  generation: 2
  labels:
    ovn.kubernetes.io/vpc_external: "true"
  name: neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93
  resourceVersion: "583728"
  uid: 18d4c654-f511-4def-a3a0-a6434d237c1e
spec:
  namespaces:
  - net2
```

3. Add the corresponding subnet.

   NOTICE that *spec.vpc* need to be specified as the name of the router to be connected to.

```yaml
kind: Subnet
apiVersion: kubeovn.io/v1
metadata:
  name: net2
spec:
  vpc: neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93
  namespaces:
  - net2
  cidrBlock: 12.0.1.0/24
  natOutgoing: false
```

4. Add pod and test.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ubuntu
  namespace: net2
spec:
  containers:
  - image: kubeovn/kube-ovn:v1.8.4
    command:
      - "sleep"
      - "604800"
    imagePullPolicy: IfNotPresent
    name: ubuntu
  restartPolicy: Always
```

There are several ways to check the correctness of the network.

1. Check virtual network topology in the master node.

   In our example, we should check the relationship of switch provider, switch net2, and router router0 to ensure they are connected correctly.

```shell
# kubectl ko nbctl show
switch 0ff17729-6e8b-4fcc-9c9f-9a114cd3cc84 (neutron-cd59e36a-37db-4c27-b709-d35379a7920f) (aka provider) # check switch provider
    port a7a2f2be-c8e2-4129-b94b-f28f566e4ea6
        type: router
        router-port: lrp-a7a2f2be-c8e2-4129-b94b-f28f566e4ea6  # check peer router
    port 5cb326b9-89bc-4ca1-9f03-66750f0c3efb
        addresses: ["fa:16:3e:f4:82:3c 192.168.1.61"]
switch b830c07e-ab42-44f9-9843-c393d6584628 (net2) # check switch net2
    port ubuntu.net2
        addresses: ["00:00:00:7A:2E:93 12.0.1.2"]
    port net2-neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93
        type: router
        addresses: ["00:00:00:91:4D:DF"]
        router-port: neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93-net2  # check peer router
switch 08ded71a-8360-4d0a-9ecb-a6dbf89ca16a (join)
    port node-ty-c3-medium-x86-02
        addresses: ["00:00:00:6E:53:FD 100.64.0.2", "unknown"]
    port join-ovn-cluster
        type: router
        addresses: ["00:00:00:32:40:B8"]
        router-port: ovn-cluster-join
switch d042bf57-bc43-4f7b-bdfe-ea927afc7659 (ovn-default)
    port kube-ovn-pinger-77m4k.kube-system
        addresses: ["00:00:00:C0:53:B8 10.16.0.9", "unknown"]
    port coredns-558bd4d5db-c5mnt.kube-system
        addresses: ["00:00:00:8D:7F:8A 10.16.0.7", "unknown"]
    port ovn-default-ovn-cluster
        type: router
        addresses: ["00:00:00:48:A1:7D"]
        router-port: ovn-cluster-ovn-default
    port coredns-558bd4d5db-p4djx.kube-system
        addresses: ["00:00:00:57:49:5A 10.16.0.6", "unknown"]
    port kube-ovn-monitor-64756fb44f-hl495.kube-system
        addresses: ["00:00:00:18:B7:C4 10.16.0.10"]
router 63d6c4d8-4aa3-41e7-8128-b8917aa6c386 (neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93) (aka router0) # check router router0
    port neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93-net2
        mac: "00:00:00:91:4D:DF"
        networks: ["12.0.1.1/24"]
    port lrp-a7a2f2be-c8e2-4129-b94b-f28f566e4ea6
        mac: "fa:16:3e:1f:52:fc"
        networks: ["192.168.1.1/24"]
```

2. Check *kube-ovn-controller* to ensure there is no error.

3. Check CRD *vpc* to ensure subnets are attached.

```shell
# kubectl get vpc
NAME                                           STANDBY   SUBNETS
neutron-22040ed5-0598-4f77-bffd-e7fd4db47e93   true      ["net2"]
ovn-cluster                                    true      ["join","ovn-default"]
```

4. Ping VMs in Kubernetes pods.

```shell
# kubectl -n net2 get pods
NAME     READY   STATUS    RESTARTS   AGE
ubuntu   1/1     Running   0          31m
# kubectl -n net2 exec -ti ubuntu bash
kubectl exec [POD] [COMMAND] is DEPRECATED and will be removed in a future version. Use kubectl exec [POD] -- [COMMAND] instead.
root@ubuntu:/kube-ovn# ping 192.168.1.61 -c2
PING 192.168.1.61 (192.168.1.61): 56 data bytes
64 bytes from 192.168.1.61: icmp_seq=0 ttl=63 time=1.513 ms
64 bytes from 192.168.1.61: icmp_seq=1 ttl=63 time=0.506 ms
--- 192.168.1.61 ping statistics ---
2 packets transmitted, 2 packets received, 0% packet loss
round-trip min/avg/max/stddev = 0.506/1.010/1.513/0.504 ms
```

## Uninstall

Delete pods, namespaces, subnets in order.
