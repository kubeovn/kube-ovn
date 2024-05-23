# Hardware Offload for Yunsilicon

The OVS software based solution is CPU intensive, affecting system performance and preventing full utilization of the available bandwidth.
Yunsilicon metaScale SmartNICs provide a drop-in accelerator for OVS which can support very high flow and policy capacities without degradation in performance
By taking use of SR-IOV technology we can achieve low network latency and high throughput.

## Prerequisites
- MCR Allinone Packages
- Yunsilicon metaScale family NICs
- SR-IOV Device Plugin
- Multus-CNI

## Installation Guide

### Install Kube-OVN with hw-offload mode enabled
1. Download latest install script

```bash
wget https://github.com/yunsilicon/kube-ovn/blob/release-1.11/dist/images/install.sh
```

2. Configure node
Edit the configuration file named `ovs-dpdk-config` on the node that needs to run ovs-dpdk. The configuration file needs to be placed in the `/opt/ovs-config` directory.

```bash
# specify log level for ovs dpdk, the value is info or dbg, default is info
VLOG=info
# specify nic offload, the value is true or false, default is true
HW_OFFLOAD=true
# specify cpu mask for ovs dpdk, not specified by default
CPU_MASK=0x02
# specify socket memory, not specified by default
SOCKET_MEM="2048,2048"
# specify encap IP
ENCAP_IP=6.6.6.208/24
# specify pci device
DPDK_DEV=0000:b3:00.0
# specify mtu, default is 1500
PF_MTU=1500
# specify bond name if bond enabled, not specified by default
BR_PHY_BOND_NAME=bond0
```

3. Install Kube-OVN

> `NOTICE`: We need to manually modify the openvswitch image in the script, please contact the technical support of yunsilicon to obtain the supporting version.

```bash
bash install.sh
```

### Setting Up SR-IOV
1. Find the device id of metaScale device, below is `b3:00.0`

```shell
[root@k8s-master ~]# lspci -d 1f67:
b3:00.0 Ethernet controller: Device 1f67:1111 (rev 02)
b3:00.1 Ethernet controller: Device 1f67:1111 (rev 02)
```

2. Find the related interface with device id, below is `p3p1`

```shell
ls -l /sys/class/net/ | grep b3:00.0
lrwxrwxrwx 1 root root 0 May  7 16:30 p3p1 -> ../../devices/pci0000:b2/0000:b2:00.0/0000:b3:00.0/net/p3p1
```

3. Check available VF number

```shell
cat /sys/class/net/p3p1/device/sriov_totalvfs
512
```

4. Create VFs

```shell
echo '10' > /sys/class/net/p3p1/device/sriov_numvfs
```

5. Find the device ids of VFs created above

```shell
lspci -d 1f67:
b3:00.0 Ethernet controller: Device 1f67:1111 (rev 02)
b3:00.1 Ethernet controller: Device 1f67:1111 (rev 02)
b3:00.2 Ethernet controller: Device 1f67:1112
b3:00.3 Ethernet controller: Device 1f67:1112
b3:00.4 Ethernet controller: Device 1f67:1112
b3:00.5 Ethernet controller: Device 1f67:1112
b3:00.6 Ethernet controller: Device 1f67:1112
b3:00.7 Ethernet controller: Device 1f67:1112
b3:01.0 Ethernet controller: Device 1f67:1112
b3:01.1 Ethernet controller: Device 1f67:1112
b3:01.2 Ethernet controller: Device 1f67:1112
b3:01.3 Ethernet controller: Device 1f67:1112
```

6. Enable switchdev mode by device id of PF

```bash
devlink dev eswitch set pci/0000:b3:00.0 mode switchdev
```

7. Disable NetworkManager if it's running

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
                "resourceName": "xsc_sriov",
                "resourcePrefix": "yunsilicon.com",
                "selectors": {
                    "vendors": ["1f67"],
                    "devices": ["1012", "1112"]
                }}
        ]
    }

```

2. Follow [SR-IOV Device Plugin](https://github.com/intel/sriov-network-device-plugin) to deploy device plugin.

> `NOTICE`: We need to manually modify the sriov-network-device-plugin image in the script, please contact the technical support of yunsilicon to obtain the supporting version.

Refer to https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin for installation

3. Check if SR-IOV devices have been discovered by device plugin

```shell
[root@k8s-master ~]# kubectl describe node <node name> | grep yunsilicon.com/xsc_sriov
  yunsilicon.com/xsc_sriov:  10
  yunsilicon.com/xsc_sriov:  10
  yunsilicon.com/xsc_sriov  0             0
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
  name: sriov-net1
  namespace: default
  annotations:
    k8s.v1.cni.cncf.io/resourceName: yunsilicon.com/xsc_sriov
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "kube-ovn",
    "plugins":[
        {
            "type":"kube-ovn",
            "server_socket":"/run/openvswitch/kube-ovn-daemon.sock",
            "provider": "sriov-net1.default.ovn"
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
    v1.multus-cni.io/default-network: default/sriov-net1
spec:
  containers:
    - name: nginx
      image: nginx:alpine
      resources:
        requests:
          yunsilicon.com/xsc_sriov: '1'
        limits:
          yunsilicon.com/xsc_sriov: '1'
```
### Verify If Offload Works

```shell
ovs-appctl dpctl/dump-flows type=offloaded
flow-dump from pmd on cpu core: 9
ct_state(-new+est-rel+rpl+trk),ct_mark(0/0x3),recirc_id(0x2d277),in_port(15),packet_type(ns=0,id=0),eth(src=00:00:00:9d:fb:1a,dst=00:00:00:ce:cf:b9),eth_type(0x0800),ipv4(dst=10.16.0.14,frag=no), packets:6, bytes:588, used:7.276s, actions:ct(zone=4,nat),recirc(0x2d278)
ct_state(-new+est-rel-rpl+trk),ct_mark(0/0x3),recirc_id(0x2d275),in_port(8),packet_type(ns=0,id=0),eth(src=00:00:00:ce:cf:b9,dst=00:00:00:9d:fb:1a),eth_type(0x0800),ipv4(dst=10.16.0.18,frag=no), packets:5, bytes:490, used:7.434s, actions:ct(zone=6,nat),recirc(0x2d276)
ct_state(-new+est-rel-rpl+trk),ct_mark(0/0x1),recirc_id(0x2d276),in_port(8),packet_type(ns=0,id=0),eth(src=00:00:00:ce:cf:b9,dst=00:00:00:9d:fb:1a/01:00:00:00:00:00),eth_type(0x0800),ipv4(frag=no), packets:5, bytes:490, used:7.434s, actions:15
recirc_id(0),in_port(15),packet_type(ns=0,id=0),eth(src=00:00:00:9d:fb:1a/01:00:00:00:00:00,dst=00:00:00:ce:cf:b9),eth_type(0x0800),ipv4(dst=10.16.0.14/255.192.0.0,frag=no), packets:6, bytes:588, used:7.277s, actions:ct(zone=6,nat),recirc(0x2d277)
recirc_id(0),in_port(8),packet_type(ns=0,id=0),eth(src=00:00:00:ce:cf:b9/01:00:00:00:00:00,dst=00:00:00:9d:fb:1a),eth_type(0x0800),ipv4(dst=10.16.0.18/255.192.0.0,frag=no), packets:6, bytes:588, used:7.434s, actions:ct(zone=4,nat),recirc(0x2d275)
ct_state(-new+est-rel+rpl+trk),ct_mark(0/0x1),recirc_id(0x2d278),in_port(15),packet_type(ns=0,id=0),eth(dst=00:00:00:ce:cf:b9/01:00:00:00:00:00),eth_type(0x0800),ipv4(frag=no), packets:6, bytes:588, used:7.277s, actions:8
```
You can find some flows if all works well.