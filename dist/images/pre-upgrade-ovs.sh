#!/bin/bash

set -euo pipefail

POD_NAMESPACE=${POD_NAMESPACE:-kube-system}

mapfile -t ovs_pods < <(kubectl -n "${POD_NAMESPACE}" get pod -l app=ovs --field-selector status.phase=Running -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

if [ "${#ovs_pods[@]}" -eq 0 ]; then
    echo "no ovs-ovn pods found in namespace ${POD_NAMESPACE}, skip pre-upgrade ovn-match-northd-version setup"
    exit 0
fi

for pod in "${ovs_pods[@]}"; do
    echo "setting ovn-match-northd-version=true in pod ${pod}"
    kubectl -n "${POD_NAMESPACE}" exec "${pod}" -c openvswitch -- \
        ovs-vsctl --timeout=10 set open . external-ids:ovn-match-northd-version=true
done
