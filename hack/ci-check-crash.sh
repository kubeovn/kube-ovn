#!/bin/bash

namespace="kube-system"

exit_code=0
# check if there are any crashed pods
for pod in `kubectl get pod -n $namespace -l component=network -o name`; do
  restartCount=`kubectl get -n $namespace $pod -o jsonpath='{.status.containerStatuses[0].restartCount}'`
  # TODO: get restart count for all containers
  if [ $restartCount -gt 0 ]; then
    exit_code=1
    echo "$pod restarted $restartCount time(s). Logs of the previous instance:"
    kubectl logs -p -n $namespace $pod
  fi
done

exit $exit_code
