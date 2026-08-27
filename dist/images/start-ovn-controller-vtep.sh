#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
OVN_SB_ADDR=${OVN_SB_ADDR:-}
KUBE_OVN_SB_PORT=${KUBE_OVN_SB_PORT:-6642}
VTEP_DB_ADDR=${VTEP_DB_ADDR:-}

if [[ -z "${VTEP_DB_ADDR}" ]]; then
  echo "VTEP_DB_ADDR must be set to the Hardware VTEP OVSDB remote (e.g. tcp:[switch-ip]:6640)"
  exit 1
fi

function gen_sb_conn_str {
  if [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "$ENABLE_SSL" == "false" ]]; then
      echo "tcp:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    else
      echo "ssl:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    fi
  else
    t=$(echo -n "${OVN_DB_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
      echo "$(for i in ${t}; do echo -n "tcp:[$i]:${KUBE_OVN_SB_PORT}",; done | sed 's/,$//')"
    else
      echo "$(for i in ${t}; do echo -n "ssl:[$i]:${KUBE_OVN_SB_PORT}",; done | sed 's/,$//')"
    fi
  fi
}

sb_addr="${OVN_SB_ADDR:-$(gen_sb_conn_str)}"

if [[ "$ENABLE_SSL" == "true" ]] || [[ "${VTEP_DB_ADDR}" == ssl:* ]] || [[ "${sb_addr}" == ssl:* ]]; then
  exec ovn-controller-vtep \
    --pidfile=/var/run/ovn/ovn-controller-vtep.pid \
    --ovnsb-db="$sb_addr" \
    --vtep-db="$VTEP_DB_ADDR" \
    --private-key=/var/run/tls/key \
    --certificate=/var/run/tls/cert \
    --ca-cert=/var/run/tls/cacert \
    "$@"
fi

exec ovn-controller-vtep \
  --pidfile=/var/run/ovn/ovn-controller-vtep.pid \
  --ovnsb-db="$sb_addr" \
  --vtep-db="$VTEP_DB_ADDR" \
  "$@"
