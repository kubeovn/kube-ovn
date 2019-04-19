# QoS

Kube-OVN supports dynamically configurations of Ingress and Egress traffic rate limiting.

Use the following annotations to specify QoS:
- `ovn.kubernetes.io/ingress_rate`: Rate limit for Ingress traffic, unit: Mbit/s
- `ovn.kubernetes.io/egress_rate`: Rate limit for Egress traffic, unit: Mbit/s

Example:

```bash
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
