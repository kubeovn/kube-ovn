#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}

function gen_conn_str {
  t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
  if [[ "$ENABLE_SSL" == "false" ]]; then
    x=$(for i in ${t}; do echo -n "tcp:[$i]:$1",; done| sed 's/,$//')
  else
    x=$(for i in ${t}; do echo -n "ssl:[$i]:$1",; done| sed 's/,$//')
  fi
  echo "$x"
}

if [[ "$ENABLE_SSL" == "false" ]]; then
  export OVN_NB_DAEMON=$(ovn-nbctl --db="$(gen_conn_str 6641)" --pidfile --detach --overwrite-pidfile)
else
  export OVN_NB_DAEMON=$(ovn-nbctl -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert --db="$(gen_conn_str 6641)" --pidfile --detach --overwrite-pidfile)
fi

exec ./kube-ovn-monitor $@
