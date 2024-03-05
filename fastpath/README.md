# Kube-OVN FastPath Module

With this FastPath module, about 20% latency between Pods that resident on different Nodes can be reduced.
After the datapath performance profile, the Netfilter hooks inside container netns and between tunnel endpoints contribute
a large portion of the CPU time. This Kube-OVN FastPath module can help bypass the unnecessary Netfilter hooks to reduce 
the latency and CPU and improve the throughput at the same time.

## How to

As kernel modules must be compiled with related kernel headers, users must compile their module with related kernel headers.

1. Install the requirements, make sure the kernel-header version is equal to the one used on Kubernetes nodes

On RPM related OS:

```bash
yum install -y kernel-devel-$(uname -r) gcc elfutils-libelf-devel
```

On DEB related OS:

```bash
apt install linux-headers-$(uname -r) make gcc-12
```

2. Build the module, for 3.x kernel go to the `3.x` dir, for 4.x ~ 6.x kernel go to the `4.x-6.x` dir
   
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
