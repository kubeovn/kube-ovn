# Kube-OVN with nodes which run ovs-dpdk or ovs-kernel

This document describes how to run Kube-OVN with nodes which run ovs-dpdk or ovs-kernel

## Prerequisite
* Node which runs ovs-dpdk must have a net card bound to the dpdk driver.
* Hugepages on the host.
## Label nodes that need to run ovs-dpdk
```bash
kubectl label nodes <node> ovn.kubernetes.io/ovs_dp_type="userspace"
```
## Set up net card
We use `driverctl`                                                                                           to persist the device driver configuration.
Here is an example to bind dpdk driver to a net card.
```bash
driverctl set-override 0000:00:0b.0 uio_pci_generic
```
For other drivers, please refer to https://www.dpdk.org/.

## configrue node
Edit the configuration file named `ovs-dpdk-config` on the node that needs to run ovs-dpdk. The configuration file needs to be placed in the `/opt/ovs-config` directory.
```bash
# specify encap IP
ENCAP_IP=192.168.122.193/24
# specify pci device
DPDK_DEV=0000:00:0b.0
```


## Set up Kube-OVN
Just run `install.sh --with-hybrid-dpdk`

## How to use
Here is an example to create a vhost-user app to use userspace datapath. We create a virtual machine using vhostuser and test if it can access the Internet. 

Install the KVM device plugin for creating virtual machines. More information is available through this website. https://github.com/kubevirt/kubernetes-device-plugins/blob/master/docs/README.kvm.md
```bash
kubectl apply -f manifests/kvm-ds.yml
```

Create NetworkAttachmentDefinition
```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: ovn-dpdk
  namespace: default
spec:
  config: >-
    {"cniVersion": "0.3.0", "type": "kube-ovn", "server_socket":
    "/run/openvswitch/kube-ovn-daemon.sock", "provider": "ovn-dpdk.default.ovn",
    "vhost_user_socket_volume_name": "vhostuser-sockets",
    "vhost_user_socket_name": "sock"}
```
Create a virtual machine image and tag it as vm-vhostuser:latest
```bash
docker build . -t  vm-vhostuser:latest
```
```dockerfile
From quay.io/kubevirt/virt-launcher:v0.46.1

# wget http://cloud.centos.org/centos/7/images/CentOS-7-x86_64-GenericCloud.qcow2
COPY CentOS-7-x86_64-GenericCloud.qcow2 /var/lib/libvirt/images/CentOS-7-x86_64-GenericCloud.qcow2
```
Create virtual machine.
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vm-config
data:
  start.sh: |
    chmod u+w /etc/libvirt/qemu.conf
    echo "hugetlbfs_mount = \"/dev/hugepages\"" >> /etc/libvirt/qemu.conf
    virtlogd &
    libvirtd &
    
    mkdir /var/lock
    
    sleep 5
    
    virsh define /root/vm/vm.xml
    virsh start vm
    
    tail -f /dev/null
  vm.xml: |
    <domain type='kvm'>
      <name>vm</name>
      <uuid>4a9b3f53-fa2a-47f3-a757-dd87720d9d1d</uuid>
      <memory unit='KiB'>2097152</memory>
      <currentMemory unit='KiB'>2097152</currentMemory>
      <memoryBacking>
        <hugepages>
          <page size='2' unit='M' nodeset='0'/>
        </hugepages>
      </memoryBacking>
      <vcpu placement='static'>2</vcpu>
      <cputune>
        <shares>4096</shares>
        <vcpupin vcpu='0' cpuset='4'/>
        <vcpupin vcpu='1' cpuset='5'/>
        <emulatorpin cpuset='1,3'/>
      </cputune>
      <os>
        <type arch='x86_64' machine='pc'>hvm</type>
        <boot dev='hd'/>
      </os>
      <features>
        <acpi/>
        <apic/>
      </features>
      <cpu mode='host-model'>
        <model fallback='allow'/>
        <topology sockets='1' cores='2' threads='1'/>
        <numa>
          <cell id='0' cpus='0-1' memory='2097152' unit='KiB' memAccess='shared'/>
        </numa>
      </cpu>
      <on_reboot>restart</on_reboot>
      <devices>
        <emulator>/usr/libexec/qemu-kvm</emulator>
        <disk type='file' device='disk'>
          <driver name='qemu' type='qcow2' cache='none'/>
          <source file='/var/lib/libvirt/images/CentOS-7-x86_64-GenericCloud.qcow2'/>
          <target dev='vda' bus='virtio'/>
        </disk>

        <interface type='vhostuser'>
          <mac address='00:00:00:0A:30:89'/>
          <source type='unix' path='/var/run/vm/sock' mode='server'/>
           <model type='virtio'/>
          <driver queues='2'>
            <host mrg_rxbuf='off'/>
          </driver>
        </interface>
        <serial type='pty'>
          <target type='isa-serial' port='0'>
            <model name='isa-serial'/>
          </target>
        </serial>
        <console type='pty'>
          <target type='serial' port='0'/>
        </console>
        <channel type='unix'>
          <source mode='bind' path='/var/lib/libvirt/qemu/channel/target/domain-1-vm/org.qemu.guest_agent.0'/>
          <target type='virtio' name='org.qemu.guest_agent.0' state='connected'/>
          <alias name='channel0'/>
          <address type='virtio-serial' controller='0' bus='0' port='1'/>
        </channel>

      </devices>
    </domain>
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vm-deployment
  labels:
    app: vm
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vm
  template:
    metadata:
      labels:
        app: vm
      annotations:
        k8s.v1.cni.cncf.io/networks: default/ovn-dpdk
        ovn-dpdk.default.ovn.kubernetes.io/ip_address: 10.16.0.96
        ovn-dpdk.default.ovn.kubernetes.io/mac_address: 00:00:00:0A:30:89
    spec:
      nodeSelector:
        ovn.kubernetes.io/ovs_dp_type: userspace
      securityContext:
        runAsUser: 0
      volumes:
        - name: vhostuser-sockets
          emptyDir: {}
        - name: xml
          configMap:
            name: vm-config
        - name: hugepage
          emptyDir:
            medium: HugePages-2Mi
        - name: libvirt-runtime
          emptyDir: {}
      containers:
        - name: vm
          image: vm-vhostuser:latest
          command: ["bash", "/root/vm/start.sh"]
          securityContext:
            capabilities:
              add:
                - NET_BIND_SERVICE
                - SYS_NICE
                - NET_RAW
                - NET_ADMIN
            privileged: false
            runAsUser: 0
          resources:
            limits:
              cpu: '2'
              devices.kubevirt.io/kvm: '1'
              memory: '8784969729'
              hugepages-2Mi: 2Gi
            requests:
              cpu: 666m
              devices.kubevirt.io/kvm: '1'
              ephemeral-storage: 50M
              memory: '4490002433'
          volumeMounts:
            - name: vhostuser-sockets
              mountPath: /var/run/vm
            - name: xml
              mountPath: /root/vm/
            - mountPath: /dev/hugepages
              name: hugepage
            - name: libvirt-runtime
              mountPath: /var/run/libvirt
```
After waiting for the Pod of the virtual machine to start, attach shell in to the Pod.
```bash
# set vm root password
bash-5.0# virsh set-user-password vm root 12345
Password set successfully for root in vm

# console to vm
bash-5.0# virsh console vm
Connected to domain 'vm'
Escape character is ^] (Ctrl + ])

CentOS Linux 7 (Core)
Kernel 3.10.0-1127.el7.x86_64 on an x86_64

localhost login: root
Password:
Last login: Fri Feb 25 09:52:54 on ttyS0
[root@localhost ~]#
```
Now you have logged in the virtual machine, you can access the Internet after configuring the IP and routing entries.
```bash
ip link set eth0 mtu 1400
ip addr add 10.16.0.96/16 dev eth0
ip ro add default via 10.16.0.1
ping 114.114.114.114
```

