#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ic_nb_status=$(ovn-appctl -t /var/run/ovn/ovn_ic_nb_db.ctl cluster/status OVN_IC_Northbound | grep Status)
ic_sb_status=$(ovn-appctl -t /var/run/ovn/ovn_ic_sb_db.ctl cluster/status OVN_IC_Southbound | grep Status)
ic_nb_role=$(ovn-appctl -t /var/run/ovn/ovn_ic_nb_db.ctl cluster/status OVN_IC_Northbound | grep Role)
ic_sb_role=$(ovn-appctl -t /var/run/ovn/ovn_ic_sb_db.ctl cluster/status OVN_IC_Southbound | grep Role)

if ! echo ${ic_nb_status} | grep -v "failed"; then
    echo "nb health check failed"
    exit 1
fi
if ! echo ${ic_sb_status} | grep -v "failed"; then
    echo "sb health check failed"
    exit 1
fi

if echo ${ic_nb_status} | grep "disconnected" && echo ${ic_nb_role} | grep "candidate"; then
    echo "nb health check failed"
    exit 1
fi
if echo ${ic_sb_status} | grep "disconnected" && echo ${ic_sb_role} | grep "candidate"; then
    echo "sb health check failed"
    exit 1
fi
