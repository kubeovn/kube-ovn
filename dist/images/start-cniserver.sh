#!/usr/bin/env bash
set -euo pipefail

CNI_SOCK=/run/openvswitch/kube-ovn-daemon.sock
OVS_SOCK=/run/openvswitch/db.sock
ENABLE_SSL=${ENABLE_SSL:-false}

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

while true; do
  if [[ -e "$OVS_SOCK" ]]; then
    /usr/share/openvswitch/scripts/ovs-ctl status && break
  fi
  echo "waiting for ovs ready"
  sleep 1
done

# update links to point to the iptables binaries
iptables -V

# If nftables not exist do not exit
set +e
iptables -P FORWARD ACCEPT
iptables-nft -P FORWARD ACCEPT
set -e

./kube-ovn-daemon --ovs-socket=${OVS_SOCK} --bind-socket=${CNI_SOCK} "$@"
