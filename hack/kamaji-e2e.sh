#!/usr/bin/env bash
#
# Set up / tear down a local Kamaji-style split-cluster environment to
# exercise kube-ovn's `installMode=controlPlaneOnly` + `dataPlaneOnly`
# Helm flow end to end.
#
# Layout the script produces:
#
#   kind cluster `mgmt`         -- runs Kamaji + cert-manager + MetalLB.
#       └── kube-ovn controlPlaneOnly install: ovn-central (single-replica,
#           PVC-backed) + ovn-nb/ovn-sb/ovn-northd Services exposed on a
#           MetalLB VIP (shared via the allow-shared-ip annotation).
#   docker container `tenant-worker-0`
#       └── kubeadm-joined to the Kamaji-hosted tenant apiserver (also on a
#           MetalLB VIP), running ovs-ovn / kube-ovn-cni / kube-ovn-controller
#           via the dataPlaneOnly install pointed at the mgmt LB.
#
# Subcommands:
#   setup      bring the whole thing up
#   teardown   tear it down
#   kubeconfig print the path to the tenant kubeconfig
#   vars       print the env variables for the tenant setup
#
# Notes:
# - The tenant worker uses containerd's native snapshotter to dodge a known
#   limitation of nested overlayfs whiteout handling.
# - All defaults are configurable via env vars; see the block below.

set -euo pipefail

MGMT_KIND_NAME=${MGMT_KIND_NAME:-mgmt}
MGMT_KIND_NODE_IMAGE=${MGMT_KIND_NODE_IMAGE:-kindest/node:v1.31.4}
TENANT_KIND_NODE_IMAGE=${TENANT_KIND_NODE_IMAGE:-kindest/node:v1.30.0}
TENANT_K8S_VERSION=${TENANT_K8S_VERSION:-v1.30.2}

MGMT_LB_VIP=${MGMT_LB_VIP:-172.18.255.210}
TENANT_LB_VIP_RANGE_START=${TENANT_LB_VIP_RANGE_START:-172.18.255.200}
TENANT_LB_VIP_RANGE_END=${TENANT_LB_VIP_RANGE_END:-172.18.255.250}

CERT_MANAGER_VERSION=${CERT_MANAGER_VERSION:-v1.15.3}
METALLB_VERSION=${METALLB_VERSION:-v0.14.8}

KUBEOVN_IMAGE=${KUBEOVN_IMAGE:-kubeovn/kube-ovn:dev}
JOB_DIR=${JOB_DIR:-/tmp/kamaji-e2e}
REGISTRY_NAME=${REGISTRY_NAME:-kamaji-e2e-reg}

CHART_DIR=${CHART_DIR:-$(cd "$(dirname "$0")/.." && pwd)/charts/kube-ovn}

cmd_vars() {
  cat <<EOF
JOB_DIR=$JOB_DIR
KUBECONFIG=$JOB_DIR/tenant.kubeconfig
KUBE_OVN_KAMAJI_MGMT_VIP=$MGMT_LB_VIP
KUBE_OVN_KAMAJI_TENANT_WORKER=tenant-worker-0
EOF
}

cmd_kubeconfig() {
  echo "$JOB_DIR/tenant.kubeconfig"
}

require_tools() {
  local missing=()
  for t in docker kind kubectl helm; do
    command -v "$t" >/dev/null 2>&1 || missing+=("$t")
  done
  if [ ${#missing[@]} -gt 0 ]; then
    echo "ERROR: missing required tools: ${missing[*]}" >&2
    exit 1
  fi
}

ensure_image() {
  if ! docker image inspect "$KUBEOVN_IMAGE" >/dev/null 2>&1; then
    echo "ERROR: $KUBEOVN_IMAGE not found in local docker; build it first (make build-dev)" >&2
    exit 1
  fi
}

setup_mgmt_cluster() {
  echo ">>> Creating mgmt kind cluster ($MGMT_KIND_NAME)..."
  cat > "$JOB_DIR/mgmt-kind.yaml" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: $MGMT_KIND_NAME
networking:
  apiServerAddress: 127.0.0.1
nodes:
  - role: control-plane
    image: $MGMT_KIND_NODE_IMAGE
EOF
  kind create cluster --config "$JOB_DIR/mgmt-kind.yaml"
  kubectl --context="kind-$MGMT_KIND_NAME" label node "$MGMT_KIND_NAME-control-plane" \
    kube-ovn/role=master --overwrite
  kind load docker-image "$KUBEOVN_IMAGE" --name "$MGMT_KIND_NAME"
}

setup_prereqs() {
  echo ">>> Installing cert-manager $CERT_MANAGER_VERSION..."
  kubectl --context="kind-$MGMT_KIND_NAME" apply \
    -f "https://github.com/cert-manager/cert-manager/releases/download/$CERT_MANAGER_VERSION/cert-manager.yaml"
  kubectl --context="kind-$MGMT_KIND_NAME" wait --for=condition=Available deploy --all \
    -n cert-manager --timeout=180s

  echo ">>> Installing MetalLB $METALLB_VERSION..."
  kubectl --context="kind-$MGMT_KIND_NAME" apply \
    -f "https://raw.githubusercontent.com/metallb/metallb/$METALLB_VERSION/config/manifests/metallb-native.yaml"
  kubectl --context="kind-$MGMT_KIND_NAME" -n metallb-system wait \
    --for=condition=Available deploy/controller --timeout=180s
  kubectl --context="kind-$MGMT_KIND_NAME" -n metallb-system rollout status \
    ds/speaker --timeout=180s

  cat <<EOF | kubectl --context="kind-$MGMT_KIND_NAME" apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata: {name: kind-pool, namespace: metallb-system}
spec:
  addresses:
    - $TENANT_LB_VIP_RANGE_START-$TENANT_LB_VIP_RANGE_END
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata: {name: empty, namespace: metallb-system}
EOF

  echo ">>> Installing Kamaji operator..."
  helm repo add clastix https://clastix.github.io/charts --force-update >/dev/null
  helm repo update clastix >/dev/null
  helm upgrade --install --kube-context="kind-$MGMT_KIND_NAME" \
    kamaji clastix/kamaji \
    --namespace kamaji-system --create-namespace \
    --set 'resources=null'
  kubectl --context="kind-$MGMT_KIND_NAME" -n kamaji-system wait \
    --for=condition=Available deploy --all --timeout=180s
}

install_control_plane() {
  echo ">>> Installing kube-ovn (controlPlaneOnly) on mgmt..."
  cat > "$JOB_DIR/mgmt-values.yaml" <<EOF
namespace: kube-system
installMode: controlPlaneOnly
OVN_CENTRAL_MODE: single

image:
  pullPolicy: Never

global:
  registry:
    address: docker.io/kubeovn
  images:
    kubeovn:
      repository: kube-ovn
      tag: dev

ovn-central:
  storage:
    storageClassName: standard
    size: 5Gi
  service:
    type: LoadBalancer
    loadBalancerIP: $MGMT_LB_VIP
    externalTrafficPolicy: Local

networking:
  ENABLE_SSL: false
EOF
  helm install --kube-context="kind-$MGMT_KIND_NAME" \
    kube-ovn "$CHART_DIR" \
    -n kube-system -f "$JOB_DIR/mgmt-values.yaml"
  kubectl --context="kind-$MGMT_KIND_NAME" wait --for=condition=Ready \
    pod -n kube-system -l app=ovn-central --timeout=300s
}

create_tenant_control_plane() {
  echo ">>> Creating TenantControlPlane via Kamaji..."
  cat > "$JOB_DIR/tenant-tcp.yaml" <<EOF
apiVersion: kamaji.clastix.io/v1alpha1
kind: TenantControlPlane
metadata:
  name: tenant
  namespace: default
spec:
  dataStore: default
  controlPlane:
    deployment:
      replicas: 1
      resources:
        apiServer:
          requests: {cpu: 100m, memory: 256Mi}
          limits:   {cpu: "1", memory: 1Gi}
        controllerManager:
          requests: {cpu: 100m, memory: 128Mi}
          limits:   {cpu: 500m, memory: 512Mi}
        scheduler:
          requests: {cpu: 100m, memory: 128Mi}
          limits:   {cpu: 500m, memory: 512Mi}
    service:
      serviceType: LoadBalancer
  kubernetes:
    version: $TENANT_K8S_VERSION
    kubelet:
      cgroupfs: systemd
    admissionControllers: [ResourceQuota, LimitRanger]
  networkProfile:
    port: 6443
  addons:
    coreDNS: {}
    kubeProxy: {}
EOF
  kubectl --context="kind-$MGMT_KIND_NAME" apply -f "$JOB_DIR/tenant-tcp.yaml"

  echo ">>> Waiting for TenantControlPlane Ready..."
  for i in $(seq 1 60); do
    if [ "$(kubectl --context="kind-$MGMT_KIND_NAME" get tcp tenant -n default \
        -o jsonpath='{.status.kubernetesResources.version.status}' 2>/dev/null)" = "Ready" ]; then
      break
    fi
    sleep 5
  done

  kubectl --context="kind-$MGMT_KIND_NAME" -n default get secret tenant-admin-kubeconfig \
    -o jsonpath='{.data.admin\.conf}' | base64 -d > "$JOB_DIR/tenant.kubeconfig"
  echo ">>> tenant kubeconfig written to $JOB_DIR/tenant.kubeconfig"
}

setup_local_registry() {
  echo ">>> Starting local registry on the kind network..."
  docker rm -f "$REGISTRY_NAME" >/dev/null 2>&1 || true
  docker run -d --name "$REGISTRY_NAME" --network=kind --restart=always \
    -p 5000:5000 registry:2 >/dev/null
  sleep 3
  REG_IP=$(docker inspect "$REGISTRY_NAME" \
    -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
  docker tag "$KUBEOVN_IMAGE" "localhost:5000/${KUBEOVN_IMAGE#*/}"
  docker push "localhost:5000/${KUBEOVN_IMAGE#*/}" >/dev/null
  echo "$REG_IP" > "$JOB_DIR/reg-ip"
}

setup_tenant_worker() {
  local reg_ip
  reg_ip=$(cat "$JOB_DIR/reg-ip")
  echo ">>> Spawning tenant-worker-0..."
  docker rm -f tenant-worker-0 >/dev/null 2>&1 || true
  docker run -d --name tenant-worker-0 \
    --privileged --network=kind \
    --hostname tenant-worker-0 \
    --tmpfs /run --tmpfs /tmp \
    -v /lib/modules:/lib/modules:ro \
    --security-opt apparmor=unconfined \
    --security-opt seccomp=unconfined \
    --cgroupns=host \
    "$TENANT_KIND_NODE_IMAGE" >/dev/null
  sleep 8

  echo ">>> Reconfiguring containerd to use native snapshotter + allow local registry..."
  docker exec tenant-worker-0 sh -c "
    sed -i 's/snapshotter = \"overlayfs\"/snapshotter = \"native\"/' /etc/containerd/config.toml
    mkdir -p /etc/containerd/certs.d/$reg_ip:5000
    cat > /etc/containerd/certs.d/$reg_ip:5000/hosts.toml <<HOSTS
server = \"http://$reg_ip:5000\"
[host.\"http://$reg_ip:5000\"]
  capabilities = [\"pull\", \"resolve\"]
  skip_verify = true
HOSTS
    cat >> /etc/containerd/config.toml <<CFG

[plugins.\"io.containerd.grpc.v1.cri\".registry]
  config_path = \"/etc/containerd/certs.d\"
CFG
    systemctl restart containerd
  "
  sleep 6

  echo ">>> Pre-pulling $KUBEOVN_IMAGE on tenant worker..."
  docker exec tenant-worker-0 crictl pull "$reg_ip:5000/${KUBEOVN_IMAGE#*/}"
  docker exec tenant-worker-0 ctr -n k8s.io images tag --force \
    "$reg_ip:5000/${KUBEOVN_IMAGE#*/}" "docker.io/${KUBEOVN_IMAGE#*/}"
}

join_tenant_worker() {
  echo ">>> Generating kubeadm join command..."
  docker run --rm --network=kind -v "$JOB_DIR/tenant.kubeconfig:/kc:ro" \
    --entrypoint kubeadm "$TENANT_KIND_NODE_IMAGE" \
    --kubeconfig=/kc token create --print-join-command > "$JOB_DIR/join.txt"
  local join_cmd
  join_cmd=$(grep "^kubeadm join" "$JOB_DIR/join.txt")
  echo ">>> Joining tenant-worker-0 to tenant apiserver..."
  docker exec tenant-worker-0 bash -c "$join_cmd --ignore-preflight-errors=all"
}

install_data_plane() {
  echo ">>> Installing kube-ovn (dataPlaneOnly) on tenant..."
  cat > "$JOB_DIR/tenant-values.yaml" <<EOF
namespace: kube-system
installMode: dataPlaneOnly

externalOvnCentral:
  endpoint: $MGMT_LB_VIP
  nbPort: 6641
  sbPort: 6642

image:
  pullPolicy: Never

global:
  registry:
    address: docker.io/kubeovn
  images:
    kubeovn:
      repository: kube-ovn
      tag: dev

networking:
  ENABLE_SSL: false
EOF
  helm install --kubeconfig "$JOB_DIR/tenant.kubeconfig" \
    kube-ovn "$CHART_DIR" \
    -n kube-system --create-namespace -f "$JOB_DIR/tenant-values.yaml"

  echo ">>> Waiting for tenant data-plane components..."
  KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system rollout status \
    deploy/kube-ovn-controller --timeout=300s
  KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system rollout status \
    ds/ovs-ovn --timeout=300s
  KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system rollout status \
    ds/kube-ovn-cni --timeout=300s
}

cmd_setup() {
  require_tools
  ensure_image
  mkdir -p "$JOB_DIR"
  setup_mgmt_cluster
  setup_prereqs
  install_control_plane
  create_tenant_control_plane
  setup_local_registry
  setup_tenant_worker
  join_tenant_worker
  install_data_plane
  echo ""
  echo "=== Kamaji e2e environment ready ==="
  cmd_vars
}

cmd_teardown() {
  echo ">>> Tearing down Kamaji e2e environment..."
  kind delete cluster --name "$MGMT_KIND_NAME" 2>/dev/null || true
  docker rm -f tenant-worker-0 "$REGISTRY_NAME" 2>/dev/null || true
  docker rmi "localhost:5000/${KUBEOVN_IMAGE#*/}" 2>/dev/null || true
  rm -rf "$JOB_DIR"
}

case "${1:-}" in
  setup)      cmd_setup ;;
  teardown)   cmd_teardown ;;
  kubeconfig) cmd_kubeconfig ;;
  vars)       cmd_vars ;;
  *)
    cat >&2 <<USAGE
Usage: $0 <setup|teardown|kubeconfig|vars>

  setup       Bring up the mgmt kind cluster + Kamaji + tenant worker and
              install both halves of kube-ovn.
  teardown    Tear everything down.
  kubeconfig  Print the path to the tenant kubeconfig (used by the e2e job).
  vars        Print the env vars consumed by the Ginkgo e2e suite.
USAGE
    exit 1 ;;
esac
