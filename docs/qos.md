# QoS

Kube-OVN supports dynamically configurations of Ingress and Egress traffic rate limiting for a single pod level and gateway level.

## Pod QoS
Use the following annotations to specify QoS:
- `ovn.kubernetes.io/ingress_rate`: Rate limit for Ingress traffic, unit: Mbit/s
- `ovn.kubernetes.io/egress_rate`: Rate limit for Egress traffic, unit: Mbit/s

Example:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: qos
  namespace: ls1
  annotations:
    ovn.kubernetes.io/ingress_rate: "3"
    ovn.kubernetes.io/egress_rate: "1"
spec:
  containers:
  - name: qos
    image: nginx:alpine
```

## Gateway QoS

Kube-OVN will create an `ovn0` interface on each host to route traffic from cluster pod network
to external network. Kube-OVN control gateway QoS by modify the QoS config of `ovn0` interface.

For a subnet with central gateway mode, only one node act as the gateway, so you can modify the
node QoS annotation to control the QoS of the subnet to external network.

```yaml
apiVersion: v1
kind: Node
metadata:
  annotations:
    ovn.kubernetes.io/ingress_rate: "3"
    ovn.kubernetes.io/egress_rate: "1"
  name: liumengxin-ovn1-192.168.16.44
```

You can also use this annotation to control the traffic from each node to external network
through these annotations.
