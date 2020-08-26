# Multi-Cluster Networking

From v1.4.0, two or more Kubernetes clusters can be connected with each other. Pods in different clusters can
communicate directly using Pod IP. Kub-OVN uses tunnel to encapsulate traffic between clusters gateways, 
only L3 connectivity for gateway nodes is required.

## Prerequest
* Subnet CIDRs in different clusters *MUST NOT* be overlapped with each other，including ovn-default and join subnets CIDRs.
* The Interconnection Controller *Should* be deployed in a region that every cluster can access by IP.
* Every cluster *Should* have at least one node(work as gateway later) that can access other gateway nodes in different clusters by IP.

## Step
1. Run Interconnection Controller in a region that can be accessed by other cluster
```bash
docker run --name=ovn-ic-db -d --network=host -v /etc/ovn/:/etc/ovn -v /var/run/ovn:/var/run/ovn -v /var/log/ovn:/var/log/ovn kubeovn/kube-ovn:v1.4.0 bash start-ic-db.sh
```
2. Create `ic-config` ConfigMap in each cluster. Edit and apply the yaml below in each cluster.
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

## Gateway High Available
Kube-OVN now supports Active-Backup mode gateway HA. You can add more nodes name in the configmap separated by commas.

Active-Active mode gateway HA is under development.
