## How to use it

When you install vlan, you host need at least two ifname, and the second ifname can reconnection

### Label host interface name in every node
```bash
kubectl label node <Node on which to deploy OVN DB> ovn.kubernetes.io/host_interface_name=<host_interface_name>
```

### Install vlan

```bash
cd kube-ovn && sh dist/images/install-vlan.sh
```

### Create vlan cr
```bash
apiVersion: kubeovn.io/v1
kind: Vlan
metadata:
  name: product
spec:
  vlanId: 10
```

### Create namespace
```bash
apiVersion: v1
kind: Namespace
metadata:
  name: product
  labels:
    name: product
```

### Create subnet
```bash
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: product
spec:
  cidrBlock: 10.100.0.0/16
  default: false
  gateway: 10.100.0.1
  gatewayType: distributed
  natOutgoing: true
  vlan: product
  namespaces:
    - product
```

### Create samplepod
```bash
kubectl run samplepod --image=nginx --namespace=product
```