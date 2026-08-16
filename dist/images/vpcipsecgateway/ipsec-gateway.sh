#!/bin/bash
set -euo pipefail

CONF_SRC_DIR="${CONF_SRC_DIR:-/etc/swanctl/kube-ovn}"
CONF_DIR="${CONF_DIR:-/etc/swanctl/conf.d}"
PSK_FILE="${PSK_FILE:-/etc/ipsec.d/psk/psk}"
CONN_SRC="${CONF_SRC_DIR}/vpc-ipsec.conf"
CONN_CONF="${CONF_DIR}/vpc-ipsec.conf"
SECRETS_CONF="${CONF_DIR}/vpc-ipsec-secrets.conf"

enable_forwarding() {
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
  if [ -f /proc/sys/net/ipv6/conf/all/forwarding ]; then
    sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null || true
  fi
}

write_secrets() {
  if [ ! -f "${PSK_FILE}" ]; then
    echo "psk file ${PSK_FILE} not found" >&2
    return 1
  fi
  # Escape double quotes for swanctl secret syntax.
  psk="$(sed 's/"/\\"/g' "${PSK_FILE}")"
  cat > "${SECRETS_CONF}" <<EOF
secrets {
    ike {
        id = %any
        secret = "${psk}"
    }
}
EOF
}

start_strongswan() {
  if [ ! -f "${CONN_SRC}" ]; then
    echo "connection config ${CONN_SRC} not found" >&2
    return 1
  fi
  mkdir -p "${CONF_DIR}"
  cp "${CONN_SRC}" "${CONN_CONF}"
  write_secrets
  # charon may already be running in some images; ignore failure.
  ipsec start >/dev/null 2>&1 || true
  swanctl --load-all || swanctl --load-conns
  swanctl --load-creds || true
}

case "${1:-run}" in
  init)
    enable_forwarding
    start_strongswan
    ;;
  run)
    enable_forwarding
    start_strongswan
    # Keep container alive and periodically reload configuration.
    while true; do
      sleep 30
      if [ -f "${CONN_SRC}" ]; then
        cp "${CONN_SRC}" "${CONN_CONF}"
      fi
      write_secrets || true
      swanctl --load-all >/dev/null 2>&1 || true
    done
    ;;
  *)
    echo "usage: $0 {init|run}" >&2
    exit 1
    ;;
esac
