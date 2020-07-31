# Pod Gateway

Kube-OVN support assign a specific pod as another pod's gateway. 
All traffic from pod to external cluster will be redirected to the gateway pod.
Thus, user can customize traffic policies in this pod like accounting, eip, qos etc.

## Usage
Use the following annotation in pod spec:
- `ovn.kubernetes.io/north_gateway`: The IP address of gateway pod

Example:

```bash
apiVersion: v1
kind: Pod
metadata:
  name: pod-gw
  annotations:
    ovn.kubernetes.io/north_gateway: 10.16.0.100
spec:
  containers:
  - name: pod-gw
    image: nginx:alpine
```
