# Multiple Cluster with Submariner

Currently, multiple Kubernetes clusters need to be deployed for many obvious reasons. 

However, some of the Kubernetes features still need to be preserved, as an example, service discover, and service IP.

This doc introduces some solutions to build multiple clusters.



## Based on Submariner (prototype)

[Submariner](https://submariner.io/getting-started/) is a project that aims to connect multiple clusters.

#### Control plane:

It implements the [doc](https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api) through a subproject called Lighthouse.

Lighthouse uses a broker container to pass information between clusters, including service information and gateway information.

Lighthouse also overlaps a DNS system on top of local kubernetes dns system for resolving domains from other clusters.

The service and endpoint information of the other clusters is stored in serviceimport and endpointslice.

```bash
$ kubectl  -n submariner-k8s-broker get serviceimport
NAME                        TYPE           IP                  AGE
nginx-nginx-test-cluster1   ClusterSetIP   ["11.96.112.197"]   85m
$ kubectl  -n submariner-k8s-broker get serviceimport -o yaml
apiVersion: v1
items:
- apiVersion: multicluster.x-k8s.io/v1alpha1
  kind: ServiceImport
  metadata:
    annotations:
      cluster-ip: 11.96.112.197
      origin-name: nginx
      origin-namespace: nginx-test
    creationTimestamp: "2022-04-27T07:27:44Z"
    generation: 1
    labels:
      lighthouse.submariner.io/sourceCluster: cluster1
      lighthouse.submariner.io/sourceName: nginx
      lighthouse.submariner.io/sourceNamespace: nginx-test
      submariner-io/clusterID: cluster1
      submariner-io/originatingNamespace: submariner-operator
    name: nginx-nginx-test-cluster1
    namespace: submariner-k8s-broker
    resourceVersion: "48194"
    uid: cd7c8a65-dee1-4325-96e7-88fdb3783546
  spec:
    ips:
    - 11.96.112.197
    ports:
    - name: http
      port: 8080
      protocol: TCP
    sessionAffinityConfig: {}
    type: ClusterSetIP
kind: List
metadata:
  resourceVersion: ""
  selfLink: ""
```

```bash
$ kubectl  -n submariner-k8s-broker get endpointslice nginx-cluster1
NAME             ADDRESSTYPE   PORTS   ENDPOINTS    AGE
nginx-cluster1   IPv4          8080    11.16.0.12   87m
$ kubectl  -n submariner-k8s-broker get endpointslice nginx-cluster1 -o yaml
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 11.16.0.12
  conditions:
    ready: true
  deprecatedTopology:
    kubernetes.io/hostname: cluster1
  hostname: nginx-db695794b-6jh7d
kind: EndpointSlice
metadata:
  creationTimestamp: "2022-04-27T07:27:44Z"
  generation: 1
  labels:
    endpointslice.kubernetes.io/managed-by: lighthouse-agent.submariner.io
    lighthouse.submariner.io/sourceNamespace: nginx-test
    multicluster.kubernetes.io/service-name: nginx
    multicluster.kubernetes.io/source-cluster: cluster1
    submariner-io/clusterID: cluster1
    submariner-io/originatingNamespace: nginx-test
  name: nginx-cluster1
  namespace: submariner-k8s-broker
  resourceVersion: "48196"
  uid: a0f52c6d-46c5-4c71-a7ce-785574362833
ports:
- name: http
  port: 8080
  protocol: TCP
```

`11.16.0.12`  is the IP address of the pod for this service `nginx` in another cluster. When this pod from another cluster is rebuilt, the new pod IP should be passed to this cluster, So that the new correct data path could be established. This whole process may takes some time, depending on your network and load.

Besides, submariner now **do not** support the existence of multiple service CIDR or pod CIDR in a cluster.

#### Data Plane:

Submariner supports custom build handlers for different CNIs. 

Currently the default data path is is used in this doc. Between the gateway nodes of clusters, traffic is encapsulated in a IPsec tunnel.  For a work node, the traffic destined to a remote cluster will be transited through VXLAN tunnel(if `vx-submarine`) to the gateway node first.

#### operations:

First you need to have two clusters, called `cluster 0` and `cluster 1`.  Note that service CIDR and pod CIDR are not allowed to overlap.

Then install `subct` as [doc](https://submariner.io/operations/deployment/).

Let's deploy submariner broker in a cluster, say `cluster0`.

```bash
$ subctl deploy-broker --kubeconfig /etc/kubernetes/admin.conf
```

Then join both clusters to the broker. 
Note that the contents of file `broker-info.subm` are the same. You can check the content by decrypting the file: `based64 -d./broker-info.subm`

```bash
$ subctl  join broker-info.subm --clusterid  cluster0 --clustercidr 10.16.0.0/16  --natt=false --cable-driver vxlan --health-check=false
```

```bash
$ subctl  join broker-info.subm --clusterid  cluster1 --clustercidr 11.16.0.0/16  --natt=false --cable-driver vxlan --health-check=false
```

Now label the gateway node. 

```bash
$ kubectl label nodes cluster0 submariner.io/gateway=true
```

```bash
$ kubectl label nodes cluster1 submariner.io/gateway=true
```

After joining the broker, there are two ways to verify if the deployment is correct:

1. ping the IP pod of another cluster from local.

   ```bash
   $ # ping 10.16.0.7 -c2
   PING 10.16.0.7 (10.16.0.7) 56(84) bytes of data.
   64 bytes from 10.16.0.7: icmp_seq=1 ttl=62 time=1.78 ms
   64 bytes from 10.16.0.7: icmp_seq=2 ttl=62 time=0.728 ms
   
   --- 10.16.0.7 ping statistics ---
   2 packets transmitted, 2 received, 0% packet loss, time 1001ms
   rtt min/avg/max/mdev = 0.728/1.256/1.785/0.529 ms
   ```

   Then deploy the service, pods and export service as this [doc](https://submariner.io/operations/usage/) describe, then check if the domain is resolvable:

   ```bash
   $ kubectl   get pods -o wide -A | grep dns
   kube-system           coredns-64897985d-b4tqh                          1/1     Running   0             21h   10.16.0.5        cluster0   <none>           <none>
   kube-system           coredns-64897985d-qvdkr                          1/1     Running   0             21h   10.16.0.6        cluster0   <none>           <none>
   submariner-operator   submariner-lighthouse-coredns-7d477dbd6d-bz5m7   1/1     Running   0             21h   10.16.0.14       cluster0   <none>           <none>
   submariner-operator   submariner-lighthouse-coredns-7d477dbd6d-xc626   1/1     Running   0             21h   10.16.0.15       cluster0   <none>           <none>
   $ dig +short nginx.nginx-test.svc.clusterset.local @10.16.0.14
   11.96.112.197
   $ kubectl  -n submariner-k8s-broker get serviceimport
   NAME                        TYPE           IP                  AGE
   nginx-nginx-test-cluster1   ClusterSetIP   ["11.96.112.197"]   85m
   ```

   

2. just use diagnose of subctl:

   ```bash
   $ subctl show all
   $ subctl diagnose all
   ```

   No errors, no problems. 

