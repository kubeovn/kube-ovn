#!/bin/bash

set -e

POD_NAMESPACE=${POD_NAMESPACE:-kube-system}

dsGenVer=`kubectl -n $POD_NAMESPACE get ds ovs-ovn -o jsonpath={.metadata.generation}`
for node in `kubectl get node -o jsonpath='{.items[*].metadata.name}'`; do
  # delete pod with old version
  kubectl -n $POD_NAMESPACE delete pod -l app=ovs,pod-template-generation!=$dsGenVer --field-selector spec.nodeName=$node
  # wait the pod with new version to be created and delete it
  while true; do
    pod=`kubectl -n $POD_NAMESPACE get pod -l app=ovs,pod-template-generation=$dsGenVer --field-selector spec.nodeName=$node -o name`
    if [ ! -z $pod ]; then
      kubectl -n $POD_NAMESPACE delete $pod --wait=false
      break
    fi
    sleep 0.1
  done
done
