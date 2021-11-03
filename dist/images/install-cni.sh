#!/bin/bash

set -u -e

if [[ -f "/proc/sys/net/ipv4/ip_forward" ]];
    then echo 1 > /proc/sys/net/ipv4/ip_forward;
fi

if [[ -f "/proc/sys/net/ipv6/conf/all/forwarding" ]];
    then echo 1 > /proc/sys/net/ipv6/conf/all/forwarding;
fi

if [[ -f "/proc/sys/net/ipv4/conf/all/rp_filter" ]];
    then echo 0 > /proc/sys/net/ipv4/conf/all/rp_filter;
fi

exit_with_error(){
  echo "$1"
  exit 1
}

CNI_BIN_SRC=/kube-ovn/kube-ovn
CNI_BIN_DST=/opt/cni/bin/kube-ovn

LOOPBACK_BIN_SRC=/loopback
LOOPBACK_BIN_DST=/opt/cni/bin/loopback

PORTMAP_BIN_SRC=/portmap
PORTMAP_BIN_DST=/opt/cni/bin/portmap

yes | cp -f $LOOPBACK_BIN_SRC $LOOPBACK_BIN_DST || exit_with_error "Failed to copy $LOOPBACK_BIN_SRC to $LOOPBACK_BIN_DST"
yes | cp -f $PORTMAP_BIN_SRC $PORTMAP_BIN_DST || exit_with_error "Failed to copy $PORTMAP_BIN_SRC to $PORTMAP_BIN_DST"
yes | cp -f $CNI_BIN_SRC $CNI_BIN_DST || exit_with_error "Failed to copy $CNI_BIN_SRC to $CNI_BIN_DST"
