# Hardware Offload for Corigine 

The OVS software based solution is CPU intensive, affecting system performance and preventing full utilization of the available bandwidth.
Corigine Agilio CX SmartNICs provide a drop-in accelerator for OVS which can support very high flow and policy capacities without degradation in performance
By taking use of SR-IOV technology we can achieve low network latency and high throughput.

## Prerequisites
- Corigine Agilio CX family NICs
- Centos 8 Stream or upstream Linux Kernel 5.7+
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

2. Edit the install script, enable hw-offload, disable traffic mirror and set the IFACE to the physical port.
Make sure that there is an IP address bind to the physical port.

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

### Setting the TC offloading firmware and VF

You can follow the [agilio-open-vswitch-tc-user-guide](https://help.netronome.com/support/solutions/articles/36000081172-agilio-open-vswitch-tc-user-guide) for more details.

1. Save the script below to manipulate the firmware

```bash
#!/bin/bash
DEVICE=${1}
DEFAULT_ASSY=scan
ASSY=${2:-${DEFAULT_ASSY}}
APP=${3:-flower}

if [ "x${DEVICE}" = "x" -o ! -e /sys/class/net/${DEVICE} ]; then
    echo Syntax: ${0} device [ASSY] [APP]
    echo
    echo This script associates the TC Offload firmware
    echo with a Netronome SmartNIC.
    echo
    echo device: is the network device associated with the SmartNIC
    echo ASSY: defaults to ${DEFAULT_ASSY}
    echo APP: defaults to flower. flower-next is supported if updated
    echo      firmware has been installed.
    exit 1
fi

# It is recommended that the assembly be determined by inspection
# The following code determines the value via the debug interface
if [ "${ASSY}x" = "scanx" ]; then
    ethtool -W ${DEVICE} 0
    DEBUG=$(ethtool -w ${DEVICE} data /dev/stdout | strings)
    SERIAL=$(echo "${DEBUG}" | grep "^SN:")
    ASSY=$(echo ${SERIAL} | grep -oE AMDA[0-9]{4})
fi

PCIADDR=$(basename $(readlink -e /sys/class/net/${DEVICE}/device))
FWDIR="/lib/firmware/netronome"

# AMDA0081 and AMDA0097 uses the same firmware
if [ "${ASSY}" = "AMDA0081" ]; then
    if [ ! -e ${FWDIR}/${APP}/nic_AMDA0081.nffw ]; then
       ln -sf nic_AMDA0097.nffw ${FWDIR}/${APP}/nic_AMDA0081.nffw
   fi
fi

FW="${FWDIR}/pci-${PCIADDR}.nffw"
ln -sf "${APP}/nic_${ASSY}.nffw" "${FW}"

# insert distro-specific initramfs section here...
```

2. Change the firmware options and reload the driver

```bash
./agilio-tc-fw-select.sh ens47np0 scan
rmmod nfp
modprobe nfp
```

3. Check the VF limits and create VF

```bash
cat /sys/class/net/ens3/device/sriov_totalvfs
65

echo 4 > /sys/class/net/ens47/device/sriov_numvfs
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
          "resourcePrefix": "corigine.com",
          "resourceName": "agilio_sriov",
          "selectors": {
                  "vendors": ["19ee"],
                  "devices": ["6003"],
                  "drivers": ["nfp_netvf"]
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
kubectl describe no containerserver  | grep corigine

corigine.com/agilio_sriov:  4
corigine.com/agilio_sriov:  4
corigine.com/agilio_sriov  0           0
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
  namespace: kube-system
  annotations:
    k8s.v1.cni.cncf.io/resourceName: corigine.com/agilio_sriov
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

### Create Pod with SR-IOV
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  annotations:
    v1.multus-cni.io/default-network: default
spec:
  containers:
  - name: nginx
    image: nginx:alpine
    resources:
      requests:
        corigine.com/agilio_sriov: '1'
      limits:
        corigine.com/agilio_sriov: '1'
```
### Verify If Offload Works

```shell
ovs-appctl dpctl/dump-flows -m type=offloaded
ufid:91cc45de-e7e9-4935-8f82-1890430b0f66, skb_priority(0/0),skb_mark(0/0),ct_state(0/0x23),ct_zone(0/0),ct_mark(0/0),ct_label(0/0x1),recirc_id(0),dp_hash(0/0),in_port(5b45c61b307e_h),packet_type(ns=0/0,id=0/0),eth(src=00:00:00:c5:6d:4e,dst=00:00:00:e7:16:ce),eth_type(0x0800),ipv4(src=0.0.0.0/0.0.0.0,dst=0.0.0.0/0.0.0.0,proto=0/0,tos=0/0,ttl=0/0,frag=no), packets:941539, bytes:62142230, used:0.260s, offloaded:yes, dp:tc, actions:54235e5753b8_h
ufid:e00768d7-e652-4d79-8182-3291d852b791, skb_priority(0/0),skb_mark(0/0),ct_state(0/0x23),ct_zone(0/0),ct_mark(0/0),ct_label(0/0x1),recirc_id(0),dp_hash(0/0),in_port(54235e5753b8_h),packet_type(ns=0/0,id=0/0),eth(src=00:00:00:e7:16:ce,dst=00:00:00:c5:6d:4e),eth_type(0x0800),ipv4(src=0.0.0.0/0.0.0.0,dst=0.0.0.0/0.0.0.0,proto=0/0,tos=0/0,ttl=0/0,frag=no), packets:82386659, bytes:115944854173, used:0.260s, offloaded:yes, dp:tc, actions:5b45c61b307e_h
```

You can find `offloaded:yes, dp:tc` if all works well.
