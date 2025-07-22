#!/bin/bash

set -ex

for resource_type in subnet vpc ip; do
  for resource in $(kubectl get "$resource_type" -o name); do
    kubectl patch "$resource" --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]'
  done
done