#!/bin/bash
/usr/share/openvswitch/scripts/ovs-ctl stop
ovs-dpctl del-dp ovs-system

nodeIPv4=""
nodeIPv6=""
if [ -n "$1" ]; then
    if [[ "$1" =~ .*,.* ]]; then
        nodeIPv4=${1%%,*}
        nodeIPv6=${1##*,}
        if [[ "$nodeIPv4" =~ .*:.* ]]; then
            nodeIPv4=${1##*,}
            nodeIPv6=${1%%,*}
        fi
    else
        if [[ "$1" =~ .*:.* ]]; then
            nodeIPv6=$1
        else
            nodeIPv4=$1
        fi
    fi
fi

iptables -t nat -D POSTROUTING -m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40local-pod-ip-nat dst -j RETURN
iptables -t nat -D POSTROUTING -m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40subnets-nat dst -j RETURN
iptables -t nat -D POSTROUTING -m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE
iptables -t nat -D POSTROUTING -m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE
iptables -t filter -D INPUT -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40subnets src -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40services dst -j ACCEPT
iptables -t filter -D INPUT -m set --match-set ovn40services src -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40subnets dst -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40subnets src -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40services dst -j ACCEPT
iptables -t filter -D FORWARD -m set --match-set ovn40services src -j ACCEPT
iptables -t filter -D OUTPUT -p udp -m udp --dport 6081 -j MARK --set-xmark 0x0

if [ -n "$nodeIPv4" ]; then
    iptables -t nat -D POSTROUTING ! -s "$nodeIPv4" -m mark --mark 0x4000/0x4000 -j MASQUERADE
    iptables -t nat -D POSTROUTING ! -s "$nodeIPv4" -m set ! --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j MASQUERADE
fi

sleep 1

ipset destroy ovn40subnets-nat
ipset destroy ovn40subnets
ipset destroy ovn40local-pod-ip-nat
ipset destroy ovn40other-node
ipset destroy ovn40services

ip6tables -t nat -D POSTROUTING -m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60local-pod-ip-nat dst -j RETURN
ip6tables -t nat -D POSTROUTING -m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60subnets-nat dst -j RETURN
ip6tables -t nat -D POSTROUTING -m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE
ip6tables -t nat -D POSTROUTING -m set --match-set ovn60local-pod-ip-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE
ip6tables -t filter -D INPUT -m set --match-set ovn60subnets dst -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60subnets src -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60services dst -j ACCEPT
ip6tables -t filter -D INPUT -m set --match-set ovn60services src -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60subnets dst -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60subnets src -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60services dst -j ACCEPT
ip6tables -t filter -D FORWARD -m set --match-set ovn60services src -j ACCEPT
ip6tables -t filter -D OUTPUT -p udp -m udp --dport 6081 -j MARK --set-xmark 0x0

if [ -n "$nodeIPv6" ]; then
    ip6tables -t nat -D POSTROUTING ! -s "$nodeIPv6" -m mark --mark 0x4000/0x4000 -j MASQUERADE
    ip6tables -t nat -D POSTROUTING ! -s "$nodeIPv6" -m set ! --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j MASQUERADE
fi

sleep 1

ipset destroy ovn6subnets-nat
ipset destroy ovn60subnets
ipset destroy ovn60local-pod-ip-nat
ipset destroy ovn60other-node
ipset destroy ovn60services

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
