#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
INSTALL_SCRIPT="$SCRIPT_DIR/install.sh"

grep -Fxq 'ENABLE_RECOMPUTE=${ENABLE_RECOMPUTE:-false}' "$INSTALL_SCRIPT"

extract_cni_args() {
  local recompute=$1
  awk -v recompute="$recompute" '
    /^      - name: cni-server$/ { in_cni_server = 1 }
    in_cni_server && /^        env:$/ { exit }
    in_cni_server && /--enable-recompute=\$ENABLE_RECOMPUTE$/ {
      sub(/\$ENABLE_RECOMPUTE$/, recompute)
      print
    }
  ' "$INSTALL_SCRIPT"
}

default_args=$(extract_cni_args false)
grep -qx -- '          - --enable-recompute=false' <<<"$default_args"

enabled_args=$(extract_cni_args true)
grep -qx -- '          - --enable-recompute=true' <<<"$enabled_args"

echo 'install.sh recompute tests passed'
