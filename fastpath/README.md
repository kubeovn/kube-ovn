# Kube-OVN FastPath Module

*NOTE*: This FastPath module relies on netfilter `NF_STOP` action, thus can ONLY work for 3.x kernel. FastPath module for 4.x kernel is in progress.

After the datapath performance profile, the Netfilter hooks inside container netns and between tunnel endpoints contribute
a large portion of the CPU time. This Kube-OVN FastPath module can help bypass the unnecessary Netfilter hooks to reduce 
the latency and CPU and improve the throughput at the same time. With this FastPath module, about 20% latency between 
pods that resident on different nodes can be reduced.

## How to install

As kernel modules must be compiled with related kernel headers, users must compile their module with related kernel headers.

Below is the step to compile the module on CentOS

1. Install the requirement, make sure the kernel-devel version is equal to the one used on Kubernetes nodes
```bash
yum install -y kernel-devel-$(uname -r) gcc
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
