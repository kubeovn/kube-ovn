#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

OVN_DB_IPS=${OVN_DB_IPS:-}
OVN_SB_HOST=${OVN_SB_HOST:-}
ENABLE_SSL=${ENABLE_SSL:-false}

function gen_conn_str {
  local port=$1
  if [[ -n "${OVN_SB_HOST}" ]]; then
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x="tcp:${OVN_SB_HOST}:${port}"
    else
      x="ssl:${OVN_SB_HOST}:${port}"
    fi
  elif [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x="tcp:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    else
      x="ssl:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    fi
  else
    t=$(echo -n "${OVN_DB_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x=$(for i in ${t}; do echo -n "tcp:[$i]:${port}",; done | sed 's/,$//')
    else
      x=$(for i in ${t}; do echo -n "ssl:[$i]:${port}",; done | sed 's/,$//')
    fi
  fi
  echo "$x"
}

# echo Connecting OVN SB "$(gen_conn_str 6642)"
if [[ "$ENABLE_SSL" == "false" ]]; then
  ovsdb-client list-dbs
else
  ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert list-dbs
fi
alias ovs-ctl='/usr/share/openvswitch/scripts/ovs-ctl'
alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovs-ctl status
ovn-ctl status_controller

# check if ovn-controller can write to ovn sb db
file="/var/log/ovn/ovn-controller.log"
if [ -e $file ]
then
  result=$(tail -6 $file 2>/dev/null || true)
  if [[ "$result" =~ "clustered database server has stale data" ]]
  then
    echo "check write to ovn sb db, clustered database server has stale data, run sb-cluster-state-reset command to restore"
    pid=$(cat /var/run/ovn/ovn-controller.pid)
    ovn-appctl -t /var/run/ovn/ovn-controller.$pid.ctl sb-cluster-state-reset
    echo "finish exec cmd sb-cluster-state-reset"
  else
    echo "check write to ovn sb db success"
  fi
fi
