# Installation


Kube-OVN includes two parts:
- Native OVS and OVN
- Controller and CNI plugins that combine OVN and Kubernetes

## Steps

1. Add label to node which will deploy the ovn db and ovn central control plan

    `kubectl label node <node to deploy ovn db> kube-ovn/role=master`
2. Install native OVS and OVN

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/master/yamls/ovn.yaml`
3. Install Controller and CNI plugins

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/master/yamls/kube-ovn.yaml`

That's all! You can try to create pod and test connectivity.

## More Configuration

### Controller Configuration

```bash
--default-cidr: Default cidr for namespace with no logical switch annotation, default: 10.16.0.0/16
--node-switch-cidr: The cidr for node switch. Default: 100.64.0.0/16
```