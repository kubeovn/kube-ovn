#!/usr/bin/env bash
set -euo pipefail

# This helper is opt-in and read-only with respect to cluster resources.
# Every command has a deadline; missing tools and disrupted components are evidence.
: "${E2E_NODEPORT_DIAGNOSTICS_DIR:?set an absolute artifact directory}"
artifact_dir="$E2E_NODEPORT_DIAGNOSTICS_DIR"
mkdir -p "$artifact_dir"

# Keep metadata on stderr so discovery stdout remains a plain Pod-name list.
# A failed diagnostic is recorded and later commands still run.
run_logged() {
  local command_status=0
  {
    date -u +'%Y-%m-%dT%H:%M:%S.%NZ'
    printf 'command:'
    printf ' %q' "$@"
    printf '\n'
  } >&2
  "$@" || command_status=$?
  {
    printf 'command exit=%s\n' "$command_status"
    date -u +'%Y-%m-%dT%H:%M:%S.%NZ'
  } >&2
  return 0
}

collect() {
  local file="$1"
  shift
  run_logged timeout 15 "$@" > "$file" 2>&1
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
  collect "$dest/runner.txt" bash -c "$(declare -f run_logged)"'
    run_logged ip -details address
    run_logged ip route
    run_logged ip -6 route
    run_logged ss -ntoi
  ' &
  while read -r node; do
    # Positional arguments are expanded by bash inside the node.
    # shellcheck disable=SC2016
    collect "$dest/$node-network.txt" docker exec "$node" bash -c "$(declare -f run_logged)"'
      run_logged ip -details address
      run_logged ip route show table all
      run_logged ip -6 route show table all
      run_logged ip rule
      run_logged ip -6 rule
      run_logged ip neigh
      run_logged ip -6 neigh
      run_logged ss -ntoi
      run_logged iptables-save -c
      run_logged ip6tables-save -c
      run_logged nft list ruleset
      run_logged conntrack -L -p tcp --dport "$1"
      run_logged conntrack -L -p tcp --dport "$2"
    ' _ "$NODEPORT_PORT" "$NODEPORT_BACKEND_PORT" &
  done < "$artifact_dir/nodes.txt"
  # Pods may be absent or terminating during the injected restart.
  run_logged timeout 5 kubectl --request-timeout=4s -n kube-system get pods -l app=ovs \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' > "$dest/ovs-pods.txt" 2> "$dest/ovs-discovery.log"
  while read -r pod; do
    collect "$dest/$pod-ovs.txt" kubectl --request-timeout=5s -n kube-system exec "$pod" -c openvswitch -- bash -c "$(declare -f run_logged)"'
      run_logged timeout 3 ovs-vsctl show
      run_logged timeout 3 ovs-vsctl --columns=name,ofport,link_state,error,external_ids list Interface
      run_logged timeout 3 ovs-ofctl -O OpenFlow13 dump-flows br-int
      run_logged timeout 3 ovs-appctl dpctl/dump-conntrack
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
