#!/bin/bash
/usr/share/openvswitch/scripts/ovs-ctl stop
ovs-dpctl del-dp ovs-system

iptables -t nat -D POSTROUTING -m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE
iptables -t nat -D POSTROUTING -m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE
iptables -t nat -D POSTROUTING -m set ! --match-set ovn40subnets src -m set --match-set ovn40subnets-nat dst -j RETURN
iptables -t nat -D POSTROUTING -m set ! --match-set ovn40subnets src -m set --match-set ovn40local-pod-ip-nat dst -j RETURN
iptables -t nat -D POSTROUTING -m set --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j MASQUERADE
iptables -t nat -D POSTROUTING -m set --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j RETURN
iptables -t filter -D INPUT -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40subnets src -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40subnets src -j ACCEPT

ipset destroy ovn40subnets-nat
ipset destroy ovn40subnets
ipset destroy ovn40local-pod-ip-nat

ip6tables -t nat -D POSTROUTING -m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE
ip6tables -t nat -D POSTROUTING -m set --match-set ovn60local-pod-ip-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE
ip6tables -t nat -D POSTROUTING -m set ! --match-set ovn60subnets src -m set --match-set ovn60subnets-nat dst -j RETURN
ip6tables -t nat -D POSTROUTING -m set ! --match-set ovn60subnets src -m set --match-set ovn60local-pod-ip-nat dst -j RETURN
ip6tables -t nat -D POSTROUTING -m set --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j MASQUERADE
ip6tables -t nat -D POSTROUTING -m set --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j RETURN
ip6tables -t filter -D INPUT -m set --match-set ovn60subnets dst -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60subnets src -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60subnets dst -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60subnets src -j ACCEPT

ipset destroy ovn6subnets-nat
ipset destroy ovn60subnets
ipset destroy ovn60local-pod-ip-nat

rm -rf /var/run/openvswitch/*
rm -rf /var/run/ovn/*
rm -rf /etc/openvswitch/*
rm -rf /etc/ovn/*
rm -rf /var/log/openvswitch/*
rm -rf /var/log/ovn/*
rm -rf /etc/cni/net.d/00-kube-ovn.conflist
rm -rf /etc/cni/net.d/01-kube-ovn.conflist
