# BGP support

Kube-OVN supports advertise pod/subnet ips to external networks by BGP protocol.
To enable BGP advertise function, you need to install kube-ovn-speaker and annotate pods/subnets that need to be exposed.


1. Label nodes that host the BGP speaker and act as overlay to underlay gateway
```bash
kubectl label nodes speaker-node-1 ovn.kubernetes.io/bgp=true
kubectl label nodes speaker-node-2 ovn.kubernetes.io/bgp=true
```

## Install kube-ovn-speaker

2. Download `kube-ovn-speaker` yaml

```bash
wget https://github.com/kubeovn/kube-ovn/blob/master/yamls/speaker.yaml
```

3. Modify the args in yaml

```bash
--neighbor-address=10.32.32.1  # The router address that need to establish bgp peers
--neighbor-as=65030            # The AS of router
--cluster-as=65000             # The AS of container network
```

4. Apply the yaml

```bash
kubectl apply -f speaker.yaml
```


*NOTE*: When more than one node host speaker, the upstream router need to support multiple path routes to act ECMP.

## Annotate pods/subnet that need to be exposed

The subnet of pods and subnets need to be advertised should set `natOutgoing` to `false`

```bash
# Enable BGP advertise
kubectl annotate pod sample ovn.kubernetes.io/bgp=true
kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp=true

# Disable BGP advertise
kubectl annotate pod perf-ovn-xzvd4 ovn.kubernetes.io/bgp-
kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp-
```
