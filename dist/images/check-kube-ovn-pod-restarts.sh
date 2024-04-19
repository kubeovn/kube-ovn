#!/bin/bash
set -eux

Namespace="kube-system"
crashedPodsNum=$(kubectl get po -n "$Namespace" -o wide | grep -E "ovn-controller|ovn-pinger|ovn-monitor|ovn-cni|ovn-central|ovs-ovn" | awk '{print $3$4}' | grep -v -c "Running0")

if [ "$crashedPodsNum" -gt 0 ]; then
  echo "some ovn pods are not running"
  kubectl get po -n "$Namespace" -o wide | grep -E "ovn-controller|ovn-pinger|ovn-monitor|ovn-cni|ovn-central|ovs-ovn"
  crashedPods=$(kubectl get po -n "$Namespace" -o wide | grep -E "ovn-controller|ovn-pinger|ovn-monitor|ovn-cni|ovn-central|ovs-ovn" |  awk '{print $1 " " $3$4}' | grep -v "Running0" | awk '{print $1}')
  for crashedPod in $crashedPods; do
    echo "kubectl logs -p -n $crashedPod | tail -n 100"
    kubectl logs -p -n "$Namespace" "$crashedPod" | tail -n 100
    echo "PLEASE CHECK THE ERROR LOGS ABOVE /|\ /|\ /|\ "
  done
  exit 1
fi
