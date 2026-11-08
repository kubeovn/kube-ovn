#!/usr/bin/env bash
set -euo pipefail

# This test owns only its uniquely named resources in the disposable CI cluster.
readonly smokeContext=kind-kube-ovn
if [[ $(kubectl config current-context) != "$smokeContext" ]]; then
  echo "This smoke test requires context $smokeContext" >&2
  exit 1
fi
readonly smokePool="runtime-ippool-smoke-${GITHUB_RUN_ID:-$$}"
readonly smokeSubnet="$smokePool"
readonly smokePod="$smokePool"
readonly addressSetName="${smokePool//-/.}"

k() {
  kubectl --context="$smokeContext" --request-timeout=15s "$@"
}

finish() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    k get ippool "$smokePool" -o yaml || true
    k get subnet "$smokeSubnet" -o yaml || true
    k get pod "$smokePod" -o yaml || true
    k -n kube-system logs -l app=kube-ovn-controller --all-containers --tail=200 --prefix || true
    k -n kube-system exec deployment/ovn-central -c ovn-central -- \
      ovn-nbctl --timeout=10 list address_set "$addressSetName" || true
  fi
  k delete pod "$smokePod" --ignore-not-found --timeout=60s || true
  k delete ippool "$smokePool" --ignore-not-found --timeout=60s || true
  k delete subnet "$smokeSubnet" --ignore-not-found --timeout=60s || true
  exit "$result"
}
trap finish EXIT

waitFor() {
  local description=$1
  shift
  for ((attempt = 0; attempt < 90; attempt++)); do
    if "$@"; then
      echo "$description: passed"
      return 0
    fi
    sleep 2
  done
  echo "Timed out: $description" >&2
  return 1
}

poolReady() {
  k get ippool "$smokePool" -o json | jq -e --argjson count "$1" '
    (.status.ips | length) == $count and
    any(.status.conditions[]?; .type == "Ready" and .status == "True") and
    ((.metadata.finalizers // []) | index("kubeovn.io/kube-ovn-controller") != null)
  ' >/dev/null
}

poolUsage() {
  k get ippool "$smokePool" -o json | jq -e --argjson used "$1" --argjson available "$2" '
    (.status.v4UsingIPs | tonumber) == $used and
    (.status.v4AvailableIPs | tonumber) == $available
  ' >/dev/null
}

membership() {
  k get ippool "$smokePool" -o json | jq -c '.status.ips'
}

addressSetMatches() {
  local expected actual
  expected=$(membership | jq -c 'sort')
  actual=$(k -n kube-system exec deployment/ovn-central -c ovn-central -- \
    ovn-nbctl --timeout=10 --format=json --data=json --columns=addresses list address_set "$addressSetName") || return 1
  jq -e --argjson expected "$expected" '
    .data | length == 1 and (.[0][0] |
      if type == "array" then .[1] else [.] end | sort) == $expected
  ' <<<"$actual" >/dev/null
}

podInPool() {
  local podIP
  podIP=$(k get pod "$smokePod" -o jsonpath='{.status.podIP}') || return 1
  membership | jq -e --arg ip "$podIP" 'index($ip) != null' >/dev/null
}

waitSettled() {
  local previous current stable=0
  previous=$(k get ippool "$smokePool" -o jsonpath='{.metadata.resourceVersion}')
  for ((attempt = 0; attempt < 60; attempt++)); do
    sleep 2
    current=$(k get ippool "$smokePool" -o jsonpath='{.metadata.resourceVersion}')
    if [[ $current == "$previous" ]]; then
      stable=$((stable + 1))
      if (( stable == 5 )); then
        echo "IPPool resourceVersion settled: $current"
        return 0
      fi
    else
      stable=0
    fi
    previous=$current
  done
  echo 'IPPool status did not settle' >&2
  return 1
}

addressSetDeleted() {
  local rows
  rows=$(k -n kube-system exec deployment/ovn-central -c ovn-central -- \
    ovn-nbctl --timeout=10 --format=json --columns=name find address_set "name=$addressSetName") || return 1
  jq -e '.data | length == 0' <<<"$rows" >/dev/null
}

k -n kube-system get deployment kube-ovn-controller -o json | jq -e '
  any(.spec.template.spec.containers[]; .name == "kube-ovn-controller" and
    any(.args[]; . == "--controller-runtime-mode=ippool"))
' >/dev/null
controllerImage=$(k -n kube-system get deployment kube-ovn-controller -o json |
  jq -er '.spec.template.spec.containers[] | select(.name == "kube-ovn-controller") | .image')

# Concurrent submission exercises the startup/dependency queue, then the Pod
# exercises the separate statistics ingress after allocation and release.
k create -f - <<EOF_RESOURCES
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: $smokeSubnet
spec:
  protocol: IPv4
  cidrBlock: 10.254.253.0/29
  gateway: 10.254.253.1
  excludeIps:
    - 10.254.253.1
  vpc: ovn-cluster
  disableGatewayCheck: true
---
apiVersion: kubeovn.io/v1
kind: IPPool
metadata:
  name: $smokePool
spec:
  subnet: $smokeSubnet
  ipCount: 2
  enableAddressSet: true
EOF_RESOURCES
waitFor 'count-based IPPool activation and finalizer' poolReady 2
waitFor 'OVN address set matches durable membership' addressSetMatches
waitFor 'initial IPPool statistics' poolUsage 0 2
initialMembership=$(membership)

k create -f - <<EOF_POD
apiVersion: v1
kind: Pod
metadata:
  name: $smokePod
  annotations:
    ovn.kubernetes.io/logical_switch: $smokeSubnet
    ovn.kubernetes.io/ip_pool: $smokePool
spec:
  containers:
    - name: sleeper
      image: $controllerImage
      imagePullPolicy: IfNotPresent
      command: ["sleep", "3600"]
EOF_POD
k wait pod "$smokePod" --for=condition=Ready --timeout=180s
waitFor 'Pod allocated from the activated pool' podInPool
waitFor 'statistics-only allocation notification' poolUsage 1 1
waitSettled

k -n kube-system rollout restart deployment/kube-ovn-controller
k -n kube-system rollout status deployment/kube-ovn-controller --timeout=180s
# A new count forces fresh work after startup; persisted Ready alone is insufficient.
k patch ippool "$smokePool" --type=merge -p '{"spec":{"ipCount":3}}'
waitFor 'IPPool reconciliation after controller restart' poolReady 3
membership | jq -e --argjson original "$initialMembership" '.[0:2] == $original' >/dev/null
waitFor 'restored and expanded OVN address set' addressSetMatches
waitFor 'restored Pod ownership and expanded statistics' poolUsage 1 2
waitFor 'Pod retains pool address after controller restart' podInPool
waitSettled

k delete pod "$smokePod" --wait=true --timeout=60s
waitFor 'statistics-only release notification' poolUsage 0 3
k delete ippool "$smokePool" --wait=true --timeout=60s
waitFor 'IPPool finalization removes OVN address set' addressSetDeleted
printf '%s\n' 'controller-runtime IPPool smoke passed'
