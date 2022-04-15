## Source identification for iptables masquerade in Gateway mode

In gateway mode, there may be scenarios where mutilple pods on the same node connect to a service outside the kubernetes cluster. It is not easy for user to  identify the data streams from different pods. This doc describe the solution to this problem for Kube-ovn cluster.



### Data stream distinguishing

Linux `conntrack` could be introduced in to list the connection from pods to the external service. 
The packets destined to our own cluster components should be excluded from the enumeration, as this example:

##### Example 0: watching the upcoming connection

```bash
# Kube-ovn is deployed as following:

$ kubectl get subnets
NAME          PROVIDER   VPC           PROTOCOL   CIDR             PRIVATE   NAT     DEFAULT   GATEWAYTYPE   V4USED   V4AVAILABLE   V6USED   V6AVAILABLE   EXCLUDEIPS
central       ovn        ovn-cluster   IPv4       192.101.0.0/16   false     true    false     distributed   0        65533         0        0             ["192.101.0.1"]
join          ovn        ovn-cluster   IPv4       100.64.0.0/16    false     false   false     distributed   3        65530         0        0             ["100.64.0.1"]
ovn-default   ovn        ovn-cluster   IPv4       10.16.0.0/16     false     true    true      centralized   19       65514         0        0             ["10.16.0.1"]
snat          ovn        ovn-cluster   IPv4       172.22.0.0/16    false     true    false     distributed   0        65533         0        0             ["172.22.0.1"]

$ kubectl get nodes -o wide
NAME                              STATUS   ROLES                  AGE   VERSION   INTERNAL-IP       EXTERNAL-IP   OS-IMAGE                KERNEL-VERSION                CONTAINER-RUNTIME
ovn1-192.168.137.176   Ready    control-plane,master   75d   v1.22.0   192.168.137.176   <none>        CentOS Linux 7 (Core)   3.10.0-1160.11.1.el7.x86_64   containerd://1.4.3
ovn2-192.168.137.177   Ready    control-plane,master   75d   v1.22.0   192.168.137.177   <none>        CentOS Linux 7 (Core)   3.10.0-1160.11.1.el7.x86_64   containerd://1.4.3
ovn3-192.168.137.178   Ready    control-plane,master   75d   v1.22.0   192.168.137.178   <none>        CentOS Linux 7 (Core)   3.10.0-1160.11.1.el7.x86_64   containerd://1.4.3

$ cat /etc/kubernetes/manifests/kube-apiserver.yaml  | grep service-cluster
    - --service-cluster-ip-range=10.96.0.0/12

# So when filtering, CIDR of pods should be specified and CIDRs in the cluster should be excluded
$ conntrack -E --event-mask NEW  | grep -E "src=10.16|src=172.22|src=192.101" | grep -v "dst=10.16\|dst=172.22\|dst=192.101\|dst=192.168\|dst=100.64\|dst=10.96"
    [NEW] icmp     1 30 src=10.16.0.4 dst=114.114.114.114 type=8 code=0 id=20879 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=20879
    [NEW] icmp     1 30 src=10.16.0.7 dst=114.114.114.114 type=8 code=0 id=14461 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=14461
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.6 dst=192.168.144.9 sport=45332 dport=1194 [UNREPLIED] src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=45332
    [NEW] icmp     1 30 src=10.16.0.6 dst=114.114.114.114 type=8 code=0 id=14797 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=14797
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.6 dst=192.168.144.9 sport=45342 dport=1194 [UNREPLIED] src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=45342
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.6 dst=192.168.144.9 sport=45354 dport=1194 [UNREPLIED] src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=45354
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.3 dst=114.114.114.114 sport=49720 dport=53 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 sport=53 dport=49720
    [NEW] icmp     1 30 src=10.16.0.4 dst=114.114.114.114 type=8 code=0 id=24077 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=24077
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.26 dst=192.168.144.9 sport=52646 dport=1194 [UNREPLIED] src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=52646
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.26 dst=192.168.144.9 sport=52656 dport=1194 [UNREPLIED] src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=52656
    [NEW] tcp      6 120 SYN_SENT src=10.16.0.26 dst=192.168.144.9 sport=52664 dport=1194 [UNREPLIED] src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=52664
    [NEW] icmp     1 30 src=10.16.0.7 dst=114.114.114.114 type=8 code=0 id=7005 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=7005
    [NEW] udp      17 30 src=10.16.0.3 dst=114.114.114.114 sport=49843 dport=53 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 sport=53 dport=49843
    [NEW] icmp     1 30 src=10.16.0.6 dst=114.114.114.114 type=8 code=0 id=27614 [UNREPLIED] src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=27614
```

In this example, we list all NEW tcp connections, and obviously, the pod IP and the source port of connection correspond to each other. So you can identify the pod IP from the source port.
So you only need to check the source port of the TCP connection on the service side to identify which pod the connection is coming from.



##### Example 1: checking the current connection

```bash
# So when filtering, CIDR of pods should be specified and CIDRs in the cluster should be excluded
$ conntrack -L | grep -E "src=10.16|src=172.22|src=192.101" | grep -v "dst=10.16\|dst=172.22\|dst=192.101\|dst=192.168\|dst=100.64\|dst=10.96"
tcp      6 93 TIME_WAIT src=10.16.0.6 dst=192.168.144.9 sport=52422 dport=1194 src=192.168.144.9 dst=192.168.137.176 sport=1194 dport=52422 [ASSURED] mark=0 use=1
conntrack v1.4.4 (conntrack-tools): 7523 flow entries have been shown.
udp      17 179 src=10.16.0.3 dst=114.114.114.114 sport=49655 dport=53 src=114.114.114.114 dst=192.168.137.176 sport=53 dport=49655 [ASSURED] mark=0 use=1
icmp     1 23 src=10.16.0.7 dst=114.114.114.114 type=8 code=0 id=2219 src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=2219 mark=0 use=1
icmp     1 13 src=10.16.0.6 dst=114.114.114.114 type=8 code=0 id=31293 src=114.114.114.114 dst=192.168.137.176 type=0 code=0 id=31293 mark=0 use=1
conntrack v1.4.4 (conntrack-tools): 7532 flow entries have been shown.
```

As in example 0, the source port of the connection could be used to identify the pod IP.