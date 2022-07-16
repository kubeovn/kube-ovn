# Windows Support

Since version 1.10, Kube-OVN supports running on a cluster with a mixture of Linux and Windows nodes.

## Known Limitations

1. Kube-OVN must be installed without SSL, so you need to set `ENABLE_SSL` to `false` if you are using installation script;
2. `kube-proxy` is not supported currently, so Kubernetes services cannot be accessed from or through Windows nodes;
3. Neither single-stack "IPv6-only" networking nor dual-stack IPv4/IPv6 networking is supported;
4. Pod/Workload QoS is not supported;
5. Windows nodes don't support dynamic subnet creation/deletion, you must have all subnets created before deploying Kube-OVN on the Windows nodes;
6. Windows nodes don't support multiple provider networks, the only provider network MUST be name `provider` if you are using underlay networking;
7. Windows nodes don't support dynamic tunnel interface configuration;
8. Windows nodes don't support dynamic provider interface configuration.

## Prerequisites

1. Kubernetes server must be at or later than version 1.17;
2. Only Windows Server 2019 with 64-bit x86_64 architecture is supported;
3. Obtain a Windows Server 2019 license (or higher) in order to configure the Windows node that hosts Windows containers;
4. You must have KB4489899 installed to make VXLAN/Overlay networking work;
5. Install Hyper-V with management tools on the Windows node.

## Installation

### Joining a Windows Worker Node

#### Install Container Runtime

> Currently only Docker Engine is supported.

Install the Containers feature by running the following command in PowerShell:

```ps1
Install-WindowsFeature -Name containers
```

Install Docker following the official document - [Install Docker Engine - Enterprise on Windows Servers](https://docs.microsoft.com/en-us/virtualization/windowscontainers/quick-start/set-up-environment?tabs=Windows-Server#install-docker).

#### Install kubeadm and kubelet

Run [PrepareNode.ps1](../dist/windows/PrepareNode.ps1) on the Windows node to install `kubeadm` and `kubelet`. Here is an example:

```ps1
.\PrepareNode.ps1 -KubernetesVersion v1.22.9 -ContainerRuntime Docker
```

#### Join the Windows Node

Use the command that was given to you when you ran kubeadm init on a control plane host. If you no longer have this command, or the token has expired, you can run kubeadm token create --print-join-command (on a control plane host) to generate a new token and join command.

Then you should be able to view the Windows node in your cluster by running:

```sh
kubectl get nodes -o wide
```

### Install Kube-OVN

Download Windows package and extract it.

#### Install Open vSwitch for Hyper-V

Turn on `TESTSIGNING` boot option or `Disable Driver Signature Enforcement` during boot. The following commands can be used:

```ps1
bcdedit /set LOADOPTIONS DISABLE_INTEGRITY_CHECKS
bcdedit /set TESTSIGNING ON
bcdedit /set nointegritychecks ON
```

Then you can open the OpenvSwitch.msi to install Open vSwitch.

You can verify the installation by querying the service status:

```ps1
PS > Get-Service | findstr ovs
Running  ovsdb-server  Open vSwitch DB Service
Running  ovs-vswitchd  Open vSwitch Service
```

#### Install OVN and Kube-OVN

Execute the `install.ps1` to install OVN and Kube-OVN on the Windows node. Here is an example:

```ps1
.\install.ps1 -KubeConfig C:\k\admin.conf -ApiServer https://192.168.140.180:6443 -ServiceCIDR 10.96.0.0/12
```

For more available parameters, please refer [install.ps1](../dist/windows/install.ps1).

By default, Kube-OVN uses the network adapter hosting the node IP as the tunnel interface. If you want to use a custom adapter, you can add a node annotation, such as `ovn.kubernetes.io/tunnel_interface=Ethernet1`, before installing Kube-OVN.

## Known Issues

1. Hyper-V cannot be installed on the Windows node due to the lack of virtualization capabilities required by Hyper-V. You can install Hyper-V with the following commands:

```ps1
Install-WindowsFeature containers
Install-WindowsFeature Hyper-V-Powershell
dism /online /enable-feature /featurename:Microsoft-Hyper-V /all /NoRestart
dism /online /disable-feature /featurename:Microsoft-Hyper-V-Online /NoRestart
Restart-Computer
```

1. Pod running on a Windows node is stuck at `Terminating` state and cannot be deleted. You can reboot the Windows nodes to solve the problem.
