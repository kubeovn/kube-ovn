#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovn-ctl status_northd
ovn-ctl status_ovnnb | grep -q '^running'
ovn-ctl status_ovnsb | grep -q '^running'

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

set +o pipefail

# check nb/sb log file
function check_log_file() {
    local log_file="/var/log/ovn/ovsdb-server-$1.log"
    if [ -e $log_file ]; then
        if grep -wE '(opened log file)|(does not match prerequisite)' $log_file 2>/dev/null | tail -n 1 | grep 'does not match prerequisite' ; then
            echo "raft inconsistency in $1 db was detected, please check $log_file for more details."
            return 1
        fi
    fi
}

check_log_file nb
check_log_file sb
