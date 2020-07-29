# Hardware Offload

The OVS software based solution is CPU intensive, affecting system performance and preventing full utilization of the available bandwidth.
Mellanox Accelerated Switching And Packet Processing (ASAP2) technology allows OVS offloading by handling OVS data-plane in Mellanox ConnectX-5 onwards NIC hardware (Mellanox Embedded Switch or eSwitch) while maintaining OVS control-plane unmodified. As a result, we observe significantly higher OVS performance without the associated CPU load.
By taking use of SR-IOV technology we can achieve low network latency and high throughput.

## Prerequisites
- Mellanox ConnectX-5 Card with OVS-Kernel ASAP² Packages
- Linux Kernel 5.7 or above
- SR-IOV Device Plugin
- Multus-CNI

## Installation Guide

### Setting Up SR-IOV
1. Find the device id of ConnectX-5 device, below is `42:00.0`
```bash
lspci -nn | grep ConnectX-5
42:00.0 Ethernet controller [0200]: Mellanox Technologies MT27800 Family [ConnectX-5] [15b3:1017]
```

2. Find the related interface with device id, below is `p4p1`
```bash
ls -l /sys/class/net/ | grep 42:00.0
lrwxrwxrwx. 1 root root 0 Jul 22 23:16 p4p1 -> ../../devices/pci0000:40/0000:40:02.0/0000:42:00.0/net/p4p1
```

3. Check available VF number
```bash
cat /sys/class/net/p4p1/device/sriov_totalvfs
8
```

4. Create VFs
```bash
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
```bash
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

3. Check if SR-IOV devices have been discovered by device plugin
```bash
kubectl describe no containerserver  | grep mellanox

mellanox.com/cx5_sriov_switchdev:  4
mellanox.com/cx5_sriov_switchdev:  4
mellanox.com/cx5_sriov_switchdev  0           0
```
### Install Multus-CNI
1. Follow [Multus-CNI](https://github.com/intel/multus-cni/) to deploy Multus-CNI

2. Create a NetworkAttachmentDefinition
```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: default
  annotations:
    k8s.v1.cni.cncf.io/resourceName: mellanox.com/cx5_sriov_switchdev
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "kube-ovn",
    "plugins":[
        {
            "type":"kube-ovn",
            "server_socket":"/run/openvswitch/kube-ovn-daemon.sock"
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
### Enable HardwareOffload in Kube-OVN
Update ovs-ovn daemonset and set the HW_OFFLOAD env to true and delete exist pods to redeploy.

```yaml
    spec:
      containers:
      - command:
        - /kube-ovn/start-ovs.sh
        env:
        - name: POD_IP
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: status.podIP
        - name: HW_OFFLOAD
          value: "true"
        image: kubeovn/kube-ovn:v1.3.0
```
### Create Pod with SR-IOV
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: perf
  annotations:
    v1.multus-cni.io/default-network: default
spec:
  containers:
  - name: perf
    image: index.alauda.cn/alaudaorg/perf
    resources:
      requests:
        mellanox.com/cx5_sriov_switchdev: '1'
      limits:
        mellanox.com/cx5_sriov_switchdev: '1'
```
