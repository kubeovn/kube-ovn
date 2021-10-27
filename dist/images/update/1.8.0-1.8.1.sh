#!/bin/bash
set -eo pipefail

IMAGE=kubeovn/kube-ovn:v1.8.1

echo "[Step 0/5] Update ovn-central"
kubectl set image deployment/ovn-central -n kube-system ovn-central="$IMAGE"
kubectl rollout status deployment/ovn-central -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 1/5] Update ovs-ovn"
kubectl set image ds/ovs-ovn -n kube-system openvswitch="$IMAGE"
kubectl delete pod -n kube-system -lapp=ovs
echo "-------------------------------"
echo ""

echo "[Step 2/5] Update kube-ovn-controller"
kubectl set image deployment/kube-ovn-controller -n kube-system kube-ovn-controller="$IMAGE"
kubectl rollout status deployment/kube-ovn-controller -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 3/5] Update kube-ovn-cni"
kubectl set image ds/kube-ovn-cni -n kube-system cni-server="$IMAGE"
kubectl rollout status daemonset/kube-ovn-cni -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 4/5] Update kube-ovn-pinger"
if [[ $(kubectl get ds -n kube-system kube-ovn-pinger -o jsonpath='{.spec.template}') =~ "tolerations" ]]; then
  kubectl patch ds/kube-ovn-pinger -n kube-system --type='json' -p='[{"op": "remove", "path": "/spec/template/spec/tolerations"}]'
fi
kubectl set image ds/kube-ovn-pinger -n kube-system pinger="$IMAGE"
kubectl rollout status daemonset/kube-ovn-pinger -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 5/5] Update kube-ovn-monitor"
kubectl set image deployment/kube-ovn-monitor -n kube-system kube-ovn-monitor="$IMAGE"
kubectl rollout status deployment/kube-ovn-monitor -n kube-system
echo "-------------------------------"
echo ""

echo "Update Success!"
echo "
                    ,,,,
                    ,::,
                   ,,::,,,,
            ,,,,,::::::::::::,,,,,
         ,,,::::::::::::::::::::::,,,
       ,,::::::::::::::::::::::::::::,,
     ,,::::::::::::::::::::::::::::::::,,
    ,::::::::::::::::::::::::::::::::::::,
   ,:::::::::::::,,   ,,:::::,,,::::::::::,
 ,,:::::::::::::,       ,::,     ,:::::::::,
 ,:::::::::::::,   :x,  ,::  :,   ,:::::::::,
,:::::::::::::::,  ,,,  ,::, ,,  ,::::::::::,
,:::::::::::::::::,,,,,,:::::,,,,::::::::::::,    ,:,   ,:,            ,xx,                            ,:::::,   ,:,     ,:: :::,    ,x
,::::::::::::::::::::::::::::::::::::::::::::,    :x: ,:xx:        ,   :xx,                          :xxxxxxxxx, :xx,   ,xx:,xxxx,   :x
,::::::::::::::::::::::::::::::::::::::::::::,    :xxxxx:,  ,xx,  :x:  :xxx:x::,  ::xxxx:           :xx:,  ,:xxx  :xx, ,xx: ,xxxxx:, :x
,::::::::::::::::::::::::::::::::::::::::::::,    :xxxxx,   :xx,  :x:  :xxx,,:xx,:xx:,:xx, ,,,,,,,,,xxx,    ,xx:   :xx:xx:  ,xxx,:xx::x
,::::::,,::::::::,,::::::::,,:::::::,,,::::::,    :x:,xxx:  ,xx,  :xx  :xx:  ,xx,xxxxxx:, ,xxxxxxx:,xxx:,  ,xxx,    :xxx:   ,xxx, :xxxx
,::::,    ,::::,   ,:::::,   ,,::::,    ,::::,    :x:  ,:xx,,:xx::xxxx,,xxx::xx: :xx::::x: ,,,,,,   ,xxxxxxxxx,     ,xx:    ,xxx,  :xxx
,::::,    ,::::,    ,::::,    ,::::,    ,::::,    ,:,    ,:,  ,,::,,:,  ,::::,,   ,:::::,            ,,:::::,        ,,      :x:    ,::
,::::,    ,::::,    ,::::,    ,::::,    ,::::,
 ,,,,,    ,::::,    ,::::,    ,::::,    ,:::,             ,,,,,,,,,,,,,
          ,::::,    ,::::,    ,::::,    ,:::,        ,,,:::::::::::::::,
          ,::::,    ,::::,    ,::::,    ,::::,  ,,,,:::::::::,,,,,,,:::,
          ,::::,    ,::::,    ,::::,     ,::::::::::::,,,,,
           ,,,,     ,::::,     ,,,,       ,,,::::,,,,
                    ,::::,
                    ,,::,
"
