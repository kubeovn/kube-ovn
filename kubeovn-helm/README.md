# Kube-OVN-helm

Currently supported version: master

Installation :

```bash
$ kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
$ kubectl label no -lnode-role.kubernetes.io/master  kube-ovn/role=master --overwrite
$ kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel  --overwrite
$ helm install --debug kubeovn ./KubeOVN-helm --set cni_conf.MASTER_NODES=${Node0},${Node1},${Node2},  --set nodes=${Node_number}
```
