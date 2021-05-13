#!/usr/bin/env bash
set -euo pipefail

CNI_SOCK=/run/openvswitch/kube-ovn-daemon.sock
OVS_SOCK=/run/openvswitch/db.sock
ENABLE_SSL=${ENABLE_SSL:-false}

function quit {
    rm -rf CNI_CONF
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

iptables -P FORWARD ACCEPT
iptables-nft -P FORWARD ACCEPT

# wait kube-ovn-controller ready
kubectl rollout status deployment/kube-ovn-controller -n "$(cat /run/secrets/kubernetes.io/serviceaccount/namespace)"
sleep 1

./kube-ovn-daemon --ovs-socket=${OVS_SOCK} --bind-socket=${CNI_SOCK} "$@"
