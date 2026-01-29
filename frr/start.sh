#!/bin/bash

docker cp evpn.conf clab-bgp-router:/etc/frr/frr.conf
docker exec clab-bgp-router /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf

kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml
kubectl apply -f config.yaml
kubectl apply -f bgp-conf.yaml
kubectl apply -f evpn-conf.yaml
kubectl wait --for=condition=Ready pod -n kube-system -l app=multus
sleep 5

kubectl apply -f egw.yaml
kubectl wait --for=condition=Ready pod -l app=vpc-egress-gateway
sleep 5

docker exec clab-bgp-router vtysh -c "show ip route vrf vrf-vpn"