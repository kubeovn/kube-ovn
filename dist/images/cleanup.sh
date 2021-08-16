#!/bin/bash
set -eu

for subnet in $(kubectl get subnet -o name); do
  kubectl patch "$subnet" --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]'
done

for vlan in $(kubectl get vlan -o name); do
  kubectl delete $vlan
done

for pn in $(kubectl get provider-network -o name); do
  kubectl delete $pn
done

sleep 3

# Delete Kube-OVN components
kubectl delete cm ovn-config ovn-ic-config ovn-external-gw-config -n kube-system --ignore-not-found=true
kubectl delete secret kube-ovn-tls -n kube-system --ignore-not-found=true
kubectl delete sa ovn -n kube-system --ignore-not-found=true
kubectl delete clusterrole system:ovn --ignore-not-found=true
kubectl delete clusterrolebinding ovn --ignore-not-found=true
kubectl delete svc ovn-nb ovn-sb ovn-northd kube-ovn-pinger kube-ovn-controller kube-ovn-cni kube-ovn-monitor -n kube-system --ignore-not-found=true
kubectl delete ds kube-ovn-cni -n kube-system --ignore-not-found=true
kubectl delete deployment ovn-central kube-ovn-controller kube-ovn-monitor -n kube-system --ignore-not-found=true
kubectl get pod --no-headers -n kube-system -lapp=ovs -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,IP:.status.podIP | awk '{
  if ($2 == "Running") {
    system("kubectl exec -n kube-system "$1" -- bash /kube-ovn/uninstall.sh "$3)
  }
}'
kubectl delete ds ovs-ovn kube-ovn-pinger -n kube-system --ignore-not-found=true
kubectl delete crd --ignore-not-found=true \
  ips.kubeovn.io \
  subnets.kubeovn.io \
  vpc-nat-gateways.kubeovn.io \
  vpcs.kubeovn.io \
  vlans.kubeovn.io \
  provider-networks.kubeovn.io \
  networks.kubeovn.io

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
  kubectl annotate pod  --all ovn.kubernetes.io/network_type- -n "$ns"
  kubectl annotate pod  --all ovn.kubernetes.io/provider_network- -n "$ns"
done
