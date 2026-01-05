#!/usr/bin/env bash
set -euo pipefail
# As a start bash, this is just for the interface recovery of containerd.

function ovs-exec {
  kubectl -n kube-system exec -i $1 -- $2
}

# for the crictl initial for containerd in ovs-ovn ds
function init-ovs-ctr() {
  for i in $(kubectl -n kube-system get pods -o wide | grep ovs-ovn | awk '{print $1}');
  do
    kubectl cp /usr/bin/crictl -n kube-system $i:/usr/bin/
    ovs-exec $i "crictl config runtime-endpoint unix:///run/containerd/containerd.sock"
  done
}

function restore-interface() {
  POD=$(echo "$1" | awk -F '.' '{print $1}')
  NS=$(echo "$1" | awk -F '.' '{print $2}')
  IP=$(kubectl -n $NS get pods $POD -o=jsonpath='{.status.podIP}')
  CIDWITHC=$(kubectl -n $NS get pods $POD -o=jsonpath={.status.containerStatuses[0].containerID})
  CID=$(echo "$CIDWITHC" | awk -F '://' '{print $2}')
  RUNTIME=$(echo "$CIDWITHC" | awk -F '://' '{print $1}')
  if [[ $RUNTIME == "containerd" ]]; then
    PID=$(ovs-exec "$2" "crictl inspect  --output go-template --template {{.info.pid}} $CID")
    # for convenience, the label here is added as the net ns for the PID
    # in CNI request of recent version, it should be the cni-XXXX-XXXX, which chould be identified by the ip netns as:
    # $ ip netns identify PID
    PIDFILE="proc/$PID/net"
    SANDBOXID=$(ovs-exec "$2" "crictl inspect  --output go-template --template {{.info.sandboxID}} $CID")
    PODIF=${SANDBOXID: 0: 12}"_h"
  fi
  # for the docker, the inspect method could be added in HERE as else if
  echo "restoring ""$POD" "$NS" "$IP" "$PID" "$PODIF"
  CMD="ovs-vsctl --timeout=30 --may-exist add-port br-int $PODIF -- set interface $PODIF external_ids:iface-id=$1 external_ids:pod_name=$POD external_ids:pod_namespace=$NS external_ids:ip=$IP external_ids:pod_netns=$PIDFILE"
  ovs-exec $2 "$CMD"
}

init-ovs-ctr

for i in $(kubectl -n kube-system get pods -o wide | grep ovs-ovn | awk '{print $1}');
  do
    NODE=$(ovs-exec $i "printenv NODE_NAME")
    NODEIP=$(ovs-exec $i "printenv OVN_DB_IPS")
    NSNAME=$(kubectl get pods -o wide -A | grep $NODE | grep -v $NODEIP | awk '{print $2 "." $1}')
    for j in $NSNAME;
    do
      echo $J
      if [ -z "$(ovs-exec $i "ovs-vsctl --no-heading --column=external_ids find interface external_ids:iface-id=$j")" ]; then
        restore-interface "$j" "$i"
      fi
    done
  done

