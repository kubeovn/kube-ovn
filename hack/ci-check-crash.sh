#!/bin/bash

set -e

# semicolon separated list of pod labels to ignore
# example: "app=kube-ovn-monitor;component=network,app=kube-ovn-pinger"
IGNORABLE_PODS=${IGNORABLE_PODS:-}

namespace="kube-system"

provider=$(kubectl get node -o jsonpath='{.items[0].spec.providerID}')
if echo "${provider}" | grep -q '^talos://'; then
  provider="talos"
else
  provider="other"
fi

IFS=';' read -r -a selectors <<< "$IGNORABLE_PODS"

exit_code=0
# check if there are any crashed pods
for pod in `kubectl get pod -n $namespace -l component=network -o name`; do
  podName=${pod#*/}
  containerTypes=(initContainer container)
  for containerType in ${containerTypes[@]}; do
    restartCounts=(`kubectl get -n $namespace $pod -o jsonpath="{.status.${containerType}Statuses[*].restartCount}"`)
    for ((i=0; i<${#restartCounts[@]}; i++)); do
      restartCount=${restartCounts[i]}
      if [ $restartCount -eq 0 ]; then
        continue
      fi

      name=`kubectl get -n $namespace $pod -o jsonpath="{.status.${containerType}Statuses[*].name}"`
      echo ">>> $containerType $name in pod $namespace/$podName restarted $restartCount time(s). Logs of the previous instance:"
      kubectl logs -p -n $namespace $pod -c $name
      if [ "$provider" = "talos" -a "$name" = "cni-server" ]; then
        if kubectl logs -p -n $namespace $pod -c $name | tail -n1 | grep -q "network not ready"; then
          continue
        fi
      fi
      for selector in "${selectors[@]}"; do
        if kubectl get pod -n $namespace -l "$selector" -o name | grep -q "^$pod$"; then
          continue 2
        fi
      done
      exit_code=1
    done
  done
done

exit $exit_code
