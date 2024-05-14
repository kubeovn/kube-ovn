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

nb_addr="$(gen_conn_str 6641)"
sb_addr="$(gen_conn_str 6642)"

exec ./kube-ovn-controller --ovn-nb-addr="$nb_addr" \
                           --ovn-sb-addr="$sb_addr" \
                           $@
