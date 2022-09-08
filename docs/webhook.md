# Webhook

From Kube-OVN v1.9.0, webhook is added back. The most important thing for webhook is ip address conflict and subnet cidr validating.

## Pre-request

- Kube-OVN without webhook
- Cert-Manager

## Cert-Manager installation

The webhook needs https, so we use cert-manager here to generate the certificate. Normally cert-manager doesn't use `hostNetwork`, so it needs CNI to allocate IP addresses. As a result, we should install Kube-OVN, cert-manager before webhook.

You can use the command downside to install Cert-Manager

`kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.6.1/cert-manager.yaml`

And the help document refers to [cert-manager](https://cert-manager.io/docs/installation/).

## Webhook installation
The webhook has not been added to the `install.sh` script. So it should be installed manually with the command `kubectl apply -f yamls/webhook.yaml`.

After installation, you can find a pod in kube-system the same namespace as other pods.

```
apple@bogon kube-ovn % kubectl get pod -n kube-system
NAME                                             READY   STATUS    RESTARTS       AGE
coredns-78fcd69978-k576h                         1/1     Running   0              2d23h
coredns-78fcd69978-m76xs                         1/1     Running   0              2d23h
etcd-kube-ovn-control-plane                      1/1     Running   0              2d23h
kube-apiserver-kube-ovn-control-plane            1/1     Running   0              2d23h
kube-controller-manager-kube-ovn-control-plane   1/1     Running   0              2d23h
kube-ovn-cni-kkgz4                               1/1     Running   0              2d21h
kube-ovn-cni-q4nf2                               1/1     Running   0              2d21h
kube-ovn-controller-7bd57d84d8-z94ck             1/1     Running   1 (2d3h ago)   2d21h
kube-ovn-webhook-5bfccc66d-b8tzh                 1/1     Running   0              30m
.
.
.
apple@bogon kube-ovn %
```

## Test
You can create a pod with static ip address `10.16.0.15`.
```
apple@bogon ovn-test % kubectl get pod -o wide
NAME                      READY   STATUS    RESTARTS   AGE     IP           NODE              NOMINATED NODE   READINESS GATES
static-7584848b74-fw9dm   1/1     Running   0          2d13h   10.16.0.15   kube-ovn-worker   <none>           <none>
apple@bogon ovn-test %
```

And use the yaml downside to create another pod with same static ip address.
```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/ip_address: 10.16.0.15
    ovn.kubernetes.io/mac_address: 00:00:00:53:6B:B6
  labels:
    app: static
  managedFields:
  name: staticip-pod
  namespace: default
spec:
  containers:
  - image: qaimages:helloworld
    imagePullPolicy: IfNotPresent
    name: qatest
```

As a result, this operation is denied by the webhook.
```
apple@bogon ovn-test % kubectl apply -f pod-static.yaml
Error from server (annotation ip address 10.16.0.15 is conflict with ip crd static-7584848b74-fw9dm.default 10.16.0.15): error when creating "pod-static.yaml": admission webhook "pod-ip-validating.kube-ovn.io" denied the request: annotation ip address 10.16.0.15 is conflict with ip crd static-7584848b74-fw9dm.default 10.16.0.15
apple@bogon ovn-test %
```
