#!/bin/bash
set -eu

kubectl delete --ignore-not-found ds kube-ovn-pinger -n kube-system
# ensure kube-ovn-pinger has been deleted
while :; do
  if [ $(kubectl get pod --no-headers -n kube-system -l app=kube-ovn-pinger | wc -l) -eq 0 ]; then
    break
  fi
  sleep 5
done

set +e
for subnet in $(kubectl get subnet -o name); do
  kubectl patch "$subnet" --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]'
  kubectl delete --ignore-not-found "$subnet"
done
set -e

for vpc in $(kubectl get vpc -o name); do
  kubectl delete --ignore-not-found $vpc
done

for gw in $(kubectl get vpc-nat-gw -o name); do
  kubectl delete --ignore-not-found $gw
done

for vlan in $(kubectl get vlan -o name); do
  kubectl delete --ignore-not-found $vlan
done

for pn in $(kubectl get provider-network -o name); do
  kubectl delete --ignore-not-found $pn
done

for vip in $(kubectl get vip -o name); do
   kubectl delete --ignore-not-found $vip
done

for eip in $(kubectl get eip -o name); do
   kubectl delete --ignore-not-found $eip
done

for snat in $(kubectl get snat -o name); do
   kubectl delete --ignore-not-found $snat
done

for dnat in $(kubectl get dnat -o name); do
   kubectl delete --ignore-not-found $dnat
done

for fip in $(kubectl get fip -o name); do
   kubectl delete --ignore-not-found $fip
done

sleep 5

# Delete Kube-OVN components
kubectl delete --ignore-not-found deploy kube-ovn-monitor -n kube-system
kubectl delete --ignore-not-found cm ovn-config ovn-ic-config ovn-external-gw-config -n kube-system
kubectl delete --ignore-not-found svc kube-ovn-pinger kube-ovn-controller kube-ovn-cni kube-ovn-monitor -n kube-system
kubectl delete --ignore-not-found ds kube-ovn-cni -n kube-system
kubectl delete --ignore-not-found deploy kube-ovn-controller -n kube-system

# ensure kube-ovn-cni has been deleted
while :; do
  if [ $(kubectl get pod --no-headers -n kube-system -l app=kube-ovn-cni | wc -l) -eq 0 ]; then
    break
  fi
  sleep 5
done

for pod in $(kubectl get pod -n kube-system -l app=ovs -o 'jsonpath={.items[?(@.status.phase=="Running")].metadata.name}'); do
  kubectl exec -n kube-system "$pod" -- bash /kube-ovn/uninstall.sh
done

kubectl delete --ignore-not-found svc ovn-nb ovn-sb ovn-northd -n kube-system
kubectl delete --ignore-not-found deploy ovn-central -n kube-system
kubectl delete --ignore-not-found ds ovs-ovn -n kube-system
kubectl delete --ignore-not-found ds ovs-ovn-dpdk -n kube-system
kubectl delete --ignore-not-found secret kube-ovn-tls -n kube-system
kubectl delete --ignore-not-found sa ovn -n kube-system
kubectl delete --ignore-not-found clusterrole system:ovn
kubectl delete --ignore-not-found clusterrolebinding ovn

# delete CRD
kubectl delete --ignore-not-found crd htbqoses.kubeovn.io security-groups.kubeovn.io ips.kubeovn.io subnets.kubeovn.io \
                                      vpc-nat-gateways.kubeovn.io vpcs.kubeovn.io vlans.kubeovn.io provider-networks.kubeovn.io \
                                      iptables-dnat-rules.kubeovn.io  iptables-eips.kubeovn.io  iptables-fip-rules.kubeovn.io \
                                      iptables-snat-rules.kubeovn.io vips.kubeovn.io

# Remove annotations/labels in namespaces and nodes
kubectl annotate no --all ovn.kubernetes.io/cidr-
kubectl annotate no --all ovn.kubernetes.io/gateway-
kubectl annotate no --all ovn.kubernetes.io/ip_address-
kubectl annotate no --all ovn.kubernetes.io/logical_switch-
kubectl annotate no --all ovn.kubernetes.io/mac_address-
kubectl annotate no --all ovn.kubernetes.io/port_name-
kubectl annotate no --all ovn.kubernetes.io/allocated-
kubectl annotate no --all ovn.kubernetes.io/chassis- 
kubectl label node --all kube-ovn/role-

kubectl get no -o name | while read node; do
  kubectl get "$node" -o 'go-template={{ range $k, $v := .metadata.labels }}{{ $k }}{{"\n"}}{{ end }}' | while read label; do
    if echo "$label" | grep -qE '^(.+\.provider-network\.kubernetes\.io/(ready|mtu|interface|exclude))$'; then
      kubectl label "$node" "$label-"
    fi
  done
done

kubectl annotate ns --all ovn.kubernetes.io/cidr-
kubectl annotate ns --all ovn.kubernetes.io/exclude_ips-
kubectl annotate ns --all ovn.kubernetes.io/gateway-
kubectl annotate ns --all ovn.kubernetes.io/logical_switch-
kubectl annotate ns --all ovn.kubernetes.io/private-
kubectl annotate ns --all ovn.kubernetes.io/allow-
kubectl annotate ns --all ovn.kubernetes.io/allocated-

# ensure kube-ovn components have been deleted
while :; do
  sleep 5
  if [ $(kubectl get pod --no-headers -n kube-system -l component=network | wc -l) -eq 0 ]; then
    break
  fi
done

# Remove annotations in all pods of all namespaces
for ns in $(kubectl get ns -o name |cut -c 11-); do
  echo "annotating pods in  ns:$ns"
  kubectl annotate pod --all ovn.kubernetes.io/cidr- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/gateway- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/ip_address- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/logical_switch- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/mac_address- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/port_name- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/allocated- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/routed- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/vlan_id- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/network_type- -n "$ns"
  kubectl annotate pod --all ovn.kubernetes.io/provider_network- -n "$ns"
done
