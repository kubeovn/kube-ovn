# Kube-OVN-helm

Currently supported version: 1.9

Installation :

```bash
$ kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
$ kubectl label no -lnode-role.kubernetes.io/control-plane  kube-ovn/role=master --overwrite
$ kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel  --overwrite

# standard install 
$ helm install --debug kubeovn ./kubeovn-helm --set MASTER_NODES=${Node0},

# high availability install
$ helm install --debug kubeovn ./kubeovn-helm --set MASTER_NODES=${Node0},${Node1},${Node2}, --set replicaCount=3

# upgrade to this version
$ helm upgrade --debug kubeovn ./kubeovn-helm --set MASTER_NODES=${Node0},${Node1},${Node2}, --set replicaCount=3
```

If you are upgrading Kube-OVN from versions prior to v1.12, you need to set `restart_ovs` to `true`:

```shell
$ helm upgrade --debug kubeovn ./kubeovn-helm --set MASTER_NODES=${Node0},${Node1},${Node2}, --set replicaCount=3 --set restart_ovs=true
```
