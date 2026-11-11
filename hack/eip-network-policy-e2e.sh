#!/usr/bin/env bash
# Regression for #7935: apply a tiered NetworkPolicy to an established EIP flow.
# Requires an installed cluster with its external gateway enabled and an external
# Docker network connected to the gateway. Reserve EIP outside Docker/IPAM pools.
# Example:
# EIP=172.19.0.230 EXTERNAL_NETWORK=kube-ovn-vlan \
#   KUBE_OVN_IMAGE=kubeovn/kube-ovn:v1.16.1 bash hack/eip-network-policy-e2e.sh
# The same ping process/ICMP identifier must survive the policy transition.
set -euo pipefail

: "${EIP:?Set an unused IPv4 EIP on the configured external subnet}"
: "${EXTERNAL_NETWORK:?Set the external Docker network connected to the gateway}"
: "${KUBE_OVN_IMAGE:?Set the installed Kube-OVN image for the test Pod}"
CLIENT_IMAGE=${CLIENT_IMAGE:-busybox:1.36}
ARTIFACT_DIR=${ARTIFACT_DIR:-$(mktemp -d /tmp/eip-network-policy.XXXXXX)}
namespace="eip-policy-$(date +%s)-$$"
client="$namespace-client"
mkdir -p "$ARTIFACT_DIR"

cleanup() {
  local result=$?
  trap - EXIT
  docker logs "$client" > "$ARTIFACT_DIR/ping.log" 2>&1 || true
  kubectl -n "$namespace" get pod,networkpolicy -o yaml > "$ARTIFACT_DIR/resources.yaml" 2>&1 || true
  # Keep the external client until the deleted EIP stops answering, so a
  # subsequent run can safely reuse this address instead of racing teardown.
  if kubectl -n "$namespace" get pod server >/dev/null 2>&1; then
    kubectl -n "$namespace" delete pod server --wait=true --timeout=120s >/dev/null || result=1
    local unreachable=false
    for ((attempt=0; attempt<30; attempt++)); do
      docker exec "$client" ping -n -c 3 -W 1 "$EIP" > "$ARTIFACT_DIR/deleted-eip-ping.log" 2>&1 || true
      if grep -Eq '3 packets transmitted, 0 packets received' "$ARTIFACT_DIR/deleted-eip-ping.log"; then
        unreachable=true
        break
      fi
      sleep 1
    done
    if [[ $unreachable != true ]]; then
      echo "FAIL: deleted EIP still answers or its unreachability could not be verified" >&2
      result=1
    else
      echo "Deleted EIP verified unreachable before reuse"
    fi
  fi
  docker rm -f "$client" >/dev/null 2>&1 || true
  kubectl delete namespace "$namespace" --ignore-not-found --wait=false >/dev/null || true
  echo "Artifacts: $ARTIFACT_DIR"
  exit "$result"
}
trap cleanup EXIT

# Explicit version gate: this regression targets the supported 1.16+ branches.
version=$(kubectl -n kube-system get deploy kube-ovn-controller -o jsonpath='{.spec.template.spec.containers[0].image}')
if [[ $version =~ :v1\.([0-9]+)\. ]] && ((10#${BASH_REMATCH[1]} < 16)); then
  echo "SKIP: this regression requires Kube-OVN 1.16+ ($version)"
  exit 0
fi

kubectl create namespace "$namespace"
kubectl -n "$namespace" run server --image="$KUBE_OVN_IMAGE" \
  --labels=app=eip-policy --annotations="ovn.kubernetes.io/eip=$EIP" \
  --command -- sleep infinity
kubectl -n "$namespace" wait pod/server --for=condition=Ready --timeout=120s

docker run -d --name "$client" --network "$EXTERNAL_NETWORK" "$CLIENT_IMAGE" \
  ping -n -i 0.2 "$EIP" >/dev/null

reply_count() {
  docker logs "$client" > "$ARTIFACT_DIR/ping-current.log" 2>&1
  awk '/bytes from/ {n++} END {print n+0}' "$ARTIFACT_DIR/ping-current.log"
}

# Positive control: do not accept a broken NAT/ARP path as successful isolation.
connected=false
for ((attempt=0; attempt<30; attempt++)); do
  if (( $(reply_count) >= 5 )); then
    connected=true
    break
  fi
  sleep 1
done
if [[ $connected != true ]]; then
  echo "FAIL: EIP did not become reachable before policy installation" >&2
  exit 1
fi

kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: tcp-only
  namespace: $namespace
spec:
  podSelector:
    matchLabels:
      app: eip-policy
  policyTypes: [Ingress, Egress]
  ingress:
  - ports:
    - protocol: TCP
      port: 22
  egress:
  - ports:
    - protocol: TCP
      port: 80
    - protocol: TCP
      port: 443
EOF

# Allow controller/northd/OVS convergence, then observe the SAME live flow.
# A fresh ping here would miss the established-connection blocked-mark bug.
sleep "${POLICY_SETTLE_SECONDS:-5}"
before=$(reply_count)
sleep "${OBSERVE_SECONDS:-10}"
docker inspect -f '{{.State.Running}}' "$client" | grep -qx true
if (( $(reply_count) != before )); then
  echo "FAIL: established ICMP flow leaked replies after TCP-only policy" >&2
  exit 1
fi

# Reverse positive control: prove the client and EIP remained functional.
kubectl -n "$namespace" delete networkpolicy tcp-only
recovered=false
for ((attempt=0; attempt<30; attempt++)); do
  if (( $(reply_count) >= before + 5 )); then
    recovered=true
    break
  fi
  sleep 1
done
if [[ $recovered != true ]]; then
  echo "FAIL: connectivity did not recover after removing policy" >&2
  exit 1
fi
echo "PASS: established EIP ICMP flow was blocked without alternating leaks"
