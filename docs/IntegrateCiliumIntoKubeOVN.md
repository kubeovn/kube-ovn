## Integrate Cilium into Kube-OVN(experimental)

![](./kube-ovn-cilium.svg) 

This document introduces the reasons and steps for integrating Cilium into Kube-OVN

### Target

We would like to integrate Cilium into Kube-OVN for the following two reasons: 

1. For Service IP that needs DNAT, Cilium is more efficient than Open_vSwitch.
2. Cilium has more advanced L4/L7 policy and metrics that can enhance network overall operational ability.

[Cilium](https://cilium.io) is an ebpf-based networking and security system.

### Prerequisite:

1. Linux system with kernel > 4.19. 

   In our experiments, Ubuntu 20.04 with kernel 5.4.0 has been chosen, because other advanced features need to be tested as well. 

   ```shell
   root@cilium-small-x86-01:~# cat /proc/version
   Linux version 5.4.0-88-generic (buildd@lgw01-amd64-008) (gcc version 9.3.0 (Ubuntu 9.3.0-17ubuntu1~20.04)) #99-Ubuntu SMP Thu Sep 23 17:29:00 UTC 2021
   ```
2. Helm is required to install Cilium, please refer [Helm Install](https://helm.sh/docs/intro/install/)

### Integration:

The integration solution is based on Cilium's CNI [Chaining mode for Calico](https://docs.cilium.io/en/stable/gettingstarted/cni-chaining-calico/).

##### 1.  Deploy Kubernetes and Kube-OVN based on the documentation

For [Kubernetes](https://kubernetes.io/docs/setup/production-environment/tools/) and [Kube-OVN](https://github.com/kubeovn/kube-ovn/blob/master/docs/install.md)

Before installing Kube-OVN, disable Kube-OVN feature `loadbalancer` and `networkpolicy ` in `install.sh` as following.

```bash
ENABLE_LB=${ENABLE_LB:-false}
ENABLE_NP=${ENABLE_NP:-false}
```

#####  2.  Create a `chaining.yaml` ConfigMap based on the following template.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cni-configuration
  namespace: kube-system
data:
  cni-config: |-
    {
      "name": "generic-veth",
      "cniVersion": "0.3.1",
      "plugins": [
        {
          "type": "kube-ovn",
          "log_level": "info",
          "datastore_type": "kubernetes",
          "mtu": 1400,
          "server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
          "ipam": {
              "type": "kube-ovn",
              "server_socket": "/run/openvswitch/kube-ovn-daemon.sock"
          }
        },
        {
          "type": "portmap",
          "snat": true,
          "capabilities": {"portMappings": true}
        },
        {
          "type": "cilium-cni"
        }
      ]
    }

```

Deploy this configmap

```bash
kubectl apply -f ./chaining.yaml
```

##### 3.  Deploy Cilium

```bash
helm repo add cilium https://helm.cilium.io/
helm install cilium cilium/cilium --version 1.10.5 \
  --namespace=kube-system \
  --set cni.chainingMode=generic-veth \
  --set cni.customConf=true \
  --set cni.configMap=cni-configuration \
  --set tunnel=disabled \
  --set enableIPv4Masquerade=false \
  --set enableIdentityMark=false
```

Cilium install could be validated based on this [document](https://docs.cilium.io/en/stable/gettingstarted/cni-chaining-calico/).

##### 4.  Change the name of the CNI configuration file on every Kubernetes node.

For ***every node*** of cluster, rename the Kube-OVN configuration file.

```bash
mv /etc/cni/net.d/01-kube-ovn.conflist /etc/cni/net.d/10-kube-ovn.conflist
```

Note: By default the configuration file of Cilium is `05-cilium.conflist`. If there are other CNI configuration files, make sure that they start with a higher number than the Cilium config file.

##### 5.  Validation

Now the Cilium is installed. 

The `cilium` CLI could be used to validate the installation. It can be installed from [here](https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli)

```bash
root@cilium-small-x86-01:~# cilium  status
    /¯¯\
 /¯¯\__/¯¯\    Cilium:         OK
 \__/¯¯\__/    Operator:       OK
 /¯¯\__/¯¯\    Hubble:         disabled
 \__/¯¯\__/    ClusterMesh:    disabled
    \__/

DaemonSet         cilium             Desired: 2, Ready: 2/2, Available: 2/2
Deployment        cilium-operator    Desired: 2, Ready: 2/2, Available: 2/2
Containers:       cilium             Running: 2
                  cilium-operator    Running: 2
Cluster Pods:     8/11 managed by Cilium
Image versions    cilium             quay.io/cilium/cilium:v1.10.5@sha256:0612218e28288db360c63677c09fafa2d17edda4f13867bcabf87056046b33bb: 2
                  cilium-operator    quay.io/cilium/operator-generic:v1.10.5@sha256:2d2f730f219d489ff0702923bf24c0002cd93eb4b47ba344375566202f56d972: 2

```

 Or you can restart pods and see if the `ciliumendpoint` has been correctly installed.

```bash
root@cilium-small-x86-01:~# kubectl get cep -A
NAMESPACE     NAME                              ENDPOINT ID   IDENTITY ID   INGRESS ENFORCEMENT   EGRESS ENFORCEMENT   VISIBILITY POLICY   ENDPOINT STATE   IPV4          IPV6
default       details-v1-79f774bdb9-7jwbb       4024          16773                                                                        ready            10.16.5.64
default       productpage-v1-6b746f74dc-wwg5j   776           28588                                                                        ready            10.16.0.128
default       ratings-v1-b6994bb9-s6dxz         2019          23365                                                                        ready            10.16.5.63
default       reviews-v1-545db77b95-jhpxl       2977          39913                                                                        ready            10.16.5.65
default       reviews-v2-7bf8c9648f-c6j6s       1973          13960                                                                        ready            10.16.5.66
default       reviews-v3-84779c7bbc-ph2r7       1875          5906                                                                         ready            10.16.5.67
kube-system   coredns-78fcd69978-w92qp          1216          6428                                                                         ready            10.16.5.60
kube-system   coredns-78fcd69978-whxbf          1230          6428                                                                         ready            10.16.5.62
```

### Replace Kube-proxy

This section introduces how to transfer DNAT from `kube-proxy` to `Cilium` as Cilium [described](https://docs.cilium.io/en/v1.9/gettingstarted/kubeproxy-free/).

##### 1.  First delete components of `kube-proxy`

```bash
kubectl -n kube-system delete ds kube-proxy
kubectl -n kube-system delete cm kube-proxy
iptables-save | grep -v KUBE | iptables-restore
```

##### 2.  Verify the availability of services

```bash
root@cilium-small-x86-01:~# kubectl get svc
NAME          TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)    AGE
productpage   ClusterIP   10.110.121.109   <none>        9080/TCP   3d19h
...
root@cilium-small-x86-01:~# curl 10.110.121.109:9080

```

##### 3.  Enabling Cilium replacement

```bash
helm upgrade cilium cilium/cilium --version 1.10.5 \
    --namespace kube-system \
    --set kubeProxyReplacement=strict \
    --set k8sServiceHost=REPLACE_WITH_API_SERVER_IP \
    --set k8sServicePort=REPLACE_WITH_API_SERVER_PORT
```

Replace`REPLACE_WITH_API_SERVER_IP` and `REPLACE_WITH_API_SERVER_PORT` below with the concrete control-plane node IP address and the kube-apiserver port number reported by `kubeadm init` (usually, it is port `6443`).

##### 4.  Verify the availability of services again

```bash
root@cilium-small-x86-01:~# curl 10.110.121.109:9080
<!DOCTYPE html>
<html>
  <head>
    <title>Simple Bookstore App</title>
<meta charset="utf-8">
<meta http-equiv="X-UA-Compatible" content="IE=edge">
<meta name="viewport" content="width=device-width, initial-scale=1">
...
```

Cilium works!
