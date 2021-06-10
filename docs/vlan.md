## Vlan/Underlay Support

By default, Kube-OVN use Geneve to encapsulate packets between hosts, which will build an overlay network above your infrastructure.
Kube-OVN also support underlay Vlan mode network for better performance and throughput.
In Vlan mode, the packets will send directly to physical switches with vlan tags.

To enable Vlan mode, at least one interface connected to the external provider network is required.
The related switch port must work in trunk mode to accept 802.1q packets. For underlay network with no vlan tag, you need
to set the VLAN ID to 0.

From v1.7.0, Mac address, MTU, IP addresses and routes attached to each interface will be copied/transferred to an OVS bridge named `br-PROVIDER_NAME` where `PROVIDER_NAME` is the provider network name.
This mechanism enables underlay mode on machines with only one physical network interface.

> By default, the provider name is `provider`.

~~By now, Geneve or Vlan network mode is a global install option, all container must work in the same network mode.
We are working at combine two networks in one cluster.~~

We introduce a new hybrid mode that allows Geneve and Vlan to exist at the same time.
You can have a subnet A using Geneve encapsulation and subnet B using Vlan tag.


![topology](vlan-topolgy.png "vlan network topology")

### Install Vlan mode

1. Get the installation script

`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.7/dist/images/install.sh`

2. Edit the `install.sh`, modify `NETWORK_TYPE` to `vlan`, `DEFAULT_VLAN_INTERFACE_NAME` to related host interface.
> NOTE: if your nodes have different nic name for vlan device you could use regex for DEFAULT_VLAN_INTERFACE_NAME or label those nodes with
   own `ovn.kubernetes.io/host_interface_name`.

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


### Install Hybrid mode

1. Get the installation script

`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.7/dist/images/install.sh`

2. Edit the `install.sh`, modify `NETWORK_TYPE` to `hybrid`, `DEFAULT_VLAN_INTERFACE_NAME` to related host interface.
> NOTE: if your nodes have different nic name for vlan device you could use regex for DEFAULT_VLAN_INTERFACE_NAME or label those nodes with
   own `ovn.kubernetes.io/host_interface_name`.

3. Install Kube-OVN

### Multiple Vlans

From v1.7.1, Kube-OVN supports multiple underlay/vlan networkings on installation.

If you want to set up multiple Vlan, you can set values of `EXTRA_PROVIDER_NAMES`, `EXTRA_VLAN_INTERFACE_NAMES`, `EXTRA_VLAN_NAMES`,
`EXTRA_VLAN_IDS` and `EXTRA_VLAN_RANGES` in `install.sh` based on your needs.
The values must be comma separated lists (except the one of `EXTRA_VLAN_RANGES` which is colon separated) and the lengths must be the same.

### Note
Vlan mode will auto-assign a VLAN to a subnet if the subnet doesn't specify a VLAN. 
The hybrid mode will not do the auto-assign, if your subnet doesn't specify a VLAN then the subnet will treat as Geneve mode.
