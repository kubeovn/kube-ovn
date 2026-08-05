#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT="$SCRIPT_DIR/kamaji-e2e.sh"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

export HCP_OVN_ENDPOINT=10.0.0.10

"$SCRIPT" render-mgmt-values > "$TMP_DIR/mgmt-values.yaml"
"$SCRIPT" render-tenant-values > "$TMP_DIR/tenant-values.yaml"
"$SCRIPT" render-tenant-control-plane > "$TMP_DIR/tenant-tcp.yaml"
"$SCRIPT" render-mgmt-kind-config > "$TMP_DIR/mgmt-kind.yaml"
TENANT_CONTROL_PLANE_REPLICAS=3 "$SCRIPT" render-tenant-control-plane > "$TMP_DIR/tenant-tcp-ha.yaml"
TENANT_CONTROL_PLANE_REPLICAS=3 "$SCRIPT" render-mgmt-kind-config > "$TMP_DIR/mgmt-kind-ha.yaml"
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

require_count() {
  local file=$1
  local expected=$2
  local text=$3
  local actual
  actual=$(grep -Fc -- "$text" "$file" || true)
  if [ "$actual" != "$expected" ]; then
    echo "expected $expected occurrence(s) in $file, got $actual: $text" >&2
    exit 1
  fi
}

require_workflow_case() {
  local file=$1
  local family=$2
  local control_plane=$3
  local replicas=$4
  awk -v family="$family" -v control_plane="$control_plane" -v replicas="$replicas" '
    $0 == "          - ip-family: " family || $0 == "            ip-family: " family {in_case=1; next}
    in_case && $0 == "            tenant-control-plane: " control_plane {seen_control_plane=1; next}
    in_case && seen_control_plane && $0 == "            tenant-control-plane-replicas: " replicas {found=1}
    in_case && $0 ~ /^          - / {in_case=0; seen_control_plane=0}
    END {exit found ? 0 : 1}
  ' "$file" || {
    echo "missing workflow case in $file: $family, $control_plane, replicas=$replicas" >&2
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
require_count "$TMP_DIR/mgmt-kind.yaml" 0 "  - role: worker"
require_count "$TMP_DIR/mgmt-kind-ha.yaml" 3 "  - role: worker"
require_count "$TMP_DIR/mgmt-kind-ha.yaml" 3 "      kube-ovn/tenant-control-plane: \"true\""
require_line "$TMP_DIR/tenant-tcp.yaml" "      replicas: 1"
reject_text "$TMP_DIR/tenant-tcp.yaml" "      nodeSelector:"
reject_text "$TMP_DIR/tenant-tcp.yaml" "      affinity:"
require_line "$TMP_DIR/tenant-tcp-ha.yaml" "      replicas: 3"
require_line "$TMP_DIR/tenant-tcp-ha.yaml" "      nodeSelector:"
require_line "$TMP_DIR/tenant-tcp-ha.yaml" "        kube-ovn/tenant-control-plane: \"true\""
require_line "$TMP_DIR/tenant-tcp-ha.yaml" "      affinity:"
require_line "$TMP_DIR/tenant-tcp-ha.yaml" "        podAntiAffinity:"
require_line "$TMP_DIR/tenant-tcp-ha.yaml" "              topologyKey: kubernetes.io/hostname"

require_line "$TMP_DIR/vars" "KUBE_OVN_HCP_OVN_NB_ADDR=tcp:10.0.0.10:30641"
require_line "$TMP_DIR/vars" "KUBE_OVN_HCP_OVN_SB_ADDR=tcp:10.0.0.10:30642"
require_line "$TMP_DIR/vars" "KUBE_OVN_KAMAJI_TENANT_CONTROL_PLANE_REPLICAS=1"
reject_text "$TMP_DIR/vars" "KUBE_OVN_KAMAJI_MGMT_VIP"

for family in ipv4 ipv6 dual; do
  E2E_IP_FAMILY="$family" "$SCRIPT" render-mgmt-values > "$TMP_DIR/mgmt-values-$family.yaml"
  E2E_IP_FAMILY="$family" "$SCRIPT" render-tenant-values > "$TMP_DIR/tenant-values-$family.yaml"
  E2E_IP_FAMILY="$family" "$SCRIPT" render-tenant-control-plane > "$TMP_DIR/tenant-tcp-$family.yaml"
  E2E_IP_FAMILY="$family" "$SCRIPT" render-tenant-kube-proxy-manifest 10.0.0.20:6443 > "$TMP_DIR/kube-proxy-manifest-$family.yaml"
done
"$SCRIPT" render-tenant-worker-docker-args > "$TMP_DIR/tenant-worker-docker-args"
"$SCRIPT" render-tenant-worker-kubelet-env > "$TMP_DIR/tenant-worker-kubelet-env"
"$SCRIPT" render-tenant-kubeovn-image > "$TMP_DIR/tenant-kubeovn-image"
"$SCRIPT" render-tenant-e2e-images > "$TMP_DIR/tenant-e2e-images"

require_line "$TMP_DIR/mgmt-values-ipv4.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-values-ipv4.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "    podCidr: 10.16.0.0/16"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "    serviceCidr: 10.96.0.0/12"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "      - 10.96.0.10"
require_line "$TMP_DIR/tenant-tcp-ipv4.yaml" "    coreDNS: {}"
reject_text "$TMP_DIR/tenant-tcp-ipv4.yaml" "    kubeProxy: {}"
require_line "$TMP_DIR/kube-proxy-manifest-ipv4.yaml" "          server: https://10.0.0.20:6443"
require_line "$TMP_DIR/kube-proxy-manifest-ipv4.yaml" "    clusterCIDR: 10.16.0.0/16"
require_line "$TMP_DIR/kube-proxy-manifest-ipv4.yaml" "      maxPerCore: 0"
require_line "$TMP_DIR/kube-proxy-manifest-ipv4.yaml" "      min: 0"
require_line "$TMP_DIR/kube-proxy-manifest-ipv4.yaml" "  name: kube-proxy-hcp-e2e"
require_line "$TMP_DIR/kube-proxy-manifest-ipv4.yaml" "          image: registry.k8s.io/kube-proxy:v1.30.2"

require_line "$TMP_DIR/mgmt-values-ipv6.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-values-ipv6.yaml" "  NET_STACK: ipv6"
require_line "$TMP_DIR/tenant-tcp-ipv6.yaml" "    podCidr: 10.16.0.0/16"
require_line "$TMP_DIR/tenant-tcp-ipv6.yaml" "    serviceCidr: 10.96.0.0/12"
require_line "$TMP_DIR/tenant-tcp-ipv6.yaml" "      - 10.96.0.10"
reject_text "$TMP_DIR/tenant-tcp-ipv6.yaml" "    coreDNS: {}"
reject_text "$TMP_DIR/tenant-tcp-ipv6.yaml" "    kubeProxy: {}"
require_line "$TMP_DIR/kube-proxy-manifest-ipv6.yaml" "          server: https://10.0.0.20:6443"
require_line "$TMP_DIR/kube-proxy-manifest-ipv6.yaml" "    clusterCIDR: 10.16.0.0/16"
require_line "$TMP_DIR/kube-proxy-manifest-ipv6.yaml" "      maxPerCore: 0"
require_line "$TMP_DIR/kube-proxy-manifest-ipv6.yaml" "      min: 0"
require_line "$TMP_DIR/kube-proxy-manifest-ipv6.yaml" "  name: kube-proxy-hcp-e2e"
require_line "$TMP_DIR/kube-proxy-manifest-ipv6.yaml" "          image: registry.k8s.io/kube-proxy:v1.30.2"

require_line "$TMP_DIR/mgmt-values-dual.yaml" "  NET_STACK: ipv4"
require_line "$TMP_DIR/tenant-values-dual.yaml" "  NET_STACK: dual_stack"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "    podCidr: 10.16.0.0/16"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "    serviceCidr: 10.96.0.0/12"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "      - 10.96.0.10"
require_line "$TMP_DIR/tenant-tcp-dual.yaml" "    coreDNS: {}"
reject_text "$TMP_DIR/tenant-tcp-dual.yaml" "    kubeProxy: {}"
require_line "$TMP_DIR/kube-proxy-manifest-dual.yaml" "          server: https://10.0.0.20:6443"
require_line "$TMP_DIR/kube-proxy-manifest-dual.yaml" "    clusterCIDR: 10.16.0.0/16"
require_line "$TMP_DIR/kube-proxy-manifest-dual.yaml" "      maxPerCore: 0"
require_line "$TMP_DIR/kube-proxy-manifest-dual.yaml" "      min: 0"
require_line "$TMP_DIR/kube-proxy-manifest-dual.yaml" "  name: kube-proxy-hcp-e2e"
require_line "$TMP_DIR/kube-proxy-manifest-dual.yaml" "          image: registry.k8s.io/kube-proxy:v1.30.2"

if E2E_IP_FAMILY=bad "$SCRIPT" render-mgmt-values > "$TMP_DIR/bad-values.yaml" 2> "$TMP_DIR/bad-values.err"; then
  echo "invalid E2E_IP_FAMILY should fail" >&2
  exit 1
fi
require_text "$TMP_DIR/bad-values.err" "unsupported E2E_IP_FAMILY: bad"

if TENANT_CONTROL_PLANE_REPLICAS=bad "$SCRIPT" render-tenant-control-plane > "$TMP_DIR/bad-replicas.yaml" 2> "$TMP_DIR/bad-replicas.err"; then
  echo "invalid TENANT_CONTROL_PLANE_REPLICAS should fail" >&2
  exit 1
fi
require_text "$TMP_DIR/bad-replicas.err" "unsupported TENANT_CONTROL_PLANE_REPLICAS: bad"

require_line "$TMP_DIR/tenant-worker-docker-args" "--tmpfs"
require_line "$TMP_DIR/tenant-worker-docker-args" "/run"
require_line "$TMP_DIR/tenant-worker-docker-args" "--volume"
require_line "$TMP_DIR/tenant-worker-docker-args" "/var"
require_line "$TMP_DIR/tenant-worker-docker-args" "--cgroupns=private"
reject_text "$TMP_DIR/tenant-worker-docker-args" "--cgroupns=host"
require_line "$TMP_DIR/tenant-worker-kubelet-env" "KUBELET_EXTRA_ARGS=--fail-swap-on=false"
require_line "$TMP_DIR/tenant-kubeovn-image" "docker.io/kubeovn/kube-ovn:dev"
require_line "$TMP_DIR/tenant-e2e-images" "ghcr.io/kubeovn/pause:3.9"
require_line "$TMP_DIR/tenant-e2e-images" "ghcr.io/kubeovn/agnhost:2.47"
require_text "$SCRIPT" "install_tenant_kube_proxy"
require_text "$SCRIPT" "Patching kube-ovn-pinger for IPv6-only tenant bootstrap"
require_text "$SCRIPT" '"hostNetwork":true'
require_text "$SCRIPT" '"dnsPolicy":"Default"'
require_text "$SCRIPT" "KubeProxyConfiguration"
require_text "$SCRIPT" "maxPerCore: 0"
require_text "$SCRIPT" "min: 0"
reject_text "$SCRIPT" "clastix.github.io/charts"
reject_text "$SCRIPT" "helm repo add clastix"
require_text "$SCRIPT" "KAMAJI_CHART_VERSION=\${KAMAJI_CHART_VERSION:-1.0.0}"
require_text "$SCRIPT" "5ce3f6c337edc63347a32b2b3e6927429181e44c"
require_text "$SCRIPT" "2e642a485eae8bd964c0a363b4ce01bd8379d05b42960b7919676f96f7c63b36"
require_text "$SCRIPT" "KAMAJI_CHART_URL=\${KAMAJI_CHART_URL:-https://raw.githubusercontent.com/clastix/charts/\$KAMAJI_CHART_SOURCE_COMMIT/kamaji-\$KAMAJI_CHART_VERSION.tgz}"
require_text "$SCRIPT" "--retry 5"
require_text "$SCRIPT" "--retry-all-errors"
require_text "$SCRIPT" "sha256sum --check"

require_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_HCP_OVN_NB_ADDR"
require_text "$SCRIPT_DIR/../makefiles/e2e.mk" "KUBE_OVN_HCP_OVN_SB_ADDR"
require_text "$SCRIPT_DIR/../makefiles/e2e.mk" '$(GINKGO_E2E_RUN) --focus=CNI:Kube-OVN ./test/e2e/kamaji/kamaji.test'
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
require_workflow_case "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" ipv4 single 1
require_workflow_case "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" ipv6 single 1
require_workflow_case "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" dual single 1
require_workflow_case "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" ipv4 ha 3
require_workflow_case "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" ipv6 ha 3
require_workflow_case "$SCRIPT_DIR/../.github/workflows/build-x86-image.yaml" dual ha 3
require_workflow_case "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" ipv4 single 1
require_workflow_case "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" ipv6 single 1
require_workflow_case "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" dual single 1
require_workflow_case "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" ipv4 ha 3
require_workflow_case "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" ipv6 ha 3
require_workflow_case "$SCRIPT_DIR/../.github/workflows/scheduled-e2e.yaml" dual ha 3
