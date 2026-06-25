#!/bin/bash
set -uo pipefail

COMPONENT=${1:-}
MODE=${2:-loop}
ENABLE_SSL=${ENABLE_SSL:-false}
TLS_RELOAD_INTERVAL=${TLS_RELOAD_INTERVAL:-5}
TLS_DIR=${TLS_DIR:-/var/run/tls}
TLS_FILES=(
  "${TLS_DIR}/cacert"
  "${TLS_DIR}/cert"
  "${TLS_DIR}/key"
)
OVN_CTL=${OVN_CTL:-/usr/share/ovn/scripts/ovn-ctl}
OVN_NB_CTL=${OVN_NB_CTL:-/var/run/ovn/ovnnb_db.ctl}
OVN_SB_CTL=${OVN_SB_CTL:-/var/run/ovn/ovnsb_db.ctl}
NB_PORT=${NB_PORT:-6641}
SB_PORT=${SB_PORT:-6642}
NB_CLUSTER_PORT=${NB_CLUSTER_PORT:-6643}
SB_CLUSTER_PORT=${SB_CLUSTER_PORT:-6644}
DB_CLUSTER_ADDR=${DB_CLUSTER_ADDR:-${POD_IP:-}}
DB_ADDR=::
if [[ "${ENABLE_BIND_LOCAL_IP:-false}" == "true" ]]; then
  DB_ADDR=${POD_IP:-}
fi
SSL_OPTIONS="-p ${TLS_DIR}/key -c ${TLS_DIR}/cert -C ${TLS_DIR}/cacert"

case "$COMPONENT" in
  ovs)
    TLS_HASH_FILE=${TLS_HASH_FILE:-/tmp/kube-ovn-tls.hash}
    ;;
  ovn-central)
    TLS_HASH_FILE=${TLS_HASH_FILE:-/tmp/kube-ovn-central-tls.hash}
    ;;
  *)
    echo "usage: $0 {ovs|ovn-central} [once|loop]"
    exit 1
    ;;
esac
TLS_LOCK_FILE=${TLS_LOCK_FILE:-${TLS_HASH_FILE}.lock}

function tls_files_hash {
  for file in "${TLS_FILES[@]}"; do
    if [[ ! -s "$file" ]]; then
      echo "kube-ovn TLS file $file is missing or empty" >&2
      return 1
    fi
  done
  sha256sum "${TLS_FILES[@]}" | sha256sum | awk '{print $1}'
}

function write_tls_hash {
  local hash=$1
  local tmp_file
  tmp_file="${TLS_HASH_FILE}.$$"
  printf "%s" "$hash" > "$tmp_file"
  mv -f "$tmp_file" "$TLS_HASH_FILE"
}

function reload_ovs_tls {
  echo "kube-ovn TLS files changed, restarting ovn-controller"
  /usr/share/ovn/scripts/ovn-ctl \
    --ovn-controller-ssl-key="${TLS_DIR}/key" \
    --ovn-controller-ssl-cert="${TLS_DIR}/cert" \
    --ovn-controller-ssl-ca-cert="${TLS_DIR}/cacert" \
    --ovn-controller-wrapper="${DEBUG_WRAPPER:-}" \
    restart_controller
}

function ovn_central_conn_addr {
  local ip=$1
  local port=$2

  echo "ssl:[${ip}]:${port}"
}

function ovn_central_conn_str {
  local port=$1
  local endpoints=()

  for ip in ${NODE_IPS//,/ }; do
    endpoints+=("$(ovn_central_conn_addr "$ip" "$port")")
  done
  local IFS=,
  echo "${endpoints[*]}"
}

function ovn_central_ssl_args {
  printf "%s\n" \
    "--ovn-nb-db-ssl-key=${TLS_DIR}/key" \
    "--ovn-nb-db-ssl-cert=${TLS_DIR}/cert" \
    "--ovn-nb-db-ssl-ca-cert=${TLS_DIR}/cacert" \
    "--ovn-sb-db-ssl-key=${TLS_DIR}/key" \
    "--ovn-sb-db-ssl-cert=${TLS_DIR}/cert" \
    "--ovn-sb-db-ssl-ca-cert=${TLS_DIR}/cacert" \
    "--ovn-northd-ssl-key=${TLS_DIR}/key" \
    "--ovn-northd-ssl-cert=${TLS_DIR}/cert" \
    "--ovn-northd-ssl-ca-cert=${TLS_DIR}/cacert"
}

function wait_ovn_ctl_status {
  local status_cmd=$1

  for _ in $(seq 1 30); do
    if "$OVN_CTL" "$status_cmd" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

function reload_ovsdb_server_tls {
  local ctl=$1

  ovn-appctl -t "$ctl" ovsdb-server/set-ssl "${TLS_DIR}/key" "${TLS_DIR}/cert" "${TLS_DIR}/cacert"
  ovn-appctl -t "$ctl" ovsdb-server/reconnect
}

function reload_ovn_central_ovsdb_tls {
  reload_ovsdb_server_tls "$OVN_NB_CTL"
  reload_ovsdb_server_tls "$OVN_SB_CTL"
}

function ovn_ctl_restart_or_status {
  local status_cmd=$1
  shift

  if "$OVN_CTL" "$@"; then
    return 0
  fi

  echo "ovn-ctl $* failed, checking ${status_cmd} before retrying" >&2
  wait_ovn_ctl_status "$status_cmd"
}

function restart_standalone_ovn_central_tls {
  local ssl_args
  mapfile -t ssl_args < <(ovn_central_ssl_args)
  ovn_ctl_restart_or_status status_northd "${ssl_args[@]}" --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS:-1}" restart_northd
}

function restart_clustered_ovn_northd_tls {
  if [[ -z "$DB_CLUSTER_ADDR" || -z "$DB_ADDR" ]]; then
    echo "failed to determine local OVN DB address" >&2
    return 1
  fi

  local ovn_ctl_args
  mapfile -t ovn_ctl_args < <(ovn_central_ssl_args)
  ovn_ctl_args+=(
    "--ovn-northd-nb-db=$(ovn_central_conn_str "$NB_PORT")"
    "--ovn-northd-sb-db=$(ovn_central_conn_str "$SB_PORT")"
  )

  ovn_ctl_restart_or_status status_northd "${ovn_ctl_args[@]}" \
    --ovn-manage-ovsdb=no \
    --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS:-1}" \
    restart_northd
}

function reload_ovn_central_tls {
  echo "kube-ovn TLS files changed, reloading ovn-central TLS"
  reload_ovn_central_ovsdb_tls || return 1

  if [[ -z "${NODE_IPS:-}" ]]; then
    restart_standalone_ovn_central_tls
    return $?
  fi
  restart_clustered_ovn_northd_tls
}

function reload_tls {
  case "$COMPONENT" in
    ovs)
      reload_ovs_tls
      ;;
    ovn-central)
      reload_ovn_central_tls
      ;;
  esac
}

function check_tls_once {
  if [[ "$ENABLE_SSL" != "true" ]]; then
    return 0
  fi

  local lock_fd
  exec {lock_fd}>"$TLS_LOCK_FILE"
  if ! flock -n "$lock_fd"; then
    exec {lock_fd}>&-
    return 0
  fi

  local current_hash
  current_hash=$(tls_files_hash) || {
    exec {lock_fd}>&-
    return 1
  }
  if [[ ! -f "$TLS_HASH_FILE" ]]; then
    write_tls_hash "$current_hash"
    exec {lock_fd}>&-
    return 0
  fi

  local previous_hash
  previous_hash=$(cat "$TLS_HASH_FILE" 2>/dev/null || true)
  if [[ "$current_hash" == "$previous_hash" ]]; then
    exec {lock_fd}>&-
    return 0
  fi

  # Keep the parent lock during reload, but do not let restarted daemons inherit it.
  if (exec {lock_fd}>&-; reload_tls); then
    write_tls_hash "$current_hash"
    exec {lock_fd}>&-
    return 0
  fi

  exec {lock_fd}>&-
  echo "failed to reload kube-ovn TLS files, will retry" >&2
  return 1
}

if [[ "$MODE" == "once" ]]; then
  check_tls_once
  exit $?
fi

while true; do
  check_tls_once || true
  sleep "$TLS_RELOAD_INTERVAL"
done
