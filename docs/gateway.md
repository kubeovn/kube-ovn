# Outgoing traffic

A gateway is been used to offer public network access fro ovn networked pods.
kube-ovn offers two kinds of the gateway, distributed gateway, and centralized gateway.

For a distributed gateway, outgoing traffic from ovn networked containers to destinations outside of ovn cidr or cluster ip range will be masqueraded with pod's host.

For a centralized gateway, outgoing traffic from ovn networked containers to destinations outside of ovn cidr or cluster ip range will be masqueraded with pod namespace given host.

## Example

Create the following namespace.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: testns
  annotations:
    ovn.kubernetes.io/gateway_type: centralized // or distributed by default
    ovn.kubernetes.io/gateway_node: node1 // if using centralied gateway, a gateway_node is needed
```

Creat the demo pods.

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: app1
  namespace: testns
  labels:
    app: app1
spec:
  selector:
    matchLabels:
      name: app1
  template:
    metadata:
      labels:
        name: app1
    spec:
      containers:
      - name: toolbox
        image: halfcrazy/toolbox
```

then open two terminal, one on master

`kubectl -n testns exec -it app1-xxxx ping 114.114.114.114`

and one on node1

`tcpdump -n -i eth0 icmp and host 114.114.114.114`