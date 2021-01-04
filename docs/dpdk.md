# Kube-OVN with OVS-DPDK

This document describes how to run Kube-OVN with OVS-DPDK.


## Prerequest
- Kubernetes >= 1.11
- Docker >= 1.12.6
- OS: CentOS 7.5/7.6/7.7, Ubuntu 16.04/18.04
- 1GB Hugepages on the host

#### Hugepages Setup:
- On the host, modify the file /etc/default/grub
- Append the following to the setting GRUB_CMDLINE_LINUX:
`default_hugepagesz=1GB hugepagesz=1G hugepages=X`
Where X is the number of 1GB hugepages you wish to create on your system. Your usecases will determine the number of hugepages required and system memory available will determine the maximum possible.
- Update Grub:
	- On legacy boot systems run: `grub2-mkconfig -o /boot/grub2/grub.cfg`
	- On EFI boot systems run: `grub2-mkconfig -o /boot/efi/EFI/centos/grub.cfg`
	**NOTE:** This filepath is an example from a CentOS system, it will differ on other distros.
- Reboot the system
- To confirm hugepages configured run: `grep Huge /proc/meminfo`

Example Output:
```
AnonHugePages:   2105344 kB
HugePages_Total:      32
HugePages_Free:       30
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:    1048576 kB
```


## Optional configuration

Open vSwitch is highly configurable using `other_config` options as described in [Open vSwitch Manual](http://www.openvswitch.org/support/dist-docs/ovs-vswitchd.conf.db.5.txt).
All of those configs can be configured using simple config file `/opt/ovs-config/config.cfg`. This file is being mounted in `ovs-ovn` pod. It contains list of `other_config` options. Each option should be placed in new line.

Example:

```
dpdk-socket-mem="1024,1024"
dpdk-init=true
pmd-cpu-mask=0x4
dpdk-lcore-mask=0x2
dpdk-hugepage-dir=/dev/hugepages
```

This example config will enable DPDK support with 1024MB of hugepages for both NUMA node 0 and NUMA node 1, PMD CPU mask 0x4, lcore mask 0x2 and hugepages in /dev/hugepages.

If file will not exist upon OVS initialization, the default configuration file will be created with values:

```
dpdk-socket-mem="1024"
dpdk-init=true
dpdk-hugepage-dir=/dev/hugepages
```
>**Note:** Please remember, that if you would like to initialize Open vSwitch with more socket memory than 1024MB, you will have to reserve this memory for `ovs-ovn` pod by editing the value `hugepages-1G` of `ovs-ovn` pod in `install.sh` script. For example, to initialize Open vSwitch using `dpdk-socket-mem="1024,1024"` the minimal value will be `hugepages-1G: 2Gi`.


## To Install

1. Download the installation script:
`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.6/dist/images/install.sh`

2. Use vim to edit the script variables to meet your requirement
```bash
 REGISTRY="index.alauda.cn/alaudak8s"
 NAMESPACE="kube-system"                # The ns to deploy kube-ovn
 POD_CIDR="10.16.0.0/16"                # Do NOT overlap with NODE/SVC/JOIN CIDR
 SVC_CIDR="10.96.0.0/12"                # Do NOT overlap with NODE/POD/JOIN CIDR
 JOIN_CIDR="100.64.0.0/16"              # Do NOT overlap with NODE/POD/SVC CIDR
 LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB
 IFACE=""                               # The nic to support container network, if empty will use the nic that the default route use
 VERSION="v1.1.0"
```

3. Run the installation script making sure to include the flag --with-dpdk= followed by the required DPDK version.
`bash install.sh --with-dpdk=19.11`
>**Note:** Current supported version is DPDK 19.11


## Multus
The DPDK enabled vhost-user sockets provided by OVS-DPDK are not suitable for use as the default network of a Kubernetes pod. We must retain the OVS (kernel) interface provided by Kube-OVN and the DPDK socket(s) must be requested as additional interface(s).

To facilitate multiple network interfaces to a pod we can use the [Multus-CNI](https://github.com/intel/multus-cni/) plugin.
To install Multus follow the [Multus quick start guide](https://github.com/intel/multus-cni/blob/master/doc/quickstart.md#installation). During installation, Multus should detect Kube-OVN has already been installed as the default Kubernetes network plugin and will automatically configure itself so Kube-OVN continues to be the default network plugin for all pods.
> **Note:** Multus determines the existing default network as the lexicographically (alphabetically) first configuration file in the /etc/cni/net.d directory.
> If another plugin has the lexicographically first config file at this location, it will be considered the default network. Rename configuration files accordingly before Multus installation.

With Multus installed, additional Network interfaces can now be requested within a pod spec.



## Userspace CNI
There is now a containerized instance of OVS-DPDK running on the node. Kube-OVN can provide all of its regular (kernal) functionality. Multus is in place to enable pods request the additional OVS-DPDK interfaces. However, OVS-DPDK does provide regular Netdev interfaces, but vhost-user sockets. These sockets cannot be attached to a pod in the usual manner where the Netdev is moved to the pod network namespace. These sockets must be mounted into the pod. Kube-OVN (at least currently) does not have this socket-mounting ability. For this functionality we can use the [Userspace CNI Network Plugin](https://github.com/intel/userspace-cni-network-plugin).


### Download, build and install Userspace CNI
>**Note:** These steps assume Go has already been installed, and the GOPATH env var has been set.
1. `go get github.com/intel/userspace-cni-network-plugin`
2. `cd $GOPATH/src/github.com/intel/userspace-cni-network-plugin`
4. `make clean`
5. `make install`
6. `make`
7. `cp userspace/userspace /opt/cni/bin`


### Userspace Network Attachment Definition
A NetworkAttachmentDefinition is used to represent the network attachments. In this case we need a NAD to represent the network interfaces provided by Userspace CNI, i.e. the OVS-DPDK interfaces. It will then be possible to request this network attachment within a pod spec and Multus will attach these to the pod as secondary interfaces in addition to the preconfigured default network, i.e. the Kube-OVN provided OVS (Kernel) interfaces.

Create the NetworkAttachmentDefinition
```
cat <<EOF | kubectl create -f -
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: ovs-dpdk-br0
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "userspace",
      "name": "ovs-dpdk-br0",
      "kubeconfig": "/etc/cni/net.d/multus.d/multus.kubeconfig",
      "logFile": "/var/log/userspace-ovs-dpdk-br0.log",
      "logLevel": "debug",
      "host": {
        "engine": "ovs-dpdk",
        "iftype": "vhostuser",
        "netType": "bridge",
        "vhost": {
          "mode": "server"
        },
        "bridge": {
          "bridgeName": "br0"
        }
      },
      "container": {
        "engine": "ovs-dpdk",
        "iftype": "vhostuser",
        "netType": "interface",
        "vhost": {
          "mode": "client"
        }
      }
    }
EOF
```
It should now be possible to request the Userspace CNI provided interfaces as annotations within a pod spec. The example below will request two OVS-DPDK interfaces, these will be in addition to the default network.
```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    k8s.v1.cni.cncf.io/networks: ovs-dpdk-br0, ovs-dpdk-br0
```


### Enable Userspace CNI to access containerized OVS-DPDK
Userspace-CNI is intended to run in an environment where OVS-DPDK is installed directly on the host, rather than in a container. Userspace-CNI makes calls to OVS-DPDK using an application called ovs-vsctl. With a containerized OVS-DPDK, this application is no longer available on the host. The following is a workaround to take ovs-vsctl calls made from the host and direct them to the appropriate Kube-OVN container running OVS-DPDK.
```
cat <<'EOF' > /usr/local/bin/ovs-vsctl
#!/bin/bash
ovsCont=$(docker ps | grep kube-ovn | grep ovs-ovn | grep -v pause | awk '{print $1}')
docker exec $ovsCont ovs-vsctl $@
EOF
```
`chmod +x /usr/local/bin/ovs-vsctl`


## CPU Mask
CPU masking is not necessary, but some advanced users may wish to use this feature in OVS-DPDK. When starting OVS-DPDK ovs-vsctl has the ability to configure a CPU mask. This should be used with something like [CPU-Manager-for-Kubernetes](https://github.com/intel/CPU-Manager-for-Kubernetes). Configuration of such a setup is complex and specific to each system. It is out of the scope of this document. Please consult OVS-DPDK and CMK documentation.


# Example DPDK Pod
A sample Kubernetes pod running a DPDK enabled Docker image.


### Dockerfile
Create the Dockerfile, name it Dockerfile.dpdk
```
FROM centos:8

ENV DPDK_VERSION=19.11.1
ENV DPDK_TARGET=x86_64-native-linuxapp-gcc
ENV DPDK_DIR=/usr/src/dpdk-stable-${DPDK_VERSION}

RUN dnf groupinstall -y 'Development Tools'
RUN dnf install -y wget numactl-devel

RUN cd /usr/src/ && \
  wget http://fast.dpdk.org/rel/dpdk-${DPDK_VERSION}.tar.xz && \
  tar xf dpdk-${DPDK_VERSION}.tar.xz && \
  rm -f dpdk-${DPDK_VERSION}.tar.xz && \
  cd ${DPDK_DIR} && \
  sed -i s/CONFIG_RTE_EAL_IGB_UIO=y/CONFIG_RTE_EAL_IGB_UIO=n/ config/common_linux && \
  sed -i s/CONFIG_RTE_LIBRTE_KNI=y/CONFIG_RTE_LIBRTE_KNI=n/ config/common_linux && \
  sed -i s/CONFIG_RTE_KNI_KMOD=y/CONFIG_RTE_KNI_KMOD=n/ config/common_linux && \
  make install T=${DPDK_TARGET} DESTDIR=install
```
Build the Docker image and tag it as dpdk:19.11. This build will take some time.
`docker build -t dpdk:19.11 -f Dockerfile.dpdk .`


### Pod Spec
Create the Pod Spec, name it pod.yaml
```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    k8s.v1.cni.cncf.io/networks: ovs-dpdk-br0, ovs-dpdk-br0
spec:
  containers:
  - name: testpmd-dpdk
    image: dpdk:19.11
    imagePullPolicy: Never
    securityContext:
        privileged: true
    command: ["tail", "-f", "/dev/null"]
    resources:
      requests:
        hugepages-1Gi: 2Gi
        memory: 2Gi
      limits:
        hugepages-1Gi: 2Gi
        memory: 2Gi
    volumeMounts:
    - mountPath: /hugepages
      name: hugepages
    - mountPath: /vhu
      name: vhu
  volumes:
  - name: vhu
    hostPath:
      path: /var/run/openvswitch/
  - name: hugepages
    emptyDir:
      medium: HugePages
  restartPolicy: Never
```
Run the pod.
`kubectl create -f pod.yaml`

The pod will be created with a kernel OVS interface provided by Kube-OVN, as the default network. In addition two secondary interfaces will be available within the pod as socket files located under `/vhu/` .

### Private socket file directory (optional)
The above pod spec will mount the directory `/var/run/openvswitch/` into the pod. This is the default location where OVS-DPDK creates it's socket files, meaning with this configuration all socket files are visible to all pods. It may be desirable to ensure that only the socket files created for a pod are visible within that pod. Userspace-CNI provides the option of mounting a unique directory containing only the relevant socket files.

The pod spec needs to be updated as shown below. The name of the volumeMount needs to be `shared-dir`  and the hostPath needs to be updated to include the unique directory for this pod. In this case we call the unique directory `pod1/`. When this pod is created, a new directory `pod1/` will be created under `/var/run/openvswitch/`. Userspace-CNI will then place only the relevant socket files in this directory and this directory is then mounted into the pod where it will appear as `/vhu/`.

<pre><code>apiVersion: v1
kind: Pod
metadata:
  annotations:
    k8s.v1.cni.cncf.io/networks: ovs-dpdk-br0, ovs-dpdk-br0
spec:
  containers:
  - name: testpmd-dpdk
    image: dpdk:19.11
    imagePullPolicy: Never
    securityContext:
        privileged: true
    command: ["tail", "-f", "/dev/null"]
    resources:
      requests:
        hugepages-1Gi: 2Gi
        memory: 2Gi
      limits:
        hugepages-1Gi: 2Gi
        memory: 2Gi
    volumeMounts:
    - mountPath: /hugepages
      name: hugepages
    - mountPath: /vhu
      <b>name: shared-dir</b>
  volumes:
  <b>- name: shared-dir</b>
    hostPath:
      <b>path: /var/run/openvswitch/pod1/</b>
  - name: hugepages
    emptyDir:
      medium: HugePages
  restartPolicy: Never
</code></pre>

Finally, we need to tell Userspace-CNI where it can find the newly generated socket files, as this default location can be configured and changed. For a Kube-OVN install, this location will be `/var/run/openvswitch/`. This location is provided to Userspace-CNI as an environment variable. Set this environment variable and restart Kubelet:
```
echo "OVS_SOCKDIR=\"/var/run/openvswitch/\"" >> /var/lib/kubelet/kubeadm-flags.env
systemctl daemon-reload && systemctl restart kubelet
```


### TestPMD
To run TestPMD:
`testpmd -m 1024 -c 0xC --file-prefix=testpmd_ --vdev=net_virtio_user0,path=<path-to-socket-file1> --vdev=net_virtio_user1,path=<path-to-socket-file2> --no-pci -- --no-lsc-interrupt --auto-start --tx-first --stats-period 1`
