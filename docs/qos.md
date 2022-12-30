# QoS

Kube-OVN supports dynamically configurations of Ingress and Egress traffic rate limiting for a single pod level and gateway level.

Before v1.9.0, Kube-OVN can use annotation ` ovn.kubernetes.io/ingress_rate` and ` ovn.kubernetes.io/egress_rate` to specify the bandwidth of pod. The unit is 'Mbit/s'. We can set QoS when creating a pod or dynamically set QoS by changing annotations for pod.

Since v1.9.0, Kube-OVN starts to support linux-htb and linux-netem QoS settings. The detailed description for the QoS can be found at [QoS](https://man7.org/linux/man-pages/man5/ovs-vswitchd.conf.db.5.html#QoS_TABLE)

> The QoS function supports both overlay and vlan mode network.

## Previous Pod QoS Setting
Use the following annotations to specify QoS:
- `ovn.kubernetes.io/ingress_rate`: Rate limit for Ingress traffic, unit: Mbit/s
- `ovn.kubernetes.io/egress_rate`: Rate limit for Egress traffic, unit: Mbit/s

## linux-htb QoS
A CRD resource is added to set QoS priority for linux-htb QoS.
CRD is defined as follows:

```
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: htbqoses.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
      - name: PRIORITY
        type: string
        jsonPath: .spec.priority
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                priority:
                  type: string					# Value in range 0 to 7.
  scope: Cluster
  names:
    plural: htbqoses
    singular: htbqos
    kind: HtbQos
    shortNames:
      - htbqos
```
The spec parameter has only one field, `htbqoses.spec.priority`, which value represents the priority. Three CRD instances are preset in image, namely

```
mac@bogon kube-ovn % kubectl get htbqos
NAME            PRIORITY
htbqos-high     1
htbqos-low      5
htbqos-medium   3
```
Specific priority values are unimportant, only relative ordering matters. The smaller the priority value, the higher the QoS priority.

A new field is added for Subnet CRD, `subnet.spec.htbqos`, which is used to specify the htbqos instance bound to subnet. The values are shown below

```
mac@bogon kube-ovn % kubectl get subnet test -o yaml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: test
spec:
  cidrBlock: 192.168.0.0/16
  default: false
  gatewayType: distributed
  htbqos: htbqos-high
  ...
```
When the subnet specifies an htbqos instance, all pods under the subnet have the same priority setting.

A new annotation is added for pod, `ovn.kubernetes.io/priority`, which value is a specific priority value, such as `ovn.kubernetes.io/priority: "50"`. It can be used to set the QoS priority parameter for pod separately.

When the subnet specifies the htbqos parameter and pod sets the QoS priority annotation, the value of pod annotation shall prevail.

**The previous annotation `ovn.kubernetes.io/ingress_rate` and `ovn.kubernetes.io/egress_rate`, can still be used to control the bidirectional bandwidth of pod.**

## linux-netem QoS
New annotations added for pod, `ovn.kubernetes.io/latency`、 `ovn.kubernetes.io/jitter`、 `ovn.kubernetes.io/limit` and
`ovn.kubernetes.io/loss`, used for setting QoS parameters of linux-netem type.

`latency` is used for traffic delay. The value is an integer value, and the unit is `ms`.

`jitter` is used for traffic delay jitter. The value is an integer value, and the unit is `ms`.

`limit` is the maximum number of packets the qdisc may hold queued at a time. The value is an integer value, such as 1000.

`loss` is an independent loss probability to the packets outgoing from the chosen network interface, the valid range for this field is from 0 to 100. For example, if the value is 20, the packet loss probability is 20%.

## Caution
linux-htb QoS and linux-netem QoS are two types of QoS. Pod cannot support both types of QoS at the same time, so the annotation settings should not conflict. The annotations cannot be set at the same time.

## Gateway QoS

Kube-OVN will create an `ovn0` interface on each host to route traffic from cluster pod network
to external network. Kube-OVN control gateway QoS by modify the QoS config of `ovn0` interface.

For a subnet with central gateway mode, only one node act as the gateway, so you can modify the
node QoS annotation to control the QoS of the subnet to external network.

```yaml
apiVersion: v1
kind: Node
metadata:
  annotations:
    ovn.kubernetes.io/ingress_rate: "3"
    ovn.kubernetes.io/egress_rate: "1"
  name: liumengxin-ovn1-192.168.16.44
```

You can also use this annotation to control the traffic from each node to external network
through these annotations.

# Test
## QoS Priority Case
When the parameter `subnet.Spec.HtbQos` is specified for subnet, such as `htbqos: htbqos-high`, and the annotation `ovn.kubernetes.io/priority` is specified for pod, such as `ovn.kubernetes.io/priority: "50"`, the actual priority settings are as follows

```
mac@bogon kube-ovn % kubectl get pod -n test -o wide
NAME                    READY   STATUS    RESTARTS   AGE     IP            NODE              NOMINATED NODE   READINESS GATES
test-57dbcb6dbd-7z9bc   1/1     Running   0          7d18h   192.168.0.2   kube-ovn-worker   <none>           <none>
test-57dbcb6dbd-vh6dq   1/1     Running   0          7d18h   192.168.0.3   kube-ovn-worker   <none>           <none>
mac@bogon kube-ovn % kubectl ko nbctl lsp-list test
af7553e0-beda-4af1-a5d4-26eb836df6ef (test-57dbcb6dbd-7z9bc.test)
cefd0820-50ee-40e5-acb6-980ea6b1bbfd (test-57dbcb6dbd-vh6dq.test)
b22bf97c-544e-4569-a0ee-6e77386c4181 (test-ovn-cluster)
mac@bogon kube-ovn % kubectl ko vsctl kube-ovn-worker list qos
_uuid               : 90d1a865-887d-4271-9874-b23b06b7d8ff
external_ids        : {iface-id=test-57dbcb6dbd-7z9bc.test, pod="test/test-57dbcb6dbd-7z9bc"}
other_config        : {}
queues              : {0=a8a3dda7-8c08-474a-848e-c9f45faba9e1}
type                : linux-htb

_uuid               : d63bb9b9-e58c-4292-b8af-97743ddc26ef
external_ids        : {iface-id=test-57dbcb6dbd-vh6dq.test, pod="test/test-57dbcb6dbd-vh6dq"}
other_config        : {}
queues              : {0=405e4b3d-38fc-42e8-876b-1db6c1c65aab}
type                : linux-htb

_uuid               : b6a25e6f-5153-4b38-ac5c-1252ace9af28
external_ids        : {}
other_config        : {}
queues              : {}
type                : linux-noop
mac@bogon kube-ovn % kubectl ko vsctl kube-ovn-worker list queue
_uuid               : 405e4b3d-38fc-42e8-876b-1db6c1c65aab
dscp                : []
external_ids        : {iface-id=test-57dbcb6dbd-vh6dq.test, pod="test/test-57dbcb6dbd-vh6dq"}
other_config        : {priority="50"}

_uuid               : a8a3dda7-8c08-474a-848e-c9f45faba9e1
dscp                : []
external_ids        : {iface-id=test-57dbcb6dbd-7z9bc.test, pod="test/test-57dbcb6dbd-7z9bc"}
other_config        : {priority="100"}
mac@bogon kube-ovn %
```

## Ingress And Egress Bandwidth Case

Create a pod with annotations `ovn.kubernetes.io/ingress_rate` and `ovn.kubernetes.io/egress_rate` as follows. Or you can modify the annotations with `kubectl` command after the pod is created.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: qos
  namespace: ls1
  annotations:
    ovn.kubernetes.io/ingress_rate: "3"
    ovn.kubernetes.io/egress_rate: "1"
spec:
  containers:
  - name: qos
    image: nginx:alpine
```

1. Create pod for performance testing with yaml
```yaml
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: perf
  namespace: ls1
  labels:
    app: perf
spec:
  selector:
    matchLabels:
      app: perf
  template:
    metadata:
      labels:
        app: perf
    spec:
      containers:
      - name: nginx
        image: kubeovn/perf
```
2. Use one pod as iperf3 server.
```bash
[root@node2 ~]# kubectl exec -it perf-4n4gt -n ls1 sh
/ # iperf3 -s
-----------------------------------------------------------
Server listening on 5201
-----------------------------------------------------------

```

3. Another pod is used as iperf3 client and run a test.
```bash
[root@node2 ~]# kubectl exec -it perf-d4mqc -n ls1 sh
/ # iperf3 -c 10.66.0.12
Connecting to host 10.66.0.12, port 5201
[  4] local 10.66.0.14 port 51544 connected to 10.66.0.12 port 5201
[ ID] Interval           Transfer     Bandwidth       Retr  Cwnd
[  4]   0.00-1.00   sec  86.4 MBytes   725 Mbits/sec    3    350 KBytes
[  4]   1.00-2.00   sec  89.9 MBytes   754 Mbits/sec  118    473 KBytes
[  4]   2.00-3.00   sec   101 MBytes   848 Mbits/sec  184    586 KBytes
[  4]   3.00-4.00   sec   104 MBytes   875 Mbits/sec  217    671 KBytes
[  4]   4.00-5.00   sec   111 MBytes   935 Mbits/sec  175    772 KBytes
[  4]   5.00-6.00   sec   100 MBytes   840 Mbits/sec  658    598 KBytes
[  4]   6.00-7.00   sec   106 MBytes   890 Mbits/sec  742    668 KBytes
[  4]   7.00-8.00   sec   102 MBytes   857 Mbits/sec  764    724 KBytes
[  4]   8.00-9.00   sec  97.4 MBytes   817 Mbits/sec  1175    764 KBytes
[  4]   9.00-10.00  sec   111 MBytes   934 Mbits/sec  1083    838 KBytes
- - - - - - - - - - - - - - - - - - - - - - - - -
[ ID] Interval           Transfer     Bandwidth       Retr
[  4]   0.00-10.00  sec  1010 MBytes   848 Mbits/sec  5119             sender
[  4]   0.00-10.00  sec  1008 MBytes   846 Mbits/sec                  receiver

iperf Done.
/ #
```

The bandwidth without limited is about 848 Mbits/sec for pod, and modify annotations for iperf3 server pod.

```bash
[root@node2 ~]# kubectl annotate --overwrite  pod perf-4n4gt -n ls1 ovn.kubernetes.io/ingress_rate=30
```

4. Use iperf3 to test bandwidth again
```bash
/ # iperf3 -c 10.66.0.12
Connecting to host 10.66.0.12, port 5201
[  4] local 10.66.0.14 port 52372 connected to 10.66.0.12 port 5201
[ ID] Interval           Transfer     Bandwidth       Retr  Cwnd
[  4]   0.00-1.00   sec  3.66 MBytes  30.7 Mbits/sec    2   76.1 KBytes
[  4]   1.00-2.00   sec  3.43 MBytes  28.8 Mbits/sec    0    104 KBytes
[  4]   2.00-3.00   sec  3.50 MBytes  29.4 Mbits/sec    0    126 KBytes
[  4]   3.00-4.00   sec  3.50 MBytes  29.3 Mbits/sec    0    144 KBytes
[  4]   4.00-5.00   sec  3.43 MBytes  28.8 Mbits/sec    0    160 KBytes
[  4]   5.00-6.00   sec  3.43 MBytes  28.8 Mbits/sec    0    175 KBytes
[  4]   6.00-7.00   sec  3.50 MBytes  29.3 Mbits/sec    0    212 KBytes
[  4]   7.00-8.00   sec  3.68 MBytes  30.9 Mbits/sec    0    294 KBytes
[  4]   8.00-9.00   sec  3.74 MBytes  31.4 Mbits/sec    0    398 KBytes
[  4]   9.00-10.00  sec  3.80 MBytes  31.9 Mbits/sec    0    526 KBytes
- - - - - - - - - - - - - - - - - - - - - - - - -
[ ID] Interval           Transfer     Bandwidth       Retr
[  4]   0.00-10.00  sec  35.7 MBytes  29.9 Mbits/sec    2             sender
[  4]   0.00-10.00  sec  34.5 MBytes  29.0 Mbits/sec                  receiver

iperf Done.
/ #
```
