#!/usr/bin/env bash
set -euo pipefail

# Run only against the disposable Kind cluster built by this CI job.
readonly smokeContext=kind-kube-ovn
if [[ $(kubectl config current-context) != "$smokeContext" ]]; then
  echo "This smoke test requires context $smokeContext" >&2
  exit 1
fi
readonly smokeVlan="runtime-vlan-smoke-${GITHUB_RUN_ID:-$$}"
readonly smokeSubnet="$smokeVlan"

k() {
  kubectl --context="$smokeContext" --request-timeout=15s "$@"
}

finish() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    k get nodes,pods -A -o wide || true
    k get provider-networks,vlans,subnets -o yaml || true
    k get events -A --sort-by=.metadata.creationTimestamp || true
    k -n kube-system logs -l app=kube-ovn-controller --all-containers --tail=200 --prefix || true
  fi
  # These names belong exclusively to this test. Remove the Subnet first so
  # its localnet port is cleaned up by the existing Subnet deletion path.
  k delete subnet "$smokeSubnet" --ignore-not-found --timeout=60s || true
  k delete vlan "$smokeVlan" --ignore-not-found --timeout=60s || true
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

vlanReady() {
  k get vlan "$1" -o json | jq -e '
    .metadata.generation as $generation |
    (.status.conflict != true) and
    any(.status.conditions[]?; .type == "Ready" and .status == "True" and .observedGeneration == $generation)
  ' >/dev/null
}

subnetReady() {
  k get subnet "$1" -o json | jq -e 'any(.status.conditions[]?; .type == "Ready" and .status == "True")' >/dev/null
}

providerReady() {
  k get provider-network provider -o json | jq -e '.status.ready == true' >/dev/null
}

providerContains() {
  k get provider-network provider -o json | jq -e --arg vlan "$1" '(.status.vlans // []) | index($vlan) != null' >/dev/null
}

providerRemoved() {
  k get provider-network provider -o json | jq -e --arg vlan "$smokeVlan" '(.status.vlans // []) | index($vlan) == null' >/dev/null
}

localnetTag() {
  local tag
  tag=$(k -n kube-system exec deployment/ovn-central -c ovn-central -- \
    ovn-nbctl --timeout=10 get logical_switch_port "localnet.$smokeSubnet" tag)
  [[ $tag == "$1" || $tag == "[$1]" ]]
}

statusVersions() {
  k get vlan "$smokeVlan" -o jsonpath='{.metadata.resourceVersion}'
  printf ':'
  k get provider-network provider -o jsonpath='{.metadata.resourceVersion}'
}

waitSettled() {
  local previous current stable=0
  previous=$(statusVersions)
  # Require ten seconds without a VLAN/ProviderNetwork write. A permanently
  # changing Ready timestamp would otherwise keep the watch queue busy.
  for ((attempt = 0; attempt < 60; attempt++)); do
    sleep 2
    current=$(statusVersions)
    if [[ $current == "$previous" ]]; then
      stable=$((stable + 1))
      if (( stable == 5 )); then
        echo "VLAN/ProviderNetwork resourceVersions settled: $current"
        return 0
      fi
    else
      stable=0
    fi
    previous=$current
  done
  echo 'VLAN/ProviderNetwork status did not settle' >&2
  return 1
}

k -n kube-system get deployment kube-ovn-controller -o json | jq -e '
  any(.spec.template.spec.containers[]; .name == "kube-ovn-controller" and
    any(.args[]; . == "--controller-runtime-mode=vlan"))
' >/dev/null
waitFor 'default ProviderNetwork readiness' providerReady
waitFor 'default VLAN readiness' vlanReady ovn-vlan
waitFor 'default VLAN membership' providerContains ovn-vlan
waitFor 'legacy default Subnet readiness' subnetReady ovn-default

# Reserve two unused nonzero VLAN IDs. No Pods use this test VLAN, so an
# external tagged network is not needed to verify the localnet tag lifecycle.
usedIds=$(k get vlans -o json | jq '[.items[].spec.id]')
smokeVlanId=4090
while jq -e --argjson id "$smokeVlanId" 'index($id) != null or index($id - 1) != null' <<<"$usedIds" >/dev/null; do
  smokeVlanId=$((smokeVlanId - 2))
  if (( smokeVlanId < 100 )); then
    echo 'No unused VLAN ID pair available' >&2
    exit 1
  fi
done
nextVlanId=$((smokeVlanId - 1))

# Submit both resources without a readiness wait between them. The Subnet
# requires VLAN readiness before it can create localnet.<subnet>.
k create -f - <<EOF_RESOURCES
apiVersion: kubeovn.io/v1
kind: Vlan
metadata:
  name: $smokeVlan
spec:
  id: $smokeVlanId
  provider: provider
---
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: $smokeSubnet
spec:
  protocol: IPv4
  cidrBlock: 10.254.254.0/24
  gateway: 10.254.254.1
  excludeIps:
    - 10.254.254.1
  vpc: ovn-cluster
  vlan: $smokeVlan
  logicalGateway: true
  disableGatewayCheck: true
EOF_RESOURCES
waitFor 'concurrent VLAN/Subnet bootstrap' vlanReady "$smokeVlan"
waitFor 'VLAN registered in ProviderNetwork' providerContains "$smokeVlan"
waitFor 'dependent legacy Subnet readiness' subnetReady "$smokeSubnet"
waitFor 'initial localnet VLAN tag' localnetTag "$smokeVlanId"

k patch vlan "$smokeVlan" --type=merge -p "{\"spec\":{\"id\":$nextVlanId}}"
waitFor 'updated VLAN generation readiness' vlanReady "$smokeVlan"
waitFor 'updated localnet VLAN tag' localnetTag "$nextVlanId"
waitSettled

k -n kube-system rollout restart deployment/kube-ovn-controller
k -n kube-system rollout status deployment/kube-ovn-controller --timeout=180s
waitFor 'VLAN readiness after controller restart' vlanReady "$smokeVlan"
waitFor 'ProviderNetwork readiness after controller restart' providerReady
waitFor 'localnet tag after controller restart' localnetTag "$nextVlanId"
waitSettled

k delete subnet "$smokeSubnet" --wait=true --timeout=60s
k delete vlan "$smokeVlan" --wait=true --timeout=60s
waitFor 'deleted VLAN removed from ProviderNetwork' providerRemoved
printf '%s\n' 'controller-runtime VLAN smoke passed'
