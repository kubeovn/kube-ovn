#!/bin/bash
set -eo pipefail

# the number could be obtained from the label of nodes
# example: kubectl  get nodes --no-headers=true -l kube-ovn/role=master | wc -l
NODES=$(kubectl  get nodes --no-headers=true | wc -l)

# 3 actions defined in configmap: install-module, install-local-module, install-module-without-header, remove-module
ACTION="install-module"

# local rpm header file name for local install only
KERNEL_HEADER=""

sed -e "s/NODENUMBER/${NODES}/g" -e "s/ACTION/${ACTION}/g" -e "s/KERNEL_HEADER/${KERNEL_HEADER}/g" \
    ../yamls/rh-mod-job.yaml > /tmp/rh-mod-compile-${ACTION}.yaml

kubectl apply -f /tmp/rh-mod-compile-${ACTION}.yaml

