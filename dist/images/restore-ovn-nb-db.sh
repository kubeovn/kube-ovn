#!/bin/bash

KUBE_OVN_NS=kube-system
# set ovn-central replicas to 0
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
ovsdb-tool cluster-to-standalone  /etc/ovn/ovnnb_db_standalone.db  /etc/ovn/ovnnb_db.db

# mv all db files
for pod in ${podNameArray[@]}
do
  kubectl exec -it -n $KUBE_OVN_NS $pod -- mv /etc/ovn/ovnnb_db.db /tmp
  kubectl exec -it -n $KUBE_OVN_NS $pod -- mv /etc/ovn/ovnsb_db.db /tmp
done

# restore db and replicas
echo "restore nb db file"
mv /etc/ovn/ovnnb_db_standalone.db /etc/ovn/ovnnb_db.db
kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas=$replicas
echo "finish restore nb db file and ovn-central replicas"
