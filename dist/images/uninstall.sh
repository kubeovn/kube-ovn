#!/bin/bash
/usr/share/openvswitch/scripts/ovs-ctl stop
ovs-dpctl del-dp ovs-system

iptables -t nat -D PREROUTING -j OVN-PREROUTING -m comment --comment "kube-ovn prerouting rules"
iptables -t nat -D POSTROUTING -j OVN-POSTROUTING -m comment --comment "kube-ovn postrouting rules"
iptables -t nat -F OVN-PREROUTING
iptables -t nat -X OVN-PREROUTING
iptables -t nat -F OVN-POSTROUTING
iptables -t nat -X OVN-POSTROUTING
iptables -t nat -F OVN-NAT-POLICY
iptables -t nat -X OVN-NAT-POLICY
iptables -t nat -F OVN-MASQUERADE
iptables -t nat -X OVN-MASQUERADE
iptables -t filter -D INPUT -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40subnets src -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40services dst -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40services src -j ACCEPT
iptables -t filter -D INPUT -p tcp -m mark ! --mark 0x4000/0x4000 -m set --match-set ovn40services dst -m conntrack --ctstate NEW -j REJECT
iptables -t filter -D FORWARD -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40subnets src -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40services dst -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40services src -j ACCEPT
iptables -t filter -D OUTPUT -p udp -m udp --dport 6081 -j MARK --set-xmark 0x0
iptables -t filter -D OUTPUT -p tcp -m mark ! --mark 0x4000/0x4000 -m set --match-set ovn40services dst -m conntrack --ctstate NEW -j REJECT
iptables -t mangle -D PREROUTING -m comment --comment "kube-ovn prerouting rules" -j OVN-PREROUTING
iptables -t mangle -D OUTPUT -m comment --comment "kube-ovn output rules" -j OVN-OUTPUT
iptables -t mangle -F OVN-PREROUTING
iptables -t mangle -X OVN-PREROUTING
iptables -t mangle -F OVN-OUTPUT
iptables -t mangle -X OVN-OUTPUT
iptables -t mangle -F OVN-POSTROUTING
iptables -t mangle -X OVN-POSTROUTING

sleep 1

ipset destroy ovn40subnets-nat
ipset destroy ovn40subnets
ipset destroy ovn40subnets-distributed-gw
ipset destroy ovn40local-pod-ip-nat
ipset destroy ovn40other-node
ipset destroy ovn40services
ipset destroy ovn40subnets-nat-policy

ip6tables -t nat -D PREROUTING -j OVN-PREROUTING -m comment --comment "kube-ovn prerouting rules"
ip6tables -t nat -D POSTROUTING -j OVN-POSTROUTING -m comment --comment "kube-ovn postrouting rules"
ip6tables -t nat -F OVN-PREROUTING
ip6tables -t nat -X OVN-PREROUTING
ip6tables -t nat -F OVN-POSTROUTING
ip6tables -t nat -X OVN-POSTROUTING
ip6tables -t nat -F OVN-NAT-POLICY
ip6tables -t nat -X OVN-NAT-POLICY
ip6tables -t nat -F OVN-MASQUERADE
ip6tables -t nat -X OVN-MASQUERADE
ip6tables -t filter -D INPUT -m set --match-set ovn60subnets dst -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60subnets src -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60services dst -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60services src -j ACCEPT
ip6tables -t filter -D INPUT -p tcp -m mark ! --mark 0x4000/0x4000 -m set --match-set ovn60services dst -m conntrack --ctstate NEW -j REJECT
ip6tables -t filter -D FORWARD -m set --match-set ovn60subnets dst -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60subnets src -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60services dst -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60services src -j ACCEPT
ip6tables -t filter -D OUTPUT -p udp -m udp --dport 6081 -j MARK --set-xmark 0x0
ip6tables -t filter -D OUTPUT -p tcp -m mark ! --mark 0x4000/0x4000 -m set --match-set ovn60services dst -m conntrack --ctstate NEW -j REJECT
ip6tables -t mangle -D PREROUTING -m comment --comment "kube-ovn prerouting rules" -j OVN-PREROUTING
ip6tables -t mangle -D OUTPUT -m comment --comment "kube-ovn output rules" -j OVN-OUTPUT
ip6tables -t mangle -F OVN-PREROUTING
ip6tables -t mangle -X OVN-PREROUTING
ip6tables -t mangle -F OVN-OUTPUT
ip6tables -t mangle -X OVN-OUTPUT
ip6tables -t mangle -F OVN-POSTROUTING
ip6tables -t mangle -X OVN-POSTROUTING

sleep 1

ipset destroy ovn60subnets-nat
ipset destroy ovn60subnets
ipset destroy ovn60subnets-distributed-gw
ipset destroy ovn60local-pod-ip-nat
ipset destroy ovn60other-node
ipset destroy ovn60services
ipset destroy ovn60subnets-nat-policy

rm -rf /var/run/openvswitch/*
rm -rf /var/run/ovn/*
rm -rf /etc/openvswitch/*
rm -rf /etc/ovn/*
rm -rf /var/log/openvswitch/*
rm -rf /var/log/ovn/*
# default
rm -rf /etc/cni/net.d/00-kube-ovn.conflist
# default
rm -rf /etc/cni/net.d/01-kube-ovn.conflist
