#!/usr/bin/env bash
set -euo pipefail

SOCK=/run/openvswitch/kube-ovn-daemon.sock

if [[ -e "$SOCK" ]]
then
    echo "previous socket exists, remove and continue"
	rm ${SOCK}
fi

./kube-ovn-daemon --ovs-socket=/run/openvswitch/db.sock --bind-socket=${SOCK} $@
