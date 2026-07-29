#!/usr/bin/env bash
#
# Set up / tear down a local Kamaji-backed tenant cluster environment to
# exercise kube-ovn's hosted OVN central Helm flow end to end.
#
# Kamaji provides the tenant Kubernetes control plane. Kube-OVN HCP is the
# chart path under test (`ovn-central.hcp.enabled=true`); it is not the same
# feature as Kamaji.
#
# Layout the script produces:
#
#   kind cluster `mgmt`         -- runs Kamaji + cert-manager + MetalLB.
#       └── kube-ovn controlPlaneOnly + ovn-central.hcp.enabled install:
#           ovn-central StatefulSet (single-replica in CI, PVC-backed) plus
#           ovn-nb/ovn-sb NodePort Services in the kube-ovn HCP namespace.
#   docker container `tenant-worker-0`
#       └── kubeadm-joined to the Kamaji-hosted tenant apiserver (also on a
#           MetalLB VIP), running ovs-ovn / kube-ovn-cni / kube-ovn-controller
#           via the dataPlaneOnly install pointed at the HCP OVN DB addresses.
#
# The accompanying Ginkgo suite under test/e2e/kamaji verifies the resulting
# cross-cluster connection and a basic Pod-gets-OVN-IP smoke test.
#
# Subcommands:
#   setup      bring the whole thing up
#   teardown   tear it down
#   kubeconfig print the path to the tenant kubeconfig (used by the e2e job)
#   vars       print the env variables the e2e suite consumes
#   render-mgmt-values   print the mgmt Helm values used by setup
#   render-tenant-values print the tenant Helm values used by setup
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
HCP_NAMESPACE=${HCP_NAMESPACE:-hcp}
HCP_OVN_ENDPOINT=${HCP_OVN_ENDPOINT:-}
HCP_OVN_REPLICAS=${HCP_OVN_REPLICAS:-1}
HCP_OVN_NB_NODE_PORT=${HCP_OVN_NB_NODE_PORT:-30641}
HCP_OVN_SB_NODE_PORT=${HCP_OVN_SB_NODE_PORT:-30642}
E2E_IP_FAMILY=${E2E_IP_FAMILY:-ipv4}

CERT_MANAGER_VERSION=${CERT_MANAGER_VERSION:-v1.15.3}
METALLB_VERSION=${METALLB_VERSION:-v0.14.8}

KUBEOVN_IMAGE=${KUBEOVN_IMAGE:-kubeovn/kube-ovn:dev}
JOB_DIR=${JOB_DIR:-/tmp/kamaji-e2e}
REGISTRY_NAME=${REGISTRY_NAME:-kamaji-e2e-reg}

CHART_DIR=${CHART_DIR:-$(cd "$(dirname "$0")/.." && pwd)/charts/kube-ovn}

chart_net_stack() {
  case "$E2E_IP_FAMILY" in
    ipv4) echo "ipv4" ;;
    ipv6) echo "ipv6" ;;
    dual) echo "dual_stack" ;;
    *)
      echo "unsupported E2E_IP_FAMILY: $E2E_IP_FAMILY" >&2
      return 1
      ;;
  esac
}

tenant_pod_cidr() {
  case "$E2E_IP_FAMILY" in
    ipv4) echo "10.16.0.0/16" ;;
    ipv6) echo "fd00:10:16::/112" ;;
    dual) echo "10.16.0.0/16" ;;
    *)
      echo "unsupported E2E_IP_FAMILY: $E2E_IP_FAMILY" >&2
      return 1
      ;;
  esac
}

tenant_service_cidr() {
  case "$E2E_IP_FAMILY" in
    ipv4) echo "10.96.0.0/12" ;;
    ipv6) echo "fd00:10:96::/112" ;;
    dual) echo "10.96.0.0/12" ;;
    *)
      echo "unsupported E2E_IP_FAMILY: $E2E_IP_FAMILY" >&2
      return 1
      ;;
  esac
}

tenant_dns_service_ips_yaml() {
  case "$E2E_IP_FAMILY" in
    ipv4)
      echo "      - 10.96.0.10"
      ;;
    ipv6)
      echo "      - fd00:10:96::10"
      ;;
    dual)
      echo "      - 10.96.0.10"
      ;;
    *)
      echo "unsupported E2E_IP_FAMILY: $E2E_IP_FAMILY" >&2
      return 1
      ;;
  esac
}

cmd_vars() {
  local endpoint
  endpoint=$(hcp_ovn_endpoint)
  cat <<EOF
JOB_DIR=$JOB_DIR
KUBECONFIG=$JOB_DIR/tenant.kubeconfig
KUBE_OVN_HCP_OVN_NB_ADDR=tcp:$endpoint:$HCP_OVN_NB_NODE_PORT
KUBE_OVN_HCP_OVN_SB_ADDR=tcp:$endpoint:$HCP_OVN_SB_NODE_PORT
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

hcp_ovn_endpoint() {
  if [ -n "$HCP_OVN_ENDPOINT" ]; then
    echo "$HCP_OVN_ENDPOINT"
    return
  fi
  docker inspect "$MGMT_KIND_NAME-control-plane" \
    -f '{{.NetworkSettings.Networks.kind.IPAddress}}'
}

hcp_ovn_nb_addr() {
  echo "tcp:$(hcp_ovn_endpoint):$HCP_OVN_NB_NODE_PORT"
}

hcp_ovn_sb_addr() {
  echo "tcp:$(hcp_ovn_endpoint):$HCP_OVN_SB_NODE_PORT"
}

cmd_render_mgmt_values() {
  local net_stack
  net_stack=$(chart_net_stack)
  cat <<EOF
namespace: kube-system
installMode: controlPlaneOnly

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
  hcp:
    enabled: true
    namespace: $HCP_NAMESPACE
    replicas: $HCP_OVN_REPLICAS
    nbAddress: $(hcp_ovn_nb_addr)
    sbAddress: $(hcp_ovn_sb_addr)
    service:
      type: NodePort
      nbNodePort: $HCP_OVN_NB_NODE_PORT
      sbNodePort: $HCP_OVN_SB_NODE_PORT
    storage:
      storageClassName: standard
      size: 5Gi

networking:
  NET_STACK: $net_stack
  ENABLE_SSL: false
EOF
}

cmd_render_tenant_values() {
  local net_stack
  net_stack=$(chart_net_stack)
  cat <<EOF
namespace: kube-system
installMode: dataPlaneOnly

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
  hcp:
    enabled: true
    nbAddress: $(hcp_ovn_nb_addr)
    sbAddress: $(hcp_ovn_sb_addr)

networking:
  NET_STACK: $net_stack
  ENABLE_SSL: false
EOF
}

cmd_render_tenant_control_plane() {
  local dns_service_ips pod_cidr service_cidr
  pod_cidr=$(tenant_pod_cidr)
  service_cidr=$(tenant_service_cidr)
  dns_service_ips=$(tenant_dns_service_ips_yaml)
  cat <<EOF
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
    # Kamaji validates podCidr and serviceCidr as single CIDRs. In the dual
    # Kube-OVN data-plane job, keep the tenant Kubernetes control plane on
    # IPv4 while Helm renders Kube-OVN with networking.NET_STACK=dual_stack.
    podCidr: $pod_cidr
    serviceCidr: $service_cidr
    dnsServiceIPs:
$dns_service_ips
  addons:
    coreDNS: {}
    kubeProxy: {}
EOF
}

cmd_render_tenant_worker_kubelet_env() {
  echo "KUBELET_EXTRA_ARGS=--fail-swap-on=false"
}

cmd_render_tenant_kubeovn_image() {
  echo "docker.io/kubeovn/kube-ovn:dev"
}

local_registry_kubeovn_image() {
  echo "localhost:5000/kubeovn/kube-ovn:dev"
}

tenant_registry_kubeovn_image() {
  local reg_ip=$1
  echo "$reg_ip:5000/kubeovn/kube-ovn:dev"
}

cmd_render_tenant_worker_docker_args() {
  printf '%s\n' \
    -d \
    --name tenant-worker-0 \
    --privileged \
    --network=kind \
    --hostname tenant-worker-0 \
    --tmpfs /run \
    --tmpfs /tmp \
    --volume /var \
    -v /lib/modules:/lib/modules:ro \
    --security-opt apparmor=unconfined \
    --security-opt seccomp=unconfined \
    --cgroupns=private \
    --restart=on-failure:1 \
    --init=false
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
  echo ">>> Installing kube-ovn hosted OVN central on mgmt..."
  kubectl --context="kind-$MGMT_KIND_NAME" create namespace "$HCP_NAMESPACE" \
    --dry-run=client -o yaml | kubectl --context="kind-$MGMT_KIND_NAME" apply -f -
  cmd_render_mgmt_values > "$JOB_DIR/mgmt-values.yaml"
  helm install --kube-context="kind-$MGMT_KIND_NAME" \
    kube-ovn "$CHART_DIR" \
    -n kube-system -f "$JOB_DIR/mgmt-values.yaml"
  if ! kubectl --context="kind-$MGMT_KIND_NAME" wait --for=condition=Ready \
    pod -n "$HCP_NAMESPACE" -l app=ovn-central --timeout=300s; then
    diagnose_mgmt_cluster
    return 1
  fi
  if ! verify_hcp_ovn_services; then
    diagnose_mgmt_cluster
    return 1
  fi
}

create_tenant_control_plane() {
  echo ">>> Creating TenantControlPlane via Kamaji..."
  cmd_render_tenant_control_plane > "$JOB_DIR/tenant-tcp.yaml"
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

diagnose_tenant_worker() {
  local diagnostics_dir="$JOB_DIR/diagnostics"
  mkdir -p "$diagnostics_dir"
  {
    echo "### docker ps"
    docker ps -a || true
    echo
    echo "### docker inspect tenant-worker-0"
    docker inspect tenant-worker-0 || true
    echo
    echo "### docker logs tenant-worker-0"
    docker logs tenant-worker-0 || true
    echo
    echo "### systemctl status kubelet"
    docker exec tenant-worker-0 systemctl status kubelet --no-pager --lines=80 || true
    echo
    echo "### journalctl -xeu kubelet"
    docker exec tenant-worker-0 journalctl -xeu kubelet --no-pager -n 200 || true
    echo
    echo "### systemctl status containerd"
    docker exec tenant-worker-0 systemctl status containerd --no-pager --lines=80 || true
    echo
    echo "### journalctl -xeu containerd"
    docker exec tenant-worker-0 journalctl -xeu containerd --no-pager -n 120 || true
    echo
    echo "### crictl ps -a"
    docker exec tenant-worker-0 crictl ps -a || true
    echo
    echo "### crictl pods"
    docker exec tenant-worker-0 crictl pods || true
    echo
    echo "### selected crictl logs"
    docker exec tenant-worker-0 sh -c '
      crictl ps -a --no-trunc 2>/dev/null |
        awk "NR > 1 && /kube-proxy|kube-ovn-controller|cni-server|openvswitch|ovs-ovn|pinger|coredns/ {print \$1, \$NF}" |
        while read -r cid cname; do
          [ -n "$cid" ] || continue
          echo "----- $cname $cid -----"
          crictl logs "$cid" 2>&1 | tail -n 200 || true
        done
    ' || true
    echo
    echo "### /var/lib/kubelet/config.yaml"
    docker exec tenant-worker-0 cat /var/lib/kubelet/config.yaml || true
    echo
    echo "### /var/lib/kubelet/kubeadm-flags.env"
    docker exec tenant-worker-0 cat /var/lib/kubelet/kubeadm-flags.env || true
    echo
    echo "### /etc/default/kubelet"
    docker exec tenant-worker-0 cat /etc/default/kubelet || true
  } > "$diagnostics_dir/tenant-worker.log" 2>&1 || true
  echo ">>> tenant worker diagnostics written to $diagnostics_dir/tenant-worker.log"
}

diagnose_mgmt_cluster() {
  local diagnostics_dir="$JOB_DIR/diagnostics"
  mkdir -p "$diagnostics_dir"
  {
    echo "### mgmt nodes"
    kubectl --context="kind-$MGMT_KIND_NAME" get nodes -o wide || true
    echo
    echo "### mgmt pods"
    kubectl --context="kind-$MGMT_KIND_NAME" get pods -A -o wide --show-labels || true
    echo
    echo "### mgmt services"
    kubectl --context="kind-$MGMT_KIND_NAME" get svc -A -o wide || true
    echo
    echo "### hcp ovn endpoints"
    kubectl --context="kind-$MGMT_KIND_NAME" -n "$HCP_NAMESPACE" get endpoints,endpointslices -o wide || true
    echo
    echo "### ovn-central pod labels"
    kubectl --context="kind-$MGMT_KIND_NAME" -n "$HCP_NAMESPACE" get pod -l app=ovn-central \
      -o jsonpath='{range .items[*]}{.metadata.name}{" labels="}{.metadata.labels}{"\n"}{end}' || true
    echo
    echo "### ovn-central describe"
    kubectl --context="kind-$MGMT_KIND_NAME" -n "$HCP_NAMESPACE" describe pod -l app=ovn-central || true
    echo
    echo "### ovn-central logs"
    kubectl --context="kind-$MGMT_KIND_NAME" -n "$HCP_NAMESPACE" logs -l app=ovn-central \
      --all-containers --tail=200 || true
    echo
    echo "### tenant control-plane pods"
    kubectl --context="kind-$MGMT_KIND_NAME" -n default get pods -o wide --show-labels || true
    echo
    echo "### tenant control-plane describe"
    kubectl --context="kind-$MGMT_KIND_NAME" -n default describe pods || true
    echo
    echo "### tenant control-plane logs"
    kubectl --context="kind-$MGMT_KIND_NAME" -n default logs -l kamaji.clastix.io/name=tenant \
      --all-containers --tail=200 || true
    echo
    echo "### mgmt events"
    kubectl --context="kind-$MGMT_KIND_NAME" get events -A --sort-by=.lastTimestamp || true
  } > "$diagnostics_dir/mgmt-cluster.log" 2>&1 || true
  echo ">>> mgmt cluster diagnostics written to $diagnostics_dir/mgmt-cluster.log"
}

diagnose_tenant_cluster() {
  local diagnostics_dir="$JOB_DIR/diagnostics"
  mkdir -p "$diagnostics_dir"
  {
    echo "### tenant nodes"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl get nodes -o wide || true
    echo
    echo "### tenant pods"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl get pods -A -o wide --show-labels || true
    echo
    echo "### tenant services"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl get svc -A -o wide || true
    echo
    echo "### tenant endpoints"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl get endpoints,endpointslices -A -o wide || true
    echo
    echo "### kube-system describe"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system describe pods || true
    echo
    echo "### kube-system logs"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system logs \
      -l 'app in (kube-ovn-cni,kube-ovn-controller,ovs,kube-ovn-pinger,kube-proxy,kube-dns)' \
      --all-containers --tail=200 || true
    echo
    echo "### kube-system previous logs"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system logs \
      -l 'app in (kube-ovn-cni,kube-ovn-controller,ovs,kube-ovn-pinger,kube-proxy,kube-dns)' \
      --all-containers --previous --tail=200 || true
    echo
    echo "### tenant events"
    KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl get events -A --sort-by=.lastTimestamp || true
  } > "$diagnostics_dir/tenant-cluster.log" 2>&1 || true
  echo ">>> tenant cluster diagnostics written to $diagnostics_dir/tenant-cluster.log"
}

verify_hcp_ovn_services() {
  local endpoint
  endpoint=$(hcp_ovn_endpoint)
  echo ">>> Verifying hosted OVN central services..."
  for svc in ovn-nb ovn-sb; do
    for i in $(seq 1 60); do
      if [ -n "$(kubectl --context="kind-$MGMT_KIND_NAME" -n "$HCP_NAMESPACE" get endpoints "$svc" \
          -o jsonpath='{.subsets[0].addresses[0].ip}' 2>/dev/null)" ]; then
        break
      fi
      if [ "$i" -eq 60 ]; then
        echo "ERROR: hosted OVN central service $svc has no endpoint" >&2
        return 1
      fi
      sleep 2
    done
  done
  docker run --rm --network=kind --entrypoint bash "$TENANT_KIND_NODE_IMAGE" -c \
    "timeout 5 bash -c '</dev/tcp/$endpoint/$HCP_OVN_NB_NODE_PORT' && timeout 5 bash -c '</dev/tcp/$endpoint/$HCP_OVN_SB_NODE_PORT'"
}

setup_local_registry() {
  echo ">>> Starting local registry on the kind network..."
  docker rm -f "$REGISTRY_NAME" >/dev/null 2>&1 || true
  docker run -d --name "$REGISTRY_NAME" --network=kind --restart=always \
    -p 5000:5000 registry:2 >/dev/null
  sleep 3
  REG_IP=$(docker inspect "$REGISTRY_NAME" \
    -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
  docker tag "$KUBEOVN_IMAGE" "$(local_registry_kubeovn_image)"
  docker push "$(local_registry_kubeovn_image)" >/dev/null
  echo "$REG_IP" > "$JOB_DIR/reg-ip"
}

setup_tenant_worker() {
  local reg_ip
  local -a docker_args
  reg_ip=$(cat "$JOB_DIR/reg-ip")
  echo ">>> Spawning tenant-worker-0..."
  docker rm -f tenant-worker-0 >/dev/null 2>&1 || true
  mapfile -t docker_args < <(cmd_render_tenant_worker_docker_args)
  docker run "${docker_args[@]}" "$TENANT_KIND_NODE_IMAGE" >/dev/null
  sleep 8

  echo ">>> Configuring tenant-worker-0 kubelet flags..."
  cmd_render_tenant_worker_kubelet_env | docker exec -i tenant-worker-0 sh -c \
    'mkdir -p /etc/default && cat > /etc/default/kubelet'

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
  docker exec tenant-worker-0 crictl pull "$(tenant_registry_kubeovn_image "$reg_ip")"
  docker exec tenant-worker-0 ctr -n k8s.io images tag --force \
    "$(tenant_registry_kubeovn_image "$reg_ip")" "$(cmd_render_tenant_kubeovn_image)"
}

join_tenant_worker() {
  echo ">>> Generating kubeadm join command..."
  if ! docker run --rm --network=kind -v "$JOB_DIR/tenant.kubeconfig:/kc:ro" \
    --entrypoint kubeadm "$TENANT_KIND_NODE_IMAGE" \
    --kubeconfig=/kc token create --print-join-command > "$JOB_DIR/join.txt"; then
    diagnose_mgmt_cluster
    diagnose_tenant_worker
    return 1
  fi
  local join_cmd
  join_cmd=$(grep "^kubeadm join" "$JOB_DIR/join.txt")
  echo ">>> Joining tenant-worker-0 to tenant apiserver..."
  if ! docker exec tenant-worker-0 bash -c "$join_cmd --ignore-preflight-errors=all"; then
    diagnose_tenant_worker
    return 1
  fi
}

install_data_plane() {
  echo ">>> Installing kube-ovn (HCP data plane) on tenant..."
  cmd_render_tenant_values > "$JOB_DIR/tenant-values.yaml"
  if ! helm install --kubeconfig "$JOB_DIR/tenant.kubeconfig" \
    kube-ovn "$CHART_DIR" \
    -n kube-system --create-namespace -f "$JOB_DIR/tenant-values.yaml"; then
    diagnose_tenant_cluster
    diagnose_tenant_worker
    return 1
  fi

  echo ">>> Waiting for tenant data-plane components..."
  if ! KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system rollout status \
    deploy/kube-ovn-controller --timeout=300s; then
    diagnose_tenant_cluster
    diagnose_tenant_worker
    return 1
  fi
  if ! KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system rollout status \
    ds/ovs-ovn --timeout=300s; then
    diagnose_tenant_cluster
    diagnose_tenant_worker
    return 1
  fi
  if ! KUBECONFIG="$JOB_DIR/tenant.kubeconfig" kubectl -n kube-system rollout status \
    ds/kube-ovn-cni --timeout=300s; then
    diagnose_tenant_cluster
    diagnose_tenant_worker
    return 1
  fi
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
  docker rmi "$(local_registry_kubeovn_image)" 2>/dev/null || true
  rm -rf "$JOB_DIR"
}

case "${1:-}" in
  setup)                cmd_setup ;;
  teardown)             cmd_teardown ;;
  kubeconfig)           cmd_kubeconfig ;;
  vars)                 cmd_vars ;;
  render-mgmt-values)   cmd_render_mgmt_values ;;
  render-tenant-values) cmd_render_tenant_values ;;
  render-tenant-control-plane) cmd_render_tenant_control_plane ;;
  render-tenant-worker-docker-args) cmd_render_tenant_worker_docker_args ;;
  render-tenant-worker-kubelet-env) cmd_render_tenant_worker_kubelet_env ;;
  render-tenant-kubeovn-image) cmd_render_tenant_kubeovn_image ;;
  *)
    cat >&2 <<USAGE
Usage: $0 <setup|teardown|kubeconfig|vars|render-mgmt-values|render-tenant-values|render-tenant-control-plane|render-tenant-worker-docker-args|render-tenant-worker-kubelet-env|render-tenant-kubeovn-image>

  setup       Bring up the mgmt kind cluster + Kamaji + tenant worker and
              install both halves of kube-ovn.
  teardown    Tear everything down.
  kubeconfig  Print the path to the tenant kubeconfig (used by the e2e job).
  vars        Print the env vars consumed by the Ginkgo e2e suite.
  render-mgmt-values
              Print the mgmt Helm values used by setup.
  render-tenant-values
              Print the tenant Helm values used by setup.
  render-tenant-control-plane
              Print the TenantControlPlane manifest used by setup.
  render-tenant-worker-docker-args
              Print the docker run arguments used for the tenant worker.
  render-tenant-worker-kubelet-env
              Print the kubelet env file used by the tenant worker.
  render-tenant-kubeovn-image
              Print the kube-ovn image reference rendered by tenant Helm values.
USAGE
    exit 1 ;;
esac
