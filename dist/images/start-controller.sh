#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}

function gen_conn_str {
  if [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "$1" == "6641" ]]; then
      if [[ "$ENABLE_SSL" == "false" ]]; then
        x="tcp:[${OVN_NB_SERVICE_HOST}]:${OVN_NB_SERVICE_PORT}"
      else
        x="ssl:[${OVN_NB_SERVICE_HOST}]:${OVN_NB_SERVICE_PORT}"
      fi
    else
      if [[ "$ENABLE_SSL" == "false" ]]; then
        x="tcp:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
      else
        x="ssl:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
      fi
    fi
  else
    t=$(echo -n "${OVN_DB_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x=$(for i in ${t}; do echo -n "tcp:[$i]:$1",; done| sed 's/,$//')
    else
      x=$(for i in ${t}; do echo -n "ssl:[$i]:$1",; done| sed 's/,$//')
    fi
  fi
  echo "$x"
}

if [[ "$ENABLE_SSL" == "false" ]]; then
  export OVN_NB_DAEMON=$(ovn-nbctl --db="$(gen_conn_str 6641)" --pidfile --detach --overwrite-pidfile)
else
  export OVN_NB_DAEMON=$(ovn-nbctl -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert --db="$(gen_conn_str 6641)" --pidfile --detach --overwrite-pidfile)
fi
exec ./kube-ovn-controller --ovn-nb-addr="$(gen_conn_str 6641)" \
                           --ovn-sb-addr="$(gen_conn_str 6642)" \
                           $@
