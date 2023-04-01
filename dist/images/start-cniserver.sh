#!/usr/bin/env bash
set -euo pipefail

CNI_SOCK=/run/openvswitch/kube-ovn-daemon.sock
OVS_SOCK=/run/openvswitch/db.sock
ENABLE_SSL=${ENABLE_SSL:-false}
SYSCTL_NF_CONNTRACK_TCP_BE_LIBERAL=${SYSCTL_NF_CONNTRACK_TCP_BE_LIBERAL:-1}
SYSCTL_IPV4_NEIGH_DEFAULT_GC_THRESH=${SYSCTL_IPV4_NEIGH_DEFAULT_GC_THRESH:-"1024 2048 4096"}

# usage: set_sysctl key value
function set_sysctl {
  echo "setting sysctl variable \"$1\" to \"$2\""
  procfs_path="/proc/sys/$(echo "$1" | tr . /)"
  if [ -f "$procfs_path" ]; then
    sysctl -w "$1=$2"
  else
    echo "path \"$procfs_path\" does not exist, skip"
  fi
}

function quit {
  rm -rf $CNI_SOCK
  exit 0
}
trap quit EXIT

if [[ -e "$CNI_SOCK" ]]
then
  echo "previous socket exists, remove and continue"
  rm ${CNI_SOCK}
fi

while true
do
  sleep 1
  if [[ -e "$OVS_SOCK" ]]
  then
    break
  else
    echo "waiting for ovs ready"
  fi
done

# update links to point to the iptables binaries
iptables -V

# If nftables not exist do not exit
set +e
iptables -P FORWARD ACCEPT
iptables-nft -P FORWARD ACCEPT
set -e

gc_thresh1=$(echo "$SYSCTL_IPV4_NEIGH_DEFAULT_GC_THRESH" | awk '{print $1}')
gc_thresh2=$(echo "$SYSCTL_IPV4_NEIGH_DEFAULT_GC_THRESH" | awk '{print $2}')
gc_thresh3=$(echo "$SYSCTL_IPV4_NEIGH_DEFAULT_GC_THRESH" | awk '{print $3}')
set_sysctl net.ipv4.neigh.default.gc_thresh1 $gc_thresh1
set_sysctl net.ipv4.neigh.default.gc_thresh2 $gc_thresh2
set_sysctl net.ipv4.neigh.default.gc_thresh3 $gc_thresh3
set_sysctl net.netfilter.nf_conntrack_tcp_be_liberal $SYSCTL_NF_CONNTRACK_TCP_BE_LIBERAL

./kube-ovn-daemon --ovs-socket=${OVS_SOCK} --bind-socket=${CNI_SOCK} "$@"
