#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
OVN_NB_ADDR=${OVN_NB_ADDR:-}
OVN_SB_ADDR=${OVN_SB_ADDR:-}
# Default to the in-cluster Service ports. Override when reaching an
# externally-exposed ovn-central whose NodePort/LoadBalancer remaps ports.
KUBE_OVN_NB_PORT=${KUBE_OVN_NB_PORT:-6641}
KUBE_OVN_SB_PORT=${KUBE_OVN_SB_PORT:-6642}

function gen_conn_str {
  if [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "$1" == "$KUBE_OVN_NB_PORT" ]]; then
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

nb_addr="${OVN_NB_ADDR:-$(gen_conn_str "$KUBE_OVN_NB_PORT")}"
sb_addr="${OVN_SB_ADDR:-$(gen_conn_str "$KUBE_OVN_SB_PORT")}"

exec ./kube-ovn-controller --ovn-nb-addr="$nb_addr" \
                           --ovn-sb-addr="$sb_addr" \
                           $@
