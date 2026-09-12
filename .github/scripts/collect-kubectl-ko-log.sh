#!/usr/bin/env bash
set -euo pipefail

KUBE_OVN_NS=${KUBE_OVN_NS:-kube-system}
log_archive_status=0
if bash dist/images/kubectl-ko log all; then
  :
else
  log_archive_status=$?
fi

mkdir -p kubectl-ko-log

if command -v docker > /dev/null 2>&1; then
  nodes=$(kubectl get pods -n "$KUBE_OVN_NS" -l app=kube-ovn-cni \
    -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' 2>/dev/null || true)
  nodes+=" $(kubectl get pods -n "$KUBE_OVN_NS" -l app=ovs \
    -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' 2>/dev/null || true)"
  if [[ -z "${nodes//[$' \t\r\n']/}" ]]; then
    nodes=$(kubectl get nodes \
      -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
  fi

  for node in $nodes; do
    [[ -n "$node" ]] || continue
    if ! docker inspect "$node" > /dev/null 2>&1; then
      echo "Warning: Docker node container $node was not found; skipping host log fallback"
      continue
    fi

    for component in kube-ovn ovn openvswitch; do
      destination="kubectl-ko-log/$node/$component"
      if find "$destination" -type f -print -quit 2>/dev/null | grep -q .; then
        continue
      fi

      mkdir -p "$destination"
      echo "Collecting $component logs from Docker node container $node"
      if ! docker cp "$node:/var/log/$component/." "$destination/" > /dev/null; then
        echo "Warning: failed to collect /var/log/$component from Docker node container $node"
      fi
    done
  done
else
  echo "Warning: Docker is not available; skipping host log fallback"
fi

tar -zcf kubectl-ko-log.tar.gz kubectl-ko-log/
exit "$log_archive_status"
