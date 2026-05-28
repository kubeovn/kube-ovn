#!/bin/bash

KUBE_OVN_NS=kube-system
MODE=${1:-cluster}

usage() {
  cat >&2 <<USAGE
Usage:
  $0                        # cluster (raft) mode: convert local clustered DB to standalone and push to all masters
  $0 cluster                # same as above (explicit)
  $0 single <backup.db>     # single-replica mode: write <backup.db> into the ovn-central-data PVC

Single-replica mode expects an already-standalone ovnnb_db.db file as input
(produced earlier by 'ovsdb-tool cluster-to-standalone' or copied from a
healthy single-replica pod).
USAGE
}

if [ "$MODE" = "single" ]; then
  BACKUP_FILE=${2:-}
  if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
    echo "ERROR: missing or unreadable backup file: '$BACKUP_FILE'"
    usage
    exit 1
  fi

  # Verify the PVC exists (sanity check that the cluster really runs in single-replica mode).
  if ! kubectl get pvc -n $KUBE_OVN_NS ovn-central-data >/dev/null 2>&1; then
    echo "ERROR: PVC $KUBE_OVN_NS/ovn-central-data not found. This script's 'single' mode only applies"
    echo "       when ovn-central was installed with ENABLE_SINGLE_REPLICA_OVN=true."
    exit 1
  fi

  echo "Restoring ovn-central from $BACKUP_FILE into PVC $KUBE_OVN_NS/ovn-central-data"

  replicas=$(kubectl get deployment -n $KUBE_OVN_NS ovn-central -o jsonpath='{.spec.replicas}')
  kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas=0
  echo "ovn-central scaled to 0 (was $replicas)"

  # Wait until the existing pod is fully gone so the PVC is detachable.
  kubectl wait --for=delete pod -l app=ovn-central -n $KUBE_OVN_NS --timeout=120s || true

  helper_pod="ovn-central-restore-$(date +%s)"
  cat <<HELPER | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: $helper_pod
  namespace: $KUBE_OVN_NS
spec:
  restartPolicy: Never
  containers:
    - name: restore
      image: busybox:1.36
      command: ["sh", "-c", "sleep 600"]
      volumeMounts:
        - name: ovn-data
          mountPath: /etc/ovn
  volumes:
    - name: ovn-data
      persistentVolumeClaim:
        claimName: ovn-central-data
HELPER

  kubectl wait --for=condition=Ready pod/$helper_pod -n $KUBE_OVN_NS --timeout=120s

  echo "Copying $BACKUP_FILE into the PVC"
  kubectl cp -n $KUBE_OVN_NS "$BACKUP_FILE" "$helper_pod:/etc/ovn/ovnnb_db.db.restore"
  kubectl exec -n $KUBE_OVN_NS $helper_pod -- sh -c '
    set -e
    [ -f /etc/ovn/ovnnb_db.db ] && mv /etc/ovn/ovnnb_db.db /etc/ovn/ovnnb_db.db.bak.$(date +%s) || true
    [ -f /etc/ovn/ovnsb_db.db ] && mv /etc/ovn/ovnsb_db.db /etc/ovn/ovnsb_db.db.bak.$(date +%s) || true
    mv /etc/ovn/ovnnb_db.db.restore /etc/ovn/ovnnb_db.db
    ls -l /etc/ovn/
  '

  kubectl delete pod -n $KUBE_OVN_NS $helper_pod --wait=true

  kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas="${replicas:-1}"
  echo "ovn-central scaled back to ${replicas:-1}"

  # Best-effort ovs-ovn restart. In a Kamaji-style controlPlaneOnly install
  # there is no ovs-ovn DaemonSet on this cluster, so missing is expected.
  if kubectl -n $KUBE_OVN_NS get ds ovs-ovn >/dev/null 2>&1; then
    echo "restart ovs-ovn"
    kubectl -n $KUBE_OVN_NS rollout restart ds ovs-ovn
  else
    echo "ovs-ovn DaemonSet not present on this cluster — skipping restart"
    echo "  (expected in controlPlaneOnly installs; restart ovs-ovn on the data-plane cluster yourself)"
  fi
  exit 0
fi

if [ "$MODE" != "cluster" ]; then
  usage
  exit 1
fi

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

echo "restart ovs-ovn"
kubectl -n $KUBE_OVN_NS rollout restart ds ovs-ovn
