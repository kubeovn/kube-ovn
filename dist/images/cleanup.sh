#!/bin/bash
set -eu

for subnet in $(kubectl get subnet -o name); do
  kubectl patch "$subnet" --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]'
done

# Delete Kube-OVN components
kubectl delete cm ovn-config ovn-ic-config ovn-external-gw-config -n kube-system --ignore-not-found=true
kubectl delete secret kube-ovn-tls -n kube-system --ignore-not-found=true
kubectl delete sa ovn -n kube-system --ignore-not-found=true
kubectl delete clusterrole system:ovn --ignore-not-found=true
kubectl delete clusterrolebinding ovn --ignore-not-found=true
kubectl delete svc ovn-nb ovn-sb kube-ovn-pinger kube-ovn-controller kube-ovn-cni -n kube-system --ignore-not-found=true
kubectl delete ds kube-ovn-cni -n kube-system --ignore-not-found=true
kubectl delete deployment ovn-central kube-ovn-controller -n kube-system --ignore-not-found=true
for ovsstatus in $(kubectl get pod --no-headers -n kube-system -lapp=ovs | awk '{print $1"+"$3}')
do
  status=`echo ${ovsstatus#*+}`
  if [  "$status" = "Running"  ]; then
    ovs=`echo ${ovsstatus%+*}`
    kubectl exec -n kube-system "$ovs" -- bash /kube-ovn/uninstall.sh
  fi
done
kubectl delete ds ovs-ovn kube-ovn-pinger -n kube-system --ignore-not-found=true
kubectl delete crd ips.kubeovn.io subnets.kubeovn.io vlans.kubeovn.io networks.kubeovn.io --ignore-not-found=true

# Remove annotations/labels in namespaces and nodes
kubectl annotate no --all ovn.kubernetes.io/cidr-
kubectl annotate no --all ovn.kubernetes.io/gateway-
kubectl annotate no --all ovn.kubernetes.io/ip_address-
kubectl annotate no --all ovn.kubernetes.io/logical_switch-
kubectl annotate no --all ovn.kubernetes.io/mac_address-
kubectl annotate no --all ovn.kubernetes.io/port_name-
kubectl annotate no --all ovn.kubernetes.io/allocated-
kubectl label node --all kube-ovn/role-

kubectl annotate ns --all ovn.kubernetes.io/cidr-
kubectl annotate ns --all ovn.kubernetes.io/exclude_ips-
kubectl annotate ns --all ovn.kubernetes.io/gateway-
kubectl annotate ns --all ovn.kubernetes.io/logical_switch-
kubectl annotate ns --all ovn.kubernetes.io/private-
kubectl annotate ns --all ovn.kubernetes.io/allow-
kubectl annotate ns --all ovn.kubernetes.io/allocated-

# Wait Pod Deletion
sleep 5

# Remove annotations in all pods of all namespaces
for ns in $(kubectl get ns -o name |cut -c 11-); do
  echo "annotating pods in  ns:$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/cidr- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/gateway- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/ip_address- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/logical_switch- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/mac_address- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/port_name- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/allocated- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/routed- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/vlan_id- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/vlan_range- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/network_types- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/provider_interface_name- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/host_interface_name- -n "$ns"
done
