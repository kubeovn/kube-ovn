Helm chart for Kube-OVN

由于 Kube-OVN 的安装，需要设置一些参数，因此使用 Helm 安装 Kube-OVN，需要按照以下步骤执行

1、查看集群节点信息
```bash
$ kubectl get node -o wide
NAME                     STATUS   ROLES           AGE     VERSION   INTERNAL-IP   EXTERNAL-IP   OS-IMAGE             KERNEL-VERSION      CONTAINER-RUNTIME
kube-ovn-control-plane   Ready    control-plane   2m35s   v1.26.0   172.18.0.3    <none>        Ubuntu 22.04.1 LTS   5.10.104-linuxkit   containerd://1.6.9
kube-ovn-worker          Ready    <none>          2m14s   v1.26.0   172.18.0.2    <none>        Ubuntu 22.04.1 LTS   5.10.104-linuxkit   containerd://1.6.9
```

2、去掉集群 master 节点污点。如果确定不需要在 master 节点调度业务 Pod，这一步可以跳过
```bash
$ kubectl taint node kube-ovn-control-plane node-role.kubernetes.io/control-plane:NoSchedule-
```

3、给节点添加 label
```bash
$ kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
$ kubectl label no -lnode-role.kubernetes.io/control-plane  kube-ovn/role=master --overwrite
# 以下 label 用于 dpdk 镜像的安装，非 dpdk 情况，可以忽略
$ kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel  --overwrite
```

4、本地添加 Kube-OVN repo 信息
```bash
% helm repo add kubeovn https://kubeovn.github.io/kube-ovn
"kubeovn" has been added to your repositories

% helm repo list
NAME         	URL
kubeovn      	https://kubeovn.github.io/kube-ovn

% helm search repo kubeovn
NAME            	CHART VERSION	APP VERSION	DESCRIPTION
kubeovn/kube-ovn	0.1.0        	1.12.0     	Helm chart for Kube-OVN
```

5、执行 helm install 安装 Kube-OVN，其中 Node0IP、Node1IP、Node2IP 分别为集群 master 节点的 IP 地址
```bash
# 单 master 节点环境
$ helm install kube-ovn kubeovn/kube-ovn --set MASTER_NODES=${Node0IP}

# 以上边的 node 信息为例，执行安装命令
% helm install kube-ovn kubeovn/kube-ovn --set MASTER_NODES=172.18.0.3
NAME: kube-ovn
LAST DEPLOYED: Fri Mar 31 12:43:43 2023
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None

# 高可用集群安装
$ helm install kube-ovn kubeovn/kube-ovn --set MASTER_NODES=${Node0IP},${Node1IP},${Node2IP}, --set replicaCount=3
```
