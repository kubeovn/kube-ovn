#!/bin/bash

set -ex

POD_NAMESPACE=${POD_NAMESPACE:-kube-system}

dsChartVer=`kubectl get ds -n $POD_NAMESPACE ovs-ovn -o jsonpath={.spec.template.metadata.annotations.chart-version}`

for node in `kubectl get node -o jsonpath='{.items[*].metadata.name}'`; do
  pods=(`kubectl -n $POD_NAMESPACE get pod -l app=ovs --field-selector spec.nodeName=$node -o name`)
  for pod in ${pods[*]}; do
    podChartVer=`kubectl -n $POD_NAMESPACE get $pod -o jsonpath={.metadata.annotations.chart-version}`
    if [ "$dsChartVer" != "$podChartVer" ]; then
      echo "deleting $pod on node $node"
      kubectl -n $POD_NAMESPACE delete $pod
    fi
  done

  while true; do
    pods=(`kubectl -n $POD_NAMESPACE get pod -l app=ovs --field-selector spec.nodeName=$node -o name`)
    if [ ${#pods[*]} -ne 0 ]; then
      break
    fi
    echo "waiting for ovs-ovn pod on node $node to be created"
    sleep 1
  done

  echo "waiting for ovs-ovn pod on node $node to be ready"
  kubectl -n $POD_NAMESPACE wait pod --for=condition=ready -l app=ovs --field-selector spec.nodeName=$node
done
