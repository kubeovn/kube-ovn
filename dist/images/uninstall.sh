#!/bin/bash
/usr/share/openvswitch/scripts/ovs-ctl stop
ovs-dpctl del-dp ovs-system

iptables -t nat -D POSTROUTING -m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE
iptables -t nat -D POSTROUTING -m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE
iptables -t filter -D INPUT -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40subnets src -j ACCEPT
ipset destroy ovn40subnets-nat
ipset destroy ovn40subnets
ipset destroy ovn40local-pod-ip-nat

rm -rf /var/run/openvswitch/*
rm -rf /var/run/ovn/*
rm -rf /etc/openvswitch/*
rm -rf /etc/ovn/*
rm -rf /var/log/openvswitch/*
rm -rf /var/log/ovn/*
rm -rf /etc/cni/net.d/00-kube-ovn.conflist
rm -rf /etc/cni/net.d/01-kube-ovn.conflist
