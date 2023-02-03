Kube-OVN provides a kubectl plugin to help better diagnose container network. You can use this plugin to tcpdump a specific pod, trace a specific packet or query ovn-nb/ovn-sb.

# Prerequisite

To enable kubectl plugin, kubectl version of 1.12 or later is recommended. You can use `kubectl version` to check the version.

# Install

1. Get the `kubectl-ko` file

```bash
wget https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.10/dist/images/kubectl-ko
```

2. Move the file to one of $PATH directories

```bash
mv kubectl-ko /usr/local/bin/kubectl-ko
```

3. Add executable permission to `kubectl-ko`

```bash
chmod +x /usr/local/bin/kubectl-ko
```

4. Check if the plugin is ready

```shell
[root@kube-ovn01 ~]# kubectl plugin list
The following compatible plugins are available:

/usr/local/bin/kubectl-ko
```

# Usage

```text
kubectl ko {subcommand} [option...]
Available Subcommands:
  [nb|sb] [status|kick|backup|dbstatus|restore]     ovn-db operations show cluster status, kick stale server, backup database, get db consistency status or restore ovn nb db when met 'inconsistent data' error
  nbctl [ovn-nbctl options ...]    invoke ovn-nbctl
  sbctl [ovn-sbctl options ...]    invoke ovn-sbctl
  vsctl {nodeName} [ovs-vsctl options ...]   invoke ovs-vsctl on the specified node
  ofctl {nodeName} [ovs-ofctl options ...]   invoke ovs-ofctl on the specified node
  dpctl {nodeName} [ovs-dpctl options ...]   invoke ovs-dpctl on the specified node
  appctl {nodeName} [ovs-appctl options ...]   invoke ovs-appctl on the specified node
  tcpdump {namespace/podname} [tcpdump options ...]     capture pod traffic
  trace {namespace/podname} {target ip address} [target mac address] {icmp|tcp|udp} [target tcp or udp port]    trace ovn microflow of specific packet
  diagnose {all|node} [nodename]    diagnose connectivity of all nodes or a specific node
  env-check check the environment configuration
  tuning {install-fastpath|local-install-fastpath|remove-fastpath|install-stt|local-install-stt|remove-stt} {centos7|centos8}} [kernel-devel-version]  deploy  kernel optimisation components to the system
  reload restart all kube-ovn components
```

1. Show ovn-sb overview

```shell
[root@node2 ~]# kubectl ko sbctl show
Chassis "36f129a9-276f-4d96-964b-7d3703001b81"
    hostname: "node1.cluster.local"
    Encap geneve
        ip: "10.0.129.96"
        options: {csum="true"}
    Port_Binding "tiller-deploy-849b7c6496-5l9r6.kube-system"
    Port_Binding "kube-ovn-pinger-5mq4g.kube-ovn"
    Port_Binding "nginx-6b4b85b77b-rk9tq.acl"
    Port_Binding "node-node1"
    Port_Binding "piquant-magpie-nginx-ingress-default-backend-84776f949b-jthhh.kube-system"
    Port_Binding "ds1-l6n7p.default"
Chassis "9ced77f4-dae4-4e0b-b3fe-15dd82104e67"
    hostname: "node2.cluster.local"
    Encap geneve
        ip: "10.0.128.15"
        options: {csum="true"}
    Port_Binding "ds1-wqpdz.default"
    Port_Binding "node-node2"
    Port_Binding "kube-ovn-pinger-8xhhv.kube-ovn"
Chassis "dc922a96-97d4-418d-a45f-8989d2b6dc91"
    hostname: "node3.cluster.local"
    Encap geneve
        ip: "10.0.128.35"
        options: {csum="true"}
    Port_Binding "ds1-dflpx.default"
    Port_Binding "coredns-585c7897d4-59xkc.kube-system"
    Port_Binding "node-node3"
    Port_Binding "kube-ovn-pinger-gc8l6.kube-ovn"
    Port_Binding "coredns-585c7897d4-7dglw.kube-system"
```

2. Dump pod ICMP traffic

```shell
[root@node2 ~]# kubectl ko tcpdump default/ds1-l6n7p icmp
+ kubectl exec -it kube-ovn-cni-wlg4s -n kube-ovn -- tcpdump -nn -i d7176fe7b4e0_h icmp
tcpdump: verbose output suppressed, use -v or -vv for full protocol decode
listening on d7176fe7b4e0_h, link-type EN10MB (Ethernet), capture size 262144 bytes
06:52:36.619688 IP 100.64.0.3 > 10.16.0.4: ICMP echo request, id 2, seq 1, length 64
06:52:36.619746 IP 10.16.0.4 > 100.64.0.3: ICMP echo reply, id 2, seq 1, length 64
06:52:37.619588 IP 100.64.0.3 > 10.16.0.4: ICMP echo request, id 2, seq 2, length 64
06:52:37.619630 IP 10.16.0.4 > 100.64.0.3: ICMP echo reply, id 2, seq 2, length 64
06:52:38.619933 IP 100.64.0.3 > 10.16.0.4: ICMP echo request, id 2, seq 3, length 64
06:52:38.619973 IP 10.16.0.4 > 100.64.0.3: ICMP echo reply, id 2, seq 3, length 64
```

3. Show ovn logical flow from a pod to a destination

```shell
[root@node2 ~]# kubectl ko trace default/ds1-l6n7p 8.8.8.8 icmp
+ kubectl exec ovn-central-5bc494cb5-np9hm -n kube-ovn -- ovn-trace ovn-default 'inport == "ds1-l6n7p.default" && ip.ttl == 64 && icmp && eth.src == 0a:00:00:10:00:05 && ip4.src == 10.16.0.4 && eth.dst == 00:00:00:B8:CA:43 && ip4.dst == 8.8.8.8 && ct.new'
# icmp,reg14=0xf,vlan_tci=0x0000,dl_src=0a:00:00:10:00:05,dl_dst=00:00:00:b8:ca:43,nw_src=10.16.0.4,nw_dst=8.8.8.8,nw_tos=0,nw_ecn=0,nw_ttl=64,icmp_type=0,icmp_code=0

ingress(dp="ovn-default", inport="ds1-l6n7p.default")
-----------------------------------------------------
 0. ls_in_port_sec_l2 (ovn-northd.c:4143): inport == "ds1-l6n7p.default" && eth.src == {0a:00:00:10:00:05}, priority 50, uuid 39453393
    next;
 1. ls_in_port_sec_ip (ovn-northd.c:2898): inport == "ds1-l6n7p.default" && eth.src == 0a:00:00:10:00:05 && ip4.src == {10.16.0.4}, priority 90, uuid 81bcd485
    next;
 3. ls_in_pre_acl (ovn-northd.c:3269): ip, priority 100, uuid 7b4f4971
    reg0[0] = 1;
    next;
 5. ls_in_pre_stateful (ovn-northd.c:3396): reg0[0] == 1, priority 100, uuid 36cdd577
    ct_next;

ct_next(ct_state=new|trk)
-------------------------
 6. ls_in_acl (ovn-northd.c:3759): ip && (!ct.est || (ct.est && ct_label.blocked == 1)), priority 1, uuid 7608af5b
    reg0[1] = 1;
    next;
10. ls_in_stateful (ovn-northd.c:3995): reg0[1] == 1, priority 100, uuid 2aba1b90
    ct_commit(ct_label=0/0x1);
    next;
16. ls_in_l2_lkup (ovn-northd.c:4470): eth.dst == 00:00:00:b8:ca:43, priority 50, uuid 5c9c3c9f
    outport = "ovn-default-ovn-cluster";
    output;

....Skip More....
```

If the pod is a virtual machine running in underlay network, you may need to add another parameter to specify the destination mac address:

```bash
kubectl ko trace default/virt-handler-7lvml 8.8.8.8 82:7c:9f:83:8c:01 icmp
```

4. Diagnose network connectivity

```shell
[root@node2 ~]# kubectl ko diagnose all
### start to diagnose node node1
I1008 07:04:40.475604   26434 ping.go:139] ovs-vswitchd and ovsdb are up
I1008 07:04:40.570824   26434 ping.go:151] ovn_controller is up
I1008 07:04:40.570859   26434 ping.go:35] start to check node connectivity
I1008 07:04:44.586096   26434 ping.go:57] ping node: node1 10.0.129.96, count: 5, loss rate 0.00%, average rtt 0.23ms
I1008 07:04:44.592764   26434 ping.go:57] ping node: node3 10.0.128.35, count: 5, loss rate 0.00%, average rtt 0.63ms
I1008 07:04:44.592791   26434 ping.go:57] ping node: node2 10.0.128.15, count: 5, loss rate 0.00%, average rtt 0.54ms
I1008 07:04:44.592889   26434 ping.go:74] start to check pod connectivity
I1008 07:04:48.669057   26434 ping.go:101] ping pod: kube-ovn-pinger-5mq4g 10.16.0.12, count: 5, loss rate 0.00, average rtt 0.18ms
I1008 07:04:48.769217   26434 ping.go:101] ping pod: kube-ovn-pinger-8xhhv 10.16.0.10, count: 5, loss rate 0.00, average rtt 0.64ms
I1008 07:04:48.769219   26434 ping.go:101] ping pod: kube-ovn-pinger-gc8l6 10.16.0.13, count: 5, loss rate 0.00, average rtt 0.73ms
I1008 07:04:48.769325   26434 ping.go:119] start to check dns connectivity
I1008 07:04:48.777062   26434 ping.go:129] resolve dns kubernetes.default.svc.cluster.local to [10.96.0.1] in 7.71ms
### finish diagnose node node1

### start to diagnose node node2
I1008 07:04:49.231462   16925 ping.go:139] ovs-vswitchd and ovsdb are up
I1008 07:04:49.241636   16925 ping.go:151] ovn_controller is up
I1008 07:04:49.241694   16925 ping.go:35] start to check node connectivity
I1008 07:04:53.254327   16925 ping.go:57] ping node: node2 10.0.128.15, count: 5, loss rate 0.00%, average rtt 0.16ms
I1008 07:04:53.354411   16925 ping.go:57] ping node: node1 10.0.129.96, count: 5, loss rate 0.00%, average rtt 15.65ms
I1008 07:04:53.354464   16925 ping.go:57] ping node: node3 10.0.128.35, count: 5, loss rate 0.00%, average rtt 15.71ms
I1008 07:04:53.354492   16925 ping.go:74] start to check pod connectivity
I1008 07:04:57.382791   16925 ping.go:101] ping pod: kube-ovn-pinger-8xhhv 10.16.0.10, count: 5, loss rate 0.00, average rtt 0.16ms
I1008 07:04:57.483725   16925 ping.go:101] ping pod: kube-ovn-pinger-5mq4g 10.16.0.12, count: 5, loss rate 0.00, average rtt 1.74ms
I1008 07:04:57.483750   16925 ping.go:101] ping pod: kube-ovn-pinger-gc8l6 10.16.0.13, count: 5, loss rate 0.00, average rtt 1.81ms
I1008 07:04:57.483813   16925 ping.go:119] start to check dns connectivity
I1008 07:04:57.490402   16925 ping.go:129] resolve dns kubernetes.default.svc.cluster.local to [10.96.0.1] in 6.56ms
### finish diagnose node node2

### start to diagnose node node3
I1008 07:04:58.094738   21692 ping.go:139] ovs-vswitchd and ovsdb are up
I1008 07:04:58.176064   21692 ping.go:151] ovn_controller is up
I1008 07:04:58.176096   21692 ping.go:35] start to check node connectivity
I1008 07:05:02.193091   21692 ping.go:57] ping node: node3 10.0.128.35, count: 5, loss rate 0.00%, average rtt 0.21ms
I1008 07:05:02.293256   21692 ping.go:57] ping node: node2 10.0.128.15, count: 5, loss rate 0.00%, average rtt 0.58ms
I1008 07:05:02.293256   21692 ping.go:57] ping node: node1 10.0.129.96, count: 5, loss rate 0.00%, average rtt 0.68ms
I1008 07:05:02.293368   21692 ping.go:74] start to check pod connectivity
I1008 07:05:06.314977   21692 ping.go:101] ping pod: kube-ovn-pinger-gc8l6 10.16.0.13, count: 5, loss rate 0.00, average rtt 0.37ms
I1008 07:05:06.415222   21692 ping.go:101] ping pod: kube-ovn-pinger-5mq4g 10.16.0.12, count: 5, loss rate 0.00, average rtt 0.82ms
I1008 07:05:06.415317   21692 ping.go:101] ping pod: kube-ovn-pinger-8xhhv 10.16.0.10, count: 5, loss rate 0.00, average rtt 0.64ms
I1008 07:05:06.415354   21692 ping.go:119] start to check dns connectivity
I1008 07:05:06.420595   21692 ping.go:129] resolve dns kubernetes.default.svc.cluster.local to [10.96.0.1] in 5.21ms
### finish diagnose node node3
```

5. Show OVN NB/SB cluster status
```shell
[root@node2 ~]# kubectl ko nb status
b9be
Name: OVN_Northbound
Cluster ID: 033e (033e333f-5031-465f-93af-7e1b2e3a82a0)
Server ID: b9be (b9be2f8e-3e4e-4374-93b7-297baf5724e7)
Address: tcp:[192.168.16.44]:6643
Status: cluster member
Role: leader
Term: 51
Leader: self
Vote: self

Last Election started 16222094 ms ago, reason: timeout
Last Election won: 16222094 ms ago
Election timer: 5000
Log: [50539, 50562]
Entries not yet committed: 0
Entries not yet applied: 0
Connections:
Disconnections: 0
Servers:
    b9be (b9be at tcp:[192.168.16.44]:6643) (self) next_index=50539 match_index=50561
```

6. Back OVN NB/SB database
```shell
[root@node2 ~]# kubectl ko nb backup
backup ovn-nb db to /root/ovnnb_db.081616201629102000.backup
[root@node2 ~]# ls -l | grep ovn_nb
-rw-r--r--  1 root root     31875 Aug 16 16:20 ovnnb_db.081616201629102000.backup
```

7. Remove stale NB/SB cluster member
```shell
[root@node2 ~]# kubectl ko nb kick aedds
```
