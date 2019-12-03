#!/bin/bash
set -euo pipefail

alias ovn-ctl='/usr/share/openvswitch/scripts/ovn-ctl'

ovn-ctl status_northd
ovn-ctl status_ovnnb
ovn-ctl status_ovnsb

# For data consistency, only store leader address in endpoint
leader=$(ovsdb-client query tcp:127.0.0.1:6641 "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
if [[ $leader =~ "true" ]]
then
   kubectl label --overwrite pod $POD_NAME -n $POD_NAMESPACE ovn-db-leader=true
else
  kubectl label pod $POD_NAME -n $POD_NAMESPACE ovn-db-leader-
fi
