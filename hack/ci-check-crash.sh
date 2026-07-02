#!/bin/bash

set -e

namespace="kube-system"

provider=$(kubectl get node -o jsonpath='{.items[0].spec.providerID}')
if echo "${provider}" | grep -q '^talos://'; then
  provider="talos"
else
  provider="other"
fi

exit_code=0
# check if there are any crashed pods
for pod in `kubectl get pod -n $namespace -l component=network -o name`; do
  podName=${pod#*/}
  if ! kubectl get -n $namespace $pod &>/dev/null; then
    echo ">>> pod $namespace/$podName no longer exists, skipping"
    continue
  fi
  containerTypes=(initContainer container)
  for containerType in ${containerTypes[@]}; do
    restartCounts=(`kubectl get -n $namespace $pod -o jsonpath="{.status.${containerType}Statuses[*].restartCount}" 2>/dev/null || true`)
    names=(`kubectl get -n $namespace $pod -o jsonpath="{.status.${containerType}Statuses[*].name}" 2>/dev/null || true`)
    if [ ${#restartCounts[@]} -eq 0 -a ${#names[@]} -eq 0 ]; then
      if ! kubectl get -n $namespace $pod >/dev/null 2>&1; then
        echo ">>> pod $namespace/$podName disappeared while checking restarts, skipping"
        continue
      fi
    fi
    for ((i=0; i<${#restartCounts[@]}; i++)); do
      restartCount=${restartCounts[i]}
      if [ $restartCount -eq 0 ]; then
        continue
      fi

      name=${names[i]}
      echo ">>> $containerType $name in pod $namespace/$podName restarted $restartCount time(s). Logs of the previous instance:"
      prevLogs=$(kubectl logs -p -n $namespace $pod -c $name 2>&1) || true
      printf '%s\n' "$prevLogs"
      if [ "$provider" = "talos" -a "$name" = "cni-server" ]; then
        if printf '%s\n' "$prevLogs" | tail -n1 | grep -q "network not ready"; then
          continue
        fi
      fi
      exit_code=1
    done
  done
done

exit $exit_code
