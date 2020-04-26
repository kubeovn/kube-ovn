# BGP support

Kube-OVN supports broadcast pod ips to external networks by BGP protocol. 
To enable BGP announce function, you need to install kube-ovn-speaker and annotate pods that need to be exposed.

## Install kube-ovn-speaker

1. Download `kube-ovn-speaker` yaml

```bash
wget https://github.com/alauda/kube-ovn/blob/master/yamls/speaker.yaml
```

2. Modify the args in yaml

```bash
--neighbor-address=10.32.32.1  # The router address that need to establish bgp peers
--neighbor-as=65030            # The AS of router 
--cluster-as=65000             # The AS of container network
```

3. Apply the yaml

```bash
kubectl apply -f speaker.yaml
```

## Annotate pods that need to be exposed

```bash
# Enable BGP
kubectl annotate pod sample ovn.kubernetes.io/bgp=true

# Disable BGP
kubectl annotate pod perf-ovn-xzvd4 ovn.kubernetes.io/bgp-
```
