#!/usr/bin/env bash
set -euo pipefail

if [[ -f "/proc/sys/net/bridge/bridge-nf-call-iptables" ]];
    then echo 1 > /proc/sys/net/bridge/bridge-nf-call-iptables;
fi

if [[ -f "/proc/sys/net/ipv4/ip_forward" ]];
    then echo 1 > /proc/sys/net/ipv4/ip_forward;
fi

if [[ -f "/proc/sys/net/ipv4/conf/all/rp_filter" ]];
    then echo 0 > /proc/sys/net/ipv4/conf/all/rp_filter;
fi

SOCK=/run/openvswitch/kube-ovn-daemon.sock

if [[ -e "$SOCK" ]]
then
    echo "previous socket exists, remove and continue"
	rm ${SOCK}
fi

./kube-ovn-daemon --ovs-socket=/run/openvswitch/db.sock --bind-socket=${SOCK} $@