# SNAT and EIP

From v1.5.0, Kube-OVN take use of the L3 gateways from OVN to implement Pod SNAT and EIP functions.
By using snat, a group of pods can share one same ip address to communicate with external services.
By using eip, external services can visit a pod with a stable ip and pod will visit external services using the same ip.

## Prerequisite
* To take use of OVN L3 Gateway, a dedicated nic *MUST* be bridged into ovs to act as the gateway between overlay and underlay, ops should use other NICs to manage the host server.
* As the nic will emit packets with nat ip directly into underlay network, administrators *MUST* make sure that these packets will not be denied by security rules.
* SNAT and EIP functions *CANNOT* work together with Cluster interconnection network

## Steps

1. Create `ovn-external-gw-config` ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ovn-external-gw-config
  namespace: kube-system
data:
  enable-external-gw: "true"
  type: "centralized"                   # centralized or distributed, when centralized external-gw-nodes config below will take effect. When distributed, every node in cluster must have a same nic and eip function will perform in distributed way.
  external-gw-nodes: "kube-ovn-worker"  # NodeName in kubernetes which will act the overlay to underlay gateway functions
  external-gw-nic: "eth1"               # The nic that will be bridged into ovs and act as overlay to underlay gateway
  external-gw-addr: "172.56.0.1/16"     # The ip and mask of the underlay physical gateway
  nic-ip: "172.56.0.100/16"             # The ip and mask of the underlay physical network for logical route external gw port
  nic-mac: "16:52:f3:13:6a:25"          # The mac of nic-ip
```

2. Wait about one minute for gateway installation get ready and check the status.

Check OVN-NB status, make sure ovn-external logical switch exists and ovn-cluster-ovn-external logical router port with correct address and gateway chassis

```shell
[root@kube-ovn ~]# kubectl ko nbctl show
switch 3de4cea7-1a71-43f3-8b62-435a57ef16a6 (ovn-external)
    port ln-ovn-external
        type: localnet
        addresses: ["unknown"]
    port ovn-external-ovn-cluster
        type: router
        router-port: ovn-cluster-ovn-external
router e1eb83ad-34be-4ed5-9a02-fcc8b1d357c4 (ovn-cluster)
    port ovn-cluster-ovn-external
        mac: "16:52:f3:13:6a:25"
        networks: ["172.56.0.254/16"]
        gateway chassis: [a5682814-2e2c-46dd-9c1c-6803ef0dab66]
```

Check OVS status， make sure the dedicated nic is bridged into OVS

```shell
[root@nrt1-x1 ~]# kubectl ko vsctl ${gateway node name} show
e7d81150-7743-4d6e-9e6f-5c688232e130
    Bridge br-external
        Port br-external
            Interface br-external
                type: internal
        Port eno2
            Interface eno2
        Port patch-ln-ovn-external-to-br-int
            Interface patch-ln-ovn-external-to-br-int
                type: patch
                options: {peer=patch-br-int-to-ln-ovn-external}
```

3. Annotate Pod with except snat and eip address

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-gw
  annotations:
    ovn.kubernetes.io/snat: 172.56.0.200
spec:
  containers:
  - name: snat-pod
    image: nginx:alpine
---
apiVersion: v1
kind: Pod
metadata:
  name: pod-gw
  annotations:
    ovn.kubernetes.io/eip: 172.56.0.233
spec:
  containers:
  - name: eip-pod
    image: nginx:alpine
```

4. Change eip or snat ip

```bash
# ovn.kubernetes.io/routed annotation need to be removed to trigger control plan update
kubectl annotate pod pod-gw ovn.kubernetes.io/eip=172.56.0.221 --overwrite
kubectl annotate pod pod-gw ovn.kubernetes.io/routed-
```

## Limitations
* No IP conflict detection for now, users should control the nat address allocation by themselves.
