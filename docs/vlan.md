## Vlan Support

By default, Kube-OVN use Geneve to encapsulate packets between hosts, which will build an overlay network above your infrastructure.
Kube-OVN also support underlay Vlan mode network for better performance and throughput.
In Vlan mode, the packets will send directly to physical switches with vlan tags.

To enable Vlan mode, a dedicated network interface is required by container network.
The related switch port must work in trunk mode to accept 802.1q packets.

By now, Geneve or Vlan network mode is a global install option, all container must work in the same network mode.
We are working at combine two networks in one cluster.

### Install Vlan mode

1. Get the installation script

`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.4/dist/images/install.sh`

2. Edit the `install.sh`, modify `NETWORK_TYPE` to `vlan`, `VLAN_INTERFACE_NAME` to related host interface.

3. Install Kube-OVN

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

Multiple Subnets can bind to one Vlan

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
  underlayGateway: true # If the gateway exist in physical switch please set underlayGateway to true, otherwise kube-ovn will create a virtual one
  namespaces:
    - product
```

### Create sample pod
```bash
kubectl run samplepod --image=nginx --namespace=product
```
