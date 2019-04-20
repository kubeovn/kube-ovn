# Gateways

A Gateway is used to enable external network connectivity for Pods within the OVN Virtual Network. Kube-OVN supports two kinds of Gateways: the distributed Gateway and the centralized Gateway.

For a distributed Gateway, outgoing traffic from Pods within the OVN network to external destinations will be masqueraded with the Node IP address where the Pod is hosted.

For a centralized gateway, outgoing traffic from Pods within the OVN network to external destinations will be masqueraded with the Gateway Node IP address for the Namespace.

## Example

Add the following annotations when creating the Namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: testns
  annotations:
    ovn.kubernetes.io/gateway_type: centralized // or distributed by default
    ovn.kubernetes.io/gateway_node: node1 // specify this if using a centralized Gateway
```

Create some Pods:

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

Open two terminals, one on the master:

`kubectl -n testns exec -it app1-xxxx ping 114.114.114.114`

And one on node1:

`tcpdump -n -i eth0 icmp and host 114.114.114.114`