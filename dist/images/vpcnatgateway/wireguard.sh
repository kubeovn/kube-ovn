#!/usr/bin/env bash
set -euo pipefail

WG_CONF="${WG_CONF:-/etc/wireguard/wg0.conf}"
WG_IF="${WG_IF:-wg0}"

enable_forward() {
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
  sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null 2>&1 || true
  if command -v iptables >/dev/null 2>&1; then
    iptables -C FORWARD -i "$WG_IF" -j ACCEPT 2>/dev/null || iptables -A FORWARD -i "$WG_IF" -j ACCEPT
    iptables -C FORWARD -o "$WG_IF" -j ACCEPT 2>/dev/null || iptables -A FORWARD -o "$WG_IF" -j ACCEPT
  fi
}

sync_interface() {
  enable_forward
  if [ ! -f "$WG_CONF" ]; then
    echo "missing wireguard config $WG_CONF" >&2
    exit 1
  fi
  if ip link show "$WG_IF" >/dev/null 2>&1; then
    wg syncconf "$WG_IF" <(wg-quick strip "$WG_CONF")
  else
    wg-quick up "$WG_CONF"
  fi
  if ! ip link show "$WG_IF" >/dev/null 2>&1; then
    echo "wireguard interface $WG_IF is not present after sync" >&2
    exit 1
  fi
}

case "${1:-}" in
  init|sync)
    sync_interface
    ;;
  *)
    echo "usage: $0 {init|sync}" >&2
    exit 1
    ;;
esac
