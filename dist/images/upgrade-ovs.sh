#!/bin/bash

podNames=`kubectl get pod -n kube-system | grep ovs-ovn | awk '{print $1}'`
for pod in $podNames
do
  echo "upgrade pod $pod"
  kubectl delete pod -n kube-system $pod

  desireNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $2'})
  currentNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $3'})
  readyNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $4'})
  availableNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $6'})
  echo "daemonset ovs-ovn, desire $desireNum, current $currentNum, ready $readyNum, available $availableNum"
  while [ $desireNum != $currentNum ] || [ $desireNum != $readyNum ] || [ $desireNum != $availableNum ]
  do
    desireNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $2'})
    currentNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $3'})
    readyNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $4'})
    availableNum=$(kubectl get daemonset -n kube-system | grep ovs-ovn | awk {'print $6'})
    echo "ovs-ovn upgrade, desire $desireNum, current $currentNum, ready $readyNum, available $availableNum"
    sleep 1
  done
done
