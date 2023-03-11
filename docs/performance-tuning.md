# Performance Tuning

To keep the simplicity and easy to use, the default installation of Kube-OVN does not contain aggressive performance tuning. 
For applications that are sensitive to network latency, administrators can use the guide below to enhance the network performance.

## Benchmark result

### Comparison between default and optimized Kube-OVN

*Environment*:
- Kubernetes: 1.22.0
- OS: CentOS 7
- Kube-OVN: 1.8.0 *Overlay* mode
- CPU: Intel(R) Xeon(R) E-2278G
- Network: 2*10Gbps, xmit_hash_policy=layer3+4

We use `qperf -t 60 <server ip> -ub -oo msg_size:1 -vu tcp_lat tcp_bw udp_lat udp_bw` to test the small packet with 1byte performance
for tcp/udp latency and throughput and compare with host network performance as baseline.

| Type               | tcp_lat (us) | udp_lat (us) | tcp_bw (Mb/s) | udp_bw(Mb/s) |
| ------------------ | ------------ | ------------ | ------------- | ------------ |
| Kube-OVN Default   | 25.7         | 22.9         | 27.1          | 1.59         |
| Kube-OVN Optimized | 13.9         | 12.9         | 27.6          | 5.57         |
| HOST Network       | 13.1         | 12.4         | 28.2          | 6.02         |

### Comparison between optimized overlay, underlay and Calico

In a different environment set, we compare the performance between optimized Kube-OVN and native Calico with packets size 1, 1k and 4k.
*Environment*:
- Kubernetes: 1.22.0
- OS: CentOS 7
- Kube-OVN: 1.8.0
- CPU: AMD EPYC 7402P 24-Core Processor
- Network: Intel Corporation Ethernet Controller XXV710 for 25GbE SFP28

`qperf -t 60 <server ip> -ub -oo msg_size:1 -vu tcp_lat tcp_bw udp_lat udp_bw`

| Type              | tcp_lat (us) | udp_lat (us) | tcp_bw (Mb/s) | udp_bw(Mb/s) |
| ----------------- | ------------ | ------------ | ------------- | ------------ |
| Kube-OVN Overlay  | 15.2         | 14.6         | 23.6          | 2.65         |
| Kube-OVN Underlay | 14.3         | 13.8         | 24.2          | 3.46         |
| Calico IPIP       | 21.4         | 20.2         | 23.6          | 1.18         |
| Calico NoEncap    | 19.3         | 16.9         | 23.6          | 1.76         |
| HOST Network      | 16.6         | 15.4         | 24.8          | 2.64         |

`qperf -t 60 <server ip> -ub -oo msg_size:1K -vu tcp_lat tcp_bw udp_lat udp_bw`

| Type              | tcp_lat (us) | udp_lat (us) | tcp_bw (Gb/s) | udp_bw(Gb/s) |
| ----------------- | ------------ | ------------ | ------------- | ------------ |
| Kube-OVN Overlay  | 16.5         | 15.8         | 10.2          | 2.77         |
| Kube-OVN Underlay | 15.9         | 14.5         | 9.6           | 3.22         |
| Calico IPIP       | 22.5         | 21.5         | 1.45          | 1.14         |
| Calico NoEncap    | 19.4         | 18.3         | 3.76          | 1.63         |
| HOST Network      | 18.1         | 16.6         | 9.32          | 2.66         |

`qperf -t 60 <server ip> -ub -oo msg_size:4K -vu tcp_lat tcp_bw udp_lat udp_bw`

| Type              | tcp_lat (us) | udp_lat (us) | tcp_bw (Gb/s) | udp_bw(Gb/s) |
| ----------------- | ------------ | ------------ | ------------- | ------------ |
| Kube-OVN Overlay  | 34.7         | 41.6         | 16.0          | 9.23         |
| Kube-OVN Underlay | 32.6         | 44           | 15.1          | 6.71         |
| Calico IPIP       | 44.8         | 52.9         | 2.94          | 3.26         |
| Calico NoEncap    | 40           | 49.6         | 6.56          | 4.19         |
| HOST Network      | 35.9         | 45.9         | 14.6          | 5.59         |

This benchmark is for reference only, the result may vary dramatically due to different hardware and software setups. 
Optimization for packets with big size and underlay latency are still in progress, we will publish the optimization 
methods and benchmark results later.

## Optimization methods

## CPU frequency scaling

When the CPU works at power save mode, its performance behavior is unstable and various. Use the performance mode to get stable behavior.

```bash
cpupower frequency-set -g performance
```

## Increasing the RX ring buffer 

A high drop rate at receive side will hurt the throughput performance.

1. Check the maximum RX ring buffer size:
```bash
# ethtool -g eno1
 Ring parameters for eno1:
 Pre-set maximums:
 RX:             4096
 RX Mini:        0
 RX Jumbo:       0
 TX:             4096
 Current hardware settings:
 RX:             255
 RX Mini:        0
 RX Jumbo:       0
 TX:             255
```
2. increase RX ring buffer:
```bash
# ethtool -G eno1 rx 4096
```

## Disable OVN LB

With profile, the OVN L2 LB need packet clone and recirculate which consumes lots CPU time and block every traffic path 
even if the packets are not designated to a LB VIP. From v1.8 the OVN LB can be disabled, for overlay network traffic to 
Service IP will use kube-proxy function to distribute the packets. You need to modify the `kube-ovn-controller` cmd args:

```yaml
command:
- /kube-ovn/start-controller.sh
args:
...
- --enable-lb=false
...
```

This can reduce about 30% of the cpu time and latency in 1byte packet test.

*Note*: In underlay mode, kube-proxy can not capture underlay traffic, if disable lb, svc can not be visited.

*Need Kube-OVN >= 1.9.0*.
If you are using underlay mode network and need kube-ovn to implement the svc function, you can set the svc cidr in ovn-nb
to bypass the conntrack system for traffic that not designate to svc.

```bash
kubectl ko nbctl set nb_global . options:svc_ipv4_cidr=10.244.0.0/16
```

## Kernel FastPath module

With Profile, the netfilter hooks inside container netns and between tunnel endpoints contribute about 25% of the CPU time
after the optimization of disabling lb. We provide a FastPath module that can bypass the nf hooks process and reduce about 
another 20% of CPU time and latency in 1 byte packet test.

The easiest way is to download the `.ko` file and `insmod` the file as [doc](https://github.com/kubeovn/tunning-package) introduce. 
The `ko` file currently support a limited number of kernel versions. However, we encourage users to submit requests, and we will support new versions soon.

If you want to compile yourself, please refer [FastPath Guide](../fastpath/README.md) to get the detail steps.


## Optimize OVS kernel module

### Download a ready-made package

Also, the easiest way is to download the ovs-kernel file and install the file as [doc](https://github.com/kubeovn/tunning-package) introduce. Detail information could be found in repo `Kube-OVN/tunning-packages`.

However, also currently the packages support a limited number of kernel versions. So we encourage users to submit requests.

You also could compile the package manually or automatically according to the following entries.

After installing the ovs kernel modules, enable the ovs stt configuration to complete the optimisation.

### manual compile

The OVS flow match process takes about 10% of the CPU time. If the OVS runs on x86 CPU with popcnt and sse instruction sets, some
compile optimization can be applied to accelerate the match process.

1. ##### Make sure the CPU support the instruction set
```bash
# cat /proc/cpuinfo  | grep popcnt
# cat /proc/cpuinfo  | grep sse4_2
```
2. ##### Compile and install the OVS kernel module.

   Steps to compile the module in CentOS:
```bash
# Make sure the related kernel headers is installed
yum install -y gcc kernel-devel-$(uname -r) python3 autoconf automake libtool rpm-build openssl-devel

git clone -b branch-2.17 --depth=1 https://github.com/openvswitch/ovs.git
cd ovs
curl -s  https://github.com/kubeovn/ovs/commit/2d2c83c26d4217446918f39d5cd5838e9ac27b32.patch |  git apply
./boot.sh
./configure --with-linux=/lib/modules/$(uname -r)/build CFLAGS="-g -O2 -mpopcnt -msse4.2"
make rpm-fedora-kmod
cd rpm/rpmbuild/RPMS/x86_64/

# Copy the rpm to every node and install
rpm -i openvswitch-kmod-2.15.2-1.el7.x86_64.rpm
```

​	 Steps to compile the modules in Ubuntu:

```bash
apt install -y autoconf automake libtool gcc build-essential libssl-dev

git clone -b branch-2.17 --depth=1 https://github.com/openvswitch/ovs.git
cd ovs
curl -s  https://github.com/kubeovn/ovs/commit/2d2c83c26d4217446918f39d5cd5838e9ac27b32.patch |  git apply
./boot.sh
./configure --prefix=/usr/ --localstatedir=/var --enable-ssl --with-linux=/lib/modules/$(uname -r)/build
make -j `nproc`
make install
make modules_install

cat > /etc/depmod.d/openvswitch.conf << EOF
override openvswitch * extra
override vport-* * extra
EOF

depmod -a
cp debian/openvswitch-switch.init /etc/init.d/openvswitch-switch
/etc/init.d/openvswitch-switch force-reload-kmod
```

### Automatically compile openvswitch rpm and distribute it:

```bash
# for centos7
$ kubectl ko tuning install-stt centos7
# for centos8
$ kubectl ko tuning install-stt centos8
```

​	A container will run and compile openvswitch rpm package. the rpm file will be automatically distributed to `/tmp/` of each nodes

​	***Optional*** if on your system, you **can not** install the kernel-devel package that matches your system version via the package management tool(yum or apt), then you will have to download the version-compatible package yourself.

If the following command fails, then you will need to download the package yourself.

```bash
$ yum install -y kernel-devel-$(uname -r)
```

​	Here are a few sites where you may find packages: [cern](https://linuxsoft.cern.ch/cern/centos/7/updates/x86_64/repoview/kernel-devel.html), [riken](http://ftp.riken.jp/Linux/cern/centos/7/updates/x86_64/repoview/kernel-devel.html). 

​	You are welcome to add new download sites.

​	Once downloaded, please move the download package to the **/tmp/** folder and then install it as follows:

```bash
# for centos7
$ kubectl ko tuning local-install-stt centos7 kernel-devel-$(uname -r)
# for centos8
$ kubectl ko tuning local-install-stt centos8 kernel-devel-$(uname -r)
```

​	After the  distribution, the user needs to install the rpm file on each node and safely **restart the node** . 

```bash
$ rpm -i openvswitch-kmod-2.15.2-1.el7.x86_64.rpm
# Note a safe reboot of master nodes
$ reboot
```

​	Remove the rpm files from `/tmp/` of each node

```bash
$ kubectl ko tuning remove-stt centos
```

### Using STT tunnel type

Popular tunnel encapsulation methods like Geneve or Vxlan use udp to wrap the origin packets. 
However, when using udp over tcp packets, lots of the tcp offloading capabilities that modern NICs provided cannot be utilized 
and leads to degraded performance compared to non-encapsulated one.

STT provides a tcp like header to encapsulate packet which can utilize all the offloading capabilities and dramatically improve the throughput of tcp traffic.
Unfortunately, this tunnel type is not embedded in kernel, you have to compile OVS kernel module as above to use STT.

```bash
kubectl set env daemonset/ovs-ovn -n kube-system TUNNEL_TYPE=stt

kubectl -n kube-system rollout restart ds ovs-ovn
```
