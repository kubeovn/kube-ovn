# OpenStack K8s Networking

Openstack and Kubernetes clusters can be connected. Pods in the different clusters can communicate directly using Pod IP. Kube-OVN uses Geneve to encapsulate traffic between Openstack and Kubernetes, only L3 connectivity for gateway nodes is required.

This document is based on Openstack *Victoria* and Kube-OVN *1.7*

# Prerequisite

- Openstack neutron should build networking  based on OVN.
- For auto route advertise, subnet CIDRs, including join subnets CIDRs, in different Openstack & Kubernetes cluster MUST NOT be overlapped. Otherwise, route CIRDs should be advertised manually.
- The Interconnection Controller SHOULD be deployed in a region that every cluster can access by IP.
- Every cluster *SHOULD* have at least one node(work as gateway later) that can access other gateway nodes in different clusters by IP.
- Cluster interconnection network now *CANNOT* work together with SNAT and EIP functions.

# One Openstack with one Kubernetes

#### 1. Run Interconnection Controller in a Kubernetes node which can be accessed by an Openstack gateway node.

```shell
$ docker run --name=ovn-ic-db -d --network=host -v /etc/ovn/:/etc/ovn -v /var/run/ovn:/var/run/ovn -v /var/log/ovn:/var/log/ovn kubeovn/kube-ovn:v1.7.3 bash start-ic-db.sh
```

#### 2. Create `ovn-ic-config` for kubernetes cluster in `kube-system` namespace.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ovn-ic-config
  namespace: kube-system
data:
  enable-ic: "true"
  az-name: "az1"                # AZ name for k8s cluster, every cluster should be different
  ic-db-host: "192.168.65.3"    # The Interconnection Controller host IP address
  ic-nb-port: "6645"            # The ic-nb port, default 6645
  ic-sb-port: "6646"            # The ic-sb port, default 6646
  gw-nodes: "az1-gw"            # The node name which acts as the interconnection gateway, get from 'kubectl get  node'
  auto-route: "true"            # Auto announces routes to all clusters. If set false, you can select announced routes later manually
```

#### 3. Create a router for Openstack cluster to connect with Kubernetes, or use an existing router. `router0` is created here as an example.

```shell
$ openstack router create router0
$ openstack router list
+--------------------------------------+---------+--------+-------+----------------------------------+
| ID                                   | Name    | Status | State | Project                          |
+--------------------------------------+---------+--------+-------+----------------------------------+
| d5b38655-249a-4192-8046-71aa4d2b4af1 | router0 | ACTIVE | UP    | 98a29ab7388347e7b5ff8bdd181ba4f9 |
+--------------------------------------+---------+--------+-------+----------------------------------+
```

#### 4. Establish ovn interconnection between Openstack and Kubernetes.

##### 	Set an availability zone name for Openstack at Openstack on central nodes:

```shell
$ ovn-nbctl set NB_Global . name=<availability zone name>
```

​	The name should be unique across all OVN deployments, e.g. ovn-opstk0, ovn-k8s0, etc.

##### 	Start the `ovn-ic` daemon on gateway nodes.

```shell
$ /usr/share/ovn/scripts/ovn-ctl --ovn-ic-nb-db=tcp:<ic-db-host>:6645 --ovn-ic-sb-db=tcp:<ic-db-host>:6646 --ovn-northd-nb-db=unix:/run/ovn/ovnnb_db.sock --ovn-northd-sb-db=unix:/run/ovn/ovnsb_db.sock start_ic
```

​	`<ic-db-host>` is configured in  `ovn-ic-config`  as `data:ic-db-host`.

##### 	Configure gateways on gateway nodes:

```shell
$ ovs-vsctl set open_vswitch . external_ids:ovn-is-interconn=true
```

##### 	Connect router0 to transit logical switch on central nodes:

​	A logical switch `ts` is already created in step 1, and `router0` is already created in step 3.

​	Thus first create a logical router port: 

```shell
$ ovn-nbctl lrp-add <router0> lrp-router0-ts 00:02:ef:11:39:4f 169.254.100.73/24
```

​    <*router0*> is router-id obtained by `ovn-nbctl show`.

​	(The mac and IP are examples.)

​	Create a logical switch port to peer with the router0.

```shell
$ ovn-nbctl lsp-add ts lsp-ts-router0 -- lsp-set-addresses lsp-ts-router0 router -- lsp-set-type lsp-ts-router0 router -- lsp-set-options lsp-ts-router0  router-port=lrp-router0-ts
```

#####  	Assign gateway(s) for the logical router port on central nodes.

```shell
$ ovn-nbctl lrp-set-gateway-chassis lrp-router0-ts <gateway name> [priority]
```

`<gateway name>` should be the chassis id of gateway node. Chassis id could be find with `ovn-sbctl show`

##### 	Enabling auto route advertise on central nodes.

```shell
$ ovn-nbctl set NB_Global . options:ic-route-adv=true options:ic-route-learn=true
```

#### 5. Verify

​	List routes in Openstack cluster.

```shell
$ ovn-nbctl lr-route-list router0
IPv4 Routes
                10.0.0.22            169.254.100.34 dst-ip (learned)
             10.16.0.0/16            169.254.100.34 dst-ip (learned)
```

​	Add a VM instance.

```shell
$ openstack network create provider
$ openstack subnet create --network provider --subnet-range 192.168.1.0/24 provider-v4
$ openstack server create --flavor 1 --image ubuntu18.04 --nic net-id=<network provider id>  --key-name mykey provider-instance
$ openstack router  add subnet <router0 id> <subnet provider-v4 id>
$ openstack console url show provider-instance
+-------+-------------------------------------------------------------------------------------------+
| Field | Value                                                                                     |
+-------+-------------------------------------------------------------------------------------------+
| type  | novnc                                                                                     |
| url   | http://controller:6080/vnc_auto.html?path=%3Ftoken%3D6ecba69c-c56c-4c49-85bc-9cb90880938b |
+-------+-------------------------------------------------------------------------------------------+
```

​	Enter the `url` in the browser to access the virtual machine via vnc, then `ping` POD-IP in Kubernetes cluster.

