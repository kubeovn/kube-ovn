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

cleanup() {
  local status=$?
  trap - EXIT
  if [ -n "$LISTENER_PID" ]; then
    kill "$LISTENER_PID" 2>/dev/null || true
    wait "$LISTENER_PID" 2>/dev/null || true
  fi
  if [ "$DATAPATH_CAPABILITY_OVERRIDDEN" = true ] && [ -n "$NODE_NAME" ] && [ -n "$DATAPATH_UUID" ]; then
    kubectl ko vsctl "$NODE_NAME" set Datapath "$DATAPATH_UUID" capabilities:psample=true >/dev/null 2>&1 || true
  fi
  if [ -n "$CONFLICT_COLLECTOR_UUID" ]; then
    kubectl ko nbctl destroy Sample_Collector "$CONFLICT_COLLECTOR_UUID" >/dev/null 2>&1 || true
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
    "external_ids:parent=$policy_parent" 'sample_new!=[]')
  [ -z "$references" ] || return 1
  references=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid find ACL \
    "external_ids:parent=$policy_parent" 'sample_est!=[]')
  [ -z "$references" ]
}

controller_sampling_failure_observed() {
  kubectl logs -n kube-system -l app=kube-ovn-controller -c kube-ovn-controller \
    --since=5m --tail=1000 2>/dev/null |
    awk -v key="$NAMESPACE/sampling-failure" '
      index($0, "error syncing sample network policy ACL \"" key "\"") &&
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
  uuids=$(node_collector_set_uuids)
  [ -n "$uuids" ]
}

node_collector_set_absent() {
  local uuids
  uuids=$(node_collector_set_uuids)
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
  local sampled_acls applications collectors node_sets
  sampled_acls=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
    find ACL 'external_ids:kube-ovn.io/sample-feature=network-policy')
  applications=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
    find Sampling_App 'external_ids:kube-ovn.io/feature=acl-sampling')
  collectors=$(kubectl ko nbctl --data=bare --no-heading --columns=_uuid \
    find Sample_Collector 'external_ids:kube-ovn.io/feature=acl-sampling')
  node_sets=$(kubectl ko vsctl "$NODE_NAME" --data=bare --no-heading --columns=_uuid \
    find Flow_Sample_Collector_Set 'external_ids:kube-ovn.io/feature=acl-sampling')
  [ -z "$sampled_acls" ] && [ -z "$applications" ] &&
    [ -z "$collectors" ] && [ -z "$node_sets" ]
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
TARGET_IP=$(kubectl get pod -n "$NAMESPACE" target -o jsonpath='{.status.podIP}')
if [[ "$TARGET_IP" == *:* ]]; then
  TARGET_ENDPOINT="[$TARGET_IP]:8080"
else
  TARGET_ENDPOINT="$TARGET_IP:8080"
fi

wait_for 'NetworkPolicy ACL sampling references' 90 sampling_references_ready "$POLICY_UID"

kubectl ko acl-sample listen --node "$NODE_NAME" >"$SAMPLE_OUTPUT" 2>"$LISTENER_LOG" &
LISTENER_PID=$!
wait_for_expected_samples

kill "$LISTENER_PID" 2>/dev/null || true
wait "$LISTENER_PID" 2>/dev/null || true
LISTENER_PID=""

CONFLICT_COLLECTOR_UUID=$(kubectl ko nbctl create Sample_Collector \
  id=1 set_id=142 probability=65535 name=acl-sampling-e2e-conflict)
if [ -z "$CONFLICT_COLLECTOR_UUID" ]; then
  echo 'failed to create the unowned controller sampling conflict' >&2
  exit 1
fi
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
if [[ "$FAILURE_TARGET_IP" == *:* ]]; then
  FAILURE_TARGET_ENDPOINT="[$FAILURE_TARGET_IP]:8080"
else
  FAILURE_TARGET_ENDPOINT="$FAILURE_TARGET_IP:8080"
fi

wait_for 'failure-injection NetworkPolicy enforcement ACLs' 90 policy_enforcement_acls_ready "$FAILURE_PORT_GROUP"
wait_for 'controller ACL sampling conflict warning' 90 controller_sampling_failure_observed
wait_for 'sampling references to remain absent after the injected failure' 30 \
  policy_sampling_references_absent "$FAILURE_PORT_GROUP"
verify_policy_connectivity "$FAILURE_TARGET_ENDPOINT"

kubectl ko nbctl destroy Sample_Collector "$CONFLICT_COLLECTOR_UUID"
CONFLICT_COLLECTOR_UUID=""
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

disable_acl_sampling deployment kube-ovn-controller kube-ovn-controller
disable_acl_sampling daemonset kube-ovn-cni cni-server
kubectl rollout status deployment/kube-ovn-controller -n kube-system --timeout=2m
kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout=2m
wait_for 'owned controller and node ACL sampling state cleanup' 90 sampling_cleanup_complete

echo 'ACL sampling E2E passed'
