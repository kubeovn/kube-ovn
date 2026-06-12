#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
KUBE_OVN_NB_PORT=${KUBE_OVN_NB_PORT:-6641}
KUBE_OVN_SB_PORT=${KUBE_OVN_SB_PORT:-6642}
OVN_NB_DAEMON_SOCKET=${OVN_NB_DAEMON_SOCKET:-/var/run/ovn/ovn-nbctl.ctl}

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

nb_addr="$(gen_conn_str "$KUBE_OVN_NB_PORT")"
sb_addr="$(gen_conn_str "$KUBE_OVN_SB_PORT")"

function ovn_nbctl_tls_args {
  if [[ -f /var/run/tls/client.crt && -f /var/run/tls/client.key && -f /var/run/tls/ca.crt ]]; then
    echo "-p /var/run/tls/client.key -c /var/run/tls/client.crt -C /var/run/tls/ca.crt"
  else
    echo "-p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert"
  fi
}

function start_ovn_nbctl_daemon {
  local daemon_socket=""
  for ((i=0; i<3; i++)); do
    if [[ "$ENABLE_SSL" == "false" ]]; then
      daemon_socket=$(ovn-nbctl --db="$nb_addr" --pidfile --detach --overwrite-pidfile)
    else
      # shellcheck disable=SC2046
      daemon_socket=$(ovn-nbctl $(ovn_nbctl_tls_args) --db="$nb_addr" --pidfile --detach --overwrite-pidfile)
    fi
    if echo -n "${daemon_socket}" | grep -qE '^/var/run/ovn/ovn-nbctl\.[0-9]+\.ctl$'; then
      ln -sfn "${daemon_socket}" "${OVN_NB_DAEMON_SOCKET}"
      export OVN_NB_DAEMON="${OVN_NB_DAEMON_SOCKET}"
      return 0
    fi
    if [ "$(echo "${daemon_socket}" | wc -c)" -gt 64 ]; then
      daemon_socket="$(echo "${daemon_socket}" | cut -c1-64)..."
    fi
    echo "invalid ovn-nbctl daemon socket: \"${daemon_socket}\""
    unset OVN_NB_DAEMON
    pkill -f ovn-nbctl || true
  done
  return 1
}

function ovn_nbctl_tls_hash {
  if [[ ! -f /var/run/tls/client.crt || ! -f /var/run/tls/client.key || ! -f /var/run/tls/ca.crt ]]; then
    return 1
  fi
  sha256sum /var/run/tls/client.crt /var/run/tls/client.key /var/run/tls/ca.crt | sha256sum | awk '{print $1}'
}

function watch_ovn_nbctl_tls {
  local last_hash=""
  last_hash=$(ovn_nbctl_tls_hash || true)
  while true; do
    sleep 30
    local current_hash=""
    current_hash=$(ovn_nbctl_tls_hash || true)
    if [[ -z "$current_hash" || "$current_hash" == "$last_hash" ]]; then
      continue
    fi
    echo "OVN DB TLS client files changed, restarting ovn-nbctl daemon"
    pkill -f ovn-nbctl || true
    if start_ovn_nbctl_daemon; then
      last_hash="$current_hash"
    else
      echo "failed to restart ovn-nbctl daemon after OVN DB TLS change"
    fi
  done
}

if ! start_ovn_nbctl_daemon; then
  echo "failed to start ovn-nbctl daemon"
  exit 1
fi

if [[ "$ENABLE_SSL" != "false" && -f /var/run/tls/client.crt && -f /var/run/tls/client.key && -f /var/run/tls/ca.crt ]]; then
  watch_ovn_nbctl_tls &
fi

exec ./kube-ovn-ic-controller  --ovn-nb-addr="$nb_addr" \
                           --ovn-sb-addr="$sb_addr" \
                           "$@"
