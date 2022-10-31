# Kube-OVN-helm

Currently supported version: 1.9

Installation :

```bash
$ kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
$ kubectl label no -lnode-role.kubernetes.io/control-plane  kube-ovn/role=master --overwrite
$ kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel  --overwrite
$ helm install --debug kubeovn ./kubeovn-helm --set MASTER_NODES=${Node0},${Node1},${Node2},
```
