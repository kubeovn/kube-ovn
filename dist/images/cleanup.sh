#!/bin/bash
set -eu

# Remove finalizers in svc
kubectl patch svc -n kube-ovn ovn-nb --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]' || true
kubectl patch svc -n kube-ovn ovn-sb --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]' || true

# Delete Kube-OVN components
kubectl delete -f https://raw.githubusercontent.com/alauda/kube-ovn/master/yamls/ovn.yaml
kubectl delete -f https://raw.githubusercontent.com/alauda/kube-ovn/master/yamls/kube-ovn.yaml

# Remove annotations in namespaces and nodes
kubectl annotate no --all ovn.kubernetes.io/cidr-
kubectl annotate no --all ovn.kubernetes.io/gateway-
kubectl annotate no --all ovn.kubernetes.io/ip_address-
kubectl annotate no --all ovn.kubernetes.io/logical_switch-
kubectl annotate no --all ovn.kubernetes.io/mac_address-
kubectl annotate no --all ovn.kubernetes.io/port_name-
kubectl annotate ns --all ovn.kubernetes.io/cidr-
kubectl annotate ns --all ovn.kubernetes.io/exclude_ips-
kubectl annotate ns --all ovn.kubernetes.io/gateway-
kubectl annotate ns --all ovn.kubernetes.io/logical_switch-
kubectl annotate ns --all ovn.kubernetes.io/private-
kubectl annotate ns --all ovn.kubernetes.io/allow-
