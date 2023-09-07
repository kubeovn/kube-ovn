#!/bin/bash
# This is a script for check db content status and db cluster status
# This script will exit with code 1 if check failed
# This script is recommended for regular check, i.e., crontab, in a temporary processing

set -euo pipefail

KUBE_OVN_NS=kube-system
IS_RESTORE=false

restoreNB(){
  echo "start restore db"
  replicas=$(kubectl get deployment -n $KUBE_OVN_NS ovn-central -o jsonpath={.spec.replicas})
  kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas=0
  echo "ovn-central original replicas is $replicas"

  # backup ovn-nb db
  declare nodeIpArray
  declare podNameArray
  declare nodeIps

  if [[ $(kubectl get deployment -n kube-system ovn-central -o jsonpath='{.spec.template.spec.containers[0].env[1]}') =~ "NODE_IPS" ]]; then
    nodeIpVals=`kubectl get deployment -n kube-system ovn-central -o jsonpath='{.spec.template.spec.containers[0].env[1].value}'`
    nodeIps=(${nodeIpVals//,/ })
  else
    nodeIps=`kubectl get node -lkube-ovn/role=master -o wide | grep -v "INTERNAL-IP" | awk '{print $6}'`
  fi
  firstIP=${nodeIps[0]}
  podNames=`kubectl get pod -n $KUBE_OVN_NS | grep ovs-ovn | awk '{print $1}'`
  echo "first nodeIP is $firstIP"

  i=0
  for nodeIp in ${nodeIps[@]}
  do
    for pod in $podNames
    do
      hostip=$(kubectl get pod -n $KUBE_OVN_NS $pod -o jsonpath={.status.hostIP})
      if [ $nodeIp = $hostip ]; then
        nodeIpArray[$i]=$nodeIp
        podNameArray[$i]=$pod
        i=`expr $i + 1`
        echo "ovs-ovn pod on node $nodeIp is $pod"
        break
      fi
    done
  done

  echo "backup nb db file"
  kubectl exec -it -n $KUBE_OVN_NS ${podNameArray[0]} -- ovsdb-tool cluster-to-standalone  /etc/ovn/ovnnb_db_standalone.db  /etc/ovn/ovnnb_db.db

  # mv all db files
  for pod in ${podNameArray[@]}
  do
    kubectl exec -it -n $KUBE_OVN_NS $pod -- mv /etc/ovn/ovnnb_db.db /tmp
    kubectl exec -it -n $KUBE_OVN_NS $pod -- mv /etc/ovn/ovnsb_db.db /tmp
  done

  # restore db and replicas
  echo "restore db file, operate in pod ${podNameArray[0]}"
  kubectl exec -it -n $KUBE_OVN_NS ${podNameArray[0]} -- mv /etc/ovn/ovnnb_db_standalone.db /etc/ovn/ovnnb_db.db
  kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas=$replicas
  kubectl -n kube-system rollout restart ds ovs-ovn
  echo "finish restore db file and ovn-central replicas"
  exit 0
}

DBabnormal(){
  if $IS_RESTORE; then
    restoreNB
  else
    exit 1
  fi
}

CENTRAL_PODS=$(kubectl get pod -n $KUBE_OVN_NS | grep ovn-central | awk '{print $1}')
<< "COMMENT"
for POD in $CENTRAL_PODS
do
  echo "pod db status for $POD"
  kubectl exec "$POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound
  kubectl exec "$POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound
  echo ""
done
COMMENT

for POD in $CENTRAL_PODS
do
  DBSTATUS=$(kubectl exec "$POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound)
  if ! [[ $DBSTATUS =~ "ok" ]]; then
    echo "$POD nb db status abnormal"
    DBabnormal
  fi
done
echo "nb db status check pass"

for POD in $CENTRAL_PODS
do
  DBSTATUS=$(kubectl exec "$POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status OVN_Southbound)
  if ! [[ $DBSTATUS =~ "ok" ]]; then
    echo "$POD sb db status abnormal"
    DBabnormal
  fi
done
echo "sb db status check pass"


CENTRAL_IPS=$(kubectl get pod -n $KUBE_OVN_NS -o wide | grep ovn-central | awk '{print $6}')

match=0
NBEPS=$(kubectl get ep -n $KUBE_OVN_NS ovn-nb | awk '{print $2}')
for IP in $CENTRAL_IPS
do
  if [[ $NBEPS =~ $IP ]]; then
    match=$((match+1))
  fi
done
if [ "$match" != 1 ]; then
  echo "nb raft check failed"
  exit 1
fi
echo "nb raft check pass"

match=0
SBEPS=$(kubectl get ep -n $KUBE_OVN_NS ovn-sb | awk '{print $2}')
for IP in $CENTRAL_IPS
do
  if [[ $SBEPS =~ $IP ]]; then
    match=$((match+1))
  fi
done
if [ "$match" != 1 ]; then
  echo "sb raft check failed"
  exit 1
fi
echo "sb raft check pass"
