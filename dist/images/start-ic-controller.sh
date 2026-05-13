#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
KUBE_OVN_NB_PORT=${KUBE_OVN_NB_PORT:-6641}
KUBE_OVN_SB_PORT=${KUBE_OVN_SB_PORT:-6642}
OVN_NB_HOST=${OVN_NB_HOST:-}
OVN_SB_HOST=${OVN_SB_HOST:-}

function gen_conn_str {
  local port=$1
  local db_host
  if [[ "${port}" == "${KUBE_OVN_NB_PORT}" ]]; then
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
    if [[ "${port}" == "${KUBE_OVN_NB_PORT}" ]]; then
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

nb_addr="$(gen_conn_str "$KUBE_OVN_NB_PORT")"
sb_addr="$(gen_conn_str "$KUBE_OVN_SB_PORT")"

for ((i=0; i<3; i++)); do
  if [[ "$ENABLE_SSL" == "false" ]]; then
    OVN_NB_DAEMON=$(ovn-nbctl --db="$nb_addr" --pidfile --detach --overwrite-pidfile)
  else
    OVN_NB_DAEMON=$(ovn-nbctl -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert --db="$nb_addr" --pidfile --detach --overwrite-pidfile)
  fi
  if echo -n "${OVN_NB_DAEMON}" | grep -qE '^/var/run/ovn/ovn-nbctl\.[0-9]+\.ctl$'; then
    export OVN_NB_DAEMON
    break
  fi
  if [ $(echo ${OVN_NB_DAEMON} | wc -c) -gt 64 ]; then
    OVN_NB_DAEMON="$(echo ${OVN_NB_DAEMON} | cut -c1-64)..."
  fi
  echo "invalid ovn-nbctl daemon socket: \"${OVN_NB_DAEMON}\""
  unset OVN_NB_DAEMON
  pkill -f ovn-nbctl
done

if [ -z "${OVN_NB_DAEMON}" ]; then
  echo "failed to start ovn-nbctl daemon"
  exit 1
fi

exec ./kube-ovn-ic-controller  --ovn-nb-addr="$nb_addr" \
                           --ovn-sb-addr="$sb_addr" \
                           $@
