# Hardware Offload for Mellanox

The OVS software based solution is CPU intensive, affecting system performance and preventing full utilization of the available bandwidth.
Mellanox Accelerated Switching And Packet Processing (ASAP2) technology allows OVS offloading by handling OVS data-plane in Mellanox ConnectX-5 onwards NIC hardware (Mellanox Embedded Switch or eSwitch) while maintaining OVS control-plane unmodified. As a result, we observe significantly higher OVS performance without the associated CPU load.
By taking use of SR-IOV technology we can achieve low network latency and high throughput.

## Prerequisites
- Mellanox ConnectX-5 Card with OVS-Kernel ASAP² Packages
- Centos 8 Stream or upstream Linux Kernel 5.7+
- MLNX-OFED 5.1+
- SR-IOV Device Plugin
- Multus-CNI

## Some known limitation

The `dp_hash` and `hash` which are used by OVN LB can not be offloaded now.
If using OVN LB it will block all acceleration as the LB flows will affect all traffic.
So when install Kube-OVN LB functions should be disabled and fall back to use kube-proxy.

## Installation Guide

### Install Kube-OVN with hw-offload mode enabled
1. Download latest install script

```bash
wget https://raw.githubusercontent.com/alauda/kube-ovn/master/dist/images/install.sh
```

2. Edit the install script, enable hw-offload, disable traffic mirror and set the IFACE to the PF.
Make sure that there is an IP address bind to the PF.

> `NOTICE`: If the PF is slave of a `bond` interface, the hardware offload across nodes may not function normally.

```bash
ENABLE_MIRROR=${ENABLE_MIRROR:-false}
HW_OFFLOAD=${HW_OFFLOAD:-true}
ENABLE_LB=${ENABLE_LB:-false}
IFACE="ensp01"
```

3. Install Kube-OVN

```bash
bash install.sh
```

### Setting Up SR-IOV
1. Find the device id of ConnectX-5 device, below is `42:00.0`

```shell
lspci -nn | grep ConnectX-5
42:00.0 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5] [15b3:1017]
```

2. Find the related interface with device id, below is `p4p1`

```shell
ls -l /sys/class/net/ | grep 42:00.0
lrwxrwxrwx. 1 root root 0 Jul 22 23:16 p4p1 -> ../../devices/pci0000:40/0000:40:02.0/0000:42:00.0/net/p4p1
```

3. Check available VF number

```shell
cat /sys/class/net/p4p1/device/sriov_totalvfs
8
```

4. Create VFs

```shell
echo '4' > /sys/class/net/p4p1/device/sriov_numvfs
ip link show p4p1
10: p4p1: <NO-CARRIER,BROADCAST,MULTICAST,UP> mtu 1500 qdisc mq state DOWN mode DEFAULT group default qlen 1000
    link/ether b8:59:9f:c1:ec:12 brd ff:ff:ff:ff:ff:ff
    vf 0 MAC 00:00:00:00:00:00, spoof checking off, link-state auto, trust off, query_rss off
    vf 1 MAC 00:00:00:00:00:00, spoof checking off, link-state auto, trust off, query_rss off
    vf 2 MAC 00:00:00:00:00:00, spoof checking off, link-state auto, trust off, query_rss off
    vf 3 MAC 00:00:00:00:00:00, spoof checking off, link-state auto, trust off, query_rss off
ip link set p4p1 up
```

5. Find the device ids of VFs created above

```shell
lspci -nn | grep ConnectX-5
42:00.0 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5] [15b3:1017]
42:00.1 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5] [15b3:1017]
42:00.2 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5 Virtual Function] [15b3:1018]
42:00.3 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5 Virtual Function] [15b3:1018]
42:00.4 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5 Virtual Function] [15b3:1018]
42:00.5 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5 Virtual Function] [15b3:1018]
```

6. Unbind VFs from driver by device id found above

```bash
echo 0000:42:00.2 > /sys/bus/pci/drivers/mlx5_core/unbind
echo 0000:42:00.3 > /sys/bus/pci/drivers/mlx5_core/unbind
echo 0000:42:00.4 > /sys/bus/pci/drivers/mlx5_core/unbind
echo 0000:42:00.5 > /sys/bus/pci/drivers/mlx5_core/unbind
```

7. Enable switchdev mode by device id of PF

```bash
devlink dev eswitch set pci/0000:42:00.0 mode switchdev
```

8. Enable hw-tc-offload, above step might change the interface name

```bash
ethtool -K enp66s0f0 hw-tc-offload on
```

9. Bind the VFs to driver again

```bash
echo 0000:42:00.2 > /sys/bus/pci/drivers/mlx5_core/bind
echo 0000:42:00.3 > /sys/bus/pci/drivers/mlx5_core/bind
echo 0000:42:00.4 > /sys/bus/pci/drivers/mlx5_core/bind
echo 0000:42:00.5 > /sys/bus/pci/drivers/mlx5_core/bind
```

10. Disable NetworkManager if it's running

```bash
systemctl stop NetworkManager
systemctl disable NetworkManager
```

### Install SR-IOV Device Plugin
1. Create a ConfigMap that defines SR-IOV resource pool configuration
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: sriovdp-config
  namespace: kube-system
data:
  config.json: |
    {
      "resourceList": [{
          "resourcePrefix": "mellanox.com",
          "resourceName": "cx5_sriov_switchdev",
          "selectors": {
                  "vendors": ["15b3"],
                  "devices": ["1018"],
                  "drivers": ["mlx5_core"]
              }
      }
      ]
    }
```

2. Follow [SR-IOV Device Plugin](https://github.com/intel/sriov-network-device-plugin) to deploy device plugin.

```bash
kubectl apply -f https://raw.githubusercontent.com/intel/sriov-network-device-plugin/master/deployments/k8s-v1.16/sriovdp-daemonset.yaml
```

3. Check if SR-IOV devices have been discovered by device plugin

```shell
kubectl describe no containerserver  | grep mellanox

mellanox.com/cx5_sriov_switchdev:  4
mellanox.com/cx5_sriov_switchdev:  4
mellanox.com/cx5_sriov_switchdev  0           0
```
### Install Multus-CNI
1. Follow [Multus-CNI](https://github.com/k8snetworkplumbingwg/multus-cni) to deploy Multus-CNI

```bash
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset.yml
```

2. Create a NetworkAttachmentDefinition
```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: default
  namespace: default
  annotations:
    k8s.v1.cni.cncf.io/resourceName: mellanox.com/cx5_sriov_switchdev
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "kube-ovn",
    "plugins":[
        {
            "type":"kube-ovn",
            "server_socket":"/run/openvswitch/kube-ovn-daemon.sock",
            "provider": "default.default.ovn"
        },
        {
            "type":"portmap",
            "capabilities":{
                "portMappings":true
            }
        }
    ]
}'
```

### Create Pod with SR-IOV
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  annotations:
    v1.multus-cni.io/default-network: default/default
spec:
  containers:
  - name: nginx
    image: nginx:alpine
    resources:
      requests:
        mellanox.com/cx5_sriov_switchdev: '1'
      limits:
        mellanox.com/cx5_sriov_switchdev: '1'
```
### Verify If Offload Works

```shell
ovs-appctl dpctl/dump-flows -m type=offloaded
ufid:91cc45de-e7e9-4935-8f82-1890430b0f66, skb_priority(0/0),skb_mark(0/0),ct_state(0/0x23),ct_zone(0/0),ct_mark(0/0),ct_label(0/0x1),recirc_id(0),dp_hash(0/0),in_port(5b45c61b307e_h),packet_type(ns=0/0,id=0/0),eth(src=00:00:00:c5:6d:4e,dst=00:00:00:e7:16:ce),eth_type(0x0800),ipv4(src=0.0.0.0/0.0.0.0,dst=0.0.0.0/0.0.0.0,proto=0/0,tos=0/0,ttl=0/0,frag=no), packets:941539, bytes:62142230, used:0.260s, offloaded:yes, dp:tc, actions:54235e5753b8_h
ufid:e00768d7-e652-4d79-8182-3291d852b791, skb_priority(0/0),skb_mark(0/0),ct_state(0/0x23),ct_zone(0/0),ct_mark(0/0),ct_label(0/0x1),recirc_id(0),dp_hash(0/0),in_port(54235e5753b8_h),packet_type(ns=0/0,id=0/0),eth(src=00:00:00:e7:16:ce,dst=00:00:00:c5:6d:4e),eth_type(0x0800),ipv4(src=0.0.0.0/0.0.0.0,dst=0.0.0.0/0.0.0.0,proto=0/0,tos=0/0,ttl=0/0,frag=no), packets:82386659, bytes:115944854173, used:0.260s, offloaded:yes, dp:tc, actions:5b45c61b307e_h
```

You can find `offloaded:yes, dp:tc` if all works well.
