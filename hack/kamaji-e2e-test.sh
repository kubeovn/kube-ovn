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
  grep -Fx -- "$expected" "$file" >/dev/null || {
    echo "missing expected line in $file: $expected" >&2
    exit 1
  }
}

reject_text() {
  local file=$1
  local unexpected=$2
  if grep -F -- "$unexpected" "$file" >/dev/null; then
    echo "unexpected text in $file: $unexpected" >&2
    exit 1
  fi
}

require_text() {
  local file=$1
  local expected=$2
  grep -F -- "$expected" "$file" >/dev/null || {
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

for family in ipv4 ipv6 dual; do
  E2E_IP_FAMILY="$family" "$SCRIPT" render-mgmt-values > "$TMP_DIR/mgmt-values-$family.yaml"
  E2E_IP_FAMILY="$family" "$SCRIPT" render-tenant-values > "$TMP_DIR/tenant-values-$family.yaml"
  E2E_IP_FAMILY="$family" "$SCRIPT" render-tenant-control-plane > "$TMP_DIR/tenant-tcp-$family.yaml"
done
"$SCRIPT" render-tenant-worker-docker-args > "$TMP_DIR/tenant-worker-docker-args"
"$SCRIPT" render-tenant-worker-kubelet-env > "$TMP_DIR/tenant-worker-kubelet-env"
"$SCRIPT" render-tenant-kubeovn-image > "$TMP_DIR/tenant-kubeovn-image"
cat > "$TMP_DIR/kube-proxy-config.in" <<'EOF'
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
bindAddress: 0.0.0.0
conntrack:
  maxPerCore: 32768
  min: 131072
  tcpCloseWaitTimeout: 1h0m0s
mode: iptables
EOF
"$SCRIPT" patch-kube-proxy-config < "$TMP_DIR/kube-proxy-config.in" > "$TMP_DIR/kube-proxy-config.out"
cat > "$TMP_DIR/kube-proxy-config-defaults.in" <<'EOF'
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
conntrack:
  tcpCloseWaitTimeout: 1h0m0s
mode: iptables
EOF
"$SCRIPT" patch-kube-proxy-config < "$TMP_DIR/kube-proxy-config-defaults.in" > "$TMP_DIR/kube-proxy-config-defaults.out"

require_line "$TMP_DIR/mgmt-values-ipv4.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-values-ipv4.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "    podCidr: 10.16.0.0/16"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "    serviceCidr: 10.96.0.0/12"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "      - 10.96.0.10"

require_line "$TMP_DIR/mgmt-values-ipv6.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-values-ipv6.yaml" "  NET_STACK: ipv6"
require_line "$TMP_DIR/tenant-tcp-ipv6.yaml" "    podCidr: 10.16.0.0/16"
require_line "$TMP_DIR/tenant-tcp-ipv6.yaml" "    serviceCidr: 10.96.0.0/12"
require_line "$TMP_DIR/tenant-tcp-ipv6.yaml" "      - 10.96.0.10"

require_line "$TMP_DIR/mgmt-values-dual.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-values-dual.yaml" "  NET_STACK: dual_stack"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "    podCidr: 10.16.0.0/16"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "    serviceCidr: 10.96.0.0/12"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "      - 10.96.0.10"

if E2E_IP_FAMILY=bad "$SCRIPT" render-mgmt-values > "$TMP_DIR/bad-values.yaml" 2> "$TMP_DIR/bad-values.err"; then
  echo "invalid E2E_IP_FAMILY should fail" >&2
  exit 1
fi
require_text "$TMP_DIR/bad-values.err" "unsupported E2E_IP_FAMILY: bad"

require_line "$TMP_DIR/tenant-worker-docker-args" "--tmpfs"
require_line "$TMP_DIR/tenant-worker-docker-args" "/run"
require_line "$TMP_DIR/tenant-worker-docker-args" "--volume"
require_line "$TMP_DIR/tenant-worker-docker-args" "/var"
require_line "$TMP_DIR/tenant-worker-docker-args" "--cgroupns=private"
reject_text "$TMP_DIR/tenant-worker-docker-args" "--cgroupns=host"
require_line "$TMP_DIR/tenant-worker-kubelet-env" "KUBELET_EXTRA_ARGS=--fail-swap-on=false"
require_line "$TMP_DIR/tenant-kubeovn-image" "docker.io/kubeovn/kube-ovn:dev"
require_line "$TMP_DIR/kube-proxy-config.out" "  maxPerCore: 0"
require_line "$TMP_DIR/kube-proxy-config.out" "  min: 0"
reject_text "$TMP_DIR/kube-proxy-config.out" "  maxPerCore: 32768"
reject_text "$TMP_DIR/kube-proxy-config.out" "  min: 131072"
require_line "$TMP_DIR/kube-proxy-config-defaults.out" "  maxPerCore: 0"
require_line "$TMP_DIR/kube-proxy-config-defaults.out" "  min: 0"
require_text "$SCRIPT" "KUBE_PROXY_CONFIGMAP_PROPAGATION_WAIT"

require_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_HCP_OVN_NB_ADDR"
require_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_HCP_OVN_SB_ADDR"
reject_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_KAMAJI_MGMT_VIP"

for source_file in \
  "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" \
  "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" \
  "$SCRIPT_DIR/kamaji-e2e.sh" \
  "$SCRIPT_DIR/../makefiles/e2e.mk" \
  "$SCRIPT_DIR/../makefiles/kind.mk" \
  "$SCRIPT_DIR/../test/e2e/kamaji/kamaji_test.go" \
  "$SCRIPT_DIR/../charts/kube-ovn/values.yaml" \
  "$SCRIPT_DIR/../charts/kube-ovn/templates/_helpers.tpl"; do
  reject_text "$source_file" "Kamaji HCP"
  reject_text "$source_file" "Kamaji-style"
  reject_text "$source_file" "external HCP ovn-central"
  reject_text "$source_file" "HCP mode was introduced"
  reject_text "$source_file" "Kube-OVN HCP E2E"
  reject_text "$source_file" "Kamaji-style hosted-control-plane"
done

require_text "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" "name: Kube-OVN Hosted OVN Central E2E"
require_text "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" "name: Kube-OVN Hosted OVN Central E2E"
