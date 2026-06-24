#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ENABLE_SSL=${ENABLE_SSL:-false}

ovn-ctl status_northd
ovn-ctl status_ovnnb | grep -q '^running'
ovn-ctl status_ovnsb | grep -q '^running'

# Standalone (single-replica) mode has no raft cluster, so `cluster/status` is
# not applicable. Fall back to a storage-status check on each local DB.
if [[ -z "${NODE_IPS:-}" ]]; then
    nb_storage=$(ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound)
    sb_storage=$(ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status OVN_Southbound)
    if echo "${nb_storage}" | grep -q "inconsistent"; then
        echo "nb health check failed: ${nb_storage}"
        exit 1
    fi
    if echo "${sb_storage}" | grep -q "inconsistent"; then
        echo "sb health check failed: ${sb_storage}"
        exit 1
    fi
    exit 0
fi

nb_status=$(ovn-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound | grep Status)
sb_status=$(ovn-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/status OVN_Southbound | grep Status)
nb_role=$(ovn-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound | grep Role)
sb_role=$(ovn-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/status OVN_Southbound | grep Role)

if ! echo ${nb_status} | grep -v "failed"; then
    echo "nb health check failed"
    exit 1
fi
if ! echo ${sb_status} | grep -v "failed"; then
    echo "sb health check failed"
    exit 1
fi

if echo ${nb_status} | grep "disconnected" && echo ${nb_role} | grep "candidate"; then
    echo "nb health check failed"
    exit 1
fi
if echo ${sb_status} | grep "disconnected" && echo ${sb_role} | grep "candidate"; then
    echo "sb health check failed"
    exit 1
fi
