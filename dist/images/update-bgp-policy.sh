#!/usr/bin/env bash
# update-bgp-policy.sh
# shellcheck disable=SC2086,SC2155

set -euo pipefail

GOBGP_BIN=${GOBGP_BIN:-$(command -v gobgp || true)}
[[ -z "$GOBGP_BIN" ]] && { echo "ERROR: gobgp binary not found" >&2; exit 1; }

die() { echo "ERROR: $*" >&2; exit 1; }

usage() {
  cat >&2 <<EOF
Usage:
  $0 set-neighbor-policy <NEIGHBOR_IP>
  $0 flush-neighbor-policy <NEIGHBOR_IP>
  $0 flush-prefix-in <NEIGHBOR_IP>
  $0 flush-prefix-out <NEIGHBOR_IP>
  $0 add-prefix <in|out> <NEIGHBOR_IP> <PREFIXS...>

Examples:
  $0 set-neighbor-policy 1.1.1.1
  $0 flush-neighbor-policy 1.1.1.1
  $0 flush-prefix-in 1.1.1.1
  $0 flush-prefix-out 1.1.1.1
  $0 add-prefix in 1.1.1.1 "0.0.0.0/0 0..32","1.1.1.0/24","10.0.0.0/8 16..32"
EOF
  exit 1
}

# Validate IPv4 format (simple regex)
validate_ip() {
  local ip=$1
  if [[ ! $ip =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    die "Invalid IPv4 address: $ip"
  fi
}

exec_cmd() { "$@" || die "failed: $*"; }

set_neighbor_policy() {
  local nbr_ip=$1; validate_ip "$nbr_ip"
  local prefix_in="prefix-${nbr_ip}-in"
  local prefix_out="prefix-${nbr_ip}-out"
  local nbr_name="neighbor-${nbr_ip}"
  local stmt_in="stmt-${nbr_ip}-in"
  local stmt_out="stmt-${nbr_ip}-out"
  local policy_in="policy-${nbr_ip}-in"
  local policy_out="policy-${nbr_ip}-out"

  echo "=== Setting policy for neighbor $nbr_ip ==="
  echo "-> Creating prefix-lists"
  exec_cmd $GOBGP_BIN policy prefix add $prefix_in   0.0.0.0/0 0..32
  exec_cmd $GOBGP_BIN policy prefix add $prefix_out  0.0.0.0/0 0..32

  echo "-> Defining neighbor"
  exec_cmd $GOBGP_BIN policy neighbor add $nbr_name $nbr_ip

  echo "-> Building inbound statement"
  exec_cmd $GOBGP_BIN policy statement add $stmt_in
  exec_cmd $GOBGP_BIN policy statement $stmt_in add action accept
  exec_cmd $GOBGP_BIN policy statement $stmt_in add condition prefix   $prefix_in
  exec_cmd $GOBGP_BIN policy statement $stmt_in add condition neighbor $nbr_name

  echo "-> Building outbound statement"
  exec_cmd $GOBGP_BIN policy statement add $stmt_out
  exec_cmd $GOBGP_BIN policy statement $stmt_out add action accept
  exec_cmd $GOBGP_BIN policy statement $stmt_out add condition prefix   $prefix_out
  exec_cmd $GOBGP_BIN policy statement $stmt_out add condition neighbor $nbr_name

  echo "-> Assembling policies"
  exec_cmd $GOBGP_BIN policy add $policy_in  $stmt_in
  exec_cmd $GOBGP_BIN policy add $policy_out $stmt_out

  echo "-> Applying to global"
  exec_cmd $GOBGP_BIN global policy import add $policy_in
  exec_cmd $GOBGP_BIN global policy export add $policy_out

  echo "=== Policy set successfully for $nbr_ip ==="
}

flush_neighbor_policy() {
  local nbr_ip=$1; validate_ip "$nbr_ip"
  local prefix_in="prefix-${nbr_ip}-in"
  local prefix_out="prefix-${nbr_ip}-out"
  local nbr_name="neighbor-${nbr_ip}"
  local stmt_in="stmt-${nbr_ip}-in"
  local stmt_out="stmt-${nbr_ip}-out"
  local policy_in="policy-${nbr_ip}-in"
  local policy_out="policy-${nbr_ip}-out"

  echo "=== Flushing policy for neighbor $nbr_ip ==="
  echo "-> Removing from global policies"
  exec_cmd $GOBGP_BIN global policy import del $policy_in
  exec_cmd $GOBGP_BIN global policy export del $policy_out

  echo "-> Removing policies"
  exec_cmd $GOBGP_BIN policy del $policy_in
  exec_cmd $GOBGP_BIN policy del $policy_out

  echo "-> Removing statements"
  exec_cmd $GOBGP_BIN policy statement del $stmt_in
  exec_cmd $GOBGP_BIN policy statement del $stmt_out

  echo "-> Removing neighbor definition"
  exec_cmd $GOBGP_BIN policy neighbor del $nbr_name

  echo "-> Removing prefix-lists"
  exec_cmd $GOBGP_BIN policy prefix del $prefix_in
  exec_cmd $GOBGP_BIN policy prefix del $prefix_out

  echo "=== Policy flushed successfully for $nbr_ip ==="
}

flush_prefix_in() {
  local nbr_ip=$1; validate_ip "$nbr_ip"
  local prefix_name="prefix-${nbr_ip}-in"

  echo "=== Flushing all entries from $prefix_name ==="
  $GOBGP_BIN policy prefix $prefix_name 2>/dev/null \
    | awk 'NR>1 && NF>=2 { print $(NF-1), $NF }' \
    | while read -r iprange mask; do
        echo "-> Deleting: $iprange $mask"
        exec_cmd $GOBGP_BIN policy prefix del $prefix_name $iprange $mask
      done

  echo "=== All entries removed from $prefix_name ==="
}

flush_prefix_out() {
  local nbr_ip=$1; validate_ip "$nbr_ip"
  local prefix_name="prefix-${nbr_ip}-out"

  echo "=== Flushing all entries from $prefix_name ==="
  $GOBGP_BIN policy prefix $prefix_name 2>/dev/null \
    | awk 'NR>1 && NF>=2 { print $(NF-1), $NF }' \
    | while read -r iprange mask; do
        echo "-> Deleting: $iprange $mask"
        exec_cmd $GOBGP_BIN policy prefix del $prefix_name $iprange $mask
      done

  echo "=== All entries removed from $prefix_name ==="
}

add_prefix() {
  local dir=$1; shift
  local nbr_ip=$1; shift
  validate_ip "$nbr_ip"
  [[ $dir != in && $dir != out ]] && die "Direction must be 'in' or 'out'"
  local prefix_name="prefix-${nbr_ip}-${dir}"

  # split comma-separated list in first argument after IP
  IFS=',' read -ra entries <<< "$*"

  echo "=== Adding prefixes to $prefix_name ==="
  for entry in "${entries[@]}"; do
    entry="${entry%\"}"
    entry="${entry#\"}"
    entry="${entry##*( )}"
    entry="${entry%%*( )}"

    if [[ $entry =~ ^([^[:space:]]+)[[:space:]]+(.+)$ ]]; then
      local ip_pref=${BASH_REMATCH[1]}
      local mask=${BASH_REMATCH[2]}
      echo "-> Adding: $ip_pref $mask"
      exec_cmd $GOBGP_BIN policy prefix add $prefix_name $ip_pref $mask
    else
      echo "-> Adding: $entry"
      exec_cmd $GOBGP_BIN policy prefix add $prefix_name $entry
    fi
  done
  echo "=== Done ==="
}

main() {
  [[ $# -lt 1 ]] && usage
  case "$1" in
    set-neighbor-policy)
      [[ $# -ne 2 ]] && usage
      set_neighbor_policy "$2"
      ;;
    flush-neighbor-policy)
      [[ $# -ne 2 ]] && usage
      flush_neighbor_policy "$2"
      ;;
    flush-prefix-in)
      [[ $# -ne 2 ]] && usage
      flush_prefix_in "$2"
      ;;
    flush-prefix-out)
      [[ $# -ne 2 ]] && usage
      flush_prefix_out "$2"
      ;;
    add-prefix)
      [[ $# -lt 4 ]] && usage
      shift
      add_prefix "$@"
      ;;
    *)
      usage
      ;;
  esac
}

main "$@"