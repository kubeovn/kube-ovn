#!/usr/bin/env bash
set -euo pipefail

SOCK=/run/openvswitch/kube-ovn-daemon.sock

cp kube-ovn /opt/cni/bin/kube-ovn
cp kube-ovn.conflist /etc/cni/net.d/kube-ovn.conflist

if [[ -e "$SOCK" ]]
then
    echo "previous socket exists, remove and continue"
	rm ${SOCK}
fi

./kube-ovn-daemon --ovs-socket=/run/openvswitch/db.sock --controller-address="${KUBE_OVN_SERVICE_HOST}:${KUBE_OVN_SERVICE_PORT}" --bind-socket=${SOCK}