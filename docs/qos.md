# QoS

Kube-OVN supports dynamically change ingress and egress traffic rate control by modifying annotation to pod spec.

Use following keys to define qos:
- `ovn.kubernetes.io/ingress_rate`: traffic rate that inbound pod, unit Mbit/s
- `ovn.kubernetes.io/egress_rate`: traffic rate that outbound pod, unit Mbit/s

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
