#!/bin/bash
set -eux
export PS4='+ $(date "+%Y-%m-%d %H:%M:%S")\011 '

kubectl delete --ignore-not-found -n kube-system ds kube-ovn-pinger
# ensure kube-ovn-pinger has been deleted
while :; do
  if [ $(kubectl get pod -n kube-system -l app=kube-ovn-pinger -o name | wc -l) -eq 0 ]; then
    break
  fi
  sleep 1
done

for gw in $(kubectl get vpc-nat-gw -o name); do
  kubectl delete --ignore-not-found $gw
done

for vd in $(kubectl  get vpc-dns -o name); do
  kubectl delete --ignore-not-found $vd
done

for vip in $(kubectl get vip -o name); do
   kubectl delete --ignore-not-found $vip
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

for eip in $(kubectl get eip -o name); do
   kubectl delete --ignore-not-found $eip
done

for odnat in $(kubectl get odnat -o name); do
   kubectl delete --ignore-not-found $odnat
done

for osnat in $(kubectl get osnat -o name); do
   kubectl delete --ignore-not-found $osnat
done

for ofip in $(kubectl get ofip -o name); do
   kubectl delete --ignore-not-found $ofip
done

for oeip in $(kubectl get oeip -o name); do
   kubectl delete --ignore-not-found $oeip
done

for slr in $(kubectl get switch-lb-rule -o name); do
   kubectl delete --ignore-not-found $slr
done

for ippool in $(kubectl get ippool -o name); do
  kubectl delete --ignore-not-found $ippool
done

set +e
for subnet in $(kubectl get subnet -o name); do
  kubectl patch "$subnet" --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]'
  kubectl delete --ignore-not-found "$subnet"
done
# subnet join will recreate, so delete subnet crd right now
kubectl delete --ignore-not-found crd subnets.kubeovn.io
set -e

for vpc in $(kubectl get vpc -o name); do
  kubectl delete --ignore-not-found $vpc
done

for vlan in $(kubectl get vlan -o name); do
  kubectl delete --ignore-not-found $vlan
done

for pn in $(kubectl get provider-network -o name); do
  kubectl delete --ignore-not-found $pn
done

# Delete Kube-OVN components
kubectl delete --ignore-not-found -n kube-system deploy kube-ovn-monitor
kubectl delete --ignore-not-found -n kube-system cm ovn-config ovn-ic-config \
  ovn-external-gw-config ovn-vpc-nat-config ovn-vpc-nat-gw-config
kubectl delete --ignore-not-found -n kube-system svc kube-ovn-pinger kube-ovn-controller kube-ovn-cni kube-ovn-monitor
kubectl delete --ignore-not-found -n kube-system deploy kube-ovn-controller
kubectl delete --ignore-not-found -n kube-system deploy ovn-ic-controller
kubectl delete --ignore-not-found -n kube-system deploy ovn-ic-server

# wait for provier-networks to be deleted before deleting kube-ovn-cni
sleep 5
kubectl delete --ignore-not-found -n kube-system ds kube-ovn-cni

# ensure kube-ovn-cni has been deleted
while :; do
  if [ $(kubectl get pod -n kube-system -l app=kube-ovn-cni -o name | wc -l) -eq 0 ]; then
    break
  fi
  sleep 1
done

for pod in $(kubectl get pod -n kube-system -l app=ovs -o 'jsonpath={.items[?(@.status.phase=="Running")].metadata.name}'); do
  kubectl exec -n kube-system "$pod" -- bash /kube-ovn/uninstall.sh
done

kubectl delete --ignore-not-found svc ovn-nb ovn-sb ovn-northd -n kube-system
kubectl delete --ignore-not-found deploy ovn-central -n kube-system
kubectl delete --ignore-not-found ds ovs-ovn -n kube-system
kubectl delete --ignore-not-found ds ovs-ovn-dpdk -n kube-system
kubectl delete --ignore-not-found secret kube-ovn-tls -n kube-system

# delete vpc-dns content
kubectl delete --ignore-not-found cm vpc-dns-config -n kube-system
kubectl delete --ignore-not-found clusterrole system:vpc-dns
kubectl delete --ignore-not-found clusterrolebinding vpc-dns
kubectl delete --ignore-not-found sa vpc-dns -n kube-system

# delete CRD
kubectl delete --ignore-not-found crd \
  security-groups.kubeovn.io \
  ippools.kubeovn.io \
  vpc-nat-gateways.kubeovn.io \
  vpcs.kubeovn.io \
  vlans.kubeovn.io \
  provider-networks.kubeovn.io \
  iptables-dnat-rules.kubeovn.io \
  iptables-snat-rules.kubeovn.io \
  iptables-fip-rules.kubeovn.io \
  iptables-eips.kubeovn.io \
  vips.kubeovn.io \
  switch-lb-rules.kubeovn.io \
  vpc-dnses.kubeovn.io \
  ovn-dnat-rules.kubeovn.io \
  ovn-snat-rules.kubeovn.io \
  ovn-fips.kubeovn.io \
  ovn-eips.kubeovn.io \
  qos-policies.kubeovn.io

# in case of ip not delete
set +e
for ip in $(kubectl get ip -o name); do
  kubectl patch "$ip" --type='json' -p '[{"op": "replace", "path": "/metadata/finalizers", "value": []}]'
  kubectl delete --ignore-not-found "$ip"
done
kubectl delete --ignore-not-found crd ips.kubeovn.io
set -e

# Remove annotations/labels in namespaces and nodes
kubectl annotate node --all ovn.kubernetes.io/cidr-
kubectl annotate node --all ovn.kubernetes.io/gateway-
kubectl annotate node --all ovn.kubernetes.io/ip_address-
kubectl annotate node --all ovn.kubernetes.io/logical_switch-
kubectl annotate node --all ovn.kubernetes.io/mac_address-
kubectl annotate node --all ovn.kubernetes.io/port_name-
kubectl annotate node --all ovn.kubernetes.io/allocated-
kubectl annotate node --all ovn.kubernetes.io/chassis- 
kubectl label node --all kube-ovn/role-

kubectl get node -o name | while read node; do
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
  sleep 10
  if [ $(kubectl get pod -n kube-system -l component=network -o name | wc -l) -eq 0 ]; then
    break
  fi
  for pod in `kubectl -n kube-system get pod -l component=network -o name`; do
    echo "$pod logs:"
    kubectl -n kube-system logs $pod --timestamps --tail 50
  done
done

# wait for all pods to be deleted before deleting serviceaccount/clusterrole/clusterrolebinding
kubectl delete --ignore-not-found sa ovn ovn-ovs kube-ovn-cni kube-ovn-app -n kube-system
kubectl delete --ignore-not-found clusterrole system:ovn system:ovn-ovs system:kube-ovn-cni system:kube-ovn-app
kubectl delete --ignore-not-found clusterrolebinding ovn ovn ovn-ovs kube-ovn-cni kube-ovn-app
kubectl delete --ignore-not-found rolebinding -n kube-system ovn kube-ovn-cni kube-ovn-app

kubectl delete --ignore-not-found -n kube-system lease kube-ovn-controller
kubectl delete --ignore-not-found -n kube-system secret ovn-ipsec-ca

# Remove annotations in all pods of all namespaces
for ns in $(kubectl get ns -o name | awk -F/ '{print $2}'); do
  echo "annotating pods in namespace $ns"
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/cidr-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/gateway-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/ip_address-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/logical_switch-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/mac_address-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/port_name-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/allocated-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/routed-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/vlan_id-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/network_type-
  kubectl annotate pod --all -n $ns ovn.kubernetes.io/provider_network-
done
