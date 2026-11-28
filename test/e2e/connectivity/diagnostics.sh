#!/usr/bin/env bash
set -euo pipefail

# This helper is opt-in and read-only with respect to cluster resources.
# Every command has a deadline; missing tools and disrupted components are evidence.
: "${E2E_NODEPORT_DIAGNOSTICS_DIR:?set an absolute artifact directory}"
artifact_dir="$E2E_NODEPORT_DIAGNOSTICS_DIR"
mkdir -p "$artifact_dir"

collect() {
  local file="$1"
  shift
  {
    date -u +'%Y-%m-%dT%H:%M:%S.%NZ'
    printf 'command:'
    printf ' %q' "$@"
    printf '\n'
    timeout 15 "$@" || printf 'command failed: exit=%s\n' "$?"
    date -u +'%Y-%m-%dT%H:%M:%S.%NZ'
  } > "$file" 2>&1
}

capture() {
  local name="$1"
  shift
  # Four 8 MB files per network namespace; headers suffice for TCP sequence analysis.
  # Redirects are opened by this user in the already-created artifact directory.
  # shellcheck disable=SC2024
  sudo -n bash -c 'echo $$ > "$1"; shift; exec timeout --signal=INT 1800 "$@"' _ \
    "$artifact_dir/$name.pid" "$@" tcpdump --immediate-mode -i any -nn -U -s 160 -C 8 -W 4 -Z root \
    -w "$artifact_dir/$name.pcap" \
    "(tcp and (port $NODEPORT_PORT or port $NODEPORT_BACKEND_PORT)) or udp port 6081 or arp or icmp or icmp6" \
    > "$artifact_dir/$name-tcpdump.log" 2>&1 < /dev/null &
  for _ in {1..30}; do
    if grep -q 'listening on' "$artifact_dir/$name-tcpdump.log"; then
      return
    fi
    sleep 0.1
  done
  printf 'Capture %s did not become ready; see %s-tcpdump.log\n' "$name" "$name" >&2
}

snapshot() {
  local phase="$1"
  local dest="$artifact_dir/$phase"
  mkdir -p "$dest"
  collect "$dest/service-endpoints-pods.yaml" kubectl --request-timeout=5s -n "$NODEPORT_NAMESPACE" \
    get service,endpointslice,pod -o yaml &
  collect "$dest/events.txt" kubectl --request-timeout=5s get events -A --sort-by=.metadata.creationTimestamp &
  collect "$dest/components.txt" kubectl --request-timeout=5s -n kube-system get pods -o wide &
  collect "$dest/runner.txt" bash -c 'ip -details address; ip route; ip -6 route; ss -ntoi' &
  while read -r node; do
    # Positional arguments are expanded by bash inside the node.
    # shellcheck disable=SC2016
    collect "$dest/$node-network.txt" docker exec "$node" bash -c '
      ip -details address; ip route show table all; ip -6 route show table all
      ip rule; ip -6 rule; ip neigh; ip -6 neigh; ss -ntoi
      iptables-save -c; ip6tables-save -c; nft list ruleset
      conntrack -L -p tcp --dport "$1"; conntrack -L -p tcp --dport "$2"
    ' _ "$NODEPORT_PORT" "$NODEPORT_BACKEND_PORT" &
  done < "$artifact_dir/nodes.txt"
  # Pods may be absent or terminating during the injected restart.
  timeout 5 kubectl --request-timeout=4s -n kube-system get pods -l app=ovs \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' > "$dest/ovs-pods.txt" 2> "$dest/ovs-discovery.log" || true
  while read -r pod; do
    collect "$dest/$pod-ovs.txt" kubectl --request-timeout=5s -n kube-system exec "$pod" -c openvswitch -- bash -c '
      timeout 3 ovs-vsctl show
      timeout 3 ovs-vsctl --columns=name,ofport,link_state,error,external_ids list Interface
      timeout 3 ovs-ofctl -O OpenFlow13 dump-flows br-int
      timeout 3 ovs-appctl dpctl/dump-conntrack
    ' &
  done < "$dest/ovs-pods.txt"
  collect "$dest/nb-load-balancers.txt" kubectl --request-timeout=5s -n kube-system exec deployment/ovn-central -- \
    ovn-nbctl --timeout=5 list load_balancer &
  wait
}

case "${1:-}" in
  start)
    : "${NODEPORT_NAMESPACE:?}" "${NODEPORT_SERVICE:?}" "${NODEPORT_PORT:?}" "${NODEPORT_BACKEND_PORT:?}"
    kubectl --request-timeout=5s get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' > "$artifact_dir/nodes.txt"
    printf 'namespace=%s service=%s nodePort=%s backendPort=%s\n' \
      "$NODEPORT_NAMESPACE" "$NODEPORT_SERVICE" "$NODEPORT_PORT" "$NODEPORT_BACKEND_PORT" > "$artifact_dir/target.txt"
    capture runner
    while read -r node; do
      # Use the runner's tcpdump inside the kind node's network namespace. This
      # survives ovs-ovn restarts and requires no package installation in nodes.
      if pid=$(timeout 5 docker inspect --format '{{.State.Pid}}' "$node"); then
        capture "$node" nsenter -t "$pid" -n
      else
        printf 'Cannot inspect kind node %s\n' "$node" >&2
      fi
    done < "$artifact_dir/nodes.txt"
    # Isolate snapshot jobs so wait does not wait for the packet captures.
    (snapshot baseline)
    ;;
  snapshot)
    snapshot first-failure
    ;;
  stop)
    for pid_file in "$artifact_dir"/*.pid; do
      [[ -f "$pid_file" ]] || continue
      # TERM is forwarded by timeout, allowing tcpdump to flush its output.
      pid=$(cat "$pid_file")
      # Ignore a stale PID if the safety timeout already expired.
      if [[ $(ps -p "$pid" -o args=) == *"$artifact_dir/"* ]]; then
        sudo -n kill -TERM "$pid" 2>> "$artifact_dir/stop.log" || true
      fi
    done
    ;;
  *)
    printf 'Usage: %s start|snapshot|stop\n' "$0" >&2
    exit 2
    ;;
esac
