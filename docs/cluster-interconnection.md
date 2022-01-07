# Multi-Cluster Networking

From v1.4.0, two or more Kubernetes clusters can be connected with each other. Pods in different clusters can
communicate directly using Pod IP. Kub-OVN uses tunnel to encapsulate traffic between clusters gateways, 
only L3 connectivity for gateway nodes is required.

## Prerequisite
* To use route auto advertise, subnet CIDRs in different clusters *MUST NOT* be overlapped with each other，including ovn-default and join subnets CIDRs. Otherwise, you should disable the auto route and add routes manually.
* The Interconnection Controller *SHOULD* be deployed in a region that every cluster can access by IP.
* Every cluster *SHOULD* have at least one node(work as gateway later) that can access other gateway nodes in different clusters by IP.
* Cluster interconnection network now *CANNOT* work together with SNAT and EIP functions.

## Auto Route Step
1. Run Interconnection Controller in a region that can be accessed by other cluster
```bash
docker run --name=ovn-ic-db -d --network=host -v /etc/ovn/:/etc/ovn -v /var/run/ovn:/var/run/ovn -v /var/log/ovn:/var/log/ovn kubeovn/kube-ovn:v1.9.0 bash start-ic-db.sh
```
​		If `containerd` replaces `docker` then the command is as follows:

```shell
ctr -n k8s.io run -d --net-host --mount="type=bind,src=/etc/ovn/,dst=/etc/ovn,options=rbind:rw" --mount="type=bind,src=/var/run/ovn,dst=/var/run/ovn,options=rbind:rw" --mount="type=bind,src=/var/log/ovn,dst=/var/log/ovn,options=rbind:rw" docker.io/kubeovn/kube-ovn:v1.9.0 ovn-ic-db bash start-ic-db.sh
```

2. Create `ovn-ic-config` ConfigMap in each cluster `kube-system` namespace. Edit and apply the yaml below in each cluster.
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ovn-ic-config
  namespace: kube-system
data:
  enable-ic: "true"
  az-name: "az1"                # AZ name for cluster, every cluster should be different
  ic-db-host: "192.168.65.3"    # The Interconnection Controller host IP address
  ic-nb-port: "6645"            # The ic-nb port, default 6645
  ic-sb-port: "6646"            # The ic-sb port, default 6646
  gw-nodes: "az1-gw"            # The node name which acts as the interconnection gateway
  auto-route: "true"            # Auto announce route to all clusters. If set false, you can select announced routes later manually
```

3. Check if interconnection is established.

```bash
## In Interconnection Controller container
[root@ic]# ovn-ic-sbctl show
availability-zone az1
    gateway deee03e0-af16-4f45-91e9-b50c3960f809
        hostname: az1-gw
        type: geneve
            ip: 192.168.42.145
        port ts-az1
            transit switch: ts
            address: ["00:00:00:50:AC:8C 169.254.100.45/24"]
availability-zone az2
    gateway e94cc831-8143-40e3-a478-90352773327b
        hostname: az2-gw
        type: geneve
            ip: 192.168.42.149
        port ts-az2
            transit switch: ts
            address: ["00:00:00:07:4A:59 169.254.100.63/24"]

## In each cluster
[root@az1 ~]# kubectl ko nbctl lr-route-list ovn-cluster
IPv4 Routes
                10.42.1.1            169.254.100.45 dst-ip (learned)
                10.42.1.3                100.64.0.2 dst-ip
                10.16.0.2                100.64.0.2 src-ip
                10.16.0.3                100.64.0.2 src-ip
                10.16.0.4                100.64.0.2 src-ip
                10.16.0.6                100.64.0.2 src-ip
             10.17.0.0/16            169.254.100.45 dst-ip (learned)
            100.65.0.0/16            169.254.100.45 dst-ip (learned)
```

If Pods cannot communicate with each other, please check the log of kube-ovn-controller.

For some specific subnet that you don't want to advertise to another cluster, you can disable the auto route advertise on the specific subnet by editing the subnet spec.
```
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: no-advertise
spec:
  cidrBlock: 10.199.0.0/16
  disableInterConnection: false
```

For manually adding routes, you need to find the 

## Manually Route Step
1. Same as AutoRoute step 1,run Interconnection Controller in a region that can be accessed by other cluster
```bash
docker run --name=ovn-ic-db -d --network=host -v /etc/ovn/:/etc/ovn -v /var/run/ovn:/var/run/ovn -v /var/log/ovn:/var/log/ovn kubeovn/kube-ovn:v8.0 bash start-ic-db.sh
```
2. Create `ic-config` ConfigMap in each cluster. Edit and apply the yaml below in each cluster. Note that `auto-route` is set to `false`
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ovn-ic-config
  namespace: kube-system
data:
  enable-ic: "true"
  az-name: "az1"                # AZ name for cluster, every cluster should be different
  ic-db-host: "192.168.65.3"    # The Interconnection Controller host IP address
  ic-nb-port: "6645"            # The ic-nb port, default 6645
  ic-sb-port: "6646"            # The ic-sb port, default 6646
  gw-nodes: "az1-gw"            # The node name which acts as the interconnection gateway
  auto-route: "false"           # Auto announce route to all clusters. If set false, you can select announced routes later manually
```

3. Find the remote gateway address in each cluster, and add routes to remote cluster.

In az1
```bash
[root@az1 ~]# kubectl ko nbctl show
switch a391d3a1-14a0-4841-9836-4bd930c447fb (ts)
    port ts-az1
        type: router
        router-port: az1-ts
    port ts-az2
        type: remote
        addresses: ["00:00:00:4B:E2:9F 169.254.100.31/24"]
```
In az2
```bash
[root@az2 ~]# kubectl ko nbctl show
switch da6138b8-de81-4908-abf9-b2224ec4edf3 (ts)
    port ts-az2
        type: router
        router-port: az2-ts
    port ts-az1
        type: remote
        addresses: ["00:00:00:FB:2A:F7 169.254.100.79/24"]
```
Record the remote lsp address in `ts` logical switch

4. Add Static route in each cluster

In az1
```bash
kubectl ko nbctl lr-route-add ovn-cluster 10.17.0.0/24 169.254.100.31
```
In az2
```bash
 kubectl ko nbctl lr-route-add ovn-cluster 10.16.0.0/24 169.254.100.79
```



## Interconnection Controller High Available

1. Run the leader Interconnection Controller in a region that can be accessed by other cluster.  

   `LEADERIP` is the IP of node where leader controller is deployed.

   `NODE_IPS` is the IP of all nodes in the interconnection Controller cluster, in the format `IP1,IP2,...,IPn`.

   `LOCALIP` is the IP of the node on which the container will run. For the leader, `LOCALIP` is its own IP.

```bash
docker run --name=ovn-ic-db -d --network=host -v /etc/ovn/:/etc/ovn -v /var/run/ovn:/var/run/ovn -v /var/log/ovn:/var/log/ovn -e LOCAL_IP="LEADERIP"  -e NODE_IPS="IP1,IP2,IP3"   kubeovn/kube-ovn:v1.9.0 bash start-ic-db.sh
```

​        If `containerd` replaces `docker` then the command is as follows:

```shell
ctr -n k8s.io run -d --net-host --mount="type=bind,src=/etc/ovn/,dst=/etc/ovn,options=rbind:rw" --mount="type=bind,src=/var/run/ovn,dst=/var/run/ovn,options=rbind:rw" --mount="type=bind,src=/var/log/ovn,dst=/var/log/ovn,options=rbind:rw"  --env="NODE_IPS="IP1,IP2,IP3"" --env="LOCAL_IP="LEADERIP"" docker.io/kubeovn/kube-ovn:v1.9.0 ovn-ic-db bash start-ic-db.sh
```

2. Run the follower Interconnection Controller in the same region. 

```bash
docker run --name=ovn-ic-db -d --network=host -v /etc/ovn/:/etc/ovn -v /var/run/ovn:/var/run/ovn -v /var/log/ovn:/var/log/ovn -e LOCAL_IP="LOCALIP"  -e NODE_IPS="IP1,IP2,IP3" -e LEADER_IP="LEADERIP"  kubeovn/kube-ovn:v1.9.0 bash start-ic-db.sh
```

​		If `containerd` replaces `docker` then the command is as follows:

```shell
ctr -n k8s.io run -d --net-host --mount="type=bind,src=/etc/ovn/,dst=/etc/ovn,options=rbind:rw" --mount="type=bind,src=/var/run/ovn,dst=/var/run/ovn,options=rbind:rw" --mount="type=bind,src=/var/log/ovn,dst=/var/log/ovn,options=rbind:rw" --env="NODE_IPS="IP1,IP2,IP3"" --env="LOCAL_IP="LEADERIP"" --env="NODE_IPS="IP1,IP2,IP3"" docker.io/kubeovn/kube-ovn:v1.9.0 ovn-ic-db bash start-ic-db.sh
```

3.  Create `ic-config` ConfigMap in each cluster as follows.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ovn-ic-config
  namespace: kube-system
data:
  enable-ic: "true"
  az-name: "az1"                												# AZ name for cluster, every cluster should be different
  ic-db-host: "192.168.65.3,192.168.65.2,192.168.65.1"  # The Interconnection Controller host IP addresses
  ic-nb-port: "6645"           													# The ic-nb port, default 6645
  ic-sb-port: "6646"            												# The ic-sb port, default 6646
  gw-nodes: "az1-gw"            												# The node name which acts as the interconnection gateway
  auto-route: "false"           # Auto announce route to all clusters. If set false, you can select announced routes later manually
```



## Gateway High Available

Kube-OVN now supports Active-Backup mode gateway HA. You can add more nodes name in the configmap separated by commas.

Active-Active mode gateway HA is under development.
