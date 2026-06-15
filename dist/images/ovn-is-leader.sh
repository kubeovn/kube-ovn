#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovn-ctl status_northd
ovn-ctl status_ovnnb
ovn-ctl status_ovnsb

POD_NAMESPACE=${POD_NAMESPACE:-kube-system}
BIND_LOCAL_ADDR=[${POD_IP:-127.0.0.1}]

function ovn_db_tls_args {
  if [[ -f /var/run/tls/client.crt && -f /var/run/tls/client.key && -f /var/run/tls/ca.crt ]]; then
    echo "-p /var/run/tls/client.key -c /var/run/tls/client.crt -C /var/run/tls/ca.crt"
  else
    echo "-p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert"
  fi
}

# Single-replica (standalone) mode: there is exactly one ovn-central pod and the
# DB is not clustered, so there is no raft leader to query and no ovn_northd
# lock to contend. Always mark this pod as the leader for nb/sb/northd, then run
# the DB consistency check and compaction.
if [[ -z "${NODE_IPS:-}" ]]; then
    kubectl label --overwrite pod "$POD_NAME" -n "$POD_NAMESPACE" \
        ovn-nb-leader=true ovn-sb-leader=true ovn-northd-leader=true

    nb_status=$(ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound)
    echo "nb $nb_status"
    if [[ $nb_status =~ "inconsistent" ]]; then
        exit 1
    fi
    sb_status=$(ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status OVN_Southbound)
    echo "sb $sb_status"
    if [[ $sb_status =~ "inconsistent" ]]; then
        exit 1
    fi

    set +e
    ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/compact
    ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/compact
    echo ""
    exit 0
fi

# For data consistency, only store leader address in endpoint
# Store ovn-nb leader to svc kube-system/ovn-nb
if [[ "$ENABLE_SSL" == "false" ]]; then
  nb_leader=$(ovsdb-client query tcp:$BIND_LOCAL_ADDR:6641 "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
else
  # shellcheck disable=SC2046
  nb_leader=$(ovsdb-client $(ovn_db_tls_args) query ssl:$BIND_LOCAL_ADDR:6641 "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
fi

if [[ $nb_leader =~ "true" ]]
then
   kubectl label --overwrite pod "$POD_NAME" -n "$POD_NAMESPACE" ovn-nb-leader=true
else
  kubectl label pod "$POD_NAME" -n "$POD_NAMESPACE" ovn-nb-leader-
fi

# Store ovn-northd leader to svc kube-system/ovn-northd
northd_status=$(ovn-appctl -t /var/run/ovn/ovn-northd.$(cat /var/run/ovn/ovn-northd.pid).ctl status)
if [[ $northd_status =~ "active" ]]
then
   kubectl label --overwrite pod "$POD_NAME" -n "$POD_NAMESPACE" ovn-northd-leader=true
else
  kubectl label pod "$POD_NAME" -n "$POD_NAMESPACE" ovn-northd-leader-
fi

# Store ovn-sb leader to svc kube-system/ovn-sb
if [[ "$ENABLE_SSL" == "false" ]]; then
  sb_leader=$(ovsdb-client query tcp:$BIND_LOCAL_ADDR:6642 "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Southbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
else
  # shellcheck disable=SC2046
  sb_leader=$(ovsdb-client $(ovn_db_tls_args) query ssl:$BIND_LOCAL_ADDR:6642 "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Southbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
fi

if [[ $sb_leader =~ "true" ]]
then
   kubectl label --overwrite pod "$POD_NAME" -n "$POD_NAMESPACE" ovn-sb-leader=true
   set +e
   northd_svc=$(kubectl get svc --ignore-not-found -n "$POD_NAMESPACE" ovn-northd)
   if [ -z "$northd_svc" ]; then
    echo "ovn-northd svc not exist"
   else
    northd_leader=$(kubectl get endpointslice -l kubernetes.io/service-name=ovn-northd -n "$POD_NAMESPACE" -o jsonpath='{range .items[*]}{range .endpoints[?(@.conditions.ready!=false)]}{.addresses[0]}{"\n"}{end}{end}' | head -n1)
    if [ "$northd_leader" == "" ]; then
       # no available northd leader try to release the lock
       if [[ "$ENABLE_SSL" == "false" ]]; then
         ovsdb-client -v -t 1 steal tcp:$BIND_LOCAL_ADDR:6642  ovn_northd
       else
         # shellcheck disable=SC2046
         ovsdb-client -v -t 1 $(ovn_db_tls_args) steal ssl:$BIND_LOCAL_ADDR:6642  ovn_northd
       fi
     fi
   fi
   set -e
else
  kubectl label pod "$POD_NAME" -n "$POD_NAMESPACE" ovn-sb-leader-
fi

nb_status=$(ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound)
echo "nb $nb_status"
if [[ $nb_status =~ "inconsistent" ]]
then
   exit 1
fi
sb_status=$(ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status OVN_Southbound)
echo "sb $sb_status"
if [[ $sb_status =~ "inconsistent" ]]
then
   exit 1
fi

set +e
ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/compact
ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/compact
echo ""
