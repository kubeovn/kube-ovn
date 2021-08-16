# Traffic Mirror

Kube-OVN support traffic mirroring that duplicates pod nic send/receive network packets to a dedicated host nic. Administrator or developer can then use it to monitor network traffic or diagnose network problems.

![alt text](mirror.png "kube-ovn mirror")

## Enable Global Level Traffic Mirror

Traffic mirror is disabled by default, you should add cmd args in cni-server when installing kube-ovn to enabled it.
- `--enable-mirror=true`: enable traffic mirror
- `--mirror-iface=mirror0`: kube-ovn will create an ovs internal port on every node to mirror pods traffic on that node. If not set will use mirror0 as the default port name

Then you can use tcpdump or other tools to diagnose traffic from interface mirror0:

```bash
tcpdump -ni mirror0
```

## Enable Pod Level Traffic Mirror
*Supported from v1.8.0*

In Global Mirror mode, all Pod traffic will be mirrored. If you just want to mirror specific Pod traffic, you should disable the global mirror option and use the Pod level annotation to mirror specific traffic

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mirror-pod
  namespace: ls1
  annotations:
    ovn.kubernetes.io/mirror: "true"
spec:
  containers:
  - name: mirror-pod
    image: nginx:alpine
```
