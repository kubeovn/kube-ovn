#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT="$SCRIPT_DIR/kamaji-e2e.sh"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

export HCP_OVN_ENDPOINT=10.0.0.10

"$SCRIPT" render-mgmt-values > "$TMP_DIR/mgmt-values.yaml"
"$SCRIPT" render-tenant-values > "$TMP_DIR/tenant-values.yaml"
"$SCRIPT" vars > "$TMP_DIR/vars"

require_line() {
  local file=$1
  local expected=$2
  grep -Fx "$expected" "$file" >/dev/null || {
    echo "missing expected line in $file: $expected" >&2
    exit 1
  }
}

reject_text() {
  local file=$1
  local unexpected=$2
  if grep -F "$unexpected" "$file" >/dev/null; then
    echo "unexpected text in $file: $unexpected" >&2
    exit 1
  fi
}

require_text() {
  local file=$1
  local expected=$2
  grep -F "$expected" "$file" >/dev/null || {
    echo "missing expected text in $file: $expected" >&2
    exit 1
  }
}

require_line "$TMP_DIR/mgmt-values.yaml" "installMode: controlPlaneOnly"
require_line "$TMP_DIR/mgmt-values.yaml" "    enabled: true"
require_line "$TMP_DIR/mgmt-values.yaml" "    namespace: hcp"
require_line "$TMP_DIR/mgmt-values.yaml" "    replicas: 1"
require_line "$TMP_DIR/mgmt-values.yaml" "    nbAddress: tcp:10.0.0.10:30641"
require_line "$TMP_DIR/mgmt-values.yaml" "    sbAddress: tcp:10.0.0.10:30642"
require_line "$TMP_DIR/mgmt-values.yaml" "      type: NodePort"
require_line "$TMP_DIR/mgmt-values.yaml" "      nbNodePort: 30641"
require_line "$TMP_DIR/mgmt-values.yaml" "      sbNodePort: 30642"

require_line "$TMP_DIR/tenant-values.yaml" "installMode: dataPlaneOnly"
require_line "$TMP_DIR/tenant-values.yaml" "    enabled: true"
require_line "$TMP_DIR/tenant-values.yaml" "    nbAddress: tcp:10.0.0.10:30641"
require_line "$TMP_DIR/tenant-values.yaml" "    sbAddress: tcp:10.0.0.10:30642"
reject_text "$TMP_DIR/tenant-values.yaml" "externalOvnCentral"

require_line "$TMP_DIR/vars" "KUBE_OVN_HCP_OVN_NB_ADDR=tcp:10.0.0.10:30641"
require_line "$TMP_DIR/vars" "KUBE_OVN_HCP_OVN_SB_ADDR=tcp:10.0.0.10:30642"
reject_text "$TMP_DIR/vars" "KUBE_OVN_KAMAJI_MGMT_VIP"

require_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_HCP_OVN_NB_ADDR"
require_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_HCP_OVN_SB_ADDR"
reject_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_KAMAJI_MGMT_VIP"

for source_file in \
  "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" \
  "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" \
  "$SCRIPT_DIR/kamaji-e2e.sh" \
  "$SCRIPT_DIR/../makefiles/e2e.mk" \
  "$SCRIPT_DIR/../makefiles/kind.mk" \
  "$SCRIPT_DIR/../test/e2e/kamaji/kamaji_test.go"; do
  reject_text "$source_file" "Kamaji HCP"
  reject_text "$source_file" "HCP mode was introduced"
  reject_text "$source_file" "Kube-OVN HCP E2E"
  reject_text "$source_file" "Kamaji-style hosted-control-plane"
done

require_text "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" "name: Kube-OVN Hosted OVN Central E2E"
require_text "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" "name: Kube-OVN Hosted OVN Central E2E"
