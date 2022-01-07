# Kube-OVN FastPath Module

*NOTE*: This FastPath module relies on netfilter `NF_STOP` action, thus can ONLY work for 3.x kernel. 
For 4.x kernel please use [FastPath for 4.18](4.18) to compile the module.

After the datapath performance profile, the Netfilter hooks inside container netns and between tunnel endpoints contribute
a large portion of the CPU time. This Kube-OVN FastPath module can help bypass the unnecessary Netfilter hooks to reduce 
the latency and CPU and improve the throughput at the same time. With this FastPath module, about 20% latency between 
pods that resident on different nodes can be reduced.

## Manual install

As kernel modules must be compiled with related kernel headers, users must compile their module with related kernel headers.

Below is the step to compile the module on CentOS

1. Install the requirement, make sure the kernel-devel version is equal to the one used on Kubernetes nodes
```bash
yum install -y kernel-devel-$(uname -r) gcc elfutils-libelf-devel
```

2. Build the module
```bash
make all
```

3. Copy the module file `kube_ovn_fastpath.ko` to Kubernetes nodes and install
```bash
insmod kube_ovn_fastpath.ko
```

4. Use `dmesg` to verify install success
```bash
[619631.323788] init_module,kube_ovn_fastpath_local_out
[619631.323798] init_module,kube_ovn_fastpath_post_routing
[619631.323800] init_module,kube_ovn_fastpath_pre_routing
[619631.323801] init_module,kube_ovn_fastpath_local_in
```

5. Remove the module
```bash
rmmod kube_ovn_fastpath.ko
```

*NOTE*: You also need to add config to load the module at boot time to survive a reboot.

## Automatic install (Experimental)

There are two ways to deploy kernel optimization modules. 

1. Automatic installation using kubectl-ko plugin

   kubectl ko plugin is available for current standard CentOS/Ubuntu distributions. The ability to specify Header-devel version is also supported by kubectl ko plugin.

   This approach will run a docker in local host node to complete the compliation, and then distributes the compiled optimization components to all nodes.  Ovs-cni will monitor and deploy the optimization components.

   1. The default installation for current standard CentOS/Ubuntu distributions is as follows:
   
   ```bash
   # for centos7
   $ kubectl ko tuning install-fastpath centos7
   # for centos8
   $ kubectl ko tuning install-fastpath centos8
   ```
   After command's execution, you can check the logs to see if it has been installed correctly.
   
   ```bash
   $ dmesg | tail -n 100 | grep kube_ovn
   ***
   [619631.323788] init_module,kube_ovn_fastpath_local_out
   [619631.323798] init_module,kube_ovn_fastpath_post_routing
   [619631.323800] init_module,kube_ovn_fastpath_pre_routing
   [619631.323801] init_module,kube_ovn_fastpath_local_in
   *** 
   ```
   
   
   
   2. ***Optional*** if on your system, you **can not** install the kernel-devel package that matches your system version via the package management tool(yum or apt), then you will have to download the version-compatible package yourself.
   
      If the following command fails, then you will need to download the package yourself.
   
      ```bash
      $ yum install -y kernel-devel-$(uname -r)
      ```
   
      Here are a few sites where you may find packages: [cern](https://linuxsoft.cern.ch/cern/centos/7/updates/x86_64/repoview/kernel-devel.html), [riken](http://ftp.riken.jp/Linux/cern/centos/7/updates/x86_64/repoview/kernel-devel.html). 
   
      You are welcome to add new download sites.
   
      Once downloaded, please move the download package to the **/tmp/** folder and then install it as follows:
   
      ```bash
      # for centos7
      $ kubectl ko tuning local-install-fastpath centos7 kernel-devel-$(uname -r)
      # for centos8
      $ kubectl ko tuning local-install-fastpath centos8 kernel-devel-$(uname -r)
      ```
   
      
   
   3. Remove the kernel optimization modules
   
   ```bash
   $ kubectl ko tuning remove-fastpath centos
   ```



2. automatic installation with flexible modification capabilities

   We also offer a more flexible approach, but we strongly recommand that the user knows exactly what he is doing.

   This approach will perform compliation and installation on each node based on the K8s Job scheduling.

   Go to the directory `~/fastpath/`, modify the parameters ACTION and KERNEL_HEADER as required, then execute the script.

   For the scenario where kernel-devel package need to be downloaded, the default requirement is to put the package under directory `/tmp/` on each node. However, a better way is to mount the package to the Job via PVC, but due to the different implementations, we recommend that the user configure this themselves rather than in our default configuration.

   The rendered Yaml file, including the commands executed within each node, is stored in `/tmp/`. Please feel free to modify the configuration as needed before deployment.