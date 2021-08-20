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
| ------------------ | -------------| -------------| --------------| -------------|
| Kube-OVN Default   | 25.7         | 22.9         | 27.1          | 1.59         |
| Kube-OVN Optimized | 13.9         | 12.9         | 27.6          | 5.57         |
| HOST Network       | 13.1         | 12.4         | 28.2          | 6.02         |

### Comparison between optimized overlay, underlay and Calico

In a different environment set, we compare the performance between optimized Kube-OVN and native Calico with packets size 1, 1k and 4k.
*Environment*:
- Kubernetes: 1.22.0
- OS: CentOS 7
- Kube-OVN: 1.8.0
- CPU: AMD EPYC 7401P 24-Core Processor
- Network: 2*10Gbps, xmit_hash_policy=layer3+4

`qperf -t 60 <server ip> -ub -oo msg_size:1 -vu tcp_lat tcp_bw udp_lat udp_bw`

| Type               | tcp_lat (us) | udp_lat (us) | tcp_bw (Mb/s) | udp_bw(Mb/s) |
| ------------------ | -------------| -------------| --------------| -------------|
| Kube-OVN Overlay   | 18.5         | 18           | 20.4          | 3.02         |
| Kube-OVN Underlay  | 18.5         | 16.3         | 20.7          | 3.92         |
| Calico IPIP        | 26.8         | 25.2         | 20.4          | 1.15         |
| Calico NoEncap     | 25.5         | 21.7         | 20.6          | 1.6         |
| HOST Network       | 20.3         | 17.8         | 20.9          | 3.4          |

`qperf -t 60 <server ip> -ub -oo msg_size:1K -vu tcp_lat tcp_bw udp_lat udp_bw`

| Type               | tcp_lat (us) | udp_lat (us) | tcp_bw (Gb/s) | udp_bw(Gb/s) |
| ------------------ | -------------| -------------| --------------| -------------|
| Kube-OVN Overlay   | 28.2         | 28.1         | 6.24          | 2.92         |
| Kube-OVN Underlay  | 28           | 27.1         | 8.95          | 3.66         |
| Calico IPIP        | 28.9         | 28.7         | 1.36          | 1.12         |
| Calico NoEncap     | 28.9         | 27.2         | 8.38          | 1.76         |
| HOST Network       | 28.5         | 27.1         | 9.34          | 3.2          |

`qperf -t 60 <server ip> -ub -oo msg_size:4K -vu tcp_lat tcp_bw udp_lat udp_bw`

| Type               | tcp_lat (us) | udp_lat (us) | tcp_bw (Gb/s) | udp_bw(Gb/s) |
| ------------------ | -------------| -------------| --------------| -------------|
| Kube-OVN Overlay   | 85.1         | 69.3         | 7.2           | 7.41         |
| Kube-OVN Underlay  | 52.7         | 59.5         | 9.2           | 11.8         |
| Calico IPIP        | 61.6         | 73.2         | 3.06          | 3.27         |
| Calico NoEncap     | 68.7         | 76.4         | 8.53          | 4.08         |
| HOST Network       | 54           | 56.1         | 9.35          | 10.3         |

This benchmark is for reference only, the result may vary dramatically due to different hardware and software setups. 
Optimization for packets with big size and underlay latency are still in progress, we will publish the optimization 
methods and benchmark results later.

## Optimization methods

### CPU frequency scaling

When the CPU works at power save mode, its performance behavior is unstable and various. Use the performance mode to get stable behavior.

```bash
cpupower frequency-set -g performance
```

### Increasing the RX ring buffer 

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

### Enable the checksum offload

In v1.7 to bypass a kernel double nat issue, we disabled the tx checksum offload of geneve tunnel. This issue has been resolved
in a different way in the latest version. Users who update from old versions can enable the checksum again. Users who use
1.7.2 or later do not need to change the settings.

```bash
# run on every ovs-ovn pod

# ethtool -K genev_sys_6081 tx on
# ethtool -K ovn0 tx on
# ovs-vsctl set open . external_ids:ovn-encap-csum=true
```

### Disable OVN LB

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

### Kernel FastPath module

With Profile, the netfilter hooks inside container netns and between tunnel endpoints contribute about 25% of the CPU time
after the optimization of disabling lb. We provide a FastPath module that can bypass the nf hooks process and reduce about 
another 20% of CPU time and latency in 1 byte packet test.

Please refer [FastPath Guide](../fastpath/README.md) to get the detail steps.

### Optimize OVS kernel module

The OVS flow match process takes about 10% of the CPU time. If the OVS runs on x86 CPU with popcnt and sse instruction sets, some
compile optimization can be applied to accelerate the match process.

1. Make sure the CPU support the instruction set
```bash
# cat /proc/cpuinfo  | grep popcnt
# cat /proc/cpuinfo  | grep sse4_2
```
2. Compile and install the OVS kernel module, below are the steps to compile the module on CentOS
```bash
# Make sure the related kernel headers is installed
yum install -y gcc kernel-devel-$(uname -r) python3 autoconf automake libtool rpm-build openssl-devel

git clone -b branch-2.15 --depth=1 https://github.com/openvswitch/ovs.git
cd ovs
./boot.sh
./configure --with-linux=/lib/modules/$(uname -r)/build CFLAGS="-g -O2 -mpopcnt -msse4.2"
make rpm-fedora-kmod
cd rpm/rpmbuild/RPMS/x86_64/

# Copy the rpm to every node and install
rpm -i openvswitch-kmod-2.15.2-1.el7.x86_64.rpm
```
