# Kube-OVN-helm

Currently supported version: 1.9

Installation :

```bash
$ kubectl label node -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
$ kubectl label node -lnode-role.kubernetes.io/control-plane  kube-ovn/role=master --overwrite
$ kubectl label node -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel  --overwrite

# standard install 
$ helm install --debug kubeovn ./charts --set MASTER_NODES=${Node0},

# high availability install
$ helm install --debug kubeovn ./charts --set MASTER_NODES=${Node0},${Node1},${Node2}, --set replicaCount=3

# upgrade to this version
$ helm upgrade --debug kubeovn ./charts --set MASTER_NODES=${Node0},${Node1},${Node2}, --set replicaCount=3
```
