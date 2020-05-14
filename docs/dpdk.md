# Kube-OVN with OVS-DPDK

This document describes how to run Kube-OVN with OVS-DPDK.


## Prerequest
- Kubernetes >= 1.11
- Docker >= 1.12.6
- OS: CentOS 7.5/7.6/7.7, Ubuntu 16.04/18.04


## To Install

<!---
NOTE: Once  PR is merged, it should no longer be necessary to clone the repo.
It should be possible to wget and run the install script as described in the Kube-OVN install document:
https://github.com/alauda/kube-ovn/blob/master/docs/install.md

TODO: Update once PR is merged.
-->

1. Clone the Kube-OVN repo
`git clone https://github.com/alauda/kube-ovn.git`

2. Navigate to the directory containing the install script
`cd kube-ovn/dist/images/`

3. Run the install script making sure to include the flag --with-dpdk= followed by the required DPDK version.  
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
>**Note:** These steps assume Go is already installed and the GOPATH env var is set.
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
  generateName: testpmd-dpdk-
  annotations:
    k8s.v1.cni.cncf.io/networks: ovs-dpdk-br0, ovs-dpdk-br0
spec:
  tolerations:
  - operator: "Exists"
    key: cmk
  containers:
  - name: testpmd-dpdk
    image: dpdk:19.11
    resources:
      requests:
        hugepages-1Gi: 2Gi
        memory: 2Gi
      limits:
        hugepages-1Gi: 2Gi
        memory: 2Gi
    command: ["tail", "-f", "/dev/null"]
    volumeMounts:
    - mountPath: /hugepages
      name: hugepages
    - mountPath: /vhu
      name: vhu
    securityContext:
        privileged: true
        runAsUser: 0
  volumes:
  - name: hugepages
    emptyDir:
      medium: HugePages
  - name: vhu
    hostPath:
      path: /var/run/openvswitch
  securityContext:
    runAsUser: 0
  restartPolicy: Never
```
Run the pod.
`kubectl create -f pod.yaml`

The pod will be created with a kernel OVS interface provided by Kube-OVN, as the default network. In addition two secondary interfaces will be available within the pod as socket files located under /vhu/ .

### TestPMD
Steps to use the DPDK test application TestPMD appear to have changed slightly with the latest versions of OVS and DPDK. Updated instructions to follow shortly.
