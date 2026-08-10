#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="acl-sampling-e2e-${GITHUB_RUN_ID:-$RANDOM}-${RANDOM}"
NAMESPACE_CREATED=false
SAMPLE_OUTPUT=acl-sampling-e2e-listener.out
LISTENER_LOG=acl-sampling-e2e-listener.err
LISTENER_PID=""
POLICY_NAME=acl-sampling
CONFLICT_COLLECTOR_UUID=""
DATAPATH_UUID=""
DATAPATH_CAPABILITY_OVERRIDDEN=false
NODE_NAME=""
: >"$SAMPLE_OUTPUT"
: >"$LISTENER_LOG"

restore_controller_collector_ownership() {
  if [ -z "$CONFLICT_COLLECTOR_UUID" ]; then
    return
  fi
  if ! kubectl ko nbctl set Sample_Collector "$CONFLICT_COLLECTOR_UUID" \
    external_ids:vendor=kube-ovn \
    'external_ids:kube-ovn.io/feature=acl-sampling' \
    'external_ids:kube-ovn.io/acl-sampling-kind=collector' \
    'external_ids:kube-ovn.io/acl-sampling-role=allow'; then
    return 1
  fi
  CONFLICT_COLLECTOR_UUID=""
}

cleanup() {
  local status=$?
  trap - EXIT
  if [ -n "$LISTENER_PID" ]; then
    kill "$LISTENER_PID" 2>/dev/null || true
    wait "$LISTENER_PID" 2>/dev/null || true
  fi
  if [ "$DATAPATH_CAPABILITY_OVERRIDDEN" = true ] && [ -n "$NODE_NAME" ] && [ -n "$DATAPATH_UUID" ]; then
    if ! kubectl ko vsctl "$NODE_NAME" set Datapath "$DATAPATH_UUID" capabilities:psample=true >/dev/null; then
      echo "failed to restore psample capability on node $NODE_NAME" >&2
    fi
  fi
  if [ -n "$CONFLICT_COLLECTOR_UUID" ]; then
    if ! restore_controller_collector_ownership >/dev/null 2>&1; then
      echo 'failed to restore ACL sampling collector ownership' >&2
    fi
  fi
  if [ "$status" -ne 0 ]; then
    echo 'ACL sample listener standard output:' >&2
    sed -n '1,240p' "$SAMPLE_OUTPUT" >&2
    echo 'ACL sample listener standard error:' >&2
    sed -n '1,240p' "$LISTENER_LOG" >&2
  else
    rm -f "$SAMPLE_OUTPUT" "$LISTENER_LOG"
  fi
  if [ "$NAMESPACE_CREATED" = true ]; then
    kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

wait_for() {
  local description=$1
  local timeout_seconds=$2
  shift 2
  local deadline=$((SECONDS + timeout_seconds))
  until "$@"; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "timed out waiting for $description" >&2
      return 1
    fi
    sleep 2
  done
}

endpoint_for_ip() {
  local ip=$1
  if [[ "$ip" == *:* ]]; then
    printf '[%s]:8080\n' "$ip"
  else
    printf '%s:8080\n' "$ip"
  fi
}

start_sample_listener() {
  : >"$SAMPLE_OUTPUT"
  : >"$LISTENER_LOG"
  kubectl ko acl-sample listen --node "$NODE_NAME" >"$SAMPLE_OUTPUT" 2>"$LISTENER_LOG" &
  LISTENER_PID=$!
}

stop_sample_listener() {
  kill "$LISTENER_PID" 2>/dev/null || true
  wait "$LISTENER_PID" 2>/dev/null || true
  LISTENER_PID=""
}

sampling_references_ready() {
  local policy_uid=$1
  local rows
  rows=$(kubectl ko nbctl --format=csv --data=bare --no-heading \
    --columns=action,sample_new,sample_est find ACL \
    "external_ids:kube-ovn.io/policy-uid=$policy_uid")
  awk -F, '
    $1 == "allow-related" && $2 ~ /^[[:xdigit:]]{8}-/ && $3 ~ /^[[:xdigit:]]{8}-/ { allow = 1 }
    $1 == "drop" && $2 ~ /^[[:xdigit:]]{8}-/ && ($3 == "" || $3 == "[]") { default_deny = 1 }
    $1 == "drop" && $3 != "" && $3 != "[]" { invalid_default_deny = 1 }
    END { exit !(allow && default_deny && !invalid_default_deny) }
  ' <<< "$rows"
}

has_sample_document() {
  local verdict=$1
  local application=$2
  local extra=$3
  awk -v verdict="verdict: $verdict" -v application="app: $application" \
    -v uid="uid: $POLICY_UID" -v extra="$extra" '
      BEGIN { RS = "---" }
      index($0, verdict) && index($0, application) && index($0, uid) && index($0, extra) { found = 1 }
      END { exit !found }
    ' "$SAMPLE_OUTPUT"
}

expected_samples_ready() {
  has_sample_document allow acl-new 'ruleIndex: 0' &&
    has_sample_document allow acl-est 'ruleIndex: 0' &&
    has_sample_document default-deny acl-new 'attribution: non-exclusive'
}

wait_for_expected_samples() {
  local deadline=$((SECONDS + 60))
  while ! expected_samples_ready; do
    if ! kill -0 "$LISTENER_PID" 2>/dev/null; then
      echo 'ACL sample listener exited before all expected samples arrived' >&2
      return 1
    fi
    for _ in $(seq 1 3); do
      kubectl exec -n "$NAMESPACE" allowed -- \
        /agnhost connect --timeout=3s "$TARGET_ENDPOINT"
    done
    if kubectl exec -n "$NAMESPACE" denied -- \
      /agnhost connect --timeout=2s "$TARGET_ENDPOINT"; then
      echo 'default-denied client unexpectedly reached the target Pod' >&2
      return 1
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo 'timed out waiting for decoded acl-new, acl-est, and default-deny samples' >&2
      return 1
    fi
    sleep 2
  done
}

policy_enforcement_acls_ready() {
  local policy_parent=$1
  local actions
  actions=$(kubectl ko nbctl --data=bare --no-heading --columns=action find ACL \
    "external_ids:parent=$policy_parent")
  grep -Fxq allow-related <<< "$actions" && grep -Fxq drop <<< "$actions"
}

policy_sampling_references_absent() {
  local policy_parent=$1
  local references
  references=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid find ACL \
    "external_ids:parent=$policy_parent" 'sample_new!=[]') || return 1
  [ -z "$references" ] || return 1
  references=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid find ACL \
    "external_ids:parent=$policy_parent" 'sample_est!=[]') || return 1
  [ -z "$references" ]
}

policy_sampling_disabled() {
  local policy_parent=$1
  local rows
  rows=$(kubectl ko nbctl --format=csv --data=bare --no-heading \
    --columns=action,sample_new,sample_est find ACL \
    "external_ids:parent=$policy_parent") || return 1
  awk -F, '
    $1 == "allow-related" { allow = 1 }
    $1 == "drop" { default_deny = 1 }
    ($2 != "" && $2 != "[]") || ($3 != "" && $3 != "[]") { sampled = 1 }
    END { exit !(allow && default_deny && !sampled) }
  ' <<< "$rows"
}

controller_sampling_failure_observed() {
  kubectl logs -n kube-system -l app=kube-ovn-controller -c kube-ovn-controller \
    --since=5m --tail=1000 2>/dev/null |
    awk -v key="$NAMESPACE/sampling-failure" '
      index($0, "error syncing sample network policy ACL") &&
        index($0, key) &&
        index($0, "sample collector ID 1 is owned by another application") { found = 1 }
      END { exit !found }
    '
}

verify_policy_connectivity() {
  local endpoint=$1
  for _ in $(seq 1 3); do
    kubectl exec -n "$NAMESPACE" allowed -- /agnhost connect --timeout=3s "$endpoint"
  done
  for _ in $(seq 1 3); do
    if kubectl exec -n "$NAMESPACE" denied -- /agnhost connect --timeout=2s "$endpoint"; then
      echo "default-denied client unexpectedly reached $endpoint" >&2
      return 1
    fi
  done
}

node_collector_set_uuids() {
  kubectl ko vsctl "$NODE_NAME" --data=bare --no-heading --columns=_uuid \
    find Flow_Sample_Collector_Set 'external_ids:kube-ovn.io/feature=acl-sampling'
}

node_collector_set_ready() {
  local uuids
  uuids=$(node_collector_set_uuids) || return 1
  [ -n "$uuids" ]
}

node_collector_set_absent() {
  local uuids
  uuids=$(node_collector_set_uuids) || return 1
  [ -z "$uuids" ]
}

node_capability_failure_observed() {
  kubectl logs -n kube-system -l app=kube-ovn-cni -c cni-server \
    --since=5m --tail=1000 2>/dev/null |
    grep -F 'does not report psample=true' >/dev/null
}

disable_acl_sampling() {
  local resource=$1
  local name=$2
  local container_name=$3
  local container_index argument_index
  container_index=$(kubectl get "$resource" -n kube-system "$name" -o json |
    jq --arg name "$container_name" '.spec.template.spec.containers | map(.name) | index($name)')
  if [ "$container_index" = "null" ]; then
    echo "cannot locate container $container_name in $resource/$name" >&2
    return 1
  fi
  argument_index=$(kubectl get "$resource" -n kube-system "$name" -o json |
    jq --argjson index "$container_index" \
      '.spec.template.spec.containers[$index].args | index("--enable-acl-sampling=true")')
  if [ "$argument_index" = "null" ]; then
    echo "cannot locate the enabled ACL sampling argument in $resource/$name" >&2
    return 1
  fi

  kubectl patch "$resource" -n kube-system "$name" --type=json -p="[{
    \"op\": \"replace\",
    \"path\": \"/spec/template/spec/containers/$container_index/args/$argument_index\",
    \"value\": \"--enable-acl-sampling=false\"
  }]"
}

sampling_cleanup_complete() {
  local sampled_acls applications collectors node_sets nodes node
  policy_sampling_disabled "$POLICY_PARENT" || return 1
  policy_sampling_disabled "$FAILURE_PORT_GROUP" || return 1
  sampled_acls=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
    find ACL 'external_ids:kube-ovn.io/sample-feature=network-policy') || return 1
  applications=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
    find Sampling_App 'external_ids:kube-ovn.io/feature=acl-sampling') || return 1
  collectors=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
    find Sample_Collector 'external_ids:kube-ovn.io/feature=acl-sampling') || return 1
  nodes=$(kubectl get nodes -o name) || return 1
  [ -n "$nodes" ] || return 1
  while IFS= read -r node; do
    node=${node#node/}
    node_sets=$(kubectl ko vsctl "$node" --data=bare --no-heading --columns=_uuid \
      find Flow_Sample_Collector_Set 'external_ids:kube-ovn.io/feature=acl-sampling') || return 1
    [ -z "$node_sets" ] || return 1
  done <<< "$nodes"
  [ -z "$sampled_acls" ] && [ -z "$applications" ] &&
    [ -z "$collectors" ]
}

NODE_NAME=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
if [ -z "$NODE_NAME" ]; then
  echo 'no Kubernetes node is available for ACL sampling E2E' >&2
  exit 1
fi

kubectl create namespace "$NAMESPACE"
NAMESPACE_CREATED=true
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: $NAMESPACE
  name: target
  labels:
    app: target
spec:
  nodeName: $NODE_NAME
  containers:
    - name: agnhost
      image: ghcr.io/kubeovn/agnhost:2.47
      args: ["netexec", "--http-port=8080"]
---
apiVersion: v1
kind: Pod
metadata:
  namespace: $NAMESPACE
  name: allowed
  labels:
    access: allowed
spec:
  nodeName: $NODE_NAME
  containers:
    - name: agnhost
      image: ghcr.io/kubeovn/agnhost:2.47
      args: ["pause"]
---
apiVersion: v1
kind: Pod
metadata:
  namespace: $NAMESPACE
  name: denied
  labels:
    access: denied
spec:
  nodeName: $NODE_NAME
  containers:
    - name: agnhost
      image: ghcr.io/kubeovn/agnhost:2.47
      args: ["pause"]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  namespace: $NAMESPACE
  name: $POLICY_NAME
spec:
  podSelector:
    matchLabels:
      app: target
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              access: allowed
      ports:
        - protocol: TCP
          port: 8080
EOF

kubectl wait -n "$NAMESPACE" --for=condition=Ready pod/target pod/allowed pod/denied --timeout=2m
POLICY_UID=$(kubectl get networkpolicy -n "$NAMESPACE" "$POLICY_NAME" -o jsonpath='{.metadata.uid}')
POLICY_PARENT="${POLICY_NAME//-/.}.${NAMESPACE//-/.}"
TARGET_IP=$(kubectl get pod -n "$NAMESPACE" target -o jsonpath='{.status.podIP}')
TARGET_ENDPOINT=$(endpoint_for_ip "$TARGET_IP")

wait_for 'NetworkPolicy ACL sampling references' 90 sampling_references_ready "$POLICY_UID"

start_sample_listener
wait_for_expected_samples
stop_sample_listener

CONFLICT_COLLECTOR_UUID=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
  find Sample_Collector id=1 'external_ids:kube-ovn.io/feature=acl-sampling')
if [ -z "$CONFLICT_COLLECTOR_UUID" ]; then
  echo 'cannot locate the owned controller sampling collector to inject a conflict' >&2
  exit 1
fi
kubectl ko nbctl clear Sample_Collector "$CONFLICT_COLLECTOR_UUID" external_ids
kubectl rollout restart deployment/kube-ovn-controller -n kube-system
kubectl rollout status deployment/kube-ovn-controller -n kube-system --timeout=2m

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: $NAMESPACE
  name: failure-target
  labels:
    app: failure-target
spec:
  nodeName: $NODE_NAME
  containers:
    - name: agnhost
      image: ghcr.io/kubeovn/agnhost:2.47
      args: ["netexec", "--http-port=8080"]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  namespace: $NAMESPACE
  name: sampling-failure
spec:
  podSelector:
    matchLabels:
      app: failure-target
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              access: allowed
      ports:
        - protocol: TCP
          port: 8080
EOF

kubectl wait -n "$NAMESPACE" --for=condition=Ready pod/failure-target --timeout=2m
FAILURE_POLICY_UID=$(kubectl get networkpolicy -n "$NAMESPACE" sampling-failure -o jsonpath='{.metadata.uid}')
FAILURE_PORT_GROUP="sampling.failure.${NAMESPACE//-/.}"
FAILURE_TARGET_IP=$(kubectl get pod -n "$NAMESPACE" failure-target -o jsonpath='{.status.podIP}')
FAILURE_TARGET_ENDPOINT=$(endpoint_for_ip "$FAILURE_TARGET_IP")

wait_for 'failure-injection NetworkPolicy enforcement ACLs' 90 policy_enforcement_acls_ready "$FAILURE_PORT_GROUP"
wait_for 'controller ACL sampling conflict warning' 90 controller_sampling_failure_observed
wait_for 'sampling references to remain absent after the injected failure' 30 \
  policy_sampling_references_absent "$FAILURE_PORT_GROUP"
verify_policy_connectivity "$FAILURE_TARGET_ENDPOINT"

restore_controller_collector_ownership
wait_for 'sampling-only retry after controller failure' 90 sampling_references_ready "$FAILURE_POLICY_UID"

NODE_COLLECTOR_UUID=$(node_collector_set_uuids | sed -n '1p')
if [ -z "$NODE_COLLECTOR_UUID" ]; then
  echo 'cannot locate the owned node ACL sampling collector set' >&2
  exit 1
fi
DATAPATH_UUID=$(kubectl ko vsctl "$NODE_NAME" --data=bare --no-heading --columns=_uuid \
  find Datapath capabilities:psample=true | sed -n '1p')
if [ -z "$DATAPATH_UUID" ]; then
  echo 'cannot locate the active psample-capable OVS datapath' >&2
  exit 1
fi

kubectl ko vsctl "$NODE_NAME" set Datapath "$DATAPATH_UUID" capabilities:psample=false
DATAPATH_CAPABILITY_OVERRIDDEN=true
kubectl ko vsctl "$NODE_NAME" destroy Flow_Sample_Collector_Set "$NODE_COLLECTOR_UUID"
kubectl rollout restart daemonset/kube-ovn-cni -n kube-system
kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout=2m
wait_for 'unsupported node capability warning' 90 node_capability_failure_observed
wait_for 'collector set to remain absent on the unsupported node' 30 node_collector_set_absent
verify_policy_connectivity "$TARGET_ENDPOINT"

kubectl ko vsctl "$NODE_NAME" set Datapath "$DATAPATH_UUID" capabilities:psample=true
DATAPATH_CAPABILITY_OVERRIDDEN=false
kubectl rollout restart daemonset/kube-ovn-cni -n kube-system
kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout=2m
wait_for 'node ACL sampling recovery' 90 node_collector_set_ready
start_sample_listener
wait_for_expected_samples
stop_sample_listener

disable_acl_sampling deployment kube-ovn-controller kube-ovn-controller
disable_acl_sampling daemonset kube-ovn-cni cni-server
kubectl rollout status deployment/kube-ovn-controller -n kube-system --timeout=2m
kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout=2m
wait_for 'owned controller and node ACL sampling state cleanup' 90 sampling_cleanup_complete
verify_policy_connectivity "$TARGET_ENDPOINT"
verify_policy_connectivity "$FAILURE_TARGET_ENDPOINT"

echo 'ACL sampling E2E passed'
