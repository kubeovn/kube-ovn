#!/usr/bin/env bash
set -euo pipefail

build_image() {
  local ko_tag="ko-chart-${GITHUB_SHA:?}"
  local ko_image="docker.io/kubeovn/kube-ovn:${ko_tag}"
  # Build every image binary from this checkout so the image's file
  # capabilities match the chart's security contexts.
  make build-go
  docker build --build-arg "VERSION=$(cat VERSION)" \
    --label "org.opencontainers.image.revision=$GITHUB_SHA" \
    --tag "$ko_image" --file dist/images/Dockerfile dist/images/
  local ko_clusters
  mapfile -t ko_clusters < <(kind get clusters)
  if [[ ${#ko_clusters[@]} != 1 ]]; then
    echo "Expected one chart-test Kind cluster, found ${#ko_clusters[@]}" >&2
    return 1
  fi
  kind load docker-image "$ko_image" --name "${ko_clusters[0]}"
  printf 'KO_CHART_TAG=%s\n' "$ko_tag" >> "${GITHUB_ENV:?}"
}

verify_bootstrap() (
  set -euo pipefail
  ko_bootstrap_results=$1
  ko_bootstrap_directory=$(mktemp -d)
  trap 'rm -rf "$ko_bootstrap_directory"' EXIT
  printf '#!/usr/bin/env bash\nset -euo pipefail\n' > "$ko_bootstrap_directory/install-cli.sh"
  # Execute the actual installer phase, changing only its local destination.
  sed -n '/^echo "\[Step 6\/6\] Run network diagnose"$/,/^chmod +x \/usr\/local\/bin\/kubectl-ko$/p' dist/images/install.sh |
    sed "s|/usr/local/bin/kubectl-ko|${ko_bootstrap_directory}/kubectl-ko|g" >> "$ko_bootstrap_directory/install-cli.sh"
  bash -x "$ko_bootstrap_directory/install-cli.sh" 2>&1 | tee "$ko_bootstrap_results/bootstrap.log"
  grep -F '+ kubectl cp -c agent kube-system/kubectl-ko-node-agent-' "$ko_bootstrap_results/bootstrap.log"
  cmp dist/images/kubectl-ko "$ko_bootstrap_directory/kubectl-ko"
  sha256sum "$ko_bootstrap_directory/kubectl-ko" |
    sed "s|${ko_bootstrap_directory}/|bootstrap/|" | tee -a "$ko_bootstrap_results/bootstrap.log"
)

verify_agent() {
  local ko_namespace=kube-system
  local ko_directory=kubectl-ko-chart-results
  mkdir -p "$ko_directory"
  kubectl -n "$ko_namespace" rollout status daemonset/kubectl-ko-node-agent --timeout=120s
  kubectl -n "$ko_namespace" get daemonset kubectl-ko-node-agent -o json > "$ko_directory/agent.json"
  jq -e '
    .spec.template.spec as $s |
    $s.hostNetwork == true and $s.hostPID == true and
    $s.automountServiceAccountToken == false and
    ($s.containers | length) == 1 and
    $s.containers[0].name == "agent" and
    $s.containers[0].command == ["/kube-ovn/kubectl-ko-node-agent"]
  ' "$ko_directory/agent.json"
  [[ $(jq -r '.spec.template.spec.containers[0].image' "$ko_directory/agent.json") == "docker.io/kubeovn/kube-ovn:${KO_CHART_TAG:?}" ]]
  local ko_node_count ko_ready_count ko_pod
  ko_node_count=$(kubectl get nodes -l kubernetes.io/os=linux -o json | jq '.items | length')
  ko_ready_count=$(jq '.status.numberReady' "$ko_directory/agent.json")
  [[ "$ko_node_count" -gt 0 && "$ko_ready_count" -eq "$ko_node_count" ]]
  verify_bootstrap "$ko_directory"
  dist/images/kubectl-ko --timeout=2m --kube-ovn-namespace "$ko_namespace" diagnose environment | tee "$ko_directory/environment.log"
  [[ $(grep -c '^Environment check on ' "$ko_directory/environment.log") -eq "$ko_node_count" ]]
  local ko_step
  for ko_step in 'check cni configuration' 'check system ipv4 config' 'check checksum value' 'check dns config' 'check firewall config' 'check geneve 6081 connection'; do
    [[ $(grep -c "$ko_step" "$ko_directory/environment.log") -eq "$ko_node_count" ]]
  done
  ko_pod=$(kubectl -n "$ko_namespace" get pods -l app=kube-ovn-pinger -o json |
    jq -er 'first(.items[] | select(.status.phase == "Running" and .metadata.deletionTimestamp == null)) | .metadata.namespace + "/" + .metadata.name')
  dist/images/kubectl-ko --timeout=2m network inspect --pod "$ko_pod" --output=json > "$ko_directory/network.json"
  jq -e '
    (.netns | length) > 0 and (.interfaces | length) > 0 and
    all(.interfaces[]; .statistics.rx.bytes >= 0 and .statistics.tx.bytes >= 0)
  ' "$ko_directory/network.json"
}

case "${1:-}" in
  build) build_image ;;
  verify) verify_agent ;;
  *) echo "Usage: $0 build|verify" >&2; exit 2 ;;
esac
