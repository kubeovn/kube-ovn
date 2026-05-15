#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
OVN_NB_HOST=${OVN_NB_HOST:-}
OVN_SB_HOST=${OVN_SB_HOST:-}

function gen_conn_str {
  local port=$1
  local db_host
  if [[ "${port}" == "6641" ]]; then
    db_host="${OVN_NB_HOST}"
  else
    db_host="${OVN_SB_HOST}"
  fi
  if [[ -n "${db_host}" ]]; then
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x="tcp:${db_host}:${port}"
    else
      x="ssl:${db_host}:${port}"
    fi
  elif [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "${port}" == "6641" ]]; then
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
      x=$(for i in ${t}; do echo -n "tcp:[$i]:${port}",; done | sed 's/,$//')
    else
      x=$(for i in ${t}; do echo -n "ssl:[$i]:${port}",; done | sed 's/,$//')
    fi
  fi
  echo "$x"
}

nb_addr="$(gen_conn_str 6641)"
sb_addr="$(gen_conn_str 6642)"

exec ./kube-ovn-controller --ovn-nb-addr="$nb_addr" \
                           --ovn-sb-addr="$sb_addr" \
                           $@
