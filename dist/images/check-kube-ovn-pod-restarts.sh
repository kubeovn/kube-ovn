#!/bin/bash
set -eux

restartsPods=$(kubectl get po -A -o wide | grep -E "kube-ovn-controller|kube-ovn-pinger|kube-ovn-monitor|kube-ovn-cni|ovn-central|ovs-ovn" | grep -v "Running   0" | wc -l)

if [ $restartsPods -gt 0 ]; then
  echo "some ovn related pods are not running"
  kubectl get po -A -o wide | grep -E "kube-ovn-controller|kube-ovn-pinger|kube-ovn-monitor|kube-ovn-cni|ovn-central|ovs-ovn" | grep -v "Running   0"
  firstBadPod=$(kubectl get po -A -o wide | grep -E "kube-ovn-controller|kube-ovn-pinger|kube-ovn-monitor|kube-ovn-cni|ovn-central|ovs-ovn" | grep -v "Running   0" | head -n1 | awk '{print $1 " " $2}')
  kubectl logs -p -n $firstBadPod | tail -n 100
  echo "PLEASE CHECK THE ERROR LOGS ABOVE /|\/|\/|\/|\ "
  exit 1
fi
